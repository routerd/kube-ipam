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

// Provides a common interface adapter for both IPv6 and IPv4 Leases and Pools
package adapter

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

var (
	_ IPPool      = (*IPv4Pool)(nil)
	_ IPPool      = (*IPv6Pool)(nil)
	_ IPLease     = (*IPv4Lease)(nil)
	_ IPLease     = (*IPv6Lease)(nil)
	_ IPLeaseList = (*IPv4LeaseList)(nil)
	_ IPLeaseList = (*IPv6LeaseList)(nil)
)

// Common accessor interface for IPPools
type IPPool interface {
	client.Object
	GetCIDR() string
	GetSpecLeaseDuration() (leaseDuration time.Duration, ok bool)
	SetAvailableIPs(int)
	SetAllocatedIPs(int)
}

// Common accessor interface for IPLeases
type IPLease interface {
	client.Object
	GetSpecType() ipamv1alpha1.IPLeaseType
	GetSpecIPPoolName() string
	GetSpecStaticAddress() string

	GetStatusConditions() *[]metav1.Condition
	GetStatusAddress() string
	SetStatusAddress(string)
	SetStatusLeaseDuration(leaseDuration time.Duration)
	GetStatusLeaseDuration() (leaseDuration time.Duration, ok bool)
	SetStatusPhase(string)
	SetStatusObservedGeneration(int64)

	HasExpired() bool
}

// Common accessor interface for IPLeaseLists
type IPLeaseList interface {
	client.ObjectList
	GetItems() []IPLease
}

func AdaptIPPool(ippool runtime.Object) IPPool {
	switch v := ippool.(type) {
	case *ipamv1alpha1.IPv4Pool:
		return &IPv4Pool{IPv4Pool: *v}

	case *ipamv1alpha1.IPv6Pool:
		return &IPv6Pool{IPv6Pool: *v}
	}
	return nil
}

func AdaptIPLease(iplease runtime.Object) IPLease {
	switch v := iplease.(type) {
	case *ipamv1alpha1.IPv4Lease:
		return &IPv4Lease{IPv4Lease: *v}

	case *ipamv1alpha1.IPv6Lease:
		return &IPv6Lease{IPv6Lease: *v}
	}
	return nil
}

func AdaptIPLeaseList(ipleaseList runtime.Object) IPLeaseList {
	switch v := ipleaseList.(type) {
	case *ipamv1alpha1.IPv4LeaseList:
		return &IPv4LeaseList{IPv4LeaseList: *v}

	case *ipamv1alpha1.IPv6LeaseList:
		return &IPv6LeaseList{IPv6LeaseList: *v}
	}
	return nil
}
