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

// SensitivePathsField is the field a step template can declare in its rendered
// value to mark which paths hold sensitive data (e.g. values read from
// Kubernetes Secrets). Outputs whose valueFrom overlaps a declared path are
// stored via SetSensitiveVar so they never land in the plaintext context
// ConfigMap. Example in a step template:
//
//	$sensitivePaths: ["password", "output.value.data"]
const SensitivePathsField = "$sensitivePaths"

// sensitiveValuePaths reads the template-declared sensitive paths, if any.
func sensitiveValuePaths(taskValue cue.Value) []string {
	v, err := value.LookupValueByScript(taskValue, SensitivePathsField)
	if err != nil || !v.Exists() {
		return nil
	}
	var paths []string
	if err := v.Decode(&paths); err != nil {
		return nil
	}
	return paths
}

// isSensitiveOutput reports whether an output's valueFrom overlaps any declared
// sensitive path: either one is a dot-separated prefix of the other (extracting
// a parent of a sensitive path still includes the sensitive value). When
// sensitive paths are declared but valueFrom is not a plain dotted path (i.e.
// an expression we cannot reason about), it is treated as sensitive: failing
// closed beats leaking a credential to a ConfigMap.
func isSensitiveOutput(valueFrom string, sensitivePaths []string) bool {
	if len(sensitivePaths) == 0 {
		return false
	}
	if !isPlainFieldPath(valueFrom) {
		return true
	}
	for _, p := range sensitivePaths {
		if valueFrom == p || strings.HasPrefix(valueFrom, p+".") || strings.HasPrefix(p, valueFrom+".") {
			return true
		}
	}
	return false
}

// isPlainFieldPath reports whether s looks like a simple dotted field path
// (no expression syntax).
func isPlainFieldPath(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9',
			r == '.', r == '_', r == '-', r == '$':
		default:
			return false
		}
	}
	return true
}

// Output get data from task value.
func Output(ctx wfContext.Context, taskValue cue.Value, step oamv1alpha1.WorkflowStep, status v1alpha1.StepStatus, stepStatus map[string]v1alpha1.StepStatus) error {
	errMsg := ""
	sensitivePaths := sensitiveValuePaths(taskValue)
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
			if isSensitiveOutput(output.ValueFrom, sensitivePaths) {
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
