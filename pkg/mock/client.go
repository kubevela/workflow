/*
Copyright 2022 The KubeVela Authors.

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

package mock

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetFn mocks client.Client's Get implementation.
type GetFn func(ctx context.Context, key client.ObjectKey, obj client.Object) error

// CreateFn mocks client.Client's Create implementation.
type CreateFn func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error

// UpdateFn mocks client.Client's Update implementation.
type UpdateFn func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error

// PatchFn mocks client.Client's Patch implementation.
type PatchFn func(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error

// Client is a client.Client test double covering the subset of methods
// (Get/Create/Update/Patch) exercised by this repo's tests. It embeds a nil
// client.Client so it satisfies the full interface at compile time; calling
// an unmocked method panics.
type Client struct {
	client.Client

	MockGet    GetFn
	MockCreate CreateFn
	MockUpdate UpdateFn
	MockPatch  PatchFn
}

// Get calls Client's MockGet function.
func (c *Client) Get(ctx context.Context, key client.ObjectKey, obj client.Object, _ ...client.GetOption) error {
	return c.MockGet(ctx, key, obj)
}

// Create calls Client's MockCreate function.
func (c *Client) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	return c.MockCreate(ctx, obj, opts...)
}

// Update calls Client's MockUpdate function.
func (c *Client) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	return c.MockUpdate(ctx, obj, opts...)
}

// Patch calls Client's MockPatch function.
func (c *Client) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	return c.MockPatch(ctx, obj, patch, opts...)
}
