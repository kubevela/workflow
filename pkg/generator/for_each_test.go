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

package generator

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/types"
)

func TestForEachBodyMode(t *testing.T) {
	cases := map[string]struct {
		instanceMode *oamv1alpha1.WorkflowExecuteMode
		stepMode     oamv1alpha1.WorkflowMode
		want         oamv1alpha1.WorkflowMode
	}{
		"DAG by default":            {want: v1alpha1.WorkflowModeDAG},
		"the workflow's sub-steps":  {instanceMode: &oamv1alpha1.WorkflowExecuteMode{SubSteps: v1alpha1.WorkflowModeStep}, want: v1alpha1.WorkflowModeStep},
		"the step's own over both":  {instanceMode: &oamv1alpha1.WorkflowExecuteMode{SubSteps: v1alpha1.WorkflowModeStep}, stepMode: v1alpha1.WorkflowModeDAG, want: v1alpha1.WorkflowModeDAG},
		"the step's own, with none": {stepMode: v1alpha1.WorkflowModeStep, want: v1alpha1.WorkflowModeStep},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			step := oamv1alpha1.WorkflowStep{
				WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "rollout", Type: types.WorkflowStepTypeStepGroup},
				Mode:             tc.stepMode,
				ForEach:          &oamv1alpha1.ForEach{Items: &apiextensionsv1.JSON{Raw: []byte(`["a"]`)}},
				SubSteps:         []oamv1alpha1.WorkflowStepBase{{Name: "deploy", Type: "t"}},
			}
			options := &types.TaskGeneratorOptions{ID: "id"}
			runner, err := generateForEachRunner(context.Background(), &types.WorkflowInstance{Mode: tc.instanceMode}, step, nil, options, types.StepGeneratorOptions{})
			r.NoError(err)
			r.Equal("rollout", runner.Name())
			r.Equal(tc.want, options.SubStepExecuteMode)
		})
	}
}
