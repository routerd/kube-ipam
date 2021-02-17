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
	"net"

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
)

// IPPoolReconciler reconciles a IPPool object
type IPPoolReconciler struct {
	client.Client
	Log       logr.Logger
	Scheme    *runtime.Scheme
	IPAMCache ipamCache
	NewIPAM   func() Ipamer
}

// +kubebuilder:rbac:groups=ipam.routerd.net,resources=ippools,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=ipam.routerd.net,resources=ippools/status,verbs=get;update;patch

func (r *IPPoolReconciler) Reconcile(
	ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	ippool := &ipamv1alpha1.IPPool{}
	if err = r.Get(ctx, req.NamespacedName, ippool); err != nil {
		return res, client.IgnoreNotFound(err)
	}
	if err := r.ensureCacheFinalizer(ctx, ippool); err != nil {
		return res, err
	}

	if !ippool.DeletionTimestamp.IsZero() {
		return res, r.handleDeletion(ctx, ippool)
	}

	ipam, err := r.IPAMCache.GetOrCreate(ctx, ippool, r.createIPAM)
	if err != nil {
		return res, err
	}

	return ctrl.Result{}, r.reportUsage(ctx, ippool, ipam)
}

func (r *IPPoolReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ipamv1alpha1.IPPool{}).
		Watches(
			&source.Kind{Type: &ipamv1alpha1.IPLease{}},
			handler.EnqueueRequestsFromMapFunc(func(obj client.Object) []reconcile.Request {
				iplease, ok := obj.(*ipamv1alpha1.IPLease)
				if !ok {
					return nil
				}

				return []reconcile.Request{
					{
						NamespacedName: types.NamespacedName{
							Name:      iplease.Spec.Pool.Name,
							Namespace: iplease.Namespace,
						},
					},
				}
			})).
		Complete(r)
}

func (r *IPPoolReconciler) handleDeletion(ctx context.Context, ippool *ipamv1alpha1.IPPool) error {
	// was deleted -> cleanup
	r.IPAMCache.Free(ippool)

	controllerutil.RemoveFinalizer(ippool, ipamCacheFinalizer)
	if err := r.Update(ctx, ippool); err != nil {
		return err
	}
	return nil
}

func (r *IPPoolReconciler) ensureCacheFinalizer(ctx context.Context, ippool *ipamv1alpha1.IPPool) error {
	if controllerutil.ContainsFinalizer(ippool, ipamCacheFinalizer) {
		return nil
	}
	controllerutil.AddFinalizer(ippool, ipamCacheFinalizer)
	if err := r.Update(ctx, ippool); err != nil {
		return err
	}
	return nil
}

func (r *IPPoolReconciler) reportUsage(
	ctx context.Context, ippool *ipamv1alpha1.IPPool, ipam Ipamer) error {
	ippool.Status.IPv4 = nil
	ippool.Status.IPv6 = nil
	if ippool.Spec.IPv4 != nil {
		ipv4Prefix := ipam.PrefixFrom(ippool.Spec.IPv4.CIDR)
		u := ipv4Prefix.Usage()
		ippool.Status.IPv4 = &ipamv1alpha1.IPTypePoolStatus{
			AvailableIPs: int(u.AvailableIPs),
			AllocatedIPs: int(u.AcquiredIPs),
		}
	}

	if ippool.Spec.IPv6 != nil {
		ipv6Prefix := ipam.PrefixFrom(ippool.Spec.IPv6.CIDR)
		u := ipv6Prefix.Usage()
		ippool.Status.IPv6 = &ipamv1alpha1.IPTypePoolStatus{
			AvailableIPs: int(u.AvailableIPs),
			AllocatedIPs: int(u.AcquiredIPs),
		}
	}
	if err := r.Status().Update(ctx, ippool); err != nil {
		return err
	}
	return nil
}

func (r *IPPoolReconciler) createIPAM(
	ctx context.Context, ippool *ipamv1alpha1.IPPool) (Ipamer, error) {
	// Create new IPAM
	ipam := r.NewIPAM()

	// Add Pool CIDRs
	if ippool.Spec.IPv4 != nil {
		_, err := ipam.NewPrefix(ippool.Spec.IPv4.CIDR)
		if err != nil {
			return ipam, err
		}
	}
	if ippool.Spec.IPv6 != nil {
		_, err := ipam.NewPrefix(ippool.Spec.IPv6.CIDR)
		if err != nil {
			return ipam, err
		}
	}

	// Add existing Leases
	ipleaseList := &ipamv1alpha1.IPLeaseList{}
	if err := r.List(ctx, ipleaseList,
		client.InNamespace(ippool.Namespace)); err != nil {
		return ipam, err
	}
	for _, iplease := range ipleaseList.Items {
		if iplease.Spec.Pool.Name != ippool.Name {
			continue
		}
		if !meta.IsStatusConditionTrue(
			iplease.Status.Conditions, ipamv1alpha1.IPLeaseBound) {
			continue
		}
		if iplease.HasExpired() {
			continue
		}

		for _, addr := range iplease.Status.Addresses {
			ip := net.ParseIP(addr)
			if ip == nil {
				continue
			}

			if ip.To4() != nil && ippool.Spec.IPv4 != nil {
				_, err := ipam.AcquireSpecificIP(ippool.Spec.IPv4.CIDR, addr)
				if err != nil {
					return ipam, err
				}
			}
			if ip.To4() == nil && ippool.Spec.IPv6 != nil {
				_, err := ipam.AcquireSpecificIP(ippool.Spec.IPv6.CIDR, addr)
				if err != nil {
					return ipam, err
				}
			}
		}
	}
	return ipam, nil
}
