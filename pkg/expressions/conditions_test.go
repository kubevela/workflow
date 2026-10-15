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
	"testing"

	"github.com/kubevela/pkg/cel"
	"github.com/stretchr/testify/require"

	wfv1alpha1 "github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/types"
)

func TestIsCondition(t *testing.T) {
	for cond, want := range map[string]bool{
		"$(steps.build.succeeded)":                true,
		"  $(context.name == \"prod\")  ":         true,
		"$(each.index > 0 && source.cfg.enabled)": true,
		"status.build.succeeded":                  false,
		`inputs.msg == "cost $(5)"`:               false,
		"always":                                  false,
		"":                                        false,
		"$(steps.a.succeeded) && $(steps.b.failed)": false,
		"x $(steps.a.succeeded)":                    false,
		"$$(steps.a.succeeded)":                     false,
		"$(steps.a.succeeded":                       false,
	} {
		require.Equal(t, want, IsCondition(cond), "%q", cond)
	}
}

func TestStepsReadsAFinishedStepsStatus(t *testing.T) {
	read := func(t *testing.T, expr string, status map[string]wfv1alpha1.StepStatus) (interface{}, error) {
		t.Helper()
		out, err := eval(t, `{"v":"$(`+expr+`)"}`, map[string]cel.Resolver{RootSteps: StepsResolver(steps(t), status, vars{})})
		if err != nil {
			return nil, err
		}
		return out.(map[string]interface{})["v"], nil
	}
	finished := map[string]struct {
		status wfv1alpha1.StepStatus
		want   map[string]interface{}
	}{
		"succeeded": {wfv1alpha1.StepStatus{Phase: wfv1alpha1.WorkflowStepPhaseSucceeded},
			map[string]interface{}{"phase": "succeeded", "succeeded": true, "failed": false, "skipped": false, "timeout": false}},
		"failed for good": {wfv1alpha1.StepStatus{Phase: wfv1alpha1.WorkflowStepPhaseFailed, Reason: types.StatusReasonFailedAfterRetries,
			Message: "boom"}, map[string]interface{}{"phase": "failed", "failed": true, "succeeded": false, "failedAfterRetries": true,
			"message": "boom", "reason": types.StatusReasonFailedAfterRetries}},
		"timed out": {wfv1alpha1.StepStatus{Phase: wfv1alpha1.WorkflowStepPhaseFailed, Reason: types.StatusReasonTimeout},
			map[string]interface{}{"failed": true, "timeout": true, "terminate": false}},
		"terminated": {wfv1alpha1.StepStatus{Phase: wfv1alpha1.WorkflowStepPhaseFailed, Reason: types.StatusReasonTerminate},
			map[string]interface{}{"failed": true, "terminate": true}},
		"skipped": {wfv1alpha1.StepStatus{Phase: wfv1alpha1.WorkflowStepPhaseSkipped},
			map[string]interface{}{"phase": "skipped", "skipped": true, "succeeded": false, "message": ""}},
	}
	for name, tc := range finished {
		t.Run(name, func(t *testing.T) {
			for field, want := range tc.want {
				got, err := read(t, "steps.build."+field, map[string]wfv1alpha1.StepStatus{"build": tc.status})
				require.NoError(t, err, field)
				require.Equal(t, want, got, field)
			}
		})
	}

	for name, status := range map[string]map[string]wfv1alpha1.StepStatus{
		"not started":      {},
		"running":          {"build": {Phase: wfv1alpha1.WorkflowStepPhaseRunning}},
		"failed, retrying": {"build": {Phase: wfv1alpha1.WorkflowStepPhaseFailed, Reason: types.StatusReasonExecute}},
	} {
		t.Run(name+" waits", func(t *testing.T) {
			_, err := read(t, "steps.build.failed", status)
			require.ErrorIs(t, err, cel.ErrNotReady)
			require.ErrorContains(t, err, "waiting for step build")
		})
	}

	t.Run("status and outputs side by side", func(t *testing.T) {
		out, err := eval(t, `{"v":"$(steps.build.succeeded ? steps.build.outputs.image : \"none\")"}`,
			map[string]cel.Resolver{RootSteps: StepsResolver(steps(t),
				map[string]wfv1alpha1.StepStatus{"build": {Phase: wfv1alpha1.WorkflowStepPhaseSucceeded}},
				vars{"image": `"acme/api:1"`})})
		require.NoError(t, err)
		require.Equal(t, map[string]interface{}{"v": "acme/api:1"}, out)
	})

	t.Run("a field steps do not have", func(t *testing.T) {
		_, err := read(t, "steps.build.colour",
			map[string]wfv1alpha1.StepStatus{"build": {Phase: wfv1alpha1.WorkflowStepPhaseSucceeded}})
		require.ErrorContains(t, err, "steps.build.colour")
		require.ErrorContains(t, err, "phase")
	})

	t.Run("a step that does not exist", func(t *testing.T) {
		_, err := read(t, "steps.nope.succeeded", map[string]wfv1alpha1.StepStatus{})
		require.ErrorContains(t, err, `no step "nope"`)
	})
}

