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

// IPPoolSpec defines the desired state of IPPool
type IPPoolSpec struct {
	// IPv4 addresss pool
	IPv4 *IPv4Pool `json:"ipv4,omitempty"`
	// IPv6 addresss pool
	IPv6 *IPv6Pool `json:"ipv6,omitempty"`
	// lease duration for leased ips.
	// Lease must be renewed in time or it will be reclaimed into the pool.
	LeaseDuration metav1.Duration `json:"leaseDuration"`
}

// IPv4 address pool configuration.
type IPv4Pool struct {
	CIDR string `json:"cidr"`
}

// IPv6 address pool configuration.
type IPv6Pool struct {
	CIDR string `json:"cidr"`
}

// IPPoolStatus defines the observed state of IPPool
type IPPoolStatus struct{}

// IPPool is the Schema for the ippools API
// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type IPPool struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IPPoolSpec   `json:"spec,omitempty"`
	Status IPPoolStatus `json:"status,omitempty"`
}

// IPPoolList contains a list of IPPool
// +kubebuilder:object:root=true
type IPPoolList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []IPPool `json:"items"`
}

func init() {
	SchemeBuilder.Register(&IPPool{}, &IPPoolList{})
}
