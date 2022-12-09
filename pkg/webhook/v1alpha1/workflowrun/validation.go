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

package workflowrun

import (
	"context"
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/workflow/api/v1alpha1"
)

// ValidateWorkflow validates the Application workflow
func (h *ValidatingHandler) ValidateWorkflow(ctx context.Context, wr *v1alpha1.WorkflowRun) field.ErrorList {
	var errs field.ErrorList
	var steps []v1alpha1.WorkflowStep
	if wr.Spec.WorkflowSpec != nil {
		steps = wr.Spec.WorkflowSpec.Steps
	} else {
		w := &v1alpha1.Workflow{}
		if err := h.Client.Get(ctx, client.ObjectKey{Namespace: wr.Namespace, Name: wr.Spec.WorkflowRef}, w); err != nil {
			errs = append(errs, field.Invalid(field.NewPath("spec", "workflowRef"), wr.Spec.WorkflowRef, fmt.Sprintf("failed to get workflow ref: %v", err)))
			return errs
		}
		steps = w.Steps
	}
	stepName := make(map[string]interface{})
	for _, step := range steps {
		if step.Name == "" {
			errs = append(errs, field.Invalid(field.NewPath("spec", "workflowSpec", "steps", "name"), step.Name, "empty step name"))
		}
		if _, ok := stepName[step.Name]; ok {
			errs = append(errs, field.Invalid(field.NewPath("spec", "workflowSpec", "steps"), step.Name, "duplicated step name"))
		}
		stepName[step.Name] = nil
		if step.Timeout != "" {
			errs = append(errs, h.ValidateTimeout(step.Name, step.Timeout)...)
		}
		for _, sub := range step.SubSteps {
			if sub.Name == "" {
				errs = append(errs, field.Invalid(field.NewPath("spec", "workflowSpec", "steps", "subSteps", "name"), sub.Name, "empty step name"))
			}
			if _, ok := stepName[sub.Name]; ok {
				errs = append(errs, field.Invalid(field.NewPath("spec", "workflowSpec", "steps", "subSteps"), sub.Name, "duplicated step name"))
			}
			stepName[sub.Name] = nil
			if step.Timeout != "" {
				errs = append(errs, h.ValidateTimeout(step.Name, step.Timeout)...)
			}
		}
	}
	return errs
}

// ValidateTimeout validates the timeout of steps
func (h *ValidatingHandler) ValidateTimeout(name, timeout string) field.ErrorList {
	var errs field.ErrorList
	_, err := time.ParseDuration(timeout)
	if err != nil {
		errs = append(errs, field.Invalid(field.NewPath("spec", "workflowSpec", "steps", "timeout"), name, "invalid timeout, please use the format of timeout like 1s, 1m, 1h or 1d"))
	}
	return errs
}