func TestStepsReadsALoopsStatus(t *testing.T) {
	status := map[string]wfv1alpha1.StepStatus{
		"per-tenant": {Phase: wfv1alpha1.WorkflowStepPhaseFailed, Reason: types.StatusReasonFailedAfterRetries},
		types.IterationName("per-tenant", 0, "fetch"): {Phase: wfv1alpha1.WorkflowStepPhaseSucceeded},
	}
	t.Run("outside the loop, a body step reads as the loop", func(t *testing.T) {
		out, err := resolve(t, `{"v":"$(steps.fetch.failed)"}`, running("summary"), status, vars{})
		require.NoError(t, err)
		require.JSONEq(t, `{"v":true}`, out.(string))
	})
	t.Run("inside the loop, a fellow body step reads as this pass", func(t *testing.T) {
		out, err := resolve(t, `{"v":"$(steps.fetch.succeeded)"}`,
			running(types.IterationName("per-tenant", 0, "greet"), 0), status, vars{})
		require.NoError(t, err)
		require.JSONEq(t, `{"v":true}`, out.(string))
	})
	t.Run("a step cannot read its own status", func(t *testing.T) {
		_, err := resolve(t, `{"v":"$(steps.summary.phase)"}`, running("summary"), status, vars{})
		require.ErrorContains(t, err, "step summary cannot read its own status")
	})
}

func TestEvaluatorCondition(t *testing.T) {
	ev, err := NewEvaluator(types.ExpressionOptions{
		Roots: []cel.Root{{Name: "source"}},
		Resolvers: map[string]cel.Resolver{"source": cel.ResolverFunc(func(ctx context.Context, _ []cel.Read) (interface{}, error) {
			if _, ok := RunFrom(ctx); !ok {
				return nil, errors.New("no run in the context")
			}
			return map[string]interface{}{"cfg": map[string]interface{}{"enabled": true}}, nil
		})},
	}, steps(t), "run-1")
	require.NoError(t, err)
	pCtx := process.NewContext(process.ContextData{Name: "onboard"})
	process.NewStepRunTimeMeta().Fill(pCtx, []process.StepMetaKV{process.WithName("notify"), process.WithLoop("eu", 2)})
	status := map[string]wfv1alpha1.StepStatus{
		"build":        {Phase: wfv1alpha1.WorkflowStepPhaseFailed, Reason: types.StatusReasonFailedAfterRetries},
		"list-tenants": {Phase: wfv1alpha1.WorkflowStepPhaseSucceeded},
	}
	cond := func(expr string) (bool, error) {
		return ev.Condition(context.Background(), types.ExpressionStep{Name: "notify", Type: "notification"}, expr,
			&stored{data: map[string]string{}}, pCtx, status)
	}

	for expr, want := range map[string]bool{
		"$(steps.build.failed)":                                       true,
		"$(steps.build.succeeded)":                                    false,
		"$(context.name == \"onboard\")":                              true,
		"$(source.cfg.enabled && each.index == 2)":                    true,
		"$(each.item == \"eu\" && steps[\"list-tenants\"].succeeded)": true,
		"  $(steps.build.failed && !steps.build.timeout)  ":           true,
	} {
		got, err := cond(expr)
		require.NoError(t, err, expr)
		require.Equal(t, want, got, expr)
	}

	t.Run("a condition must be a bool", func(t *testing.T) {
		_, err := cond("$(steps.build.phase)")
		require.ErrorContains(t, err, "is string, but a step's if must be a bool")
	})
	t.Run("a read still to be answered waits", func(t *testing.T) {
		_, err := ev.Condition(context.Background(), types.ExpressionStep{Name: "notify"}, "$(steps.build.failed)",
			&stored{data: map[string]string{}}, pCtx, map[string]wfv1alpha1.StepStatus{})
		require.ErrorIs(t, err, cel.ErrNotReady)
	})
	t.Run("not a condition", func(t *testing.T) {
		_, err := cond("status.build.failed")
		require.ErrorContains(t, err, "not a $( ) condition")
	})
}
