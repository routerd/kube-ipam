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

// References an object in the local namespace.
type LocalObjectReference struct {
	Name string `json:"name"`
}

// Common IPLease Condition Types
const (
	IPLeaseBound = "Bound"
)

type IPLeaseType string

const (
	// Static IP Address allocation.
	// The Lease will only successfully bind if the requested IPs are not yet occupied.
	IPLeaseTypeStatic = "Static"
	// Dynamic IP Address allocation.
	// Acquire any free IP address from the selected pool.
	IPLeaseTypeDynamic = "Dynamic"
)

type IPLeaseStatic struct {
	// Static address to lease.
	Address string `json:"address"`
}
