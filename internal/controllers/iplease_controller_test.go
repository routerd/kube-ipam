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
	"testing"
	"time"

	"github.com/metal-stack/go-ipam"
	goipam "github.com/metal-stack/go-ipam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"inet.af/netaddr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"routerd.net/kube-ipam/api/v1alpha1"
	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

func TestIPLeaseReconciler_reportAllocatedIPs(t *testing.T) {
	t.Run("reports IPs", func(t *testing.T) {
		c := NewClient()
		ipam := &ipamMock{}

		r := &IPLeaseReconciler{
			Client: c,
		}

		iplease := &ipamv1alpha1.IPLease{}
		c.StatusMock.On("Update", mock.Anything, iplease, mock.Anything).Return(nil)

		ctx := context.Background()
		err := r.reportAllocatedIPs(ctx, iplease, ipam, []goipam.IP{{IP: netaddr.MustParseIP("192.0.2.1")}})
		require.NoError(t, err)

		assert.Equal(t, []string{"192.0.2.1"}, iplease.Status.Addresses)
	})
}

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

func TestIPLeaseReconciler_allocateIPs(t *testing.T) {
	t.Run("should requeue request when IPAM cache is empty", func(t *testing.T) {
		iplease := &ipamv1alpha1.IPLease{}
		ippool := &ipamv1alpha1.IPPool{}
		ipamCache := &ipamCacheMock{}

		ipamCache.On("Get", ippool).Return((Ipamer)(nil), false)

		r := &IPLeaseReconciler{
			IPAMCache: ipamCache,
		}
		ctx := context.Background()
		res, err := r.allocateIPs(ctx, NewLogger(t), iplease, ippool)
		require.NoError(t, err)
		assert.True(t, res.Requeue)
	})

	t.Run("should allocate static IPs", func(t *testing.T) {
		client := NewClient()
		ippool := &ipamv1alpha1.IPPool{
			Spec: ipamv1alpha1.IPPoolSpec{
				IPv4: &ipamv1alpha1.IPTypePool{
					CIDR: "172.20.0.2/24",
				},
				IPv6: &ipamv1alpha1.IPTypePool{
					CIDR: "fd9c:fd74:6b8d:1020::2/64",
				},
			},
		}
		iplease := &ipamv1alpha1.IPLease{
			Spec: ipamv1alpha1.IPLeaseSpec{
				Static: &ipamv1alpha1.StaticIPLease{
					Addresses: []string{
						"172.20.0.2",
						"fd9c:fd74:6b8d:1020::2",
					},
				},
			},
		}

		ipv4 := &ipam.IP{
			IP: netaddr.MustParseIP("172.20.0.2"),
		}
		ipv6 := &ipam.IP{
			IP: netaddr.MustParseIP("fd9c:fd74:6b8d:1020::2"),
		}
		ipamer := &ipamMock{}
		ipamer.
			On("AcquireSpecificIP", ippool.Spec.IPv4.CIDR, "172.20.0.2").
			Return(ipv4, nil)
		ipamer.
			On("AcquireSpecificIP", ippool.Spec.IPv6.CIDR, "fd9c:fd74:6b8d:1020::2").
			Return(ipv6, nil)

		ipamCache := &ipamCacheMock{}
		ipamCache.On("Get", ippool).Return(ipamer, true)

		client.StatusMock.
			On("Update", mock.Anything, mock.Anything, mock.Anything).
			Return(nil)

		r := &IPLeaseReconciler{
			Client:    client,
			IPAMCache: ipamCache,
		}
		ctx := context.Background()
		res, err := r.allocateIPs(ctx, NewLogger(t), iplease, ippool)
		require.NoError(t, err)
		assert.False(t, res.Requeue)

		ipamer.AssertExpectations(t)
		assert.Equal(t,
			iplease.Spec.Static.Addresses, iplease.Status.Addresses)
	})

	t.Run("should allocate dynamic IPs", func(t *testing.T) {
		client := NewClient()
		ippool := &ipamv1alpha1.IPPool{
			Spec: ipamv1alpha1.IPPoolSpec{
				IPv4: &ipamv1alpha1.IPTypePool{
					CIDR: "172.20.0.2/24",
				},
				IPv6: &ipamv1alpha1.IPTypePool{
					CIDR: "fd9c:fd74:6b8d:1020::2/64",
				},
			},
		}
		iplease := &ipamv1alpha1.IPLease{
			Spec: ipamv1alpha1.IPLeaseSpec{
				IPFamilies: []corev1.IPFamily{
					corev1.IPv4Protocol,
					corev1.IPv6Protocol,
				},
			},
		}

		ipv4 := &ipam.IP{
			IP: netaddr.MustParseIP("172.20.0.2"),
		}
		ipv6 := &ipam.IP{
			IP: netaddr.MustParseIP("fd9c:fd74:6b8d:1020::2"),
		}
		ipamer := &ipamMock{}
		ipamer.
			On("AcquireIP", ippool.Spec.IPv4.CIDR).
			Return(ipv4, nil)
		ipamer.
			On("AcquireIP", ippool.Spec.IPv6.CIDR).
			Return(ipv6, nil)

		ipamCache := &ipamCacheMock{}
		ipamCache.On("Get", ippool).Return(ipamer, true)

		client.StatusMock.
			On("Update", mock.Anything, mock.Anything, mock.Anything).
			Return(nil)

		r := &IPLeaseReconciler{
			Client:    client,
			IPAMCache: ipamCache,
		}
		ctx := context.Background()
		res, err := r.allocateIPs(ctx, NewLogger(t), iplease, ippool)
		require.NoError(t, err)
		assert.False(t, res.Requeue)

		ipamer.AssertExpectations(t)
		assert.Equal(t,
			[]string{
				"172.20.0.2", "fd9c:fd74:6b8d:1020::2",
			}, iplease.Status.Addresses)
	})
}

