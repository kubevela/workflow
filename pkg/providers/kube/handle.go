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

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ktypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	monitorContext "github.com/kubevela/pkg/monitor/context"
	"github.com/kubevela/pkg/multicluster"
	"github.com/kubevela/pkg/util/k8s"
	"github.com/kubevela/pkg/util/k8s/patch"

	wfContext "github.com/kubevela/workflow/pkg/context"
	velacue "github.com/kubevela/workflow/pkg/cue"
	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	"github.com/kubevela/workflow/pkg/types"
)

const (
	// ProviderName is provider name for install.
	ProviderName = "kube"
	// AnnoWorkflowLastAppliedConfig is the annotation for last applied config
	AnnoWorkflowLastAppliedConfig = "workflow.oam.dev/last-applied-configuration"
	// AnnoWorkflowLastAppliedTime is annotation for last applied time
	AnnoWorkflowLastAppliedTime = "workflow.oam.dev/last-applied-time"
)

// Dispatcher is a client for apply resources.
type Dispatcher func(ctx context.Context, cluster, owner string, manifests ...*unstructured.Unstructured) error

// Deleter is a client for delete resources.
type Deleter func(ctx context.Context, cluster, owner string, manifest *unstructured.Unstructured) error

// Handlers handles resources.
type Handlers struct {
	Apply  Dispatcher
	Delete Deleter
}

type filters struct {
	Namespace      string            `json:"namespace"`
	MatchingLabels map[string]string `json:"matchingLabels"`
}

type provider struct {
	labels   map[string]string
	handlers Handlers
	cli      client.Client
}

const (
	// WorkflowResourceCreator is the creator name of workflow resource
	WorkflowResourceCreator string = "workflow"
)

func handleContext(ctx context.Context, cluster string) context.Context {
	return multicluster.WithCluster(ctx, cluster)
}

type dispatcher struct {
	cli client.Client
}

func (d *dispatcher) apply(ctx context.Context, cluster, owner string, workloads ...*unstructured.Unstructured) error {
	for _, workload := range workloads {
		existing := new(unstructured.Unstructured)
		existing.GetObjectKind().SetGroupVersionKind(workload.GetObjectKind().GroupVersionKind())
		if err := d.cli.Get(ctx, ktypes.NamespacedName{
			Namespace: workload.GetNamespace(),
			Name:      workload.GetName(),
		}, existing); err != nil {
			if errors.IsNotFound(err) {
				// TODO: make the annotation optional
				b, err := workload.MarshalJSON()
				if err != nil {
					return err
				}
				if err := k8s.AddAnnotation(workload, AnnoWorkflowLastAppliedConfig, string(b)); err != nil {
					return err
				}
				if err := d.cli.Create(ctx, workload); err != nil {
					return err
				}
			} else {
				return err
			}
		} else {
			patcher, err := patch.ThreeWayMergePatch(existing, workload, &patch.PatchAction{
				UpdateAnno:            true,
				AnnoLastAppliedConfig: AnnoWorkflowLastAppliedConfig,
				AnnoLastAppliedTime:   AnnoWorkflowLastAppliedTime,
			})
			if err != nil {
				return err
			}
			if err := d.cli.Patch(ctx, workload, patcher); err != nil {
				return err
			}
		}
	}
	return nil
}

func (d *dispatcher) delete(ctx context.Context, cluster, owner string, manifest *unstructured.Unstructured) error {
	return d.cli.Delete(ctx, manifest)
}

// Patch patch CR in cluster.
func (h *provider) Patch(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error {
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
	multiCtx := handleContext(ctx, cluster)
	if err := h.cli.Get(multiCtx, key, obj); err != nil {
		return err
	}
	baseVal := cuecontext.New().CompileString("").FillPath(cue.ParsePath(""), obj)
	patcher, err := v.LookupValue("patch")
	if err != nil {
		return err
	}

	base, err := model.NewBase(baseVal)
	if err != nil {
		return err
	}
	if err := base.Unify(patcher.CueValue()); err != nil {
		return err
	}
	workload, err := base.Unstructured()
	if err != nil {
		return err
	}
	for k, v := range h.labels {
		if err := k8s.AddLabel(workload, k, v); err != nil {
			return err
		}
	}
	if err := h.handlers.Apply(multiCtx, cluster, WorkflowResourceCreator, workload); err != nil {
		return err
	}
	return velacue.FillUnstructuredObject(v, workload, "result")
}

// Apply create or update CR in cluster.
func (h *provider) Apply(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error {
	val, err := v.LookupValue("value")
	if err != nil {
		return err
	}
	var workload = new(unstructured.Unstructured)
	if err := val.UnmarshalTo(workload); err != nil {
		return err
	}
	if workload.GetNamespace() == "" {
		workload.SetNamespace("default")
	}
	for k, v := range h.labels {
		if err := k8s.AddLabel(workload, k, v); err != nil {
			return err
		}
	}
	cluster, err := v.GetString("cluster")
	if err != nil {
		return err
	}
	deployCtx := handleContext(ctx, cluster)
	if err := h.handlers.Apply(deployCtx, cluster, WorkflowResourceCreator, workload); err != nil {
		return err
	}
	return velacue.FillUnstructuredObject(v, workload, "value")
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
	deployCtx := handleContext(ctx, cluster)
	if err := h.handlers.Apply(deployCtx, cluster, WorkflowResourceCreator, workloads...); err != nil {
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
	readCtx := handleContext(ctx, cluster)
	if err := h.cli.Get(readCtx, key, obj); err != nil {
		return v.FillObject(err.Error(), "err")
	}
	return velacue.FillUnstructuredObject(v, obj, "value")
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
	readCtx := handleContext(ctx, cluster)
	if err := h.cli.List(readCtx, list, listOpts...); err != nil {
		return v.FillObject(err.Error(), "err")
	}
	return velacue.FillUnstructuredObject(v, list, "list")
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
	deleteCtx := handleContext(ctx, cluster)

	if filterValue, err := v.LookupValue("filter"); err == nil {
		filter := &filters{}
		if err := filterValue.UnmarshalTo(filter); err != nil {
			return err
		}
		labelSelector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: filter.MatchingLabels})
		if err != nil {
			return err
		}
		if err := h.cli.DeleteAllOf(deleteCtx, obj, &client.DeleteAllOfOptions{ListOptions: client.ListOptions{Namespace: filter.Namespace, LabelSelector: labelSelector}}); err != nil {
			return v.FillObject(err.Error(), "err")
		}
		return nil
	}

	if err := h.handlers.Delete(deleteCtx, cluster, WorkflowResourceCreator, obj); err != nil {
		return v.FillObject(err.Error(), "err")
	}

	return nil
}

// Install register handlers to provider discover.
func Install(p types.Providers, cli client.Client, labels map[string]string, handlers *Handlers) {
	if handlers == nil {
		d := &dispatcher{
			cli: cli,
		}
		handlers = &Handlers{
			Apply:  d.apply,
			Delete: d.delete,
		}
	}
	prd := &provider{
		cli:      cli,
		handlers: *handlers,
		labels:   labels,
	}
	p.Register(ProviderName, map[string]types.Handler{
		"apply":             prd.Apply,
		"apply-in-parallel": prd.ApplyInParallel,
		"read":              prd.Read,
		"list":              prd.List,
		"delete":            prd.Delete,
		"patch":             prd.Patch,
	})
}
