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

package hooks

import (
	"strings"

	"github.com/pkg/errors"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	wfTypes "github.com/kubevela/workflow/pkg/types"
)

// Input set data to parameter.
func Input(ctx wfContext.Context, paramValue *value.Value, step v1alpha1.WorkflowStep) error {
	for _, input := range step.Inputs {
		inputValue, err := ctx.GetVar(strings.Split(input.From, ".")...)
		if err != nil {
			return errors.WithMessagef(err, "get input from [%s]", input.From)
		}
		if err := paramValue.FillValueByScript(inputValue, input.ParameterKey); err != nil {
			return err
		}
	}
	return nil
}

// Output get data from task value.
func Output(ctx wfContext.Context, taskValue *value.Value, step v1alpha1.WorkflowStep, status v1alpha1.StepStatus) error {
	if wfTypes.IsStepFinish(status.Phase, status.Reason) {
		for _, output := range step.Outputs {
			v, err := taskValue.LookupByScript(output.ValueFrom)
			if err != nil {
				return err
			}
			if v.Error() != nil {
				v, err = taskValue.MakeValue("null")
				if err != nil {
					return err
				}
			}
			if err := ctx.SetVar(v, output.Name); err != nil {
				return err
			}
		}
	}

	return nil
}
