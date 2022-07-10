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

package kube

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ktypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/authentication/user"
	"k8s.io/apiserver/pkg/endpoints/request"
	"sigs.k8s.io/controller-runtime/pkg/client"

	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue"
	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	monitorContext "github.com/kubevela/workflow/pkg/monitor/context"
	"github.com/kubevela/workflow/pkg/types"
)

const (
	// ProviderName is provider name for install.
	ProviderName = "kube"
)

// Dispatcher is a client for apply resources.
type Dispatcher func(ctx context.Context, cluster string, manifests ...*unstructured.Unstructured) error

// Deleter is a client for delete resources.
type Deleter func(ctx context.Context, cluster string, manifest *unstructured.Unstructured) error

type ContextHandler func(ctx context.Context, cluster string, userInfo user.Info) context.Context

type Handlers struct {
	Apply          Dispatcher
	Delete         Deleter
	ContextHandler ContextHandler
}

type provider struct {
	userInfo user.Info
	handlers Handlers
	cli      client.Client
}

type contextKey string

const (
	// ClusterContextKey is the name of cluster using in client http context
	ClusterContextKey = contextKey("ClusterName")
)

func contextHandler(ctx context.Context, cluster string, userInfo user.Info) context.Context {
	c := context.WithValue(ctx, ClusterContextKey, cluster)
	if userInfo != nil {
		c = request.WithUser(c, userInfo)
	}
	return c
}

type dispatcher struct {
	cli    client.Client
	owners []metav1.OwnerReference
}

