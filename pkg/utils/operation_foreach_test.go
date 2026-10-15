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
	"encoding/json"
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
		"scale-0-each": {item: "a", index: 0}, "scale-0-endpoint": "e0",
		"scale-1-each": {item: "b", index: 1}, "scale-1-endpoint": "e1",
		"scale-2-each": {item: "c", index: 2},
		"scaler-0-each": "another step's",
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
		r.Equal([]string{"regions", "scale-0-each", "scale-0-endpoint", "scaler-0-each"}, varNames(t, cm))
		r.Equal(`["a","b","c"]`, cm.Data[builtin.PinnedItemsKey("id")], "the loop reruns the same list")
	})

	t.Run("only that item, in DAG", func(t *testing.T) {
		r := require.New(t)
		status, cm, err := CleanStatusFromStep(single(v1alpha1.WorkflowModeDAG), singleStatus(), stepMode, forEachContext(singleVars), "scale-1")
		r.NoError(err)
		r.Equal([]string{"scale-0", "scale-2"}, subNames(status, "scale"))
		r.Equal([]string{"regions", "scale-0-each", "scale-0-endpoint", "scale-2-each", "scaler-0-each"}, varNames(t, cm))
	})

	t.Run("a group's failed sub-step only, as in a plain step-group", func(t *testing.T) {
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
		vars := `{"rollout-0-each": {}, "rollout-0-health": 1, "rollout-1-each": {}, "rollout-1-health": 0, health: [1, 0]}`
		status, cm, err := CleanStatusFromStep(steps, status, stepMode, forEachContext(vars), "rollout-1-verify")
		r.NoError(err)
		r.Equal([]string{"rollout-0-deploy", "rollout-0-verify", "rollout-1-deploy"}, subNames(status, "rollout"), "the item's succeeded deploy is kept")
		r.Equal([]string{"rollout-0-each", "rollout-0-health", "rollout-1-each"}, varNames(t, cm), "the rerun sub-step's output and the collected list are cleared")
	})

	group := func(mode oamv1alpha1.WorkflowMode, subs ...oamv1alpha1.WorkflowStepBase) []oamv1alpha1.WorkflowStep {
		return []oamv1alpha1.WorkflowStep{{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "rollout", Type: "step-group"},
			Mode:             mode,
			ForEach:          &oamv1alpha1.ForEach{From: "regions"},
			SubSteps:         subs,
		}}
	}
	groupStatus := func(subs ...v1alpha1.StepStatus) []v1alpha1.WorkflowStepStatus {
		return []v1alpha1.WorkflowStepStatus{{
			StepStatus:     v1alpha1.StepStatus{ID: "id", Name: "rollout", Phase: v1alpha1.WorkflowStepPhaseFailed},
			SubStepsStatus: subs,
		}}
	}
	sub := func(name string, phase v1alpha1.WorkflowStepPhase) v1alpha1.StepStatus {
		return v1alpha1.StepStatus{Name: name, Phase: phase}
	}

	t.Run("a group's failed sub-step and what depends on it, in a DAG body", func(t *testing.T) {
		r := require.New(t)
		steps := group(v1alpha1.WorkflowModeDAG,
			oamv1alpha1.WorkflowStepBase{Name: "a", Type: "t"},
			oamv1alpha1.WorkflowStepBase{Name: "b", Type: "t", Outputs: oamv1alpha1.StepOutputs{{Name: "out-b", ValueFrom: "v"}}},
			oamv1alpha1.WorkflowStepBase{Name: "c", Type: "t", DependsOn: []string{"a"}},
		)
		status := groupStatus(
			sub("rollout-0-a", v1alpha1.WorkflowStepPhaseFailed),
			sub("rollout-0-b", v1alpha1.WorkflowStepPhaseSucceeded),
			sub("rollout-0-c", v1alpha1.WorkflowStepPhaseSkipped),
		)
		vars := `{"rollout-0-each": {}, "rollout-0-out-b": 1}`
		status, cm, err := CleanStatusFromStep(steps, status, stepMode, forEachContext(vars), "rollout-0-a")
		r.NoError(err)
		r.Equal([]string{"rollout-0-b"}, subNames(status, "rollout"), "b does not depend on a, so it is kept")
		r.Equal([]string{"rollout-0-each", "rollout-0-out-b"}, varNames(t, cm))
	})

	t.Run("a group's failed sub-step and every later one, in a StepByStep body", func(t *testing.T) {
		r := require.New(t)
		steps := group(v1alpha1.WorkflowModeStep,
			oamv1alpha1.WorkflowStepBase{Name: "deploy", Type: "t"},
			oamv1alpha1.WorkflowStepBase{Name: "verify", Type: "t"},
			oamv1alpha1.WorkflowStepBase{Name: "report", Type: "t"},
		)
		status := groupStatus(
			sub("rollout-0-deploy", v1alpha1.WorkflowStepPhaseSucceeded),
			sub("rollout-0-verify", v1alpha1.WorkflowStepPhaseSucceeded),
			sub("rollout-0-report", v1alpha1.WorkflowStepPhaseSucceeded),
			sub("rollout-1-deploy", v1alpha1.WorkflowStepPhaseSucceeded),
			sub("rollout-1-verify", v1alpha1.WorkflowStepPhaseFailed),
			sub("rollout-1-report", v1alpha1.WorkflowStepPhaseSkipped),
			sub("rollout-2-deploy", v1alpha1.WorkflowStepPhaseSkipped),
		)
		status, _, err := CleanStatusFromStep(steps, status, stepMode, forEachContext(`{}`), "rollout-1-verify")
		r.NoError(err)
		r.Equal([]string{"rollout-0-deploy", "rollout-0-verify", "rollout-0-report", "rollout-1-deploy"}, subNames(status, "rollout"),
			"the item keeps its deploy; later items rerun in full")
	})

	t.Run("the loop itself", func(t *testing.T) {
		r := require.New(t)
		status, cm, err := CleanStatusFromStep(single(""), singleStatus(), stepMode, forEachContext(singleVars), "scale")
		r.NoError(err)
		r.Empty(status)
		r.Equal([]string{"regions", "scaler-0-each"}, varNames(t, cm))
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
			wfContext.ConfigMapKeyVars:     `{"a-0-each": {}, "b-0-each": {item: "old"}, "b-0-result": 1, result: [1], regions: ["x"]}`,
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
			wfContext.ConfigMapKeyVars:     `{"a-0-each": {}, regions: ["x"]}`,
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

func TestCleanStatusFromKeyedForEach(t *testing.T) {
	stepMode := oamv1alpha1.WorkflowExecuteMode{Steps: v1alpha1.WorkflowModeStep, SubSteps: v1alpha1.WorkflowModeDAG}
	keyedContext := func(vars string, labels ...string) *corev1.ConfigMap {
		b, err := json.Marshal(labels)
		require.NoError(t, err)
		return &corev1.ConfigMap{Data: map[string]string{
			wfContext.ConfigMapKeyVars:   vars,
			builtin.PinnedItemsKey("id"): `[{},{},{}]`,
			builtin.ItemLabelsKey("id"):  string(b),
		}}
	}

	t.Run("a keyed item and every later one", func(t *testing.T) {
		r := require.New(t)
		steps := []oamv1alpha1.WorkflowStep{{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "scale", Type: "t", Outputs: oamv1alpha1.StepOutputs{{Name: "endpoint", ValueFrom: "v"}}},
			ForEach:          &oamv1alpha1.ForEach{From: "targets", Key: []string{"cluster"}},
		}}
		status := []v1alpha1.WorkflowStepStatus{{
			StepStatus: v1alpha1.StepStatus{ID: "id", Name: "scale", Phase: v1alpha1.WorkflowStepPhaseFailed},
			SubStepsStatus: []v1alpha1.StepStatus{
				{Name: "scale-prod-us", Phase: v1alpha1.WorkflowStepPhaseSucceeded},
				{Name: "scale-prod-eu", Phase: v1alpha1.WorkflowStepPhaseFailed},
				{Name: "scale-ap", Phase: v1alpha1.WorkflowStepPhaseSkipped},
			},
		}}
		vars := `{"scale-prod-us-each": {}, "scale-prod-us-endpoint": 1, "scale-prod-eu-each": {}, "scale-prod-eu-endpoint": 2, "scale-ap-each": {}}`
		status, cm, err := CleanStatusFromStep(steps, status, stepMode, keyedContext(vars, "prod-us", "prod-eu", "ap"), "scale-prod-eu")
		r.NoError(err)
		r.Equal([]string{"scale-prod-us"}, subNames(status, "scale"))
		r.Equal([]string{"scale-prod-us-each", "scale-prod-us-endpoint"}, varNames(t, cm))
		r.Contains(cm.Data, builtin.ItemLabelsKey("id"), "the loop keeps its keys, as it keeps its list")
	})

	t.Run("a keyed group whose keys share a prefix", func(t *testing.T) {
		r := require.New(t)
		steps := []oamv1alpha1.WorkflowStep{{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "rollout", Type: "step-group"},
			ForEach:          &oamv1alpha1.ForEach{From: "targets", Key: []string{"cluster"}},
			SubSteps: []oamv1alpha1.WorkflowStepBase{
				{Name: "deploy", Type: "t", Outputs: oamv1alpha1.StepOutputs{{Name: "endpoint", ValueFrom: "v"}}},
				{Name: "verify", Type: "t"},
			},
		}}
		status := []v1alpha1.WorkflowStepStatus{{
			StepStatus: v1alpha1.StepStatus{ID: "id", Name: "rollout", Phase: v1alpha1.WorkflowStepPhaseFailed},
			SubStepsStatus: []v1alpha1.StepStatus{
				{Name: "rollout-prod-us-deploy", Phase: v1alpha1.WorkflowStepPhaseSucceeded},
				{Name: "rollout-prod-us-verify", Phase: v1alpha1.WorkflowStepPhaseSucceeded},
				{Name: "rollout-prod-us-shop-deploy", Phase: v1alpha1.WorkflowStepPhaseSucceeded},
				{Name: "rollout-prod-us-shop-verify", Phase: v1alpha1.WorkflowStepPhaseFailed},
			},
		}}
		vars := `{"rollout-prod-us-each": {}, "rollout-prod-us-endpoint": 1, "rollout-prod-us-shop-each": {}, "rollout-prod-us-shop-endpoint": 2, endpoint: [1, 2]}`
		status, cm, err := CleanStatusFromStep(steps, status, stepMode, keyedContext(vars, "prod-us", "prod-us-shop"), "rollout-prod-us-shop-verify")
		r.NoError(err)
		r.Equal([]string{"rollout-prod-us-deploy", "rollout-prod-us-verify", "rollout-prod-us-shop-deploy"}, subNames(status, "rollout"),
			"only the prod-us-shop item's verify reruns; prod-us is a different item, not a prefix of it")
		r.Equal([]string{"rollout-prod-us-each", "rollout-prod-us-endpoint", "rollout-prod-us-shop-each", "rollout-prod-us-shop-endpoint"}, varNames(t, cm))
	})

	t.Run("the keyed loop itself drops its keys with its list", func(t *testing.T) {
		r := require.New(t)
		steps := []oamv1alpha1.WorkflowStep{{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "scale", Type: "t"},
			ForEach:          &oamv1alpha1.ForEach{From: "targets", Key: []string{"cluster"}},
		}}
		status := []v1alpha1.WorkflowStepStatus{{
			StepStatus:     v1alpha1.StepStatus{ID: "id", Name: "scale", Phase: v1alpha1.WorkflowStepPhaseFailed},
			SubStepsStatus: []v1alpha1.StepStatus{{Name: "scale-prod-us", Phase: v1alpha1.WorkflowStepPhaseFailed}},
		}}
		_, cm, err := CleanStatusFromStep(steps, status, stepMode, keyedContext(`{"scale-prod-us-each": {}}`, "prod-us"), "scale")
		r.NoError(err)
		r.Empty(varNames(t, cm))
		r.NotContains(cm.Data, builtin.PinnedItemsKey("id"))
		r.NotContains(cm.Data, builtin.ItemLabelsKey("id"), "the list is read again, and so are its keys")
	})
}
