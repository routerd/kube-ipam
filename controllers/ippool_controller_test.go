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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	ipamv1alpha1 "routerd.net/kube-ipam/api/v1alpha1"
)

func TestIPPoolReconciler_handleDeletion(t *testing.T) {
	c := NewClient()
	ipamCache := &ipamCacheMock{}

	r := &IPPoolReconciler{
		Client:    c,
		IPAMCache: ipamCache,
	}

	ippool := &ipamv1alpha1.IPPool{
		ObjectMeta: metav1.ObjectMeta{
			Finalizers: []string{
				ipamCacheFinalizer,
			},
		},
	}
	c.On("Update", mock.Anything, mock.Anything, mock.Anything).Return(nil)
	ipamCache.On("Free", mock.Anything).Return(nil)

	ctx := context.Background()
	err := r.handleDeletion(ctx, ippool)
	require.NoError(t, err)

	ipamCache.AssertCalled(t, "Free", ippool)
	c.AssertCalled(t, "Update", mock.Anything, ippool, mock.Anything)
}

func TestIPPoolReconciler_ensureCacheFinalizer(t *testing.T) {
	t.Run("adds finalizer if absent", func(t *testing.T) {
		c := NewClient()
		r := &IPPoolReconciler{
			Client: c,
		}

		ippool := &ipamv1alpha1.IPPool{}
		c.On("Update", mock.Anything, ippool, mock.Anything).Return(nil)

		ctx := context.Background()
		err := r.ensureCacheFinalizer(ctx, ippool)
		require.NoError(t, err)

		assert.Equal(t, []string{
			ipamCacheFinalizer,
		}, ippool.Finalizers)
		c.AssertCalled(t, "Update", mock.Anything, ippool, mock.Anything)
	})

	t.Run("doesn't add finalizer if present", func(t *testing.T) {
		c := NewClient()
		r := &IPPoolReconciler{
			Client: c,
		}

		ippool := &ipamv1alpha1.IPPool{
			ObjectMeta: metav1.ObjectMeta{
				Finalizers: []string{
					ipamCacheFinalizer,
				},
			},
		}
		c.On("Update", mock.Anything, ippool, mock.Anything).Return(nil)

		ctx := context.Background()
		err := r.ensureCacheFinalizer(ctx, ippool)
		require.NoError(t, err)

		c.AssertNotCalled(t, "Update", mock.Anything, mock.Anything, mock.Anything)
	})
}
