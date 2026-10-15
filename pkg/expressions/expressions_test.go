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
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/cel"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	wfv1alpha1 "github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/features"
	"github.com/kubevela/workflow/pkg/types"
)

// vars is a workflow context's variables, as outputs leave them.
type vars map[string]string

func (v vars) GetVar(paths ...string) (cue.Value, error) {
	raw, ok := v[strings.Join(paths, ".")]
	if !ok {
		return cue.Value{}, fmt.Errorf("var %s not found", strings.Join(paths, "."))
	}
	return cuecontext.New().CompileString(raw), nil
}

func eval(t *testing.T, props string, resolvers map[string]cel.Resolver) (interface{}, error) {
	t.Helper()
	engine, err := NewEngine()
	require.NoError(t, err)
	plan, err := engine.PlanJSON([]byte(props))
	require.NoError(t, err)
	return plan.Eval(context.Background(), resolvers, cel.TreeOptions{})
}

func steps(t *testing.T) []v1alpha1.WorkflowStep {
	t.Helper()
	return []v1alpha1.WorkflowStep{
		{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "list-tenants", Type: "read-config",
			Outputs: v1alpha1.StepOutputs{{Name: "tenants", ValueFrom: "output.value.tenants"}}}},
		{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "group", Type: "step-group"},
			SubSteps: []v1alpha1.WorkflowStepBase{{Name: "build", Type: "build",
				Outputs: v1alpha1.StepOutputs{{Name: "image", ValueFrom: "output.image"}}}}},
		{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "notify", Type: "notification",
			Properties: &runtime.RawExtension{Raw: []byte(`{}`)}}},
	}
}

func TestStepsReadsAnOutputTheStepDeclares(t *testing.T) {
	status := map[string]wfv1alpha1.StepStatus{
		"list-tenants": {Phase: wfv1alpha1.WorkflowStepPhaseSucceeded},
		"build":        {Phase: wfv1alpha1.WorkflowStepPhaseSucceeded},
	}
	out, err := eval(t, `{"n":"$(steps[\"list-tenants\"].outputs.tenants.size())","img":"$(steps.build.outputs.image)"}`,
		map[string]cel.Resolver{RootSteps: StepsResolver(steps(t), status, vars{
			"tenants": `["a","b"]`,
			"image":   `"acme/api:1.2"`,
		})})
	require.NoError(t, err)
	require.Equal(t, map[string]interface{}{"n": int64(2), "img": "acme/api:1.2"}, out,
		"a sub-step's output reads by its own name")
}

func TestStepsWaitsForAProducerStillToFinish(t *testing.T) {
	for name, status := range map[string]map[string]wfv1alpha1.StepStatus{
		"not started": {},
		"running":     {"build": {Phase: wfv1alpha1.WorkflowStepPhaseRunning}},
		"failed, retrying": {"build": {Phase: wfv1alpha1.WorkflowStepPhaseFailed,
			Reason: types.StatusReasonExecute}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := eval(t, `{"img":"$(steps.build.outputs.image)"}`,
				map[string]cel.Resolver{RootSteps: StepsResolver(steps(t), status, vars{})})
			require.ErrorIs(t, err, cel.ErrNotReady)
			require.ErrorContains(t, err, "waiting for step build")
		})
	}
}

func TestStepsWaitsForAnOutputNotSetYet(t *testing.T) {
	status := map[string]wfv1alpha1.StepStatus{"build": {Phase: wfv1alpha1.WorkflowStepPhaseSucceeded}}
	_, err := eval(t, `{"img":"$(steps.build.outputs.image)"}`,
		map[string]cel.Resolver{RootSteps: StepsResolver(steps(t), status, vars{})})
	require.ErrorIs(t, err, cel.ErrNotReady)
	require.ErrorContains(t, err, "step build has no output image yet")

	out, err := eval(t, `{"img":"$(has(steps.build.outputs.image) ? steps.build.outputs.image : \"none\")"}`,
		map[string]cel.Resolver{RootSteps: StepsResolver(steps(t), status, vars{})})
	require.NoError(t, err, "a guarded read does not wait")
	require.Equal(t, map[string]interface{}{"img": "none"}, out)
}

func TestStepsFailsWhenTheProducerNeverWillFinish(t *testing.T) {
	for phase, want := range map[wfv1alpha1.WorkflowStepPhase]string{
		wfv1alpha1.WorkflowStepPhaseSkipped: "step build was skipped",
		wfv1alpha1.WorkflowStepPhaseFailed:  "step build failed",
	} {
		t.Run(string(phase), func(t *testing.T) {
			status := map[string]wfv1alpha1.StepStatus{"build": {Phase: phase, Reason: types.StatusReasonTimeout}}
			_, err := eval(t, `{"img":"$(steps.build.outputs.image)"}`,
				map[string]cel.Resolver{RootSteps: StepsResolver(steps(t), status, vars{"image": `null`})})
			require.Error(t, err)
			require.False(t, errors.Is(err, cel.ErrNotReady), "it never will be ready")
			require.ErrorContains(t, err, want)
		})
	}
}

