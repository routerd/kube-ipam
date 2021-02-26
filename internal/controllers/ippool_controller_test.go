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
	"testing"

	goipam "github.com/metal-stack/go-ipam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
	"routerd.net/kube-ipam/internal/controllers/adapter"
)

func TestIPPoolReconciler(t *testing.T) {
	// Test a simple End-to-End reconcile operation
	// after the IPAM cache was already filled
	c := NewClient()

	ippool := adapter.AdaptIPPool(&ipamv1alpha1.IPv4Pool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pool",
			Namespace: "default",
			UID:       types.UID("1234"),
		},
		Spec: ipamv1alpha1.IPv4PoolSpec{
			CIDR: "192.0.2.0/24",
		},
	})
	ippoolNN := types.NamespacedName{
		Name:      ippool.GetName(),
		Namespace: ippool.GetNamespace(),
	}

	ipam := &ipamMock{}
	ipamCache := &ipamCacheMock{}

	ipamCache.
		On("GetOrCreate", mock.Anything, mock.Anything, mock.Anything).
		Return(ipam, nil)

	ipv4Prefix := &goipam.Prefix{
		Cidr: ippool.GetCIDR(),
	}
	ipam.On("PrefixFrom", "192.0.2.0/24").Return(ipv4Prefix)

	c.On("Get", mock.Anything, ippoolNN, mock.Anything).
		Run(func(args mock.Arguments) {
			ipv4pool := args.Get(2).(*adapter.IPv4Pool)
			*ipv4pool = *(ippool.(*adapter.IPv4Pool))
		}).
		Return(nil)
	c.On("Update",
		mock.Anything, mock.AnythingOfType("*adapter.IPv4Pool"), mock.Anything).
		Return(nil)
	var poolStatusUpdate *adapter.IPv4Pool
	c.StatusMock.
		On("Update",
			mock.Anything, mock.AnythingOfType("*adapter.IPv4Pool"), mock.Anything).
		Run(func(args mock.Arguments) {
			poolStatusUpdate = args.Get(1).(*adapter.IPv4Pool)
		}).
		Return(nil)

	r := &IPPoolReconciler{
		Client:     c,
		IPAMCache:  ipamCache,
		IPPoolType: adapter.AdaptIPPool(&ipamv1alpha1.IPv4Pool{}),
	}
	ctx := context.Background()
	res, err := r.Reconcile(ctx, ctrl.Request{NamespacedName: ippoolNN})
	require.NoError(t, err)
	assert.False(t, res.Requeue)
	assert.Empty(t, res.RequeueAfter)

	ipam.AssertExpectations(t)
	c.AssertExpectations(t)

	assert.Equal(t, 256, poolStatusUpdate.Status.AvailableIPs)
}

func TestIPPoolReconciler_createIPAM(t *testing.T) {
	c := NewClient()
	ipam := &ipamMock{}
	r := &IPPoolReconciler{
		Client:          c,
		NewIPAM:         func() Ipamer { return ipam },
		IPLeaseType:     adapter.AdaptIPLease(&ipamv1alpha1.IPv4Lease{}),
		IPLeaseListType: adapter.AdaptIPLeaseList(&ipamv1alpha1.IPv4LeaseList{}),
	}

	ippool := adapter.AdaptIPPool(&ipamv1alpha1.IPv4Pool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test-ns",
		},
		Spec: ipamv1alpha1.IPv4PoolSpec{
			CIDR: "192.0.2.0/24",
		},
	})

	c.On("List", mock.Anything, mock.AnythingOfType("*adapter.IPv4LeaseList"), mock.Anything).
		Run(func(args mock.Arguments) {
			leaseList := args.Get(1).(*adapter.IPv4LeaseList)
			leaseList.Items = []ipamv1alpha1.IPv4Lease{
				{
					Spec: ipamv1alpha1.IPv4LeaseSpec{
						Pool: ipamv1alpha1.LocalObjectReference{
							Name: ippool.GetName(),
						},
					},
					Status: ipamv1alpha1.IPv4LeaseStatus{
						Address: "192.0.2.1",
						Conditions: []metav1.Condition{
							{Type: ipamv1alpha1.IPLeaseBound, Status: metav1.ConditionTrue},
						},
					},
				},
				{ // not bound
					Spec: ipamv1alpha1.IPv4LeaseSpec{
						Pool: ipamv1alpha1.LocalObjectReference{
							Name: ippool.GetName(),
						},
					},
					Status: ipamv1alpha1.IPv4LeaseStatus{
						Address: "192.0.2.2",
						Conditions: []metav1.Condition{
							{Type: ipamv1alpha1.IPLeaseBound, Status: metav1.ConditionFalse},
						},
					},
				},
				{ // expired
					Spec: ipamv1alpha1.IPv4LeaseSpec{
						Pool: ipamv1alpha1.LocalObjectReference{
							Name: ippool.GetName(),
						},
					},
					Status: ipamv1alpha1.IPv4LeaseStatus{
						LeaseDuration: &metav1.Duration{},
						Address:       "192.0.2.3",
						Conditions: []metav1.Condition{
							{Type: ipamv1alpha1.IPLeaseBound, Status: metav1.ConditionTrue},
						},
					},
				},
			}
		}).
		Return(nil)
	ipam.On("NewPrefix", mock.Anything).Return((*goipam.Prefix)(nil), nil)
	ipam.On("AcquireSpecificIP", mock.Anything, mock.Anything).Return((*goipam.IP)(nil), nil)

	ctx := context.Background()
	i, err := r.createIPAM(ctx, ippool)
	require.NoError(t, err)
	require.Same(t, ipam, i)

	ipam.AssertCalled(t, "AcquireSpecificIP", ippool.GetCIDR(), "192.0.2.1")

	// Unbound
	ipam.AssertNotCalled(t, "AcquireSpecificIP", ippool.GetCIDR(), "192.0.2.2")

	// Expired
	ipam.AssertNotCalled(t, "AcquireSpecificIP", ippool.GetCIDR(), "192.0.2.3")
}

func TestIPPoolReconciler_handleDeletion(t *testing.T) {
	c := NewClient()
	ipamCache := &ipamCacheMock{}

	r := &IPPoolReconciler{
		Client:    c,
		IPAMCache: ipamCache,
	}

	ippool := adapter.AdaptIPPool(&ipamv1alpha1.IPv4Pool{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{
				ipamCacheFinalizer,
			},
		},
	})
	c.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	ipamCache.On("Free", mock.Anything).Return(nil)

	ctx := context.Background()
	err := r.handleDeletion(ctx, ippool)
	require.NoError(t, err)

	ipamCache.AssertCalled(t, "Free", ippool)
	c.AssertCalled(t, "Update", mock.Anything, ippool, mock.Anything)
}
