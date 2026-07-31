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
	"github.com/kubevela/workflow/pkg/cue/sensitive"
	wfTypes "github.com/kubevela/workflow/pkg/types"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
)

// Input set data to parameter.
func Input(ctx wfContext.Context, paramValue cue.Value, step oamv1alpha1.WorkflowStep) (cue.Value, error) {
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

// OutputWithSensitive builds the post-stop hook for a step whose template has
// already been read for `// +sensitive` markers.
//
// sensitivePaths are the template paths the definition author marked, absolute
// from the template root so they compare directly against an output's
// valueFrom. extractErr is whatever reading those markers failed with; a step
// whose markers could not be read routes every output to the Secret, because
// "I could not tell" must not be mistaken for "nothing is secret".
//
// The marker is read from the template text, which the hook never sees -- its
// taskValue is the evaluated result, and comments do not survive evaluation --
// so the paths are derived where the template is loaded and closed over here.
// That is also what keeps the parse off the per-run path.
func OutputWithSensitive(sensitivePaths []string, extractErr error) wfTypes.TaskPostStopHook {
	return func(ctx wfContext.Context, taskValue cue.Value, step oamv1alpha1.WorkflowStep, status v1alpha1.StepStatus, stepStatus map[string]v1alpha1.StepStatus) error {
		return output(ctx, taskValue, step, status, stepStatus, sensitivePaths, extractErr)
	}
}

// isSensitiveOutput reports whether an output has to be kept out of the
// plaintext context ConfigMap.
//
// Three ways to be sensitive, in order of how much we know:
//
//   - the markers could not be read at all (extractErr): everything is, since
//     the alternative is leaking a credential we were told about but failed to
//     parse.
//   - valueFrom overlaps a marked path, in either direction. See
//     sensitive.Covers.
//   - the template marked something and valueFrom is an expression rather than
//     a plain path: there is no proving an expression does not embed a marked
//     value, so it fails closed.
func isSensitiveOutput(valueFrom string, sensitivePaths []string, extractErr error) bool {
	if extractErr != nil {
		return true
	}
	if len(sensitivePaths) == 0 {
		return false
	}
	if !sensitive.IsPlainFieldPath(valueFrom) {
		return true
	}
	return sensitive.Covers(valueFrom, sensitivePaths)
}

// Output get data from task value. It is the no-markers case of
// OutputWithSensitive, kept for callers that install the hook without a
// template to read -- a step group, for one, has no template of its own.
func Output(ctx wfContext.Context, taskValue cue.Value, step oamv1alpha1.WorkflowStep, status v1alpha1.StepStatus, stepStatus map[string]v1alpha1.StepStatus) error {
	return output(ctx, taskValue, step, status, stepStatus, nil, nil)
}

func output(ctx wfContext.Context, taskValue cue.Value, step oamv1alpha1.WorkflowStep, status v1alpha1.StepStatus, stepStatus map[string]v1alpha1.StepStatus, sensitivePaths []string, extractErr error) error {
	errMsg := ""
	if wfTypes.IsStepFinish(status.Phase, status.Reason) {
		SetAdditionalNameInStatus(stepStatus, step.Name, step.Properties, status)
		for _, output := range step.Outputs {
			v, err := value.LookupValueByScript(taskValue, output.ValueFrom)
			if (err != nil && strings.Contains(err.Error(), "not exist") || v.Err() != nil) && !strings.Contains(output.ValueFrom, "$returns.") {
				parts := strings.Split(output.ValueFrom, "output.")
				if len(parts) > 1 {
					if parts[0] == "" {
						v, err = value.LookupValueByScript(taskValue, fmt.Sprintf("output.$returns.%s", strings.Join(parts[1:], ".")))
					} else {
						v, err = value.LookupValueByScript(taskValue, fmt.Sprintf("%soutput.$returns.%s", parts[0], strings.Join(parts[1:], ".")))
					}

				} else {
					v, err = value.LookupValueByScript(taskValue, fmt.Sprintf("%s.$returns", output.ValueFrom))
				}
			}
			// if the error is not nil and the step is not skipped, return the error
			if err != nil && status.Phase != v1alpha1.WorkflowStepPhaseSkipped {
				errMsg += fmt.Sprintf("failed to get output from %s: %s\n", output.ValueFrom, err.Error())
			}
			// if the error is not nil, set the value to null
			if err != nil || v.Err() != nil {
				v = taskValue.Context().CompileString("null")
			}
			setVar := ctx.SetVar
			if isSensitiveOutput(output.ValueFrom, sensitivePaths, extractErr) {
				setVar = ctx.SetSensitiveVar
			}
			if err := setVar(v, output.Name); err != nil {
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
