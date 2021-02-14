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

	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

func TestIPPoolReconciler_reportUsage(t *testing.T) {
	c := NewClient()
	ipam := &ipamMock{}
	r := &IPPoolReconciler{
		Client: c,
	}

	ippool := &ipamv1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test-ns",
		},
		Spec: ipamv1alpha1.IPPoolSpec{
			IPv4: &ipamv1alpha1.IPTypePool{
				CIDR: "192.0.2.0/24",
			},
			IPv6: &ipamv1alpha1.IPTypePool{
				CIDR: "2001:DB8::/64",
			},
		},
	}

	ipv4Prefix := &goipam.Prefix{
		Cidr: ippool.Spec.IPv4.CIDR,
	}
	ipam.On("PrefixFrom", "192.0.2.0/24").Return(ipv4Prefix)

	ipv6Prefix := &goipam.Prefix{
		Cidr: ippool.Spec.IPv6.CIDR,
	}
	ipam.On("PrefixFrom", "2001:DB8::/64").Return(ipv6Prefix)

	c.StatusMock.On("Update", mock.Anything, ippool, mock.Anything).Return(nil)

	ctx := context.Background()
	err := r.reportUsage(ctx, ippool, ipam)
	require.NoError(t, err)

	assert.Equal(t, &ipamv1alpha1.IPTypePoolStatus{
		AvailableIPs: 256,
	}, ippool.Status.IPv4)
	assert.Equal(t, &ipamv1alpha1.IPTypePoolStatus{
		AvailableIPs: 2147483647,
	}, ippool.Status.IPv6)
}

func TestIPPoolReconciler_createIPAM(t *testing.T) {
	c := NewClient()
	ipam := &ipamMock{}
	r := &IPPoolReconciler{
		Client:  c,
		NewIPAM: func() Ipamer { return ipam },
	}

	ippool := &ipamv1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test",
			Namespace: "test-ns",
		},
		Spec: ipamv1alpha1.IPPoolSpec{
			IPv4: &ipamv1alpha1.IPTypePool{
				CIDR: "192.0.2.0/24",
			},
			IPv6: &ipamv1alpha1.IPTypePool{
				CIDR: "2001:DB8::/64",
			},
		},
	}

	c.On("List", mock.Anything, mock.AnythingOfType("*v1alpha1.IPLeaseList"), mock.Anything).
		Run(func(args mock.Arguments) {
			leaseList := args.Get(1).(*ipamv1alpha1.IPLeaseList)
			leaseList.Items = []ipamv1alpha1.IPLease{
				{
					Spec: ipamv1alpha1.IPLeaseSpec{
						Pool: ipamv1alpha1.LocalObjectReference{
							Name: ippool.Name,
						},
					},
					Status: ipamv1alpha1.IPLeaseStatus{
						Addresses: []string{
							"192.0.2.1",
							"2001:DB8::1",
						},
						Conditions: []metav1.Condition{
							{Type: ipamv1alpha1.IPLeaseBound, Status: metav1.ConditionTrue},
						},
					},
				},
				{ // not bound
					Spec: ipamv1alpha1.IPLeaseSpec{
						Pool: ipamv1alpha1.LocalObjectReference{
							Name: ippool.Name,
						},
					},
					Status: ipamv1alpha1.IPLeaseStatus{
						Addresses: []string{
							"192.0.2.2",
							"2001:DB8::2",
						},
						Conditions: []metav1.Condition{
							{Type: ipamv1alpha1.IPLeaseBound, Status: metav1.ConditionFalse},
						},
					},
				},
				{ // expired
					Spec: ipamv1alpha1.IPLeaseSpec{
						Pool: ipamv1alpha1.LocalObjectReference{
							Name: ippool.Name,
						},
					},
					Status: ipamv1alpha1.IPLeaseStatus{
						LeaseDuration: &metav1.Duration{},
						Addresses: []string{
							"192.0.2.3",
							"2001:DB8::3",
						},
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

	ipam.AssertCalled(t, "AcquireSpecificIP", ippool.Spec.IPv4.CIDR, "192.0.2.1")
	ipam.AssertCalled(t, "AcquireSpecificIP", ippool.Spec.IPv6.CIDR, "2001:DB8::1")

	// Unbound
	ipam.AssertNotCalled(t, "AcquireSpecificIP", ippool.Spec.IPv4.CIDR, "192.0.2.2")
	ipam.AssertNotCalled(t, "AcquireSpecificIP", ippool.Spec.IPv6.CIDR, "2001:DB8::2")

	// Expired
	ipam.AssertNotCalled(t, "AcquireSpecificIP", ippool.Spec.IPv4.CIDR, "192.0.2.3")
	ipam.AssertNotCalled(t, "AcquireSpecificIP", ippool.Spec.IPv6.CIDR, "2001:DB8::3")
}

func TestIPPoolReconciler_handleDeletion(t *testing.T) {
	c := NewClient()
	ipamCache := &ipamCacheMock{}

	r := &IPPoolReconciler{
		Client:    c,
		IPAMCache: ipamCache,
	}

	ippool := &ipamv1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{
				ipamCacheFinalizer,
			},
		},
	}
	c.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	ipamCache.On("Free", mock.Anything).Return(nil)

	ctx := context.Background()
	err := r.handleDeletion(ctx, ippool)
	require.NoError(t, err)

	ipamCache.AssertCalled(t, "Free", ippool)
	c.AssertCalled(t, "Update", mock.Anything, ippool, mock.Anything)
}

func TestIPPoolReconciler_ensureCacheFinalizer(t *testing.T) {
	t.Run("adds finalizer if absent", func(t *testing.T) {
		c := NewClient()
		r := &IPPoolReconciler{
			Client: c,
		}

		ippool := &ipamv1alpha1.IPPool{}
		c.On("Update", mock.Anything, ippool, mock.Anything).Return(nil)

		ctx := context.Background()
		err := r.ensureCacheFinalizer(ctx, ippool)
		require.NoError(t, err)

		assert.Equal(t, []string{
			ipamCacheFinalizer,
		}, ippool.Finalizers)
		c.AssertCalled(t, "Update", mock.Anything, ippool, mock.Anything)
	})

	t.Run("doesn't add finalizer if present", func(t *testing.T) {
		c := NewClient()
		r := &IPPoolReconciler{
			Client: c,
		}

		ippool := &ipamv1alpha1.IPPool{
			ObjectMeta: metav1.ObjectMeta{
				Finalizers: []string{
					ipamCacheFinalizer,
				},
			},
		}
		c.On("Update", mock.Anything, ippool, mock.Anything).Return(nil)

		ctx := context.Background()
		err := r.ensureCacheFinalizer(ctx, ippool)
		require.NoError(t, err)

		c.AssertNotCalled(t, "Update", mock.Anything, mock.Anything, mock.Anything)
	})
}
