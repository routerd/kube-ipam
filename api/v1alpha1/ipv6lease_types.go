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

package v1alpha1

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IPv6LeaseSpec defines the desired state of IPv6Lease
type IPv6LeaseSpec struct {
	// References the IPv6Pool to lease an IP from.
	Pool LocalObjectReference `json:"pool"`

	// Type of the IPv6Lease.
	// +kubebuilder:default="Dynamic"
	// +kubebuilder:validation:Enum=Dynamic;Static
	Type IPLeaseType `json:"type"`

	// Static IP lease settings.
	Static *IPLeaseStatic `json:"static,omitempty"`

	// Renew time is the time when the lease holder has last updated the lease.
	// Falls back to .metadata.creationTimestamp if not set.
	RenewTime metav1.MicroTime `json:"renewTime,omitempty"`
}

// IPv6LeaseStatus defines the observed state of IPv6Lease
type IPv6LeaseStatus struct {
	// The most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions is a list of status conditions ths object is in.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Human readable status aggregated from conditions.
	Phase string `json:"phase,omitempty"`
	// Leased IP Address.
	Address string `json:"address,omitempty"`
	// Duration of the lease, if empty lease does not expire.
	LeaseDuration *metav1.Duration `json:"leaseDuration,omitempty"`
}

// IPv6Lease is the Schema for the ipv6leases API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Pool",type="string",JSONPath=".spec.pool.name"
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Address",type="string",JSONPath=".status.address"
// +kubebuilder:printcolumn:name="Renew",type="date",JSONPath=".spec.renewTime"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type IPv6Lease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IPv6LeaseSpec   `json:"spec,omitempty"`
	Status IPv6LeaseStatus `json:"status,omitempty"`
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

func (lease *IPv6Lease) HasExpired() bool {
	if lease.Status.LeaseDuration == nil {
		// no lease duration
		// -> can't expire
		return false
	}

	now := time.Now().UTC()
	renewTime := lease.CreationTimestamp.Time
	if !lease.Spec.RenewTime.IsZero() {
		renewTime = lease.Spec.RenewTime.Time
	}

	return renewTime.UTC().Add(lease.Status.LeaseDuration.Duration).Before(now)
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

func (lease *IPv6Lease) GetSpecType() IPLeaseType {
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

// IPv6LeaseList contains a list of IPv6Lease
// +kubebuilder:object:root=true
type IPv6LeaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPv6Lease `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IPv6Lease{}, &IPv6LeaseList{})
}
