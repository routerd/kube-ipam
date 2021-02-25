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

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IPLeaseSpec defines the desired state of IPLease
type IPLeaseSpec struct {
	// References the pool to lease an IP from.
	Pool LocalObjectReference `json:"pool"`
	// Static IP lease settings.
	Static *StaticIPLease `json:"static,omitempty"`
	// IPFamilies that this lease should include.
	IPFamilies []corev1.IPFamily `json:"ipFamilies"`
	// Renew time is the time when the lease holder has last updated the lease.
	// Falls back to .metadata.creationTimestamp if not set.
	RenewTime metav1.MicroTime `json:"renewTime,omitempty"`
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
	// Duration of the lease, if empty lease does not expire.
	LeaseDuration *metav1.Duration `json:"leaseDuration,omitempty"`
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
// +kubebuilder:printcolumn:name="Renew",type="date",JSONPath=".spec.renewTime"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type IPLease struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   IPLeaseSpec   `json:"spec,omitempty"`
	Status IPLeaseStatus `json:"status,omitempty"`
}

func (lease *IPLease) HasExpired() bool {
	if lease.Status.LeaseDuration == nil {
		// no lease duration
		// -> can't expire
		return false
	}

	now := time.Now().UTC()
	// default RenewTime to creation time.
	// TODO: move this into a Defaulting Webhook.
	renewTime := lease.CreationTimestamp.Time
	if !lease.Spec.RenewTime.IsZero() {
		renewTime = lease.Spec.RenewTime.Time
	}

	return renewTime.UTC().Add(lease.Status.LeaseDuration.Duration).Before(now)
}

func (lease *IPLease) HasIPv4() bool {
	for _, ipFamily := range lease.Spec.IPFamilies {
		if ipFamily == corev1.IPv4Protocol {
			return true
		}
	}
	return false
}

func (lease *IPLease) HasIPv6() bool {
	for _, ipFamily := range lease.Spec.IPFamilies {
		if ipFamily == corev1.IPv6Protocol {
			return true
		}
	}
	return false
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
