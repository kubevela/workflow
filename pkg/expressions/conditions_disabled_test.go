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
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/util/validation/field"

	v1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
)

// Without expressions, a $( ) if is refused where it is written, on a step or a
// sub-step; any other if is CUE and is left alone.
func TestConditionsWithoutExpressions(t *testing.T) {
	steps := []v1alpha1.WorkflowStep{
		condStep("a", ""),
		condStep("b", "$(steps.a.succeeded)"),
		condStep("c", "status.a.succeeded"),
		condStep("d", "$(steps.a.succeeded) && $(steps.a.failed)"),
		{WorkflowStepBase: v1alpha1.WorkflowStepBase{Name: "group", Type: "step-group"},
			SubSteps: []v1alpha1.WorkflowStepBase{{Name: "inner", Type: "notify", If: "$(steps.a.failed)"}}},
	}
	errs := ConditionsWithoutExpressions(field.NewPath("spec", "workflowSpec", "steps"), steps, "turn them on")
	require.Len(t, errs, 2)
	require.Equal(t, "spec.workflowSpec.steps[1].if", errs[0].Field)
	require.Equal(t, field.ErrorTypeForbidden, errs[0].Type)
	require.Contains(t, errs[0].Detail, "turn them on")
	require.Equal(t, "spec.workflowSpec.steps[4].subSteps[0].if", errs[1].Field)
}