func TestIPLeaseReconciler_allocateStaticIPs_releaseIPsIfAllCouldNotBeAcquired(t *testing.T) {
	commonIPPool := &ipamv1alpha1.IPPool{
		Spec: ipamv1alpha1.IPPoolSpec{
			IPv4: &ipamv1alpha1.IPTypePool{
				CIDR: "172.20.0.2/24",
			},
			IPv6: &ipamv1alpha1.IPTypePool{
				CIDR: "fd9c:fd74:6b8d:1020::2/64",
			},
		},
	}
	commonLease := &ipamv1alpha1.IPLease{
		Spec: ipamv1alpha1.IPLeaseSpec{
			Static: &ipamv1alpha1.StaticIPLease{
				Addresses: []string{
					"172.20.0.2",
					"fd9c:fd74:6b8d:1020::2",
				},
			},
		},
	}

	tests := []struct {
		name              string
		ippool            *ipamv1alpha1.IPPool
		iplease           *ipamv1alpha1.IPLease
		prepareIpamerMock func(
			ipamer *ipamMock, ippool *ipamv1alpha1.IPPool)
		expectError error
	}{
		{
			name:    "release IPv6 when IPv4 can not be acquired",
			ippool:  commonIPPool,
			iplease: commonLease,
			prepareIpamerMock: func(ipamer *ipamMock, ippool *ipamv1alpha1.IPPool) {
				ipv4 := &ipam.IP{
					IP: netaddr.MustParseIP("172.20.0.2"),
				}
				ipv6 := &ipam.IP{
					IP: netaddr.MustParseIP("fd9c:fd74:6b8d:1020::2"),
				}

				ipamer.
					On("AcquireSpecificIP", ippool.Spec.IPv4.CIDR, "172.20.0.2").
					Return(ipv4, goipam.ErrNoIPAvailable)
				ipamer.
					On("AcquireSpecificIP", ippool.Spec.IPv6.CIDR, "fd9c:fd74:6b8d:1020::2").
					Return(ipv6, nil)

				ipamer.On("ReleaseIP", ipv6).Return((*ipam.Prefix)(nil), nil)
			},
		},
		{
			name:    "release IPv4 when IPv6 can not be acquired",
			ippool:  commonIPPool,
			iplease: commonLease,
			prepareIpamerMock: func(ipamer *ipamMock, ippool *ipamv1alpha1.IPPool) {
				ipv4 := &ipam.IP{
					IP: netaddr.MustParseIP("172.20.0.2"),
				}
				ipv6 := &ipam.IP{
					IP: netaddr.MustParseIP("fd9c:fd74:6b8d:1020::2"),
				}

				ipamer.
					On("AcquireSpecificIP", ippool.Spec.IPv4.CIDR, "172.20.0.2").
					Return(ipv4, nil)
				ipamer.
					On("AcquireSpecificIP", ippool.Spec.IPv6.CIDR, "fd9c:fd74:6b8d:1020::2").
					Return(ipv6, goipam.ErrNoIPAvailable)
				ipamer.On("ReleaseIP", ipv4).Return((*ipam.Prefix)(nil), nil)
			},
		},
		{
			name:   "release IP if one of the IPs can't be parsed",
			ippool: commonIPPool,
			iplease: &ipamv1alpha1.IPLease{
				Spec: ipamv1alpha1.IPLeaseSpec{
					Static: &ipamv1alpha1.StaticIPLease{
						Addresses: []string{
							"172.20.0.2",
							"fd9c:fd74:6b8d:1020::2:::::::stuff",
						},
					},
				},
			},
			prepareIpamerMock: func(ipamer *ipamMock, ippool *ipamv1alpha1.IPPool) {
				ipv4 := &ipam.IP{
					IP: netaddr.MustParseIP("172.20.0.2"),
				}

				ipamer.
					On("AcquireSpecificIP", ippool.Spec.IPv4.CIDR, "172.20.0.2").
					Return(ipv4, nil)
				ipamer.On("ReleaseIP", ipv4).Return((*ipam.Prefix)(nil), nil)
			},
		},
		{
			name: "release IPv4 if IPv6 is not supported by pool",
			ippool: &ipamv1alpha1.IPPool{
				Spec: ipamv1alpha1.IPPoolSpec{
					IPv4: &ipamv1alpha1.IPTypePool{
						CIDR: "172.20.0.2/24",
					},
				},
			},
			iplease: commonLease,
			prepareIpamerMock: func(ipamer *ipamMock, ippool *ipamv1alpha1.IPPool) {
				ipv4 := &ipam.IP{
					IP: netaddr.MustParseIP("172.20.0.2"),
				}

				ipamer.
					On("AcquireSpecificIP", ippool.Spec.IPv4.CIDR, "172.20.0.2").
					Return(ipv4, nil)
				ipamer.On("ReleaseIP", ipv4).Return((*ipam.Prefix)(nil), nil)
			},
			expectError: fmt.Errorf("boom"),
		},
		{
			name: "release IPv6 if IPv4 is not supported by pool",
			ippool: &ipamv1alpha1.IPPool{
				Spec: ipamv1alpha1.IPPoolSpec{
					IPv6: &ipamv1alpha1.IPTypePool{
						CIDR: "fd9c:fd74:6b8d:1020::2/64",
					},
				},
			},
			iplease: commonLease,
			prepareIpamerMock: func(ipamer *ipamMock, ippool *ipamv1alpha1.IPPool) {
				ipv6 := &ipam.IP{
					IP: netaddr.MustParseIP("fd9c:fd74:6b8d:1020::2"),
				}

				ipamer.
					On("AcquireSpecificIP", ippool.Spec.IPv6.CIDR, "fd9c:fd74:6b8d:1020::2").
					Return(ipv6, nil)
				ipamer.On("ReleaseIP", ipv6).Return((*ipam.Prefix)(nil), nil)
			},
			expectError: fmt.Errorf("boom"),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := NewClient()
			client.StatusMock.
				On("Update", mock.Anything, mock.Anything, mock.Anything).
				Return(nil)

			ipamer := &ipamMock{}
			test.prepareIpamerMock(ipamer, test.ippool)

			r := &IPLeaseReconciler{
				Client: client,
			}
			ctx := context.Background()
			res, err := r.allocateStaticIPs(
				ctx, ipamer, test.iplease, test.ippool)
			require.NoError(t, err)
			assert.False(t, res.Requeue)
			assert.Equal(t, 5*time.Second, res.RequeueAfter)

			ipamer.AssertExpectations(t)
		})
	}
}
