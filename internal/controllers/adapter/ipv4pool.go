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

	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

var _ IPPool = (*IPv4Pool)(nil)

type IPv4Pool struct {
	ipamv1alpha1.IPv4Pool
}

func (pool *IPv4Pool) GetCIDR() string {
	return pool.Spec.CIDR
}

func (pool *IPv4Pool) SetAvailableIPs(available int) {
	pool.Status.AvailableIPs = available
}

func (pool *IPv4Pool) SetAllocatedIPs(allocated int) {
	pool.Status.AllocatedIPs = allocated
}

func (pool *IPv4Pool) GetSpecLeaseDuration() (
	leaseDuration time.Duration, ok bool) {
	if pool.Spec.LeaseDuration == nil {
		return
	}
	return pool.Spec.LeaseDuration.Duration, true
}