func TestStepsGuardedReadOfASkippedStep(t *testing.T) {
	guarded := `{"img":"$(has(steps.build.outputs.image) ? steps.build.outputs.image : \"none\")"}`
	skipped := map[string]wfv1alpha1.StepStatus{"build": {Phase: wfv1alpha1.WorkflowStepPhaseSkipped, Reason: types.StatusReasonSkip}}
	out, err := eval(t, guarded, map[string]cel.Resolver{RootSteps: StepsResolver(steps(t), skipped, vars{"image": `null`})})
	require.NoError(t, err, "a skipped step's outputs are absent, which a guard answers")
	require.Equal(t, map[string]interface{}{"img": "none"}, out)

	failed := map[string]wfv1alpha1.StepStatus{"build": {Phase: wfv1alpha1.WorkflowStepPhaseFailed, Reason: types.StatusReasonTimeout}}
	_, err = eval(t, guarded, map[string]cel.Resolver{RootSteps: StepsResolver(steps(t), failed, vars{"image": `null`})})
	require.ErrorContains(t, err, "step build failed", "a guard covers a step that did not run, not one that failed")
}

func TestStepsUnderSuspendOnFailure(t *testing.T) {
	featuregatetesting.SetFeatureGateDuringTest(t, utilfeature.DefaultFeatureGate, features.EnableSuspendOnFailure, true)
	read := func(phase wfv1alpha1.WorkflowStepPhase) error {
		status := map[string]wfv1alpha1.StepStatus{"build": {Phase: phase, Reason: types.StatusReasonTimeout}}
		_, err := eval(t, `{"img":"$(steps.build.outputs.image)"}`,
			map[string]cel.Resolver{RootSteps: StepsResolver(steps(t), status, vars{})})
		return err
	}
	err := read(wfv1alpha1.WorkflowStepPhaseSkipped)
	require.ErrorContains(t, err, "step build was skipped", "a skipped step never runs, whatever the gate")
	require.False(t, errors.Is(err, cel.ErrNotReady))

	require.ErrorIs(t, read(wfv1alpha1.WorkflowStepPhaseFailed), cel.ErrNotReady,
		"a failed step suspends the run and may be resumed, so the read waits")
}

func TestStepsRefusesWhatNoStepDeclares(t *testing.T) {
	for props, want := range map[string]string{
		`{"x":"$(steps.nope.outputs.image)"}`:    `no step "nope"`,
		`{"x":"$(steps.build.outputs.tag)"}`:     `step build declares no output "tag"`,
		`{"x":"$(steps.build.phase)"}`:           `read steps.build.outputs.<name>`,
		`{"x":"$(steps.build)"}`:                 `read steps.build.outputs.<name>`,
		`{"x":"$(steps[\"notify\"].outputs.y)"}`: `step notify declares no output "y"`,
	} {
		t.Run(props, func(t *testing.T) {
			_, err := eval(t, props, map[string]cel.Resolver{RootSteps: StepsResolver(steps(t), nil, vars{})})
			require.ErrorContains(t, err, want)
			require.False(t, errors.Is(err, cel.ErrNotReady))
		})
	}
}

func TestContextReadsTheStepsContext(t *testing.T) {
	pCtx := process.NewContext(process.ContextData{Name: "onboard", Namespace: "acme",
		CustomData: map[string]interface{}{"env": "prod"}})
	pCtx.PushData("stepName", "notify")
	out, err := eval(t, `{"m":"$(context.name)/$(context.stepName) in $(context.namespace), $(context.env)"}`,
		map[string]cel.Resolver{RootContext: ContextResolver(pCtx)})
	require.NoError(t, err)
	require.Equal(t, map[string]interface{}{"m": "onboard/notify in acme, prod"}, out,
		"a WorkflowRun's own context is offered beside the built-ins")
}

func TestEachReadsTheIteration(t *testing.T) {
	pCtx := process.NewContext(process.ContextData{Name: "onboard"})
	process.NewStepRunTimeMeta().Fill(pCtx, []process.StepMetaKV{
		process.WithLoop(map[string]interface{}{"name": "acme"}, 2)})
	resolvers := map[string]cel.Resolver{RootEach: EachResolver(pCtx), RootContext: ContextResolver(pCtx)}

	out, err := eval(t, `{"m":"$(each.item.name) #$(each.index)","whole":"$(each.item)"}`, resolvers)
	require.NoError(t, err)
	require.Equal(t, map[string]interface{}{"m": "acme #2", "whole": map[string]interface{}{"name": "acme"}}, out,
		"each reads as inputs[].from does")

	out, err = eval(t, `{"m":"$(context.each.item.name)"}`, resolvers)
	require.NoError(t, err)
	require.Equal(t, map[string]interface{}{"m": "acme"}, out, "and as a CUE template does")
}

func TestEachOutsideAnIteration(t *testing.T) {
	_, err := eval(t, `{"m":"$(each.item)"}`,
		map[string]cel.Resolver{RootEach: EachResolver(process.NewContext(process.ContextData{}))})
	require.ErrorContains(t, err, "each is only set inside a forEach")
}

func TestNewEngineTakesTheHostsRoots(t *testing.T) {
	engine, err := NewEngine(cel.Root{Name: "source"}, cel.Root{Name: RootContext})
	require.NoError(t, err, "a host may declare context itself; it answers it with its own resolver")
	plan, err := engine.PlanJSON([]byte(`{"x":"$(source.cfg.data.a)$(steps.b.outputs.c)$(each.index)$(context.name)"}`))
	require.NoError(t, err)
	require.Len(t, plan.Reads("source"), 1)

	_, err = NewEngine()
	require.NoError(t, err)
	plain, _ := NewEngine()
	_, err = plain.PlanJSON([]byte(`{"x":"$(source.cfg.data.a)"}`))
	require.Error(t, err, "without KubeVela there is no source root")
}
