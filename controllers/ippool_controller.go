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
	goipam "github.com/metal-stack/go-ipam"
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
}

// +kubebuilder:rbac:groups=ipam.routerd.net,resources=ippools,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ipam.routerd.net,resources=ippools/status,verbs=get;update;patch

func (r *IPPoolReconciler) Reconcile(
	ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {

	ippool := &ipamv1alpha1.IPPool{}
	if err = r.Get(ctx, req.NamespacedName, ippool); err != nil {
		return res, client.IgnoreNotFound(err)
	}
	controllerutil.AddFinalizer(ippool, ipamCacheFinalizer)
	if err = r.Update(ctx, ippool); err != nil {
		return res, err
	}

	if !ippool.DeletionTimestamp.IsZero() {
		// was deleted -> cleanup
		r.IPAMCache.Free(ippool)

		controllerutil.RemoveFinalizer(ippool, ipamCacheFinalizer)
		if err = r.Update(ctx, ippool); err != nil {
			return res, err
		}
		return res, nil
	}

	ipam, err := r.IPAMCache.GetOrCreate(ctx, ippool, r.createIPAM)
	if err != nil {
		return res, err
	}

	ippool.Status.IPv4 = nil
	ippool.Status.IPv6 = nil
	if ippool.Spec.IPv4 != nil {
		ipv4Prefix := ipam.PrefixFrom(ippool.Spec.IPv4.CIDR)
		u := ipv4Prefix.Usage()
		ippool.Status.IPv4 = &ipamv1alpha1.PoolStatus{
			AvailableIPs: int(u.AvailableIPs),
			AcquiredIPs:  int(u.AcquiredIPs),
		}
	}

	if ippool.Spec.IPv6 != nil {
		ipv6Prefix := ipam.PrefixFrom(ippool.Spec.IPv6.CIDR)
		u := ipv6Prefix.Usage()
		ippool.Status.IPv6 = &ipamv1alpha1.PoolStatus{
			AvailableIPs: int(u.AvailableIPs),
			AcquiredIPs:  int(u.AcquiredIPs),
		}
	}

	if err := r.Status().Update(ctx, ippool); err != nil {
		return res, err
	}

	return ctrl.Result{}, nil
}

func (r *IPPoolReconciler) createIPAM(
	ctx context.Context, ippool *ipamv1alpha1.IPPool) (goipam.Ipamer, error) {
	// Create new IPAM
	ipam := goipam.New()

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
		if !meta.IsStatusConditionTrue(
			iplease.Status.Conditions, ipamv1alpha1.IPLeaseBound) {
			// not bound
			continue
		}

		if !iplease.Status.ExpireTime.IsZero() &&
			!iplease.Spec.LastRenewTime.Before(&iplease.Status.ExpireTime) {
			// already expired
			continue
		}

		for _, addr := range iplease.Status.Addresses {
			_, ipnet, err := net.ParseCIDR(addr)
			if err != nil || ipnet == nil {
				// log!
				continue
			}
			_, err = ipam.AcquireSpecificIP(ipnet.Network(), ipnet.IP.String())
			if err != nil {
				return ipam, err
			}
		}
	}
	return ipam, nil
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
