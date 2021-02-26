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

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
	"routerd.net/kube-ipam/internal/controllers/adapter"
)

type IPPoolReconciler struct {
	client.Client
	Log       logr.Logger
	Scheme    *runtime.Scheme
	IPAMCache ipamCache
	NewIPAM   func() Ipamer

	IPPoolType      adapter.IPPool
	IPLeaseType     adapter.IPLease
	IPLeaseListType adapter.IPLeaseList
}

func (r *IPPoolReconciler) Reconcile(
	ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	ippool := adapter.AdaptIPPool(
		r.IPPoolType.ClientObject().DeepCopyObject())
	if err = r.Get(ctx, req.NamespacedName, ippool.ClientObject()); err != nil {
		return res, client.IgnoreNotFound(err)
	}
	if err := r.ensureCacheFinalizer(ctx, ippool); err != nil {
		return res, err
	}

	if !ippool.GetDeletionTimestamp().IsZero() {
		return res, r.handleDeletion(ctx, ippool)
	}

	ipam, err := r.IPAMCache.GetOrCreate(ctx, ippool, r.createIPAM)
	if err != nil {
		return res, err
	}

	return res, r.reportUsage(ctx, ippool, ipam)
}

func (r *IPPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(r.IPPoolType.ClientObject()).
		Watches(
			&source.Kind{Type: r.IPLeaseType.ClientObject()},
			handler.EnqueueRequestsFromMapFunc(func(obj client.Object) []reconcile.Request {
				iplease, ok := obj.(adapter.IPLease)
				if !ok {
					return nil
				}

				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name:      iplease.GetSpecIPPoolName(),
							Namespace: iplease.GetNamespace(),
						},
					},
				}
			})).
		Complete(r)
}

// ensure that the IPPool has the ipamCacheFinalizer,
// so we remove the allocated Ipamer from the IPAM Cache.
func (r *IPPoolReconciler) ensureCacheFinalizer(
	ctx context.Context, ippool adapter.IPPool) error {
	if controllerutil.ContainsFinalizer(ippool.ClientObject(), ipamCacheFinalizer) {
		return nil
	}
	controllerutil.AddFinalizer(ippool.ClientObject(), ipamCacheFinalizer)
	if err := r.Update(ctx, ippool.ClientObject()); err != nil {
		return err
	}
	return nil
}

// handle the deletion of an IPPool by clearing the Ipamer and
// removing the finalizer
func (r *IPPoolReconciler) handleDeletion(
	ctx context.Context, ippool adapter.IPPool) error {
	r.IPAMCache.Free(ippool)

	controllerutil.RemoveFinalizer(ippool.ClientObject(), ipamCacheFinalizer)
	if err := r.Update(ctx, ippool.ClientObject()); err != nil {
		return err
	}
	return nil
}

// create an IPAM instance for the given pool and seed it from cache.
func (r *IPPoolReconciler) createIPAM(
	ctx context.Context, ippool adapter.IPPool) (Ipamer, error) {
	// Create new IPAM
	ipam := r.NewIPAM()

	// Add Pool CIDR
	_, err := ipam.NewPrefix(ippool.GetCIDR())
	if err != nil {
		return nil, err
	}

	// Add existing Leases
	ipleaseList := adapter.AdaptIPLeaseList(
		r.IPLeaseListType.ClientObjectList().DeepCopyObject())
	if err := r.List(ctx, ipleaseList.ClientObjectList(),
		client.InNamespace(ippool.GetNamespace())); err != nil {
		return ipam, err
	}
	for _, iplease := range ipleaseList.GetItems() {
		if iplease.GetSpecIPPoolName() != ippool.GetName() {
			continue
		}
		if !meta.IsStatusConditionTrue(
			*iplease.GetStatusConditions(), ipamv1alpha1.IPLeaseBound) {
			continue
		}
		if iplease.HasExpired() {
			continue
		}

		_, err := ipam.AcquireSpecificIP(
			ippool.GetCIDR(), iplease.GetStatusAddress())
		if err != nil {
			return ipam, err
		}
	}
	return ipam, nil
}

func (r *IPPoolReconciler) reportUsage(
	ctx context.Context, ippool adapter.IPPool, ipam Ipamer) error {
	ipv4Prefix := ipam.PrefixFrom(ippool.GetCIDR())
	u := ipv4Prefix.Usage()
	ippool.SetAvailableIPs(int(u.AvailableIPs))
	ippool.SetAllocatedIPs(int(u.AcquiredIPs))

	if err := r.Status().Update(ctx, ippool.ClientObject()); err != nil {
		return err
	}
	return nil
}
