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
	"strings"
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/require"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/types"
)

var targets = []any{
	map[string]any{"cluster": "prod-us", "namespace": "shop"},
	map[string]any{"cluster": "prod-eu", "namespace": "shop"},
}

func keyedStep(typ string, key ...string) oamv1alpha1.WorkflowStep {
	step := singleStep(typ, oamv1alpha1.ForEach{From: "targets", Key: key})
	return step
}

func keyedGroup(key ...string) oamv1alpha1.WorkflowStep {
	return oamv1alpha1.WorkflowStep{
		WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "rollout", Type: types.WorkflowStepTypeStepGroup},
		ForEach:          &oamv1alpha1.ForEach{From: "targets", Key: key},
		SubSteps: []oamv1alpha1.WorkflowStepBase{
			{Name: "deploy", Type: "t", Outputs: oamv1alpha1.StepOutputs{{Name: "endpoint", ValueFrom: "v"}}},
			{Name: "verify", Type: "t", DependsOn: []string{"deploy"}},
		},
	}
}

func TestItemLabels(t *testing.T) {
	cases := map[string]struct {
		step   oamv1alpha1.WorkflowStep
		items  []any
		labels []string
		err    string
	}{
		"no key": {step: keyedStep("t"), items: targets},
		"one path": {
			step: keyedStep("t", "cluster"), items: targets, labels: []string{"prod-us", "prod-eu"},
		},
		"a composite key": {
			step: keyedStep("t", "cluster", "namespace"), items: targets, labels: []string{"prod-us-shop", "prod-eu-shop"},
		},
		"a nested path and an integer": {
			step:   keyedStep("t", "spec.zone", "wave"),
			items:  []any{map[string]any{"spec": map[string]any{"zone": "a"}, "wave": float64(1)}},
			labels: []string{"a-1"},
		},
		"a missing part": {
			step: keyedStep("t", "region"), items: targets, err: `item 0 has no value at key region`,
		},
		"an item that is not an object": {
			step: keyedStep("t", "cluster"), items: []any{"prod-us"}, err: `item 0 has no value at key cluster`,
		},
		"an object part": {
			step: keyedStep("t", "spec"), items: []any{map[string]any{"spec": map[string]any{"zone": "a"}}}, err: `item 0: key spec must be a string or an integer`,
		},
		"a fraction": {
			step: keyedStep("t", "wave"), items: []any{map[string]any{"wave": 1.5}}, err: `item 0: key wave must be a string or an integer`,
		},
		"a dot": {
			step: keyedStep("t", "host"), items: []any{map[string]any{"host": "a.example.com"}}, err: `item 0: key "a.example.com" may only hold letters, digits, '-' and '_', and start and end with a letter or digit`,
		},
		"too long": {
			step: keyedStep("t", "host"), items: []any{map[string]any{"host": strings.Repeat("a", 64)}}, err: `is over 63 characters`,
		},
		"a duplicate": {
			step:  keyedStep("t", "namespace"),
			items: targets,
			err:   `items 0 and 1 have the same key "shop"; pick key fields that tell them apart`,
		},
		"a join that collides": {
			step:  keyedStep("t", "a", "b"),
			items: []any{map[string]any{"a": "x-y", "b": "z"}, map[string]any{"a": "x", "b": "y-z"}},
			err:   `items 0 and 1 have the same key "x-y-z"`,
		},
		"a group whose names collide across items": {
			step: oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "rollout", Type: types.WorkflowStepTypeStepGroup},
				ForEach:          &oamv1alpha1.ForEach{From: "targets", Key: []string{"k"}},
				SubSteps:         []oamv1alpha1.WorkflowStepBase{{Name: "x-y", Type: "t"}, {Name: "y", Type: "t"}},
			},
			items: []any{map[string]any{"k": "a"}, map[string]any{"k": "a-x"}},
			err:   `items 0 and 1 both name a step rollout-a-x-y`,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			labels, err := itemLabels(tc.step, tc.items)
			if tc.err != "" {
				r.ErrorContains(err, tc.err)
				return
			}
			r.NoError(err)
			r.Equal(tc.labels, labels)
		})
	}
}

