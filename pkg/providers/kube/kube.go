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
	_ "embed"
	"encoding/json"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ktypes "k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cuexruntime "github.com/kubevela/pkg/cue/cuex/runtime"
	"github.com/kubevela/pkg/multicluster"
	"github.com/kubevela/pkg/util/k8s"
	"github.com/kubevela/pkg/util/k8s/patch"

	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
)

const (
	// AnnoWorkflowLastAppliedConfig is the annotation for last applied config
	AnnoWorkflowLastAppliedConfig = "workflow.oam.dev/last-applied-configuration"
	// AnnoWorkflowLastAppliedTime is annotation for last applied time
	AnnoWorkflowLastAppliedTime = "workflow.oam.dev/last-applied-time"
)

const (
	// WorkflowResourceCreator is the creator name of workflow resource
	WorkflowResourceCreator string = "workflow"
)

func handleContext(ctx context.Context, cluster string) context.Context {
	return multicluster.WithCluster(ctx, cluster)
}

func apply(ctx context.Context, cli client.Client, _, _ string, workloads ...*unstructured.Unstructured) error {
	for _, workload := range workloads {
		existing := new(unstructured.Unstructured)
		existing.GetObjectKind().SetGroupVersionKind(workload.GetObjectKind().GroupVersionKind())
		if err := cli.Get(ctx, ktypes.NamespacedName{
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
				if err := cli.Create(ctx, workload); err != nil {
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
			if err := cli.Patch(ctx, workload, patcher); err != nil {
				return err
			}
		}
	}
	return nil
}

// nolint:revive
func delete(ctx context.Context, cli client.Client, _, _ string, manifest *unstructured.Unstructured) error {
	return cli.Delete(ctx, manifest)
}

// ListFilter filter for list resources
type ListFilter struct {
	Namespace      string            `json:"namespace,omitempty"`
	MatchingLabels map[string]string `json:"matchingLabels,omitempty"`
}

// ResourceVars .
type ResourceVars struct {
	Resource *unstructured.Unstructured `json:"value"`
	Filter   *ListFilter                `json:"filter,omitempty"`
	Cluster  string                     `json:"cluster,omitempty"`
}

// ResourceReturnVars .
type ResourceReturnVars struct {
	Resource *unstructured.Unstructured `json:"value"`
	Error    string                     `json:"err,omitempty"`
}

// ResourceParams .
type ResourceParams = providertypes.Params[ResourceVars]

// ResourceReturns .
type ResourceReturns = providertypes.Returns[ResourceReturnVars]

func getHandlers(runtimeParams providertypes.RuntimeParams) *providertypes.KubeHandlers {
	if runtimeParams.KubeHandlers != nil {
		return runtimeParams.KubeHandlers
	}
	return &providertypes.KubeHandlers{
		Apply:  apply,
		Delete: delete,
	}
}

// Apply create or update CR in cluster.
func Apply(ctx context.Context, params *ResourceParams) (*ResourceReturns, error) {
	workload := params.Params.Resource
	handlers := getHandlers(params.RuntimeParams)
	if workload.GetNamespace() == "" {
		workload.SetNamespace("default")
	}
	for k, v := range params.RuntimeParams.Labels {
		if err := k8s.AddLabel(workload, k, v); err != nil {
			return nil, err
		}
	}
	deployCtx := handleContext(ctx, params.Params.Cluster)
	if err := handlers.Apply(deployCtx, params.KubeClient, params.Params.Cluster, WorkflowResourceCreator, workload); err != nil {
		return nil, err
	}
	return &ResourceReturns{
		Returns: ResourceReturnVars{
			Resource: workload,
		},
	}, nil
}

// ApplyInParallelVars .
type ApplyInParallelVars struct {
	Resources []*unstructured.Unstructured `json:"value"`
	Cluster   string                       `json:"cluster,omitempty"`
}

// ApplyInParallelReturnVars .
type ApplyInParallelReturnVars struct {
	Resource []*unstructured.Unstructured `json:"value"`
}

// ApplyInParallelParams .
type ApplyInParallelParams = providertypes.Params[ApplyInParallelVars]

// ApplyInParallelReturns .
type ApplyInParallelReturns = providertypes.Returns[ApplyInParallelReturnVars]

// ApplyInParallel create or update CRs in parallel.
func ApplyInParallel(ctx context.Context, params *ApplyInParallelParams) (*ApplyInParallelReturns, error) {
	workloads := params.Params.Resources
	handlers := getHandlers(params.RuntimeParams)
	for i := range workloads {
		if workloads[i].GetNamespace() == "" {
			workloads[i].SetNamespace("default")
		}
	}
	deployCtx := handleContext(ctx, params.Params.Cluster)
	if err := handlers.Apply(deployCtx, params.KubeClient, params.Params.Cluster, WorkflowResourceCreator, workloads...); err != nil {
		return nil, err
	}
	return &ApplyInParallelReturns{
		Returns: ApplyInParallelReturnVars{
			Resource: workloads,
		},
	}, nil
}

// Patch patch CR in cluster.
func Patch(ctx context.Context, params *providertypes.Params[cue.Value]) (cue.Value, error) {
	handlers := getHandlers(params.RuntimeParams)
	val := params.Params.LookupPath(cue.ParsePath("value"))
	obj := new(unstructured.Unstructured)
	b, err := val.MarshalJSON()
	if err != nil {
		return cue.Value{}, err
	}
	if err := json.Unmarshal(b, obj); err != nil {
		return cue.Value{}, err
	}
	key := client.ObjectKeyFromObject(obj)
	if key.Namespace == "" {
		key.Namespace = "default"
	}
	cluster, err := params.Params.LookupPath(cue.ParsePath("cluster")).String()
	if err != nil {
		return cue.Value{}, err
	}
	multiCtx := handleContext(ctx, cluster)
	if err := params.KubeClient.Get(multiCtx, key, obj); err != nil {
		return cue.Value{}, err
	}
	baseVal := cuecontext.New().CompileString("").FillPath(cue.ParsePath(""), obj)
	patcher := params.Params.LookupPath(cue.ParsePath("patch"))

	base, err := model.NewBase(baseVal)
	if err != nil {
		return cue.Value{}, err
	}
	if err := base.Unify(patcher); err != nil {
		return cue.Value{}, err
	}
	workload, err := base.Unstructured()
	if err != nil {
		return cue.Value{}, err
	}
	for k, v := range params.RuntimeParams.Labels {
		if err := k8s.AddLabel(workload, k, v); err != nil {
			return cue.Value{}, err
		}
	}
	if err := handlers.Apply(multiCtx, params.KubeClient, cluster, WorkflowResourceCreator, workload); err != nil {
		return cue.Value{}, err
	}
	return params.Params.FillPath(value.FieldPath("$returns", "result"), workload), nil
}

// Read get CR from cluster.
func Read(ctx context.Context, params *ResourceParams) (*ResourceReturns, error) {
	workload := params.Params.Resource
	key := client.ObjectKeyFromObject(workload)
	if key.Namespace == "" {
		key.Namespace = "default"
	}
	readCtx := handleContext(ctx, params.Params.Cluster)
	if err := params.KubeClient.Get(readCtx, key, workload); err != nil {
		return &ResourceReturns{
			Returns: ResourceReturnVars{
				Resource: workload,
				Error:    err.Error(),
			},
		}, nil
	}
	return &ResourceReturns{
		Returns: ResourceReturnVars{
			Resource: workload,
		},
	}, nil
}

// ListReturnVars .
type ListReturnVars struct {
	Resources *unstructured.UnstructuredList `json:"values"`
	Error     string                         `json:"err,omitempty"`
}

// ListReturns .
type ListReturns = providertypes.Returns[ListReturnVars]

// List lists CRs from cluster.
func List(ctx context.Context, params *ResourceParams) (*ListReturns, error) {
	workload := params.Params.Resource
	list := &unstructured.UnstructuredList{Object: map[string]interface{}{
		"kind":       workload.GetKind(),
		"apiVersion": workload.GetAPIVersion(),
	}}

	filter := params.Params.Filter
	listOpts := []client.ListOption{
		client.InNamespace(filter.Namespace),
		client.MatchingLabels(filter.MatchingLabels),
	}
	readCtx := handleContext(ctx, params.Params.Cluster)
	if err := params.KubeClient.List(readCtx, list, listOpts...); err != nil {
		return &ListReturns{
			Returns: ListReturnVars{
				Resources: list,
				Error:     err.Error(),
			},
		}, nil
	}
	return &ListReturns{
		Returns: ListReturnVars{
			Resources: list,
		},
	}, nil
}

// Delete deletes CR from cluster.
func Delete(ctx context.Context, params *ResourceParams) (*ResourceReturns, error) {
	workload := params.Params.Resource
	handlers := getHandlers(params.RuntimeParams)
	deleteCtx := handleContext(ctx, params.Params.Cluster)

	if filter := params.Params.Filter; filter != nil {
		labelSelector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{MatchLabels: filter.MatchingLabels})
		if err != nil {
			return nil, err
		}
		if err := params.KubeClient.DeleteAllOf(deleteCtx, workload, &client.DeleteAllOfOptions{ListOptions: client.ListOptions{Namespace: filter.Namespace, LabelSelector: labelSelector}}); err != nil {
			return &ResourceReturns{
				Returns: ResourceReturnVars{
					Resource: workload,
					Error:    err.Error(),
				},
			}, nil
		}
		return nil, nil
	}

	if err := handlers.Delete(deleteCtx, params.KubeClient, params.Params.Cluster, WorkflowResourceCreator, workload); err != nil {
		return &ResourceReturns{
			Returns: ResourceReturnVars{
				Resource: workload,
				Error:    err.Error(),
			},
		}, nil
	}

	return nil, nil
}

//go:embed kube.cue
var template string

// GetTemplate get kube template.
func GetTemplate() string {
	return template
}

// GetProviders get kube providers.
func GetProviders() map[string]cuexruntime.ProviderFn {
	return map[string]cuexruntime.ProviderFn{
		"apply":             providertypes.GenericProviderFn[ResourceVars, ResourceReturns](Apply),
		"apply-in-parallel": providertypes.GenericProviderFn[ApplyInParallelVars, ApplyInParallelReturns](ApplyInParallel),
		"read":              providertypes.GenericProviderFn[ResourceVars, ResourceReturns](Read),
		"list":              providertypes.GenericProviderFn[ResourceVars, ListReturns](List),
		"delete":            providertypes.GenericProviderFn[ResourceVars, ResourceReturns](Delete),
		"patch":             providertypes.NativeProviderFn(Patch),
	}
}
