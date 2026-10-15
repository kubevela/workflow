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

package builtin

import (
	"context"
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/cel"
	"github.com/stretchr/testify/require"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/expressions"
	"github.com/kubevela/workflow/pkg/tasks/custom"
	"github.com/kubevela/workflow/pkg/types"
)

// A $( ) condition over a finished step decides as the CUE if it replaces does,
// over every phase and reason a step can end in. Over a step still to finish, a
// CUE if reads its status as it stands and a condition waits for it.
func TestConditionsDecideAsTheirCUEEquivalents(t *testing.T) {
	steps := []oamv1alpha1.WorkflowStep{
		{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "build", Type: "build",
			Outputs: oamv1alpha1.StepOutputs{{Name: "mode", ValueFrom: "value"}}}},
		{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "other", Type: "build"}},
	}
	ev, err := expressions.NewEvaluator(types.ExpressionOptions{}, steps, "run")
	require.NoError(t, err)
	wfCtx := newWorkflowContextForTest(t)
	require.NoError(t, wfCtx.SetVar(cuecontext.New().CompileString(`"canary"`), "mode"))
	pCtx := process.NewContext(process.ContextData{Name: "app"})
	process.NewStepRunTimeMeta().Fill(pCtx, []process.StepMetaKV{process.WithName("deploy")})

	// Each pair is a CUE if and the condition that replaces it.
	pairs := [][2]string{
		{"status.build.succeeded", "$(steps.build.succeeded)"},
		{"status.build.failed", "$(steps.build.failed)"},
		{"status.build.skipped", "$(steps.build.skipped)"},
		{"status.build.timeout", "$(steps.build.timeout)"},
		{"status.build.failedAfterRetries", "$(steps.build.failedAfterRetries)"},
		{"status.build.terminate", "$(steps.build.terminate)"},
		{`status.build.phase == "failed"`, `$(steps.build.phase == "failed")`},
		{`status.build.phase == "succeeded"`, `$(steps.build.phase == "succeeded")`},
		{`status.build.reason == "Timeout"`, `$(steps.build.reason == "Timeout")`},
		{`status.build.message == "boom"`, `$(steps.build.message == "boom")`},
		{"!status.build.succeeded", "$(!steps.build.succeeded)"},
		{"status.build.failed || status.build.skipped", "$(steps.build.failed || steps.build.skipped)"},
		{"status.build.succeeded && status.other.succeeded", "$(steps.build.succeeded && steps.other.succeeded)"},
		{"status.build.failed && !status.build.timeout", "$(steps.build.failed && !steps.build.timeout)"},
	}
	phases := []v1alpha1.WorkflowStepPhase{v1alpha1.WorkflowStepPhaseSucceeded, v1alpha1.WorkflowStepPhaseFailed,
		v1alpha1.WorkflowStepPhaseSkipped, v1alpha1.WorkflowStepPhaseRunning, v1alpha1.WorkflowStepPhasePending,
		v1alpha1.WorkflowStepPhaseSuspending}
	reasons := []string{"", types.StatusReasonWait, types.StatusReasonSkip, types.StatusReasonRendering,
		types.StatusReasonExecute, types.StatusReasonSuspend, types.StatusReasonTerminate, types.StatusReasonParameter,
		types.StatusReasonInput, types.StatusReasonOutput, types.StatusReasonFailedAfterRetries,
		types.StatusReasonTimeout, types.StatusReasonAction}

	checked := 0
	for _, phase := range phases {
		for _, reason := range reasons {
			status := map[string]v1alpha1.StepStatus{
				"build": {Name: "build", Phase: phase, Reason: reason, Message: "boom"},
				"other": {Name: "other", Phase: v1alpha1.WorkflowStepPhaseSucceeded},
			}
			finished := types.IsStepFinish(phase, reason)
			for _, pair := range pairs {
				name := fmt.Sprintf("%s/%s: %s", phase, reason, pair[0])
				cueIf, celIf := pair[0], pair[1]
				reader := oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "deploy", If: cueIf}}
				want, cueErr := custom.ValidateIfValue(wfCtx, reader, status, cue.Value{})
				got, err := ev.Condition(context.Background(), types.ExpressionStep{Name: "deploy"}, celIf, wfCtx, pCtx, status)
				if !finished {
					require.ErrorIs(t, err, cel.ErrNotReady, "%s: a condition waits for a step still to finish", name)
					continue
				}
				require.NoError(t, err, name)
				if cueErr != nil {
					// CUE's status leaves out an empty reason, so a CUE if reading it
					// fails and skips its step; a condition reads it as "".
					require.Empty(t, reason, "%s: CUE fails only on a field it leaves out: %v", name, cueErr)
					require.ErrorContains(t, cueErr, "undefined field: reason", name)
					require.False(t, got, "%s: an empty reason equals no reason named", name)
					checked++
					continue
				}
				require.Equal(t, want, got, "%s: CUE says %v, the condition %v", name, want, got)
				checked++
			}
		}
	}
	t.Logf("%d finished-step pairs agree", checked)
	require.Greater(t, checked, 0)

	t.Run("an input and the output it reads", func(t *testing.T) {
		status := map[string]v1alpha1.StepStatus{"build": {Name: "build", Phase: v1alpha1.WorkflowStepPhaseSucceeded}}
		for _, pair := range [][2]string{
			{`inputs.mode == "canary"`, `$(steps.build.outputs.mode == "canary")`},
			{`inputs.mode != "canary"`, `$(steps.build.outputs.mode != "canary")`},
			{`inputs.mode == "canary" && status.build.succeeded`, `$(steps.build.outputs.mode == "canary" && steps.build.succeeded)`},
		} {
			reader := oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "deploy", If: pair[0],
				Inputs: oamv1alpha1.StepInputs{{From: "mode"}}}}
			want, err := custom.ValidateIfValue(wfCtx, reader, status, cue.Value{})
			require.NoError(t, err, pair[0])
			got, err := ev.Condition(context.Background(), types.ExpressionStep{Name: "deploy"}, pair[1], wfCtx, pCtx, status)
			require.NoError(t, err, pair[1])
			require.Equal(t, want, got, "%s against %s", pair[0], pair[1])
		}
	})
}
