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
	"sync"

	goipam "github.com/metal-stack/go-ipam"
	"k8s.io/apimachinery/pkg/types"

	"routerd.net/kube-ipam/internal/controllers/adapter"
)

const ipamCacheFinalizer = "ipam.routerd.net/ipam-cache"

type ipamCache interface {
	GetOrCreate(
		ctx context.Context, ippool adapter.IPPool,
		create ipamCreateFn,
	) (Ipamer, error)
	Free(ippool adapter.IPPool)
	Get(ippool adapter.IPPool) (Ipamer, bool)
}

type Ipamer interface {
	NewPrefix(cidr string) (*goipam.Prefix, error)
	PrefixFrom(cidr string) *goipam.Prefix
	AcquireIP(cidr string) (*goipam.IP, error)
	AcquireSpecificIP(cidr, ip string) (*goipam.IP, error)
	ReleaseIPFromPrefix(cid, ip string) error
	ReleaseIP(*goipam.IP) (*goipam.Prefix, error)
}

type ipamCreateFn func(
	ctx context.Context, ippool adapter.IPPool,
) (Ipamer, error)

// IPAMCache caches Ipamer instances for IPPool objects.
type IPAMCache struct {
	ipams    map[types.UID]Ipamer
	ipamsMux sync.RWMutex
}

var _ ipamCache = (*IPAMCache)(nil)

func NewIPAMCache() *IPAMCache {
	return &IPAMCache{
		ipams: map[types.UID]Ipamer{},
	}
}

// GetOrCreate returns a cached IPAM instance or
// calls the createFn to create the instance.
func (i *IPAMCache) GetOrCreate(
	ctx context.Context, ippool adapter.IPPool,
	create ipamCreateFn,
) (Ipamer, error) {
	i.ipamsMux.Lock()
	defer i.ipamsMux.Unlock()

	if ipam, ok := i.ipams[ippool.GetUID()]; ok {
		return ipam, nil
	}

	ipam, err := create(ctx, ippool)
	if err != nil {
		return nil, err
	}
	i.ipams[ippool.GetUID()] = ipam
	return ipam, nil
}

// Get returns a cached IPAM instance if it exists.
func (i *IPAMCache) Get(ippool adapter.IPPool) (Ipamer, bool) {
	i.ipamsMux.RLock()
	defer i.ipamsMux.RUnlock()
	ipam, ok := i.ipams[ippool.GetUID()]
	return ipam, ok
}

// Free removes a cached IPAM instance for the given IPPool.
func (i *IPAMCache) Free(ippool adapter.IPPool) {
	i.ipamsMux.Lock()
	defer i.ipamsMux.Unlock()

	delete(i.ipams, ippool.GetUID())
}
