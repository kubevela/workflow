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

	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/types"
)

type contextKey string

const (
	workflowContextKey contextKey = "workflowContext"
	processContextKey  contextKey = "processContext"
	actionKey          contextKey = "action"
	labelsKey          contextKey = "labels"
)

// LegacyGenericProviderFn is the legacy provider function
type LegacyGenericProviderFn[T any, U any] func(context.Context, *types.LegacyParams[T]) (*U, error)

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
	ret, err := fn(ctx, &types.LegacyParams[T]{Params: *params, RuntimeParams: runtimeParams})
	if err != nil {
		return value, err
	}
	return value.FillPath(cue.ParsePath(""), ret), nil
}

// LegacyNativeProviderFn is the legacy native provider function
type LegacyNativeProviderFn func(context.Context, *types.LegacyParams[cue.Value]) (cue.Value, error)

// Call marshal value into json and decode into underlying function input
// parameters, then fill back the returned output value
func (fn LegacyNativeProviderFn) Call(ctx context.Context, value cue.Value) (cue.Value, error) {
	runtimeParams := RuntimeParamsFrom(ctx)
	return fn(ctx, &types.LegacyParams[cue.Value]{Params: value, RuntimeParams: runtimeParams})
}

// WithLabelParams returns a copy of parent in which the labels value is set
func WithLabelParams(parent context.Context, labels map[string]string) context.Context {
	return context.WithValue(parent, labelsKey, labels)
}

// WithRuntimeParams returns a copy of parent in which the runtime params value is set
func WithRuntimeParams(parent context.Context, params types.RuntimeParams) context.Context {
	ctx := context.WithValue(parent, workflowContextKey, params.WorkflowContext)
	ctx = context.WithValue(ctx, processContextKey, params.ProcessContext)
	ctx = context.WithValue(ctx, actionKey, params.Action)
	return ctx
}

// RuntimeParamsFrom returns the runtime params value stored in ctx, if any.
func RuntimeParamsFrom(ctx context.Context) types.RuntimeParams {
	params := types.RuntimeParams{}
	if wfCtx, ok := ctx.Value(workflowContextKey).(wfContext.Context); ok {
		params.WorkflowContext = wfCtx
	}
	if pCtx, ok := ctx.Value(processContextKey).(process.Context); ok {
		params.ProcessContext = pCtx
	}
	if action, ok := ctx.Value(actionKey).(types.Action); ok {
		params.Action = action
	}
	if labels, ok := ctx.Value(labelsKey).(map[string]string); ok {
		params.Labels = labels
	}
	return params
}

// Dispatcher is a client for apply resources.
type Dispatcher func(ctx context.Context, cluster, owner string, manifests ...*unstructured.Unstructured) error

// Deleter is a client for delete resources.
type Deleter func(ctx context.Context, cluster, owner string, manifest *unstructured.Unstructured) error

// KubeHandlers handles resources.
type KubeHandlers struct {
	Apply  Dispatcher
	Delete Deleter
}
