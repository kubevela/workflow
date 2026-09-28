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

package utils

import (
	"sort"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/tasks/builtin"
)

func forEachContext(vars string) *corev1.ConfigMap {
	return &corev1.ConfigMap{Data: map[string]string{
		wfContext.ConfigMapKeyVars:   vars,
		builtin.PinnedItemsKey("id"): `["a","b","c"]`,
		"unrelated":                  "kept",
	}}
}

func varNames(t *testing.T, cm *corev1.ConfigMap) []string {
	v := cuecontext.New().CompileString(cm.Data[wfContext.ConfigMapKeyVars])
	require.NoError(t, v.Err())
	it, err := v.Fields(cue.All())
	require.NoError(t, err)
	var names []string
	for it.Next() {
		names = append(names, it.Selector().Unquoted())
	}
	sort.Strings(names)
	return names
}

func subNames(status []v1alpha1.WorkflowStepStatus, step string) []string {
	var names []string
	for _, s := range status {
		if s.Name != step {
			continue
		}
		for _, sub := range s.SubStepsStatus {
			names = append(names, sub.Name)
		}
	}
	return names
}

func stepNames(status []v1alpha1.WorkflowStepStatus) []string {
	var names []string
	for _, s := range status {
		names = append(names, s.Name)
	}
	return names
}

