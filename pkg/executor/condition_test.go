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

package executor

import (
	"errors"
	"fmt"
	"testing"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/cel"
	"github.com/stretchr/testify/require"

	"github.com/kubevela/workflow/pkg/types"
)

func TestConditionPreCheck(t *testing.T) {
	step := func(cond string) oamv1alpha1.WorkflowStep {
		return oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: "notify", If: cond}}
	}
	answer := func(run bool, err error) *types.PreCheckOptions {
		return &types.PreCheckOptions{Condition: func() (bool, error) { return run, err }}
	}
	for _, tc := range []struct {
		name    string
		cond    string
		options *types.PreCheckOptions
		handled bool
		want    *types.PreCheckResult
		err     string
	}{
		{"true runs the step", "$(steps.build.succeeded)", answer(true, nil), true, &types.PreCheckResult{}, ""},
		{"false skips it", "$(steps.build.succeeded)", answer(false, nil), true, &types.PreCheckResult{Skip: true}, ""},
		{"a read to be answered holds it back", "$(steps.build.succeeded)",
			answer(false, fmt.Errorf("steps.build.succeeded: %w", cel.ErrNotReady)), true,
			&types.PreCheckResult{Wait: true, Message: "steps.build.succeeded: " + cel.ErrNotReady.Error()}, ""},
		{"an error skips it, saying why", "$(steps.build.phase)", answer(false, errors.New("must be a bool")), true,
			&types.PreCheckResult{Skip: true}, "must be a bool"},
		{"a CUE if is left to CUE", "status.build.succeeded", answer(true, nil), false, nil, ""},
		{"a $( ) inside a CUE string is CUE", `inputs.msg == "cost $(5)"`, answer(true, nil), false, nil, ""},
		{"without an evaluator, a $( ) if fails the step rather than being read as CUE", "$(steps.build.succeeded)",
			&types.PreCheckOptions{}, true, &types.PreCheckResult{Fail: true, Message: conditionWithoutExpressionsMessage}, ""},
		{"no options", "$(steps.build.succeeded)", nil, true,
			&types.PreCheckResult{Fail: true, Message: conditionWithoutExpressionsMessage}, ""},
		{"no options, a CUE if is left to CUE", "status.build.succeeded", nil, false, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, handled, err := conditionPreCheck(step(tc.cond), tc.options)
			require.Equal(t, tc.handled, handled)
			require.Equal(t, tc.want, got)
			if tc.err == "" {
				require.NoError(t, err)
			} else {
				require.ErrorContains(t, err, tc.err)
			}
		})
	}
}
