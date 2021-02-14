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

	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

const ipamCacheFinalizer = "ipam.routerd.net/ipam-cache"

type IPAMCache struct {
	ipams    map[types.UID]goipam.Ipamer
	ipamsMux sync.RWMutex
}

type ipamCache interface {
	GetOrCreate(
		ctx context.Context, ippool *ipamv1alpha1.IPPool,
		create func(ctx context.Context, ippool *ipamv1alpha1.IPPool) (goipam.Ipamer, error),
	) (goipam.Ipamer, error)
	Free(ippool *ipamv1alpha1.IPPool)
	Get(ippool *ipamv1alpha1.IPPool) (goipam.Ipamer, bool)
}

func NewIPAMCache() *IPAMCache {
	return &IPAMCache{
		ipams: map[types.UID]goipam.Ipamer{},
	}
}

func (i *IPAMCache) GetOrCreate(
	ctx context.Context, ippool *ipamv1alpha1.IPPool,
	create func(ctx context.Context, ippool *ipamv1alpha1.IPPool) (goipam.Ipamer, error),
) (goipam.Ipamer, error) {
	i.ipamsMux.Lock()
	defer i.ipamsMux.Unlock()

	if ipam, ok := i.ipams[ippool.UID]; ok {
		return ipam, nil
	}

	ipam, err := create(ctx, ippool)
	if err != nil {
		return nil, err
	}
	i.ipams[ippool.UID] = ipam
	return ipam, nil
}

func (i *IPAMCache) Get(ippool *ipamv1alpha1.IPPool) (goipam.Ipamer, bool) {
	i.ipamsMux.RLock()
	defer i.ipamsMux.RUnlock()
	ipam, ok := i.ipams[ippool.UID]
	return ipam, ok
}

func (i *IPAMCache) Free(ippool *ipamv1alpha1.IPPool) {
	i.ipamsMux.Lock()
	defer i.ipamsMux.Unlock()

	delete(i.ipams, ippool.UID)
}
