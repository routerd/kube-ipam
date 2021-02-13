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

	"github.com/giantswarm/ipam"
	"github.com/giantswarm/micrologger"
	"github.com/giantswarm/microstorage/memory"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

// IPLeaseReconciler reconciles a IPLease object
type IPLeaseReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=ipam.routerd.net,resources=ipleases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ipam.routerd.net,resources=ipleases/status,verbs=get;update;patch

func (r *IPLeaseReconciler) Reconcile(
	ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	// log = r.Log.WithValues("iplease", req.NamespacedName)

	iplease := &ipamv1alpha1.IPLease{}
	if err = r.Get(ctx, req.NamespacedName, iplease); client.IgnoreNotFound(err) != nil {
		return
	}
	if !iplease.Spec.LastRenewTime.Before(&iplease.Status.ExpireTime) ||
		meta.IsStatusConditionTrue(iplease.Status.Conditions, ipamv1alpha1.IPLeaseBound) {
		// this lease has expired or is already Bound
		// nothing to do
		return
	}

	ippool := &ipamv1alpha1.IPPool{}
	if err = r.Get(ctx, types.NamespacedName{
		Name:      iplease.Spec.Pool.Name,
		Namespace: iplease.Namespace,
	}, ippool); err != nil {
		return
	}

	leases, err := r.listIPLeasesForPool(ctx, ippool)
	if err != nil {
		return
	}

	// All the already bound IPs are here.
	boundIPs := r.lendedIPs(leases)

	var expectedIPs int
	if ippool.Spec.IPv4 != nil {
		expectedIPs++

		// find a free IPv4
		_, ipv4net, err := net.ParseCIDR(ippool.Spec.IPv4.CIDR)
		if err != nil {
			// invalid pool configuration
			return res, err
		}

		if err := addFreeIP(ctx, iplease, ipv4net, boundIPs); err != nil {
			return res, err
		}
	}
	if ippool.Spec.IPv6 != nil {
		expectedIPs++

		// find a free IPv6
		_, ipv6net, err := net.ParseCIDR(ippool.Spec.IPv6.CIDR)
		if err != nil {
			// invalid pool configuration
			return res, err
		}

		if err := addFreeIP(ctx, iplease, ipv6net, boundIPs); err != nil {
			return res, err
		}
	}

	iplease.Status.ObservedGeneration = iplease.Generation
	if len(iplease.Status.Addresses) == expectedIPs {
		// Bound successfully
		iplease.Status.Phase = "Bound"
		meta.SetStatusCondition(&iplease.Status.Conditions, metav1.Condition{
			Type:               "Bound",
			Reason:             "IPAllocated",
			ObservedGeneration: iplease.Generation,
			Status:             metav1.ConditionTrue,
		})
	} else {
		// somethings wrong
		// Bound successfully
		iplease.Status.Phase = "Pending"
		meta.SetStatusCondition(&iplease.Status.Conditions, metav1.Condition{
			Type:               "Bound",
			Reason:             "Pending",
			ObservedGeneration: iplease.Generation,
			Status:             metav1.ConditionFalse,
		})
	}
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

func addFreeIP(
	ctx context.Context, iplease *ipamv1alpha1.IPLease,
	ipnet *net.IPNet, boundIPs []net.IPNet,
) error {

	storage, err := memory.New(memory.Config{})
	if err != nil {
		return err
	}
	db, err := ipam.New(ipam.Config{
		// TODO: adapt operator logger
		Logger:           &noopLogger{},
		Storage:          storage,
		Network:          ipnet,
		AllocatedSubnets: boundIPs,
	})
	if err != nil {
		return err
	}

	var mask net.IPMask
	if ipnet.IP.To4() == nil {
		mask = net.CIDRMask(128, 128)
	} else {
		mask = net.CIDRMask(32, 32)
	}
	got, err := db.CreateSubnet(ctx, mask, "", nil)
	if err != nil {
		return err
	}
	iplease.Status.Addresses = append(iplease.Status.Addresses, got.String())
	return nil
}

func (r *IPLeaseReconciler) lendedIPs(leases []ipamv1alpha1.IPLease) []net.IPNet {
	var out []net.IPNet
	for _, iplease := range leases {
		if !meta.IsStatusConditionTrue(
			iplease.Status.Conditions, ipamv1alpha1.IPLeaseBound) ||
			!iplease.Spec.LastRenewTime.Before(&iplease.Status.ExpireTime) {
			// IPLease is either not yet bound or already expired
			// -> skip
			continue
		}

		for _, addr := range iplease.Status.Addresses {
			_, ipNet, err := net.ParseCIDR(addr)
			if err != nil || ipNet == nil {
				continue
			}
			out = append(out, *ipNet)
		}
	}
	return out
}

func (r *IPLeaseReconciler) buildLendedIPsMap(
	leases []ipamv1alpha1.IPLease) (ips map[string]struct{}) {
	ips = map[string]struct{}{}

	for _, iplease := range leases {
		if !meta.IsStatusConditionTrue(
			iplease.Status.Conditions, ipamv1alpha1.IPLeaseBound) ||
			!iplease.Spec.LastRenewTime.Before(&iplease.Status.ExpireTime) {
			// IPLease is either not yet bound or already expired
			// -> skip
			continue
		}

		for _, addr := range iplease.Status.Addresses {
			ips[addr] = struct{}{}
		}
	}

	return ips
}

func (r *IPLeaseReconciler) listIPLeasesForPool(
	ctx context.Context, ippool *ipamv1alpha1.IPPool) ([]ipamv1alpha1.IPLease, error) {
	ipleaseList := &ipamv1alpha1.IPLeaseList{}
	if err := r.List(ctx, ipleaseList,
		client.InNamespace(ippool.Namespace)); err != nil {
		return nil, err
	}
	return ipleaseList.Items, nil
}

type noopLogger struct{}

var _ micrologger.Logger = (*noopLogger)(nil)

func (l *noopLogger) Log(keyVals ...interface{})                         {}
func (l *noopLogger) LogCtx(ctx context.Context, keyVals ...interface{}) {}
func (l *noopLogger) With(keyVals ...interface{}) micrologger.Logger     { return l }
