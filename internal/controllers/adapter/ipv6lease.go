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

package adapter

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

var (
	_ IPLease     = (*IPv6Lease)(nil)
	_ IPLeaseList = (*IPv6LeaseList)(nil)
)

type IPv6Lease struct {
	ipamv1alpha1.IPv6Lease
}

func (lease *IPv6Lease) GetSpecIPPoolName() string {
	return lease.Spec.Pool.Name
}

func (lease *IPv6Lease) GetStatusConditions() *[]metav1.Condition {
	return &lease.Status.Conditions
}

func (lease *IPv6Lease) GetStatusAddress() string {
	return lease.Status.Address
}

func (lease *IPv6Lease) SetStatusAddress(address string) {
	lease.Status.Address = address
}

func (lease *IPv6Lease) SetStatusLeaseDuration(leaseDuration time.Duration) {
	if leaseDuration == 0 {
		lease.Status.LeaseDuration = nil
	} else {
		lease.Status.LeaseDuration = &metav1.Duration{Duration: leaseDuration}
	}
}

func (lease *IPv6Lease) GetStatusLeaseDuration() (
	leaseDuration time.Duration, ok bool) {
	if lease.Status.LeaseDuration == nil {
		return
	}
	return lease.Status.LeaseDuration.Duration, true
}

func (lease *IPv6Lease) GetSpecType() ipamv1alpha1.IPLeaseType {
	return lease.Spec.Type
}

func (lease *IPv6Lease) GetSpecStaticAddress() string {
	if lease.Spec.Static == nil {
		return ""
	}
	return lease.Spec.Static.Address
}

func (lease *IPv6Lease) SetStatusPhase(phase string) {
	lease.Status.Phase = phase
}

func (lease *IPv6Lease) SetStatusObservedGeneration(observedGeneration int64) {
	lease.Status.ObservedGeneration = observedGeneration
}

type IPv6LeaseList struct {
	ipamv1alpha1.IPv6LeaseList
}

func (leaseList *IPv6LeaseList) GetItems() []IPLease {
	out := make([]IPLease, len(leaseList.Items))
	for i, lease := range leaseList.Items {
		out[i] = &IPv6Lease{lease}
	}
	return out
}
