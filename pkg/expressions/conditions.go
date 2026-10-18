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
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	v1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/cel"
	"github.com/kubevela/pkg/cel/template"

	wfv1alpha1 "github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/types"
)

// conditionKey is where a condition sits in the one-field tree it is planned as.
const conditionKey = "if"

// IsCondition reports whether a step's if is a $( ) condition: exactly one
// expression, the whole of the value. Any other if is CUE, as it always was; a
// "$(" inside a CUE string does not make one.
func IsCondition(cond string) bool {
	parsed, err := template.Parse(strings.TrimSpace(cond))
	if err != nil || !parsed.HasExpr() {
		return false
	}
	_, sole := parsed.SoleExpr()
	return sole
}

// conditionTree is a condition as the one-field tree its expression is planned
// and evaluated in, the way a property is.
func conditionTree(cond string) ([]byte, error) {
	return json.Marshal(map[string]string{conditionKey: strings.TrimSpace(cond)})
}

// Condition evaluates a step's $( ) if, reading what the step's properties may.
// It must be a bool. A read still to be answered is cel.ErrNotReady, so the step
// waits rather than being skipped.
func (e *Evaluator) Condition(ctx context.Context, step types.ExpressionStep, cond string, wfCtx wfContext.Context,
	pCtx process.Context, status map[string]wfv1alpha1.StepStatus) (bool, error) {
	if !IsCondition(cond) {
		return false, fmt.Errorf("%q is not a $( ) condition", cond)
	}
	raw, err := conditionTree(cond)
	if err != nil {
		return false, err
	}
	plan, err := e.engine.PlanJSON(raw)
	if err != nil {
		return false, err
	}
	out, err := plan.Eval(withRun(ctx, RunScope{Run: e.run, Step: step.Name, StepType: step.Type, Store: wfCtx}),
		e.resolvers(wfCtx, pCtx, status), cel.TreeOptions{})
	if err != nil {
		return false, err
	}
	v := out.(map[string]interface{})[conditionKey]
	b, ok := v.(bool)
	if !ok {
		return false, fmt.Errorf("%s is %s, but a step's if must be a bool", strings.TrimSpace(cond), kindName(v))
	}
	return b, nil
}

// kindName names a resolved value's type the way a refusal reads.
func kindName(v interface{}) string {
	switch v.(type) {
	case string:
		return "string"
	case bool:
		return "bool"
	case int64, int, float64:
		return "a number"
	case []interface{}:
		return "a list"
	case map[string]interface{}:
		return "a map"
	case nil:
		return "null"
	}
	return fmt.Sprintf("%T", v)
}

// ConditionsWithoutExpressions refuses every $( ) if, on a step or a sub-step,
// of a run that does not read expressions, with why as the reason. Without an
// evaluator the engine reads such an if as CUE, which it is not, and skips the
// step.
func ConditionsWithoutExpressions(stepsPath *field.Path, steps []v1alpha1.WorkflowStep, why string) field.ErrorList {
	var errs field.ErrorList
	for i, step := range steps {
		if IsCondition(step.If) {
			errs = append(errs, field.Forbidden(stepsPath.Index(i).Child("if"), why))
		}
		for j, sub := range step.SubSteps {
			if IsCondition(sub.If) {
				errs = append(errs, field.Forbidden(stepsPath.Index(i).Child("subSteps").Index(j).Child("if"), why))
			}
		}
	}
	return errs
}

// misplacedConditionMessage refuses an if holding a $( ) that is not the whole
// of it.
const misplacedConditionMessage = "a $( ) condition must be the whole if, one expression: " +
	"write $(a && b), not $(a) && $(b)"

// MisplacedConditions refuses every if, on a step or a sub-step, that holds a
// $( ) outside a CUE string without being one $( ) expression. Such an if is
// not a condition, and as CUE it never compiles, so the step would be skipped.
func MisplacedConditions(stepsPath *field.Path, steps []v1alpha1.WorkflowStep) field.ErrorList {
	var errs field.ErrorList
	for i, step := range steps {
		if MisplacedCondition(step.If) {
			errs = append(errs, field.Invalid(stepsPath.Index(i).Child("if"), step.If, misplacedConditionMessage))
		}
		for j, sub := range step.SubSteps {
			if MisplacedCondition(sub.If) {
				errs = append(errs, field.Invalid(stepsPath.Index(i).Child("subSteps").Index(j).Child("if"),
					sub.If, misplacedConditionMessage))
			}
		}
	}
	return errs
}

// MisplacedCondition reports an if with a $( ) outside its CUE strings and
// comments that is not one $( ) expression. A $$( is an escape, not an expression.
func MisplacedCondition(cond string) bool {
	return !IsCondition(cond) && expressionOutsideStrings(cond)
}

// expressionOutsideStrings reports a $( ) in CUE source outside its string and
// bytes literals and its comments.
func expressionOutsideStrings(src string) bool {
	for i := 0; i < len(src); i++ {
		switch c := src[i]; c {
		case '"', '\'':
			i = endOfLiteral(src, i, 0)
		case '#':
			hashes := len(src[i:]) - len(strings.TrimLeft(src[i:], "#"))
			if next := i + hashes; next < len(src) && (src[next] == '"' || src[next] == '\'') {
				i = endOfLiteral(src, next, hashes)
			} else {
				i = next - 1
			}
		case '/':
			if strings.HasPrefix(src[i:], "//") {
				if nl := strings.IndexByte(src[i:], '\n'); nl >= 0 {
					i += nl
				} else {
					return false
				}
			}
		case '$':
			if strings.HasPrefix(src[i:], "$$(") {
				i += 2
				continue
			}
			if strings.HasPrefix(src[i:], "$(") {
				return true
			}
		}
	}
	return false
}

// endOfLiteral returns the index of the last byte of the literal whose opening
// quote is at src[start], or the last index when it is unclosed. The literal is
// single-line or, opened with three quotes, multiline; with hashes before its
// quote it is raw, closed by the quote and as many hashes, with no escapes.
func endOfLiteral(src string, start, hashes int) int {
	quote := src[start : start+1]
	if strings.HasPrefix(src[start:], strings.Repeat(quote, 3)) {
		quote = strings.Repeat(quote, 3)
	}
	closing := quote + strings.Repeat("#", hashes)
	for i := start + len(quote); i < len(src); i++ {
		if hashes == 0 && src[i] == '\\' {
			i++
			continue
		}
		if strings.HasPrefix(src[i:], closing) {
			return i + len(closing) - 1
		}
	}
	return len(src) - 1
}
