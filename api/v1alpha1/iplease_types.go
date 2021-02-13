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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IPLeaseSpec defines the desired state of IPLease
type IPLeaseSpec struct {
	// References the pool to lease an IP from.
	Pool LocalObjectReference `json:"pool"`
	// Static IP lease settings.
	Static *StaticIPLease `json:"static,omitempty"`
	// Time this lease was renewed the last time.
	LastRenewTime metav1.Time `json:"lastRenewTime,omitempty"`
}

type StaticIPLease struct {
	// List of addresses to try to lease from the pool.
	Addresses []string `json:"addresses"`
}

type LocalObjectReference struct {
	Name string `json:"name"`
}

// IPLeaseStatus defines the observed state of IPLease
type IPLeaseStatus struct {
	// The most recent generation observed by the controller.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
	// Conditions is a list of status conditions ths object is in.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// Human readable status aggregated from conditions.
	Phase string `json:"phase,omitempty"`
	// List of leased addresses.
	Addresses []string `json:"addresses,omitempty"`
	// Time this lease expires without renewal.
	// If this time is After .spec.lastRenewTime,
	// clients need to acquire a new lease.
	ExpireTime metav1.Time `json:"expireTime,omitempty"`
}

// IPLease Condition Types
const (
	IPLeaseBound = "Bound"
)

// IPLease is the Schema for the ipleases API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Addresses",type="string",JSONPath=".status.addresses"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type IPLease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IPLeaseSpec   `json:"spec,omitempty"`
	Status IPLeaseStatus `json:"status,omitempty"`
}

// IPLeaseList contains a list of IPLease
// +kubebuilder:object:root=true
type IPLeaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPLease `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IPLease{}, &IPLeaseList{})
}
