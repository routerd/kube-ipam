/*
Copyright 2021 The routerd authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	goipam "github.com/metal-stack/go-ipam"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
	"routerd.net/kube-ipam/internal/controllers/adapter"
)

// IPLeaseReconciler reconciles a IPLease object
type IPLeaseReconciler struct {
	client.Client
	Log       logr.Logger
	Scheme    *runtime.Scheme
	IPAMCache ipamCache

	IPPoolType  adapter.IPPool
	IPLeaseType adapter.IPLease
}

// +kubebuilder:rbac:groups=ipam.routerd.net,resources=ipleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ipam.routerd.net,resources=ipleases/status,verbs=get;update;patch

func (r *IPLeaseReconciler) Reconcile(
	ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := r.Log.WithValues("iplease", req.NamespacedName)

	iplease := adapter.AdaptIPLease(
		r.IPLeaseType.DeepCopyObject())
	if err = r.Get(ctx, req.NamespacedName, iplease); err != nil {
		return res, client.IgnoreNotFound(err)
	}
	defer func() {
		// ensure that no matter how we exit the reconcile function,
		// we want to reconcile the IPLease after the lease duration expired.
		leaseDuration, ok := iplease.GetStatusLeaseDuration()
		if !ok {
			return
		}
		log.Info("waiting for lease expire", "duration", leaseDuration)
		res.RequeueAfter = leaseDuration
	}()

	if err := r.ensureCacheFinalizerAndOwner(ctx, iplease); err != nil {
		return res, fmt.Errorf("ensuring finalizer: %w", err)
	}
	if !iplease.GetDeletionTimestamp().IsZero() {
		return res, r.handleDeletion(ctx, log, iplease)
	}
	if err := r.deleteIfExpired(ctx, log, iplease); err != nil {
		return res, err
	}

	// Guard IP Allocation
	if meta.IsStatusConditionTrue(
		*iplease.GetStatusConditions(), ipamv1alpha1.IPLeaseBound) {
		// already Bound or not Bound and Expired
		// just check if expireTime needs updating
		return res, nil
	}

	ippool := adapter.AdaptIPPool(r.IPPoolType.DeepCopyObject())
	if err = r.Get(ctx, types.NamespacedName{
		Name:      iplease.GetSpecIPPoolName(),
		Namespace: iplease.GetNamespace(),
	}, ippool); err != nil {
		return res, err
	}

	return r.allocateIPs(ctx, log, iplease, ippool)
}

func (r *IPLeaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		WithOptions(controller.Options{
			// This Reconciler can work with multiple workers at once.
			MaxConcurrentReconciles: 10,
		}).
		For(r.IPLeaseType).
		Complete(r)
}

func (r *IPLeaseReconciler) allocateIPs(
	ctx context.Context, log logr.Logger,
	iplease adapter.IPLease, ippool adapter.IPPool,
) (ctrl.Result, error) {
	ipam, ok := r.IPAMCache.Get(ippool)
	if !ok {
		log.Info(
			"missing IPAM cache, waiting for cache sync",
			"ippool", ippool.GetNamespace()+"/"+ippool.GetName(),
			"ippool.uid", ippool.GetUID())
		return ctrl.Result{Requeue: true}, nil
	}

	switch iplease.GetSpecType() {
	case ipamv1alpha1.IPLeaseTypeDynamic:
		log.Info("trying allocating dynamic ip from pool")
		return r.allocateDynamicIPs(ctx, ipam, iplease, ippool)

	case ipamv1alpha1.IPLeaseTypeStatic:
		log.Info("trying allocating static ip from lease")
		return r.allocateStaticIPs(ctx, ipam, iplease, ippool)
	}
	return ctrl.Result{}, nil
}

func (r *IPLeaseReconciler) allocateStaticIPs(
	ctx context.Context, ipam Ipamer,
	iplease adapter.IPLease, ippool adapter.IPPool,
) (res ctrl.Result, err error) {
	var allocatedIPs []goipam.IP

	defer func() {
		if err != nil {
			// if we encounter any error or could not acquire all IPs,
			// we want to free the acquired IPs so they are not blocked.
			for _, ip := range allocatedIPs {
				_, _ = ipam.ReleaseIP(&ip)
			}
		}
	}()

	ip, err := ipam.AcquireSpecificIP(
		ippool.GetCIDR(), iplease.GetSpecStaticAddress())
	if err == nil {
		allocatedIPs = append(allocatedIPs, *ip)
	} else if !errors.Is(err, goipam.ErrNoIPAvailable) {
		return res, err
	}

	if errors.Is(err, goipam.ErrNoIPAvailable) {
		iplease.SetStatusPhase("Unavailable")
		iplease.SetStatusObservedGeneration(iplease.GetGeneration())
		meta.SetStatusCondition(iplease.GetStatusConditions(), metav1.Condition{
			Type:   ipamv1alpha1.IPLeaseBound,
			Reason: "Unavailable",
			Message: fmt.Sprintf(
				"could not allocate IP: %s",
				iplease.GetSpecStaticAddress()),
			ObservedGeneration: iplease.GetGeneration(),
			Status:             metav1.ConditionFalse,
		})
		return ctrl.Result{
			// Retry to allocate later.
			RequeueAfter: 5 * time.Second,
		}, r.Status().Update(ctx, iplease)
	}

	return ctrl.Result{}, r.reportAllocatedIPs(ctx, iplease, ipam, ip)
}

func (r *IPLeaseReconciler) allocateDynamicIPs(
	ctx context.Context, ipam Ipamer,
	iplease adapter.IPLease, ippool adapter.IPPool,
) (res ctrl.Result, err error) {
	// Make sure we report the Lease Duration if set on pool.
	if leaseDuration, ok := ippool.GetSpecLeaseDuration(); ok {
		iplease.SetStatusLeaseDuration(leaseDuration)
	}

	ip, err := ipam.AcquireIP(ippool.GetCIDR())
	if !errors.Is(err, goipam.ErrNoIPAvailable) {
		return ctrl.Result{}, err
	}

	if errors.Is(err, goipam.ErrNoIPAvailable) {
		iplease.SetStatusPhase("Unavailable")
		iplease.SetStatusObservedGeneration(iplease.GetGeneration())
		meta.SetStatusCondition(iplease.GetStatusConditions(), metav1.Condition{
			Type:               ipamv1alpha1.IPLeaseBound,
			Reason:             "Unavailable",
			Message:            "No more IPs available from pool.",
			ObservedGeneration: iplease.GetGeneration(),
			Status:             metav1.ConditionFalse,
		})
		return ctrl.Result{
			// Retry to allocate later.
			RequeueAfter: 5 * time.Second,
		}, nil
	}

	return ctrl.Result{}, r.reportAllocatedIPs(ctx, iplease, ipam, ip)
}

func (r *IPLeaseReconciler) reportAllocatedIPs(
	ctx context.Context, iplease adapter.IPLease,
	ipam Ipamer, allocatedIP *goipam.IP,
) error {
	iplease.SetStatusAddress(allocatedIP.IP.String())
	iplease.SetStatusPhase("Bound")

	iplease.SetStatusObservedGeneration(iplease.GetGeneration())
	meta.SetStatusCondition(iplease.GetStatusConditions(), metav1.Condition{
		Type:               ipamv1alpha1.IPLeaseBound,
		Reason:             "IPAllocated",
		Message:            "successfully allocated ips",
		ObservedGeneration: iplease.GetGeneration(),
		Status:             metav1.ConditionTrue,
	})
	if err := r.Status().Update(ctx, iplease); err != nil {
		// ensure to free IP again if we fail to commit to storage
		_, _ = ipam.ReleaseIP(allocatedIP)
		return err
	}
	return nil
}

func (r *IPLeaseReconciler) handleDeletion(
	ctx context.Context, log logr.Logger, iplease adapter.IPLease) error {
	// Lookup Pool to get the IPAM instance managing this address pool.
	ippool := r.IPPoolType.DeepCopyObject().(adapter.IPPool)
	err := r.Get(ctx, types.NamespacedName{
		Name:      iplease.GetSpecIPPoolName(),
		Namespace: iplease.GetNamespace(),
	}, ippool)
	if err != nil && !k8serrors.IsNotFound(err) {
		// Some other error
		return err
	}

	if err == nil {
		// IPPool Found
		if err := r.freeLease(log, ippool, iplease); err != nil {
			return fmt.Errorf("free lease: %w", err)
		}
	}

	// Cleanup Finalizer
	controllerutil.RemoveFinalizer(iplease, ipamCacheFinalizer)
	if err = r.Update(ctx, iplease); err != nil {
		return err
	}
	return nil
}

// check when the IPLease expires
func (r *IPLeaseReconciler) deleteIfExpired(
	ctx context.Context, log logr.Logger, iplease adapter.IPLease) error {
	if iplease.HasExpired() {
		log.Info("lease expired")
		return r.Delete(ctx, iplease)
	}
	return nil
}

// Ensure the cache finalizer is present
func (r *IPLeaseReconciler) ensureCacheFinalizerAndOwner(ctx context.Context, iplease adapter.IPLease) error {
	if controllerutil.ContainsFinalizer(iplease, ipamCacheFinalizer) {
		return nil
	}
	controllerutil.AddFinalizer(iplease, ipamCacheFinalizer)
	if err := r.Update(ctx, iplease); err != nil {
		return err
	}
	return nil
}

func (r *IPLeaseReconciler) freeLease(
	log logr.Logger,
	ippool adapter.IPPool, iplease adapter.IPLease) error {
	ipam, ok := r.IPAMCache.Get(ippool)
	if !ok {
		return nil
	}

	err := ipam.ReleaseIPFromPrefix(ippool.GetCIDR(), iplease.GetStatusAddress())
	if errors.Is(err, goipam.ErrNotFound) {
		return nil
	}
	return err
}