func TestCleanStatusFromForEachIteration(t *testing.T) {
	single := func(mode oamv1alpha1.WorkflowMode) []oamv1alpha1.WorkflowStep {
		return []oamv1alpha1.WorkflowStep{
			{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
					Name:    "scale",
					Type:    "t",
					Outputs: oamv1alpha1.StepOutputs{{Name: "endpoint", ValueFrom: "v"}},
				},
				ForEach: &oamv1alpha1.ForEach{From: "regions", Mode: mode},
			},
			{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "next", Type: "t"}},
		}
	}
	singleStatus := func() []v1alpha1.WorkflowStepStatus {
		return []v1alpha1.WorkflowStepStatus{
			{
				StepStatus: v1alpha1.StepStatus{ID: "id", Name: "scale", Phase: v1alpha1.WorkflowStepPhaseFailed},
				SubStepsStatus: []v1alpha1.StepStatus{
					{Name: "scale-0", Phase: v1alpha1.WorkflowStepPhaseSucceeded},
					{Name: "scale-1", Phase: v1alpha1.WorkflowStepPhaseFailed},
					{Name: "scale-2", Phase: v1alpha1.WorkflowStepPhaseSkipped},
				},
			},
			{StepStatus: v1alpha1.StepStatus{Name: "next", Phase: v1alpha1.WorkflowStepPhaseSkipped}},
		}
	}
	singleVars := `{
		"scale-0-loop": {item: "a", index: 0}, "scale-0-endpoint": "e0",
		"scale-1-loop": {item: "b", index: 1}, "scale-1-endpoint": "e1",
		"scale-2-loop": {item: "c", index: 2},
		"scaler-0-loop": "another step's",
		endpoint: ["e0", "e1", null],
		regions: ["a", "b", "c"],
	}`
	stepMode := oamv1alpha1.WorkflowExecuteMode{Steps: v1alpha1.WorkflowModeStep, SubSteps: v1alpha1.WorkflowModeDAG}

	t.Run("an item and every later one, in StepByStep", func(t *testing.T) {
		r := require.New(t)
		status, cm, err := CleanStatusFromStep(single(""), singleStatus(), stepMode, forEachContext(singleVars), "scale-1")
		r.NoError(err)
		r.Equal([]string{"scale-0"}, subNames(status, "scale"))
		r.Equal([]string{"scale"}, stepNames(status), "steps after the loop rerun")
		r.Equal(v1alpha1.WorkflowStepPhaseRunning, status[0].Phase)
		r.Equal([]string{"regions", "scale-0-endpoint", "scale-0-loop", "scaler-0-loop"}, varNames(t, cm))
		r.Equal(`["a","b","c"]`, cm.Data[builtin.PinnedItemsKey("id")], "the loop reruns the same list")
	})

	t.Run("only that item, in DAG", func(t *testing.T) {
		r := require.New(t)
		status, cm, err := CleanStatusFromStep(single(v1alpha1.WorkflowModeDAG), singleStatus(), stepMode, forEachContext(singleVars), "scale-1")
		r.NoError(err)
		r.Equal([]string{"scale-0", "scale-2"}, subNames(status, "scale"))
		r.Equal([]string{"regions", "scale-0-endpoint", "scale-0-loop", "scale-2-loop", "scaler-0-loop"}, varNames(t, cm))
	})

	t.Run("a group's item from start, for any of its sub-steps", func(t *testing.T) {
		r := require.New(t)
		steps := []oamv1alpha1.WorkflowStep{{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "rollout", Type: "step-group"},
			ForEach:          &oamv1alpha1.ForEach{From: "regions"},
			SubSteps: []oamv1alpha1.WorkflowStepBase{
				{Name: "deploy", Type: "t"},
				{Name: "verify", Type: "t", Outputs: oamv1alpha1.StepOutputs{{Name: "health", ValueFrom: "v"}}},
			},
		}}
		status := []v1alpha1.WorkflowStepStatus{{
			StepStatus: v1alpha1.StepStatus{ID: "id", Name: "rollout", Phase: v1alpha1.WorkflowStepPhaseFailed},
			SubStepsStatus: []v1alpha1.StepStatus{
				{Name: "rollout-0-deploy", Phase: v1alpha1.WorkflowStepPhaseSucceeded},
				{Name: "rollout-0-verify", Phase: v1alpha1.WorkflowStepPhaseSucceeded},
				{Name: "rollout-1-deploy", Phase: v1alpha1.WorkflowStepPhaseSucceeded},
				{Name: "rollout-1-verify", Phase: v1alpha1.WorkflowStepPhaseFailed},
			},
		}}
		vars := `{"rollout-0-loop": {}, "rollout-0-health": 1, "rollout-1-loop": {}, "rollout-1-health": 0, health: [1, 0]}`
		status, cm, err := CleanStatusFromStep(steps, status, stepMode, forEachContext(vars), "rollout-1-verify")
		r.NoError(err)
		r.Equal([]string{"rollout-0-deploy", "rollout-0-verify"}, subNames(status, "rollout"))
		r.Equal([]string{"rollout-0-health", "rollout-0-loop"}, varNames(t, cm), "a group's collected outputs are cleared too")
	})

	t.Run("the loop itself", func(t *testing.T) {
		r := require.New(t)
		status, cm, err := CleanStatusFromStep(single(""), singleStatus(), stepMode, forEachContext(singleVars), "scale")
		r.NoError(err)
		r.Empty(status)
		r.Equal([]string{"regions", "scaler-0-loop"}, varNames(t, cm))
		r.NotContains(cm.Data, builtin.PinnedItemsKey("id"), "the list is read again")
		r.Equal("kept", cm.Data["unrelated"])
	})

	t.Run("a later loop is cleared as well, not only its status", func(t *testing.T) {
		r := require.New(t)
		steps := []oamv1alpha1.WorkflowStep{
			{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "a", Type: "t"},
				ForEach:          &oamv1alpha1.ForEach{From: "regions"},
			},
			{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{
					Name:    "b",
					Type:    "t",
					Outputs: oamv1alpha1.StepOutputs{{Name: "result", ValueFrom: "v"}},
				},
				ForEach: &oamv1alpha1.ForEach{From: "regions"},
			},
		}
		status := []v1alpha1.WorkflowStepStatus{
			{
				StepStatus:     v1alpha1.StepStatus{ID: "a-id", Name: "a", Phase: v1alpha1.WorkflowStepPhaseFailed},
				SubStepsStatus: []v1alpha1.StepStatus{{Name: "a-0", Phase: v1alpha1.WorkflowStepPhaseFailed}},
			},
			{
				StepStatus:     v1alpha1.StepStatus{ID: "b-id", Name: "b", Phase: v1alpha1.WorkflowStepPhaseSucceeded},
				SubStepsStatus: []v1alpha1.StepStatus{{Name: "b-0", Phase: v1alpha1.WorkflowStepPhaseSucceeded}},
			},
		}
		cm := &corev1.ConfigMap{Data: map[string]string{
			wfContext.ConfigMapKeyVars:     `{"a-0-loop": {}, "b-0-loop": {item: "old"}, "b-0-result": 1, result: [1], regions: ["x"]}`,
			builtin.PinnedItemsKey("a-id"): `["x"]`,
			builtin.PinnedItemsKey("b-id"): `["old"]`,
		}}
		status, cm, err := CleanStatusFromStep(steps, status, stepMode, cm, "a-0")
		r.NoError(err)
		r.Equal([]string{"a"}, stepNames(status))
		r.Equal([]string{"regions"}, varNames(t, cm), "b reruns under a new ID, so nothing of its last run may be read")
		r.Contains(cm.Data, builtin.PinnedItemsKey("a-id"), "a reruns the same list")
		r.NotContains(cm.Data, builtin.PinnedItemsKey("b-id"))
	})

	t.Run("a later loop that has not run yet", func(t *testing.T) {
		r := require.New(t)
		steps := []oamv1alpha1.WorkflowStep{
			{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "a", Type: "t"},
				ForEach:          &oamv1alpha1.ForEach{From: "regions"},
			},
			{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "b", Type: "t"},
				ForEach:          &oamv1alpha1.ForEach{From: "regions"},
			},
		}
		status := []v1alpha1.WorkflowStepStatus{{
			StepStatus:     v1alpha1.StepStatus{ID: "a-id", Name: "a", Phase: v1alpha1.WorkflowStepPhaseFailed},
			SubStepsStatus: []v1alpha1.StepStatus{{Name: "a-0", Phase: v1alpha1.WorkflowStepPhaseFailed}},
		}}
		cm := &corev1.ConfigMap{Data: map[string]string{
			wfContext.ConfigMapKeyVars:     `{"a-0-loop": {}, regions: ["x"]}`,
			builtin.PinnedItemsKey("a-id"): `["x"]`,
		}}
		status, cm, err := CleanStatusFromStep(steps, status, stepMode, cm, "a-0")
		r.NoError(err)
		r.Equal([]string{"a"}, stepNames(status))
		r.Equal([]string{"regions"}, varNames(t, cm))
		r.Contains(cm.Data, builtin.PinnedItemsKey("a-id"))
	})

	t.Run("a pass that is not in the status", func(t *testing.T) {
		_, _, err := CleanStatusFromStep(single(""), singleStatus(), stepMode, forEachContext(singleVars), "scale-7")
		require.ErrorContains(t, err, "failed step scale-7 not found")
	})

	t.Run("a context whose vars are not a struct", func(t *testing.T) {
		_, _, err := CleanStatusFromStep(single(""), singleStatus(), stepMode, forEachContext(`[]`), "scale-1")
		require.ErrorContains(t, err, "not a struct")
	})

	t.Run("an item that did not fail", func(t *testing.T) {
		_, _, err := CleanStatusFromStep(single(""), singleStatus(), stepMode, forEachContext(singleVars), "scale-0")
		require.ErrorContains(t, err, "can not restart from a non-failed step")
	})
}