func TestForEachKeyNamesTheItems(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	b, err := cuecontext.New().CompileString(`[{cluster: "prod-us", namespace: "shop"}, {cluster: "prod-eu", namespace: "shop"}]`).MarshalJSON()
	r.NoError(err)
	r.NoError(wfCtx.SetVar(cuecontext.New().CompileBytes(b), "targets"))
	f := newFixture()
	runner, err := ForEach(keyedStep("scale-component", "cluster", "namespace"), f.options("loop-id", ""))
	r.NoError(err)
	status, _, err := runner.Run(wfCtx, runOptions(&runningEngine{wfCtx: wfCtx}))
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseSucceeded, status.Phase)
	r.Equal([]string{"scale-prod-us-shop", "scale-prod-eu-shop"}, f.names())

	r.Equal([]string{"prod-us-shop", "prod-eu-shop"}, ItemLabels(wfCtx.GetMutableValue, "loop-id"), "the labels are kept for restart and backoff")
	loopVar, err := wfCtx.GetVar("scale-prod-eu-shop-each")
	r.NoError(err)
	got, err := loopVar.MarshalJSON()
	r.NoError(err)
	r.JSONEq(`{"item": {"cluster": "prod-eu", "namespace": "shop"}, "index": 1}`, string(got))
	_, err = wfCtx.GetVar("scale-prod-us-shop-endpoint")
	r.NoError(err, "outputs are scoped by key")
	collected, err := wfCtx.GetVar("endpoint")
	r.NoError(err)
	var endpoints []map[string]string
	r.NoError(collected.Decode(&endpoints))
	r.Len(endpoints, 2, "collected outputs stay in item order")
	r.Equal("prod-us", endpoints[0]["cluster"])
}

func TestForEachKeyedGroup(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	r.NoError(wfCtx.SetVar(cuecontext.New().CompileString(`[{cluster: "prod-us", namespace: "shop"}]`), "targets"))
	f := newFixture()
	runner, err := ForEach(keyedGroup("cluster", "namespace"), f.options("loop-id", ""))
	r.NoError(err)
	status, _, err := runner.Run(wfCtx, runOptions(&runningEngine{wfCtx: wfCtx}))
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseSucceeded, status.Phase)
	r.Equal([]string{"rollout-prod-us-shop-deploy", "rollout-prod-us-shop-verify"}, f.names())
	r.Equal([]string{"rollout-prod-us-shop-deploy"}, f.generated[1].DependsOn, "dependsOn stays within the item")
}

func TestForEachKeyFailsTheLoop(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	r.NoError(wfCtx.SetVar(cuecontext.New().CompileString(`[{cluster: "prod-us", namespace: "shop"}, {cluster: "prod-eu", namespace: "shop"}]`), "targets"))
	f := newFixture()
	runner, err := ForEach(keyedStep("t", "namespace"), f.options("loop-id", ""))
	r.NoError(err)
	status, operation, err := runner.Run(wfCtx, runOptions(&runningEngine{wfCtx: wfCtx}))
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseFailed, status.Phase)
	r.Equal(types.StatusReasonParameter, status.Reason)
	r.Contains(status.Message, `items 0 and 1 have the same key "shop"`)
	r.True(operation.Terminated)
	r.Empty(f.generated)
}

func TestForEachKeyedFailureMessage(t *testing.T) {
	r := require.New(t)
	wfCtx := newWorkflowContextForTest(t)
	r.NoError(wfCtx.SetVar(cuecontext.New().CompileString(`[{cluster: "a"}, {cluster: "b"}]`), "targets"))
	f := newFixture()
	runner, err := ForEach(keyedStep("fail-on-b", "cluster"), f.options("loop-id", ""))
	r.NoError(err)
	status, _, err := runner.Run(wfCtx, runOptions(&runningEngine{wfCtx: wfCtx}))
	r.NoError(err)
	r.Equal(v1alpha1.WorkflowStepPhaseFailed, status.Phase)
	r.Equal(`item 1 (b) failed: map[cluster:b] is unreachable`, status.Message)
}

func TestKeyedIterationNames(t *testing.T) {
	r := require.New(t)
	labels := []string{"prod-us", "prod-us-shop"}
	group := keyedGroup("cluster")

	index, ok := IterationIndex(group, labels, "rollout-prod-us-shop-deploy")
	r.True(ok)
	r.Equal(1, index, "the longer key is matched whole, not read as prod-us with sub-step shop-deploy")
	template, ok := IterationTemplate(group, labels, "rollout-prod-us-verify")
	r.True(ok)
	r.Equal("verify", template.Name)
	_, ok = IterationIndex(group, labels, "rollout-prod-eu-deploy")
	r.False(ok, "a key the loop does not hold")

	index, ok = IterationVarIndex(group, labels, "rollout-prod-us-shop-endpoint")
	r.True(ok)
	r.Equal(1, index)
	r.Equal("rollout-prod-us-endpoint", IterationVarName(group, labels, 0, "endpoint"))

	index, ok = IterationIndex(group, nil, "rollout-1-deploy")
	r.True(ok, "without labels, names are read by index")
	r.Equal(1, index)

	r.Nil(ItemLabels(func(...string) string { return "" }, "id"), "no stored labels")
	r.True(IsForEachContextKey(PinnedItemsKey("id")))
	r.True(IsForEachContextKey(ItemLabelsKey("id")))
	r.False(IsForEachContextKey("id.other"))
}
