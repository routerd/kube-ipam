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
	"sigs.k8s.io/controller-runtime/pkg/client"

	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

var (
	_ IPLease     = (*IPv4Lease)(nil)
	_ IPLeaseList = (*IPv4LeaseList)(nil)
)

type IPv4Lease struct {
	ipamv1alpha1.IPv4Lease
}

func (lease *IPv4Lease) ClientObject() client.Object {
	return &lease.IPv4Lease
}

func (lease *IPv4Lease) GetSpecIPPoolName() string {
	return lease.Spec.Pool.Name
}

func (lease *IPv4Lease) GetStatusConditions() *[]metav1.Condition {
	return &lease.Status.Conditions
}

func (lease *IPv4Lease) GetStatusAddress() string {
	return lease.Status.Address
}

func (lease *IPv4Lease) SetStatusAddress(address string) {
	lease.Status.Address = address
}

func (lease *IPv4Lease) SetStatusLeaseDuration(leaseDuration time.Duration) {
	if leaseDuration == 0 {
		lease.Status.LeaseDuration = nil
	} else {
		lease.Status.LeaseDuration = &metav1.Duration{Duration: leaseDuration}
	}
}

func (lease *IPv4Lease) GetStatusLeaseDuration() (
	leaseDuration time.Duration, ok bool) {
	if lease.Status.LeaseDuration == nil {
		return
	}
	return lease.Status.LeaseDuration.Duration, true
}

func (lease *IPv4Lease) GetSpecType() ipamv1alpha1.IPLeaseType {
	return lease.Spec.Type
}

func (lease *IPv4Lease) GetSpecStaticAddress() string {
	if lease.Spec.Static == nil {
		return ""
	}
	return lease.Spec.Static.Address
}

func (lease *IPv4Lease) SetStatusPhase(phase string) {
	lease.Status.Phase = phase
}

func (lease *IPv4Lease) SetStatusObservedGeneration(observedGeneration int64) {
	lease.Status.ObservedGeneration = observedGeneration
}

type IPv4LeaseList struct {
	ipamv1alpha1.IPv4LeaseList
}

func (lease *IPv4LeaseList) ClientObjectList() client.ObjectList {
	return &lease.IPv4LeaseList
}

func (leaseList *IPv4LeaseList) GetItems() []IPLease {
	out := make([]IPLease, len(leaseList.Items))
	for i, lease := range leaseList.Items {
		out[i] = &IPv4Lease{lease}
	}
	return out
}
