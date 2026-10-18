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

package workflowrun

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	feature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/features"
	"github.com/kubevela/workflow/pkg/types"
)

func conditionRun(annotations map[string]string) *v1alpha1.WorkflowRun {
	return &v1alpha1.WorkflowRun{
		ObjectMeta: metav1.ObjectMeta{Name: "run", Namespace: "default", Annotations: annotations},
		Spec: v1alpha1.WorkflowRunSpec{WorkflowSpec: &oamv1alpha1.WorkflowSpec{Steps: []oamv1alpha1.WorkflowStep{
			{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "a", Type: "suspend"}},
			{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "b", Type: "suspend", If: "$(steps.a.succeeded)"}},
		}}},
	}
}

// A $( ) if is refused unless the WorkflowRun reads expressions, naming the switch
// that is off, rather than admitted and then skipped as an invalid CUE condition.
func TestStepConditionsRefusedWithoutExpressions(t *testing.T) {
	h := &ValidatingHandler{}

	t.Run("gate off", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, feature.DefaultFeatureGate, features.EnableCelExpressions, false)
		errs := h.ValidateWorkflow(context.Background(), conditionRun(map[string]string{types.AnnotationCelExpressions: "true"})).ToAggregate()
		require.Error(t, errs)
		require.Contains(t, errs.Error(), "spec.workflowSpec.steps[1].if")
		require.Contains(t, errs.Error(), "EnableCelExpressions feature gate")
	})

	t.Run("not opted in", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, feature.DefaultFeatureGate, features.EnableCelExpressions, true)
		errs := h.ValidateWorkflow(context.Background(), conditionRun(nil)).ToAggregate()
		require.Error(t, errs)
		require.Contains(t, errs.Error(), types.AnnotationCelExpressions)
	})

	t.Run("a sub-step condition", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, feature.DefaultFeatureGate, features.EnableCelExpressions, true)
		run := conditionRun(nil)
		run.Spec.WorkflowSpec.Steps = append(run.Spec.WorkflowSpec.Steps, oamv1alpha1.WorkflowStep{
			WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "group", Type: "step-group"},
			SubSteps:         []oamv1alpha1.WorkflowStepBase{{Name: "inner", Type: "suspend", If: "$(steps.a.succeeded)"}},
		})
		errs := h.ValidateWorkflow(context.Background(), run).ToAggregate()
		require.Error(t, errs)
		require.Contains(t, errs.Error(), "spec.workflowSpec.steps[2].subSteps[0].if")
	})

	t.Run("opted in, the condition is admitted", func(t *testing.T) {
		featuregatetesting.SetFeatureGateDuringTest(t, feature.DefaultFeatureGate, features.EnableCelExpressions, true)
		require.Empty(t, h.ValidateWorkflow(context.Background(), conditionRun(map[string]string{types.AnnotationCelExpressions: "true"})))
	})
}