func (d *dispatcher) apply(ctx context.Context, cluster string, workloads ...*unstructured.Unstructured) error {
	for _, workload := range workloads {
		existing := new(unstructured.Unstructured)
		existing.GetObjectKind().SetGroupVersionKind(workload.GetObjectKind().GroupVersionKind())
		if err := d.cli.Get(ctx, ktypes.NamespacedName{
			Namespace: workload.GetNamespace(),
			Name:      workload.GetName(),
		}, existing); err != nil {
			if errors.IsNotFound(err) {
				workload.SetOwnerReferences(d.owners)
				if err := d.cli.Create(ctx, workload); err != nil {
					return err
				}
			} else {
				return err
			}
		} else {
			if err := d.cli.Patch(ctx, workload, client.Merge); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *dispatcher) delete(ctx context.Context, cluster string, manifest *unstructured.Unstructured) error {
	return d.cli.Delete(ctx, manifest)
}

// Apply create or update CR in cluster.
func (h *provider) Apply(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error {
	val, err := v.LookupValue("value")
	if err != nil {
		return err
	}
	var workload = new(unstructured.Unstructured)
	pv, err := v.Field("patch")
	if pv.Exists() && err == nil {
		base, err := model.NewBase(val.CueValue())
		if err != nil {
			return err
		}

		patcher, err := model.NewOther(pv)
		if err != nil {
			return err
		}
		if err := base.Unify(patcher); err != nil {
			return err
		}
		workload, err = base.Unstructured()
		if err != nil {
			return err
		}
	} else if err := val.UnmarshalTo(workload); err != nil {
		return err
	}
	if workload.GetNamespace() == "" {
		workload.SetNamespace("default")
	}
	cluster, err := v.GetString("cluster")
	if err != nil {
		return err
	}
	deployCtx := h.handlers.ContextHandler(ctx, cluster, h.userInfo)
	if err := h.handlers.Apply(deployCtx, cluster, workload); err != nil {
		return err
	}
	return cue.FillUnstructuredObject(v, workload, "value")
}

// ApplyInParallel create or update CRs in parallel.
func (h *provider) ApplyInParallel(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error {
	val, err := v.LookupValue("value")
	if err != nil {
		return err
	}
	iter, err := val.CueValue().List()
	if err != nil {
		return err
	}
	workloadNum := 0
	for iter.Next() {
		workloadNum++
	}
	var workloads = make([]*unstructured.Unstructured, workloadNum)
	if err = val.UnmarshalTo(&workloads); err != nil {
		return err
	}
	for i := range workloads {
		if workloads[i].GetNamespace() == "" {
			workloads[i].SetNamespace("default")
		}
	}
	cluster, err := v.GetString("cluster")
	if err != nil {
		return err
	}
	deployCtx := h.handlers.ContextHandler(ctx, cluster, h.userInfo)
	if err := h.handlers.Apply(deployCtx, cluster, workloads...); err != nil {
		return err
	}
	return nil
}

// Read get CR from cluster.
func (h *provider) Read(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error {
	val, err := v.LookupValue("value")
	if err != nil {
		return err
	}
	obj := new(unstructured.Unstructured)
	if err := val.UnmarshalTo(obj); err != nil {
		return err
	}
	key := client.ObjectKeyFromObject(obj)
	if key.Namespace == "" {
		key.Namespace = "default"
	}
	cluster, err := v.GetString("cluster")
	if err != nil {
		return err
	}
	readCtx := h.handlers.ContextHandler(ctx, cluster, h.userInfo)
	if err := h.cli.Get(readCtx, key, obj); err != nil {
		return v.FillObject(err.Error(), "err")
	}
	return cue.FillUnstructuredObject(v, obj, "value")
}

// List lists CRs from cluster.
func (h *provider) List(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error {
	r, err := v.LookupValue("resource")
	if err != nil {
		return err
	}
	resource := &metav1.TypeMeta{}
	if err := r.UnmarshalTo(resource); err != nil {
		return err
	}
	list := &unstructured.UnstructuredList{Object: map[string]interface{}{
		"kind":       resource.Kind,
		"apiVersion": resource.APIVersion,
	}}

	type filters struct {
		Namespace      string            `json:"namespace"`
		MatchingLabels map[string]string `json:"matchingLabels"`
	}
	filterValue, err := v.LookupValue("filter")
	if err != nil {
		return err
	}
	filter := &filters{}
	if err := filterValue.UnmarshalTo(filter); err != nil {
		return err
	}
	cluster, err := v.GetString("cluster")
	if err != nil {
		return err
	}
	listOpts := []client.ListOption{
		client.InNamespace(filter.Namespace),
		client.MatchingLabels(filter.MatchingLabels),
	}
	readCtx := h.handlers.ContextHandler(ctx, cluster, h.userInfo)
	if err := h.cli.List(readCtx, list, listOpts...); err != nil {
		return v.FillObject(err.Error(), "err")
	}
	return cue.FillUnstructuredObject(v, list, "list")
}

// Delete deletes CR from cluster.
func (h *provider) Delete(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error {
	val, err := v.LookupValue("value")
	if err != nil {
		return err
	}
	obj := new(unstructured.Unstructured)
	if err := val.UnmarshalTo(obj); err != nil {
		return err
	}
	cluster, err := v.GetString("cluster")
	if err != nil {
		return err
	}
	deleteCtx := h.handlers.ContextHandler(ctx, cluster, h.userInfo)
	if err := h.handlers.Delete(deleteCtx, cluster, obj); err != nil {
		return v.FillObject(err.Error(), "err")
	}
	return nil
}

// Install register handlers to provider discover.
func Install(p types.Providers, cli client.Client, userInfo user.Info, owners []metav1.OwnerReference, handlers *Handlers) {
	if handlers == nil {
		d := &dispatcher{
			cli:    cli,
			owners: owners,
		}
		handlers = &Handlers{
			ContextHandler: contextHandler,
			Apply:          d.apply,
			Delete:         d.delete,
		}
	}
	prd := &provider{
		userInfo: userInfo,
		cli:      cli,
		handlers: *handlers,
	}
	p.Register(ProviderName, map[string]types.Handler{
		"apply":             prd.Apply,
		"apply-in-parallel": prd.ApplyInParallel,
		"read":              prd.Read,
		"list":              prd.List,
		"delete":            prd.Delete,
	})
}
