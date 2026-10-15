/*
Copyright 2026 The KubeVela Authors.

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

package expressions

import (
	"bytes"
	"context"
	"encoding/json"

	"github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/cel"
	"k8s.io/apimachinery/pkg/runtime"

	wfv1alpha1 "github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/types"
)

// MayHoldExpression reports whether a JSON document may hold a $( ) expression:
// it holds the marker, as written or with either character as a JSON escape.
func MayHoldExpression(raw []byte) bool {
	return bytes.Contains(raw, []byte("$(")) || bytes.Contains(raw, []byte(`\u0024`)) ||
		bytes.Contains(raw, []byte(`\u0028`))
}

// Evaluator resolves the expressions in one workflow's step properties.
type Evaluator struct {
	engine *cel.Engine
	steps  workflowSteps
	host   map[string]cel.Resolver
	run    string
	// refused is, by reader as written and then step read, why a read could never
	// be answered, as WithReadDependencies found.
	refused map[string]map[string]string
}

var _ types.PropertyEvaluator = (*Evaluator)(nil)

// NewEvaluator builds the evaluator for a workflow's steps, with the host's roots
// and resolvers beside the workflow's own. run identifies this run of the
// workflow to the host's resolvers (RunFrom).
func NewEvaluator(opts types.ExpressionOptions, steps []v1alpha1.WorkflowStep, run string) (*Evaluator, error) {
	engine, err := NewEngine(opts.Roots...)
	if err != nil {
		return nil, err
	}
	return &Evaluator{engine: engine, steps: newWorkflowSteps(steps), host: opts.Resolvers, run: run}, nil
}

// Properties returns properties with every expression replaced by its value. A
// read that cannot be answered yet returns an error that is cel.ErrNotReady.
func (e *Evaluator) Properties(ctx context.Context, step types.ExpressionStep, properties *runtime.RawExtension, wfCtx wfContext.Context,
	pCtx process.Context, status map[string]wfv1alpha1.StepStatus) (*runtime.RawExtension, error) {
	// "$(" also covers the "$$(" escape, which evaluation unescapes.
	if properties == nil || !MayHoldExpression(properties.Raw) {
		return properties, nil
	}
	plan, err := e.engine.PlanJSON(properties.Raw)
	if err != nil {
		return nil, err
	}
	resolvers := map[string]cel.Resolver{
		RootContext: ContextResolver(pCtx),
		RootSteps:   stepsResolver(e.steps, status, wfCtx, e.steps.readerOf(pCtx), e.refused),
		RootEach:    EachResolver(pCtx),
	}
	for root, r := range e.host {
		resolvers[root] = r
	}
	out, err := plan.Eval(withRun(ctx, RunScope{Run: e.run, Step: step.Name, StepType: step.Type, Store: wfCtx}), resolvers, cel.TreeOptions{})
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	return &runtime.RawExtension{Raw: raw}, nil
}
