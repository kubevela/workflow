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

package client

import (
	"context"
	"reflect"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/workflow/pkg/monitor/metrics"
)

func monitor(verb string, obj runtime.Object) func() {
	o := obj.GetObjectKind().GroupVersionKind()
	_, isUnstructured := obj.(*unstructured.Unstructured)
	_, isUnstructuredList := obj.(*unstructured.UnstructuredList)
	un := "structured"
	if isUnstructured || isUnstructuredList {
		un = "unstructured"
	}
	kind := o.Kind
	if kind == "" {
		if t := reflect.TypeOf(obj); t.Kind() == reflect.Ptr {
			kind = t.Elem().Name()
		} else {
			kind = t.Name()
		}
	}
	kind = strings.TrimSuffix(kind, "List")
	begin := time.Now()
	return func() {
		v := time.Since(begin).Seconds()
		metrics.ClientRequestHistogram.WithLabelValues(verb, kind, o.GroupVersion().String(), un).Observe(v)
	}
}

type monitorCache struct {
	cache.Cache
}

func (c *monitorCache) Get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	cb := monitor("GetCache", obj)
	defer cb()
	return c.Cache.Get(ctx, key, obj)
}

func (c *monitorCache) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	cb := monitor("ListCache", list)
	defer cb()
	return c.Cache.List(ctx, list, opts...)
}

type monitorClient struct {
	client.Client
}

func (c *monitorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object) error {
	cb := monitor("Get", obj)
	defer cb()
	return c.Client.Get(ctx, key, obj)
}

func (c *monitorClient) List(ctx context.Context, list client.ObjectList, opts ...client.ListOption) error {
	cb := monitor("List", list)
	defer cb()
	return c.Client.List(ctx, list, opts...)
}

func (c *monitorClient) Create(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
	cb := monitor("Create", obj)
	defer cb()
	return c.Client.Create(ctx, obj, opts...)
}

func (c *monitorClient) Delete(ctx context.Context, obj client.Object, opts ...client.DeleteOption) error {
	cb := monitor("Delete", obj)
	defer cb()
	return c.Client.Delete(ctx, obj, opts...)
}

func (c *monitorClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	cb := monitor("Update", obj)
	defer cb()
	return c.Client.Update(ctx, obj, opts...)
}

func (c *monitorClient) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	cb := monitor("Patch", obj)
	defer cb()
	return c.Client.Patch(ctx, obj, patch, opts...)
}

func (c *monitorClient) DeleteAllOf(ctx context.Context, obj client.Object, opts ...client.DeleteAllOfOption) error {
	cb := monitor("DeleteAllOf", obj)
	defer cb()
	return c.Client.DeleteAllOf(ctx, obj, opts...)
}

func (c *monitorClient) Status() client.StatusWriter {
	return &monitorStatusWriter{c.Client.Status()}
}

type monitorStatusWriter struct {
	client.StatusWriter
}

func (w *monitorStatusWriter) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	cb := monitor("StatusUpdate", obj)
	defer cb()
	return w.StatusWriter.Update(ctx, obj, opts...)
}

func (w *monitorStatusWriter) Patch(ctx context.Context, obj client.Object, patch client.Patch, opts ...client.PatchOption) error {
	cb := monitor("StatusPatch", obj)
	defer cb()
	return w.StatusWriter.Patch(ctx, obj, patch, opts...)
}
