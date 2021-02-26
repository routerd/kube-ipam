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

	"github.com/metal-stack/go-ipam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"inet.af/netaddr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"

	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
	"routerd.net/kube-ipam/internal/controllers/adapter"
)

func TestIPLeaseReconciler(t *testing.T) {
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
	iplease := adapter.AdaptIPLease(&ipamv1alpha1.IPv4Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "lease",
			Namespace: "default",
		},
		Spec: ipamv1alpha1.IPv4LeaseSpec{
			Pool: ipamv1alpha1.LocalObjectReference{
				Name: ippool.GetName(),
			},
			Type: ipamv1alpha1.IPLeaseTypeStatic,
			Static: &ipamv1alpha1.IPLeaseStatic{
				Address: "192.0.2.23",
			},
		},
	})
	ipleaseNN := types.NamespacedName{
		Name:      iplease.GetName(),
		Namespace: iplease.GetNamespace(),
	}

	ipv4 := &ipam.IP{
		IP: netaddr.MustParseIP("192.0.2.23"),
	}
	ipam := &ipamMock{}
	ipamCache := &ipamCacheMock{}
	ipam.
		On("AcquireSpecificIP", ippool.GetCIDR(), iplease.GetSpecStaticAddress()).
		Return(ipv4, nil)

	ipamCache.
		On("Get", ippool).
		Return(ipam, true)

	c.On("Get", mock.Anything, ipleaseNN, mock.AnythingOfType("*adapter.IPv4Lease")).
		Run(func(args mock.Arguments) {
			ipv4lease := args.Get(2).(*adapter.IPv4Lease)
			*ipv4lease = *(iplease.(*adapter.IPv4Lease))
		}).
		Return(nil)
	c.On("Get", mock.Anything, ippoolNN, mock.AnythingOfType("*adapter.IPv4Pool")).
		Run(func(args mock.Arguments) {
			ipv4pool := args.Get(2).(*adapter.IPv4Pool)
			*ipv4pool = *(ippool.(*adapter.IPv4Pool))
		}).
		Return(nil)
	c.On("Update",
		mock.Anything, mock.AnythingOfType("*adapter.IPv4Lease"), mock.Anything).
		Return(nil)
	var leaseStatusUpdate *adapter.IPv4Lease
	c.StatusMock.
		On("Update",
			mock.Anything, mock.AnythingOfType("*adapter.IPv4Lease"), mock.Anything).
		Run(func(args mock.Arguments) {
			leaseStatusUpdate = args.Get(1).(*adapter.IPv4Lease)
		}).
		Return(nil)

	r := &IPLeaseReconciler{
		Client:      c,
		Log:         NewLogger(t),
		IPAMCache:   ipamCache,
		IPPoolType:  adapter.AdaptIPPool(&ipamv1alpha1.IPv4Pool{}),
		IPLeaseType: adapter.AdaptIPLease(&ipamv1alpha1.IPv4Lease{}),
	}

	ctx := context.Background()
	res, err := r.Reconcile(ctx, ctrl.Request{
		NamespacedName: ipleaseNN,
	})
	require.NoError(t, err)
	assert.False(t, res.Requeue)
	assert.Empty(t, res.RequeueAfter)

	assert.Equal(t,
		iplease.GetSpecStaticAddress(), leaseStatusUpdate.Status.Address)
}
