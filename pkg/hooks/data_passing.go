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
	"encoding/json"
	"fmt"
	"strings"

	"cuelang.org/go/cue"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	wfTypes "github.com/kubevela/workflow/pkg/types"
)

// Input set data to parameter.
func Input(ctx wfContext.Context, paramValue cue.Value, step v1alpha1.WorkflowStep) (cue.Value, error) {
	filledVal := paramValue
	for _, input := range step.Inputs {
		inputValue, err := ctx.GetVar(strings.Split(input.From, ".")...)
		if err != nil {
			inputValue, err = value.LookupValueByScript(paramValue, input.From)
			if err != nil {
				return filledVal, errors.WithMessagef(err, "get input from [%s]", input.From)
			}
		}
		if input.ParameterKey != "" {
			filledVal, err = value.SetValueByScript(filledVal, inputValue, strings.Join([]string{"parameter", input.ParameterKey}, "."))
			if err != nil || filledVal.Err() != nil {
				if err != nil {
					return paramValue, err
				}
				if filledVal.Err() != nil {
					return filledVal, filledVal.Err()
				}
			}
		}
	}
	return filledVal, nil
}

// Output get data from task value.
func Output(ctx wfContext.Context, taskValue cue.Value, step v1alpha1.WorkflowStep, status v1alpha1.StepStatus, stepStatus map[string]v1alpha1.StepStatus) error {
	errMsg := ""
	if wfTypes.IsStepFinish(status.Phase, status.Reason) {
		SetAdditionalNameInStatus(stepStatus, step.Name, step.Properties, status)
		for _, output := range step.Outputs {
			v, err := value.LookupValueByScript(taskValue, output.ValueFrom)
			// if the error is not nil and the step is not skipped, return the error
			if err != nil && status.Phase != v1alpha1.WorkflowStepPhaseSkipped {
				errMsg += fmt.Sprintf("failed to get output from %s: %s\n", output.ValueFrom, err.Error())
			}
			// if the error is not nil, set the value to null
			if err != nil || v.Err() != nil {
				v = taskValue.Context().CompileString("null")
			}
			if err := ctx.SetVar(v, output.Name); err != nil {
				errMsg += fmt.Sprintf("failed to set output %s: %s\n", output.Name, err.Error())
			}
		}
	}

	if errMsg != "" {
		return errors.New(errMsg)
	}
	return nil
}

// SetAdditionalNameInStatus sets additional name from properties to status map
func SetAdditionalNameInStatus(stepStatus map[string]v1alpha1.StepStatus, name string, properties *runtime.RawExtension, status v1alpha1.StepStatus) { //nolint:revive,unused
	if stepStatus == nil || properties == nil {
		return
	}
	o := struct {
		Name      string `json:"name"`
		Component string `json:"component"`
	}{}
	js, err := properties.MarshalJSON()
	if err != nil {
		return
	}
	if err := json.Unmarshal(js, &o); err != nil {
		return
	}
	additionalName := ""
	switch {
	case o.Name != "":
		additionalName = o.Name
	case o.Component != "":
		additionalName = o.Component
	default:
		return
	}
	if _, ok := stepStatus[additionalName]; !ok {
		stepStatus[additionalName] = status
		return
	}
}
