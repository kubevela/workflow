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

package types

import (
	"context"
	"encoding/json"

	"cuelang.org/go/cue"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/pkg/util/singleton"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/types"
)

// ContextKey is the key type for context values.
type ContextKey string

const (
	// WorkflowContextKey is the key for workflow context.
	WorkflowContextKey ContextKey = "workflowContext"
	// ProcessContextKey is the key for process context.
	ProcessContextKey ContextKey = "processContext"
	// ActionKey is the key for action.
	ActionKey ContextKey = "action"
	// LabelsKey is the key for labels.
	LabelsKey ContextKey = "labels"
	// KubeHandlersKey is the key for kube handlers.
	KubeHandlersKey ContextKey = "kubeHandlers"
	// KubeClientKey is the key for kube client.
	KubeClientKey ContextKey = "kubeClient"
)

// Dispatcher is a client for apply resources.
type Dispatcher func(ctx context.Context, client client.Client, cluster, owner string, manifests ...*unstructured.Unstructured) error

// Deleter is a client for delete resources.
type Deleter func(ctx context.Context, client client.Client, cluster, owner string, manifest *unstructured.Unstructured) error

// KubeHandlers handles resources.
type KubeHandlers struct {
	Apply  Dispatcher
	Delete Deleter
}

// RuntimeParams is the runtime parameters of a provider.
type RuntimeParams struct {
	WorkflowContext wfContext.Context
	ProcessContext  process.Context
	Action          types.Action
	FieldLabel      string
	Labels          map[string]string
	KubeHandlers    *KubeHandlers
	KubeClient      client.Client
}

type Params[T any] struct {
	Params T `json:"$params"`
	RuntimeParams
}

type Returns[T any] struct {
	Returns T `json:"$returns"`
}

// GenericProviderFn is the provider function
type GenericProviderFn[T any, U any] func(context.Context, *Params[T]) (*U, error)

// Call marshal value into json and decode into underlying function input
// parameters, then fill back the returned output value
func (fn GenericProviderFn[T, U]) Call(ctx context.Context, value cue.Value) (cue.Value, error) {
	type p struct {
		Params T `json:"$params"`
	}
	params := new(p)
	bs, err := value.MarshalJSON()
	if err != nil {
		return value, err
	}
	if err = json.Unmarshal(bs, params); err != nil {
		return value, err
	}
	runtimeParams := RuntimeParamsFrom(ctx)
	label, _ := value.Label()
	runtimeParams.FieldLabel = label
	ret, err := fn(ctx, &Params[T]{Params: params.Params, RuntimeParams: runtimeParams})
	if err != nil {
		return value, err
	}
	return value.FillPath(cue.ParsePath(""), ret), nil
}

// LegacyParams is the legacy input parameters of a provider.
type LegacyParams[T any] struct {
	Params T
	RuntimeParams
}

// LegacyGenericProviderFn is the legacy provider function
type LegacyGenericProviderFn[T any, U any] func(context.Context, *LegacyParams[T]) (*U, error)

// Call marshal value into json and decode into underlying function input
// parameters, then fill back the returned output value
func (fn LegacyGenericProviderFn[T, U]) Call(ctx context.Context, value cue.Value) (cue.Value, error) {
	params := new(T)
	bs, err := value.MarshalJSON()
	if err != nil {
		return value, err
	}
	if err = json.Unmarshal(bs, params); err != nil {
		return value, err
	}
	runtimeParams := RuntimeParamsFrom(ctx)
	label, _ := value.Label()
	runtimeParams.FieldLabel = label
	ret, err := fn(ctx, &LegacyParams[T]{Params: *params, RuntimeParams: runtimeParams})
	if err != nil {
		return value, err
	}
	return value.FillPath(cue.ParsePath(""), ret), nil
}

// NativeProviderFn is the legacy native provider function
type NativeProviderFn func(context.Context, *Params[cue.Value]) (cue.Value, error)

// Call marshal value into json and decode into underlying function input
// parameters, then fill back the returned output value
func (fn NativeProviderFn) Call(ctx context.Context, value cue.Value) (cue.Value, error) {
	runtimeParams := RuntimeParamsFrom(ctx)
	return fn(ctx, &Params[cue.Value]{Params: value, RuntimeParams: runtimeParams})
}

// LegacyNativeProviderFn is the legacy native provider function
type LegacyNativeProviderFn func(context.Context, *LegacyParams[cue.Value]) (cue.Value, error)

// Call marshal value into json and decode into underlying function input
// parameters, then fill back the returned output value
func (fn LegacyNativeProviderFn) Call(ctx context.Context, value cue.Value) (cue.Value, error) {
	runtimeParams := RuntimeParamsFrom(ctx)
	return fn(ctx, &LegacyParams[cue.Value]{Params: value, RuntimeParams: runtimeParams})
}

// WithLabelParams returns a copy of parent in which the labels value is set
func WithLabelParams(parent context.Context, labels map[string]string) context.Context {
	return context.WithValue(parent, LabelsKey, labels)
}

// WithRuntimeParams returns a copy of parent in which the runtime params value is set
func WithRuntimeParams(parent context.Context, params RuntimeParams) context.Context {
	ctx := context.WithValue(parent, WorkflowContextKey, params.WorkflowContext)
	ctx = context.WithValue(ctx, ProcessContextKey, params.ProcessContext)
	ctx = context.WithValue(ctx, ActionKey, params.Action)
	return ctx
}

// RuntimeParamsFrom returns the runtime params value stored in ctx, if any.
func RuntimeParamsFrom(ctx context.Context) RuntimeParams {
	params := RuntimeParams{}
	if wfCtx, ok := ctx.Value(WorkflowContextKey).(wfContext.Context); ok {
		params.WorkflowContext = wfCtx
	}
	if pCtx, ok := ctx.Value(ProcessContextKey).(process.Context); ok {
		params.ProcessContext = pCtx
	}
	if action, ok := ctx.Value(ActionKey).(types.Action); ok {
		params.Action = action
	}
	if labels, ok := ctx.Value(LabelsKey).(map[string]string); ok {
		params.Labels = labels
	}
	if kubeHandlers, ok := ctx.Value(KubeHandlersKey).(*KubeHandlers); ok {
		params.KubeHandlers = kubeHandlers
	}
	if kubeClient, ok := ctx.Value(KubeClientKey).(client.Client); ok {
		params.KubeClient = kubeClient
	} else {
		params.KubeClient = singleton.KubeClient.Get()
	}
	return params
}
