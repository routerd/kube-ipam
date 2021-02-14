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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"routerd.net/kube-ipam/api/v1alpha1"
	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

func TestIPLeaseReconciler_handleDeletion(t *testing.T) {
	c := NewClient()
	ipamCache := &ipamCacheMock{}
	ipam := &ipamMock{}

	r := &IPLeaseReconciler{
		Client:    c,
		IPAMCache: ipamCache,
	}

	ippool := &ipamv1alpha1.IPPool{
		Spec: ipamv1alpha1.IPPoolSpec{
			IPv4: &ipamv1alpha1.IPTypePool{
				CIDR: "192.0.2.0/24",
			},
			IPv6: &ipamv1alpha1.IPTypePool{
				CIDR: "2001:DB8::1",
			},
		},
	}

	iplease := &ipamv1alpha1.IPLease{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{
				ipamCacheFinalizer,
			},
			Namespace: "test-ns",
		},
		Spec: ipamv1alpha1.IPLeaseSpec{
			Pool: ipamv1alpha1.LocalObjectReference{
				Name: "test-pool",
			},
		},
		Status: ipamv1alpha1.IPLeaseStatus{
			Addresses: []string{
				"192.0.2.1",
				"192.0.2.2",
				"2001:DB8::1",
				"2001:DB8::2",
			},
		},
	}
	c.On("Get",
		mock.Anything, types.NamespacedName{
			Name:      iplease.Spec.Pool.Name,
			Namespace: iplease.Namespace,
		}, mock.AnythingOfType("*v1alpha1.IPPool"), mock.Anything).
		Run(func(args mock.Arguments) {
			p := args.Get(2).(*v1alpha1.IPPool)
			*p = *ippool
		}).
		Return(nil)
	c.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	ipamCache.On("Get", mock.Anything).Return(ipam, true)
	ipam.On("ReleaseIPFromPrefix", mock.Anything, mock.Anything).Return(nil)

	ctx := context.Background()
	err := r.handleDeletion(ctx, nil, iplease)
	require.NoError(t, err)

	c.AssertCalled(t, "Update", mock.Anything, iplease, mock.Anything)
	ipam.AssertCalled(t, "ReleaseIPFromPrefix", ippool.Spec.IPv4.CIDR, iplease.Status.Addresses[0])
	ipam.AssertCalled(t, "ReleaseIPFromPrefix", ippool.Spec.IPv4.CIDR, iplease.Status.Addresses[1])
	ipam.AssertCalled(t, "ReleaseIPFromPrefix", ippool.Spec.IPv6.CIDR, iplease.Status.Addresses[2])
	ipam.AssertCalled(t, "ReleaseIPFromPrefix", ippool.Spec.IPv6.CIDR, iplease.Status.Addresses[3])
}

func TestIPLeaseReconciler_ensureCacheFinalizer(t *testing.T) {
	t.Run("adds finalizer if absent", func(t *testing.T) {
		c := NewClient()
		r := &IPLeaseReconciler{
			Client: c,
		}

		ippool := &ipamv1alpha1.IPLease{}
		c.On("Update", mock.Anything, ippool, mock.Anything).Return(nil)

		ctx := context.Background()
		err := r.ensureCacheFinalizer(ctx, ippool)
		require.NoError(t, err)

		assert.Equal(t, []string{
			ipamCacheFinalizer,
		}, ippool.Finalizers)
	})

	t.Run("doesn't add finalizer if present", func(t *testing.T) {
		c := NewClient()
		r := &IPLeaseReconciler{
			Client: c,
		}

		ippool := &ipamv1alpha1.IPLease{
			ObjectMeta: metav1.ObjectMeta{
				Finalizers: []string{
					ipamCacheFinalizer,
				},
			},
		}

		ctx := context.Background()
		err := r.ensureCacheFinalizer(ctx, ippool)
		require.NoError(t, err)
	})
}
