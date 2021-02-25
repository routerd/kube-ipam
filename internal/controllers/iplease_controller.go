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
	"net"
	"strings"
	"time"

	"github.com/go-logr/logr"
	goipam "github.com/metal-stack/go-ipam"
	corev1 "k8s.io/api/core/v1"
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
)

// IPLeaseReconciler reconciles a IPLease object
type IPLeaseReconciler struct {
	client.Client
	Log       logr.Logger
	Scheme    *runtime.Scheme
	IPAMCache ipamCache
}

// +kubebuilder:rbac:groups=ipam.routerd.net,resources=ipleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ipam.routerd.net,resources=ipleases/status,verbs=get;update;patch

func (r *IPLeaseReconciler) Reconcile(
	ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	log := r.Log.WithValues("iplease", req.NamespacedName)

	iplease := &ipamv1alpha1.IPLease{}
	if err = r.Get(ctx, req.NamespacedName, iplease); err != nil {
		return res, client.IgnoreNotFound(err)
	}
	defer func() {
		// ensure that no matter how we exit the reconcile function,
		// we want to reconcile the IPLease after the lease duration expired.
		if iplease.Status.LeaseDuration == nil {
			return
		}
		log.Info("waiting for lease expire", "duration", iplease.Status.LeaseDuration.Duration)
		res.RequeueAfter = iplease.Status.LeaseDuration.Duration
	}()

	if err := r.ensureCacheFinalizer(ctx, iplease); err != nil {
		return res, fmt.Errorf("ensuring finalizer: %w", err)
	}
	if !iplease.DeletionTimestamp.IsZero() {
		return res, r.handleDeletion(ctx, log, iplease)
	}
	if err := r.deleteIfExpired(ctx, log, iplease); err != nil {
		return res, err
	}

	// Guard IP Allocation
	if meta.IsStatusConditionTrue(iplease.Status.Conditions, ipamv1alpha1.IPLeaseBound) {
		// already Bound or not Bound and Expired
		// just check if expireTime needs updating
		return res, nil
	}

	ippool := &ipamv1alpha1.IPPool{}
	if err = r.Get(ctx, types.NamespacedName{
		Name:      iplease.Spec.Pool.Name,
		Namespace: iplease.Namespace,
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
		For(&ipamv1alpha1.IPLease{}).
		Complete(r)
}

func (r *IPLeaseReconciler) allocateIPs(
	ctx context.Context, log logr.Logger,
	iplease *ipamv1alpha1.IPLease, ippool *ipamv1alpha1.IPPool,
) (ctrl.Result, error) {
	ipam, ok := r.IPAMCache.Get(ippool)
	if !ok {
		log.Info("missing IPAM cache, waiting for cache sync", "ippool", ippool.Namespace+"/"+ippool.Name, "ippool.uid", ippool.UID)
		return ctrl.Result{Requeue: true}, nil
	}

	if iplease.Spec.Static == nil {
		log.Info("trying allocating dynamic ip from pool")
		return r.allocateDynamicIPs(ctx, ipam, iplease, ippool)
	}
	log.Info("trying allocating static ip from lease")
	return r.allocateStaticIPs(ctx, ipam, iplease, ippool)
}

func (r *IPLeaseReconciler) allocateStaticIPs(
	ctx context.Context, ipam Ipamer,
	iplease *ipamv1alpha1.IPLease, ippool *ipamv1alpha1.IPPool,
) (res ctrl.Result, err error) {
	var (
		unavailableIPs []string
		allocatedIPs   []goipam.IP
	)

	defer func() {
		if err != nil || len(unavailableIPs) > 0 {
			// if we encounter any error or could not acquire all IPs,
			// we want to free the acquired IPs so they are not blocked.
			for _, ip := range allocatedIPs {
				_, _ = ipam.ReleaseIP(&ip)
			}
		}
	}()

	for _, addr := range iplease.Spec.Static.Addresses {
		ip := net.ParseIP(addr)
		if ip == nil {
			unavailableIPs = append(unavailableIPs, addr)
			continue
		}

		if ip.To4() != nil {
			// try to acquire specific IPv4
			if ippool.Spec.IPv4 == nil {
				// can't allocate an IPv4 if the Pool has no IPv4 CIDR.
				unavailableIPs = append(unavailableIPs, addr)
				continue
			}

			ip, err := ipam.AcquireSpecificIP(ippool.Spec.IPv4.CIDR, addr)
			if errors.Is(err, goipam.ErrNoIPAvailable) {
				unavailableIPs = append(unavailableIPs, addr)
				continue
			}
			if err != nil {
				return ctrl.Result{}, err
			}
			allocatedIPs = append(allocatedIPs, *ip)
			continue
		} else {
			// try to acquire specific IPv6
			if ippool.Spec.IPv6 == nil {
				// can't allocate an IPv6 if the Pool has no IPv6 CIDR.
				unavailableIPs = append(unavailableIPs, addr)
				continue
			}

			ip, err := ipam.AcquireSpecificIP(ippool.Spec.IPv6.CIDR, addr)
			if errors.Is(err, goipam.ErrNoIPAvailable) {
				unavailableIPs = append(unavailableIPs, addr)
				continue
			}
			if err != nil {
				return ctrl.Result{}, err
			}
			allocatedIPs = append(allocatedIPs, *ip)
			continue
		}
	}

	if len(unavailableIPs) > 0 {
		// ensure to free IPs we wanted to allocate
		for _, ip := range allocatedIPs {
			_, _ = ipam.ReleaseIP(&ip)
		}

		iplease.Status.Phase = "Unavailable"
		iplease.Status.ObservedGeneration = iplease.Generation
		meta.SetStatusCondition(&iplease.Status.Conditions, metav1.Condition{
			Type:               ipamv1alpha1.IPLeaseBound,
			Reason:             "Unavailable",
			Message:            fmt.Sprintf("could not allocate IPs: %s", strings.Join(unavailableIPs, ", ")),
			ObservedGeneration: iplease.Generation,
			Status:             metav1.ConditionFalse,
		})
		return ctrl.Result{
			// Retry to allocate later.
			RequeueAfter: 5 * time.Second,
		}, r.Status().Update(ctx, iplease)
	}

	return ctrl.Result{}, r.reportAllocatedIPs(ctx, iplease, ipam, allocatedIPs)
}

func (r *IPLeaseReconciler) allocateDynamicIPs(
	ctx context.Context, ipam Ipamer,
	iplease *ipamv1alpha1.IPLease, ippool *ipamv1alpha1.IPPool,
) (res ctrl.Result, err error) {
	// Make sure we report the Lease Duration.
	iplease.Status.LeaseDuration = ippool.Spec.LeaseDuration

	var (
		unavailableCIDRs      []string
		unavailableIPFamilies []string
		allocatedIPs          []goipam.IP
	)

	defer func() {
		if err != nil ||
			len(unavailableCIDRs) > 0 ||
			len(unavailableIPFamilies) > 0 {
			// if we encounter any error or could not acquire all IPs,
			// we want to free the acquired IPs so they are not blocked.
			for _, ip := range allocatedIPs {
				_, _ = ipam.ReleaseIP(&ip)
			}
		}
	}()

	if iplease.HasIPv4() {
		if ippool.Spec.IPv4 != nil {
			// IPv4
			ip, err := ipam.AcquireIP(ippool.Spec.IPv4.CIDR)
			if errors.Is(err, goipam.ErrNoIPAvailable) {
				unavailableCIDRs = append(unavailableCIDRs, ippool.Spec.IPv4.CIDR)
			} else if err != nil {
				return ctrl.Result{}, err
			}

			allocatedIPs = append(allocatedIPs, *ip)
		} else {
			// Pool does not support IPv4
			unavailableIPFamilies = append(unavailableIPFamilies, string(corev1.IPv4Protocol))
		}
	}

	if iplease.HasIPv6() {
		if ippool.Spec.IPv6 != nil {
			// IPv6
			ip, err := ipam.AcquireIP(ippool.Spec.IPv6.CIDR)
			if errors.Is(err, goipam.ErrNoIPAvailable) {
				unavailableCIDRs = append(unavailableCIDRs, ippool.Spec.IPv6.CIDR)
			} else if err != nil {
				return ctrl.Result{}, err
			}

			allocatedIPs = append(allocatedIPs, *ip)
		} else {
			// Pool does not support IPv6
			unavailableIPFamilies = append(unavailableIPFamilies, string(corev1.IPv6Protocol))
		}
	}

	if len(unavailableCIDRs) > 0 ||
		len(unavailableIPFamilies) > 0 {
		msg := "could not allocate IPs"
		if len(unavailableCIDRs) > 0 {
			msg += " from CIDRS: " + strings.Join(unavailableCIDRs, ", ")
		}
		if len(unavailableIPFamilies) > 0 {
			msg += " from unsupported IP Families: " + strings.Join(unavailableIPFamilies, ", ")
		}

		iplease.Status.Phase = "Unavailable"
		iplease.Status.ObservedGeneration = iplease.Generation
		meta.SetStatusCondition(&iplease.Status.Conditions, metav1.Condition{
			Type:               ipamv1alpha1.IPLeaseBound,
			Reason:             "Unavailable",
			Message:            msg,
			ObservedGeneration: iplease.Generation,
			Status:             metav1.ConditionFalse,
		})
		return ctrl.Result{
			// Retry to allocate later.
			RequeueAfter: 5 * time.Second,
		}, nil
	}

	return ctrl.Result{}, r.reportAllocatedIPs(ctx, iplease, ipam, allocatedIPs)
}

func (r *IPLeaseReconciler) reportAllocatedIPs(
	ctx context.Context, iplease *ipamv1alpha1.IPLease,
	ipam Ipamer, allocatedIPs []goipam.IP,
) error {
	for _, ip := range allocatedIPs {
		iplease.Status.Addresses = append(iplease.Status.Addresses, ip.IP.String())
	}
	iplease.Status.Phase = "Bound"
	iplease.Status.ObservedGeneration = iplease.Generation
	meta.SetStatusCondition(&iplease.Status.Conditions, metav1.Condition{
		Type:               ipamv1alpha1.IPLeaseBound,
		Reason:             "IPAllocated",
		Message:            "successfully allocated ips",
		ObservedGeneration: iplease.Generation,
		Status:             metav1.ConditionTrue,
	})
	if err := r.Status().Update(ctx, iplease); err != nil {
		return err
	}
	return nil
}

func (r *IPLeaseReconciler) handleDeletion(
	ctx context.Context, log logr.Logger, iplease *ipamv1alpha1.IPLease) error {
	// Lookup Pool to get the IPAM instance managing this address pool.
	ippool := &ipamv1alpha1.IPPool{}
	err := r.Get(ctx, types.NamespacedName{
		Name:      iplease.Spec.Pool.Name,
		Namespace: iplease.Namespace,
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
	ctx context.Context, log logr.Logger, iplease *ipamv1alpha1.IPLease) error {
	if iplease.HasExpired() {
		log.Info("lease expired")
		return r.Delete(ctx, iplease)
	}
	return nil
}

// Ensure the cache finalizer is present
func (r *IPLeaseReconciler) ensureCacheFinalizer(ctx context.Context, iplease *ipamv1alpha1.IPLease) error {
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
	ippool *ipamv1alpha1.IPPool, iplease *ipamv1alpha1.IPLease) error {
	ipam, ok := r.IPAMCache.Get(ippool)
	if !ok {
		return nil
	}

	for _, addr := range iplease.Status.Addresses {
		ip := net.ParseIP(addr)
		if ip == nil {
			continue
		}

		if ippool.Spec.IPv4 != nil &&
			ip.To4() != nil {
			// Free IPv4
			err := ipam.ReleaseIPFromPrefix(ippool.Spec.IPv4.CIDR, addr)
			if errors.Is(err, goipam.ErrNotFound) {
				// don't care
				continue
			}

			if err != nil {
				log.Error(err, "could not release IPv4 %s from %s", addr, ippool.Spec.IPv4.CIDR)
			}
		}

		if ippool.Spec.IPv6 != nil &&
			ip.To4() == nil {
			// Free IPv6
			err := ipam.ReleaseIPFromPrefix(ippool.Spec.IPv6.CIDR, addr)
			if errors.Is(err, goipam.ErrNotFound) {
				// don't care
				continue
			}

			if err != nil {
				log.Error(err, "could not release IPv6 %s from %s", addr, ippool.Spec.IPv6.CIDR)
			}
		}
	}
	return nil
}
