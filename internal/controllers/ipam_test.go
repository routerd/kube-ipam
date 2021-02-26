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
	"testing"

	goipam "github.com/metal-stack/go-ipam"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
	"routerd.net/kube-ipam/internal/controllers/adapter"
)

func TestIPAMCache(t *testing.T) {
	ippool := adapter.AdaptIPPool(&ipamv1alpha1.IPv4Pool{
		ObjectMeta: metav1.ObjectMeta{
			UID: types.UID("1234"),
		},
	})

	cache := NewIPAMCache()

	// initial get
	ipam, ok := cache.Get(ippool)
	assert.False(t, ok)
	assert.Nil(t, ipam)

	ctx := context.Background()

	// create ipam object
	var createCalled bool
	ipam, err := cache.GetOrCreate(
		ctx, ippool,
		func(ctx context.Context, ippool adapter.IPPool) (Ipamer, error) {
			createCalled = true
			return nil, nil
		})
	require.NoError(t, err)
	assert.True(t, createCalled, "createFn should have been called")
	assert.Nil(t, ipam)

	// Create again is not calling createFN
	var createCalledAgain bool
	ipam, err = cache.GetOrCreate(
		ctx, ippool,
		func(ctx context.Context, ippool adapter.IPPool) (Ipamer, error) {
			createCalledAgain = true
			return nil, nil
		})
	require.NoError(t, err)
	assert.False(t, createCalledAgain, "createFn should NOT have been called")
	assert.Nil(t, ipam)

	// new get
	ipam, ok = cache.Get(ippool)
	assert.True(t, ok)
	assert.Nil(t, ipam)

	cache.Free(ippool)

	// get after free
	ipam, ok = cache.Get(ippool)
	assert.False(t, ok)
	assert.Nil(t, ipam)
}

type ipamCacheMock struct {
	mock.Mock
}

func (m *ipamCacheMock) GetOrCreate(
	ctx context.Context, ippool adapter.IPPool,
	create ipamCreateFn,
) (Ipamer, error) {
	args := m.Called(ctx, ippool, create)
	ipam, _ := args.Get(0).(Ipamer)
	err, _ := args.Error(1).(error)
	return ipam, err
}

func (m *ipamCacheMock) Free(ippool adapter.IPPool) {
	m.Called(ippool)
}

func (m *ipamCacheMock) Get(ippool adapter.IPPool) (Ipamer, bool) {
	args := m.Called(ippool)
	ipam, _ := args.Get(0).(Ipamer)
	return ipam, args.Bool(1)
}

type ipamMock struct {
	mock.Mock
}

func (m *ipamMock) NewPrefix(cidr string) (*goipam.Prefix, error) {
	args := m.Called(cidr)
	err, _ := args.Error(1).(error)
	return args.Get(0).(*goipam.Prefix), err
}

func (m *ipamMock) PrefixFrom(cidr string) *goipam.Prefix {
	args := m.Called(cidr)
	return args.Get(0).(*goipam.Prefix)
}

func (m *ipamMock) AcquireIP(cidr string) (*goipam.IP, error) {
	args := m.Called(cidr)
	err, _ := args.Error(1).(error)
	return args.Get(0).(*goipam.IP), err
}

func (m *ipamMock) AcquireSpecificIP(cidr, ip string) (*goipam.IP, error) {
	args := m.Called(cidr, ip)
	err, _ := args.Error(1).(error)
	return args.Get(0).(*goipam.IP), err

}

func (m *ipamMock) ReleaseIPFromPrefix(cidr, ip string) error {
	args := m.Called(cidr, ip)
	err, _ := args.Error(0).(error)
	return err
}

func (m *ipamMock) ReleaseIP(ip *goipam.IP) (*goipam.Prefix, error) {
	args := m.Called(ip)
	err, _ := args.Error(1).(error)
	return args.Get(0).(*goipam.Prefix), err
}
