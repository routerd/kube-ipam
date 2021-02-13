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
	"fmt"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	// log := r.Log.WithValues("iplease", req.NamespacedName)

	iplease := &ipamv1alpha1.IPLease{}
	if err = r.Get(ctx, req.NamespacedName, iplease); err != nil {
		return res, client.IgnoreNotFound(err)
	}
	controllerutil.AddFinalizer(iplease, ipamCacheFinalizer)
	if err = r.Update(ctx, iplease); err != nil {
		return res, err
	}

	if !iplease.DeletionTimestamp.IsZero() {
		ippool := &ipamv1alpha1.IPPool{}
		err = r.Get(ctx, types.NamespacedName{
			Name:      iplease.Spec.Pool.Name,
			Namespace: iplease.Namespace,
		}, ippool)
		if err != nil && !errors.IsNotFound(err) {
			// Some other error
			return res, err
		}

		if err == nil {
			// IPPool Found
			if ipam, ok := r.IPAMCache.Get(ippool); ok {
				for _, addr := range iplease.Status.Addresses {
					if ippool.Spec.IPv4 != nil {
						_ = ipam.ReleaseIPFromPrefix(ippool.Spec.IPv4.CIDR, addr)
					}
					if ippool.Spec.IPv6 != nil {
						_ = ipam.ReleaseIPFromPrefix(ippool.Spec.IPv6.CIDR, addr)
					}
				}
			}
		}

		controllerutil.RemoveFinalizer(iplease, ipamCacheFinalizer)
		if err = r.Update(ctx, iplease); err != nil {
			return res, err
		}
		return res, nil
	}

	if !iplease.Status.ExpireTime.IsZero() &&
		!iplease.Spec.LastRenewTime.Before(&iplease.Status.ExpireTime) {
		// lease has already expired
		return res, nil
	}
	if meta.IsStatusConditionTrue(iplease.Status.Conditions, ipamv1alpha1.IPLeaseBound) {
		// already Bound
		return res, nil
	}

	ippool := &ipamv1alpha1.IPPool{}
	if err = r.Get(ctx, types.NamespacedName{
		Name:      iplease.Spec.Pool.Name,
		Namespace: iplease.Namespace,
	}, ippool); err != nil {
		return res, err
	}

	if ipam, ok := r.IPAMCache.Get(ippool); ok {

		if ippool.Spec.IPv4 != nil {
			ipv4, err := ipam.AcquireIP(ippool.Spec.IPv4.CIDR)
			if err != nil {
				// TODO: Improve error handling
				return res, err
			}
			iplease.Status.Addresses = append(iplease.Status.Addresses, ipv4.IP.String())
		}

		if ippool.Spec.IPv6 != nil {
			ipv6, err := ipam.AcquireIP(ippool.Spec.IPv6.CIDR)
			if err != nil {
				// TODO: Improve error handling
				return res, err
			}
			iplease.Status.Addresses = append(iplease.Status.Addresses, ipv6.IP.String())
		}

	} else {
		// retry later
		return res, fmt.Errorf(
			"no IPPool registered for %s/%s", ippool.Namespace, ippool.Name)
	}

	iplease.Status.ObservedGeneration = iplease.Generation
	// Bound successfully
	iplease.Status.Phase = "Bound"
	meta.SetStatusCondition(&iplease.Status.Conditions, metav1.Condition{
		Type:               "Bound",
		Reason:             "IPAllocated",
		ObservedGeneration: iplease.Generation,
		Status:             metav1.ConditionTrue,
	})
	if err = r.Status().Update(ctx, iplease); err != nil {
		return
	}
	return ctrl.Result{}, nil
}

func (r *IPLeaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ipamv1alpha1.IPLease{}).
		Complete(r)
}
