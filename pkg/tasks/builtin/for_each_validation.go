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

package builtin

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/types"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/pkg/cel/template"
)

// ValidateForEachSteps checks the forEach of each step at admission. base is the path of
// the steps list. allowExpression lets forEach.items be a single $( ) expression, which
// only a caller that resolves expressions before the workflow runs can accept.
func ValidateForEachSteps(base *field.Path, steps []oamv1alpha1.WorkflowStep, allowExpression bool) field.ErrorList {
	var errs field.ErrorList
	for i, step := range steps {
		if step.ForEach == nil {
			continue
		}
		path := base.Index(i)
		errs = append(errs, validateForEach(path.Child("forEach"), step, allowExpression)...)
		errs = append(errs, validateBodyOutputs(path, step)...)
		errs = append(errs, validateGeneratedNames(base, steps, i)...)
		errs = append(errs, validateLoopNames(base, steps, i)...)
	}
	return errs
}

// validateLoopNames rejects a forEach step named <other>-<index>..., or <other>-...
// for a keyed loop, after another forEach step. Only then can the two loops generate the
// same step names, or the other loop's restart clear this one's variables.
func validateLoopNames(base *field.Path, steps []oamv1alpha1.WorkflowStep, loop int) field.ErrorList {
	var errs field.ErrorList
	for i, other := range steps {
		if i == loop || other.ForEach == nil {
			continue
		}
		_, _, indexed := cutIndex(other.Name, steps[loop].Name)
		keyed := len(other.ForEach.Key) > 0 && strings.HasPrefix(steps[loop].Name, other.Name+"-")
		if indexed || keyed {
			errs = append(errs, field.Invalid(base.Index(loop).Child("name"), steps[loop].Name,
				fmt.Sprintf("is named like the passes of forEach step %s", other.Name)))
		}
	}
	return errs
}

func validateForEach(path *field.Path, step oamv1alpha1.WorkflowStep, allowExpression bool) field.ErrorList {
	var errs field.ErrorList
	forEach := step.ForEach
	if (forEach.Items != nil) == (forEach.From != "") {
		errs = append(errs, field.Invalid(path, step.Name, "needs exactly one of items and from"))
	}
	switch forEach.Mode {
	case "", v1alpha1.WorkflowModeDAG, v1alpha1.WorkflowModeStep:
	default:
		errs = append(errs, field.NotSupported(path.Child("mode"), forEach.Mode,
			[]string{string(v1alpha1.WorkflowModeStep), string(v1alpha1.WorkflowModeDAG)}))
	}
	seenKeys := map[string]bool{}
	for j, key := range forEach.Key {
		keyPath := path.Child("key").Index(j)
		if key == "" || strings.HasPrefix(key, ".") || strings.HasSuffix(key, ".") || strings.Contains(key, "..") {
			errs = append(errs, field.Invalid(keyPath, key, "is not a path into the item"))
			continue
		}
		if seenKeys[key] {
			errs = append(errs, field.Duplicate(keyPath, key))
		}
		seenKeys[key] = true
	}
	if forEach.Items == nil {
		return errs
	}
	itemsPath := path.Child("items")
	var items []any
	if err := json.Unmarshal(forEach.Items.Raw, &items); err == nil && items != nil {
		if len(items) > types.MaxForEachItems {
			errs = append(errs, field.Invalid(itemsPath, len(items), fmt.Sprintf("has %d items, over the limit of %d", len(items), types.MaxForEachItems)))
		}
		return errs
	}
	var expr string
	isExpr := json.Unmarshal(forEach.Items.Raw, &expr) == nil && strings.Contains(expr, "$(")
	switch {
	case isExpr && !soleExpression(expr):
		// Text around an expression, or two of them, resolve to a string.
		errs = append(errs, field.Invalid(itemsPath, expr, "must be a list or one $( ) expression that resolves to one"))
	case isExpr && allowExpression:
	case isExpr:
		errs = append(errs, field.Invalid(itemsPath, expr, "must be a list; a $( ) expression here needs expressions turned on: "+
			`the EnableCelExpressions feature gate, and workflowrun.oam.dev/cel-expressions: "true" on the WorkflowRun `+
			`or app.oam.dev/cel-expressions: "true" on the Application`))
	default:
		errs = append(errs, field.Invalid(itemsPath, string(forEach.Items.Raw), "must be a list"))
	}
	return errs
}

func validateBodyOutputs(path *field.Path, step oamv1alpha1.WorkflowStep) field.ErrorList {
	var errs field.ErrorList
	check := func(p *field.Path, outputs oamv1alpha1.StepOutputs) {
		for j, o := range outputs {
			if o.Name == eachRoot {
				errs = append(errs, field.Invalid(p.Child("outputs").Index(j).Child("name"), o.Name,
					fmt.Sprintf("%q is reserved inside a forEach, where inputs read the current item from it", eachRoot)))
			}
		}
	}
	if _, group := body(step); group {
		if len(step.Outputs) > 0 {
			errs = append(errs, field.Invalid(path.Child("outputs"), step.Name,
				"a step-group with forEach has no outputs of its own; declare them on its sub-steps, whose outputs are collected into lists"))
		}
		for j, sub := range step.SubSteps {
			check(path.Child("subSteps").Index(j), sub.Outputs)
		}
		return errs
	}
	check(path, step.Outputs)
	return errs
}

// validateGeneratedNames rejects any other step whose name one of loop's iterations
// could take, since step status is keyed by name, and any output named like a variable
// its iterations keep. A keyed loop's names are known only once its list resolves, so
// every name it could generate is reserved.
func validateGeneratedNames(base *field.Path, steps []oamv1alpha1.WorkflowStep, loop int) field.ErrorList {
	step := steps[loop]
	label := `(0|[1-9][0-9]*)`
	if len(step.ForEach.Key) > 0 {
		label = `(` + keySyntax + `)`
	}
	prefix := regexp.QuoteMeta(step.Name) + `-` + label
	vars := []string{eachRoot}
	for _, name := range CollectedOutputs(step) {
		vars = append(vars, regexp.QuoteMeta(name))
	}
	varPattern := regexp.MustCompile("^" + prefix + "-(" + strings.Join(vars, "|") + ")$")
	pattern := regexp.MustCompile("^" + prefix + "$")
	if _, group := body(step); group {
		subs := make([]string, 0, len(step.SubSteps))
		for _, sub := range step.SubSteps {
			subs = append(subs, regexp.QuoteMeta(sub.Name))
		}
		pattern = regexp.MustCompile("^" + prefix + "-(" + strings.Join(subs, "|") + ")$")
	}
	var errs field.ErrorList
	clash := func(p *field.Path, name string) {
		if pattern.MatchString(name) {
			errs = append(errs, field.Invalid(p.Child("name"), name, fmt.Sprintf("clashes with the names forEach step %s generates", step.Name)))
		}
	}
	// A restart of the loop clears every variable its passes could have written.
	clashVar := func(p *field.Path, outputs oamv1alpha1.StepOutputs) {
		for j, o := range outputs {
			if varPattern.MatchString(o.Name) {
				errs = append(errs, field.Invalid(p.Child("outputs").Index(j).Child("name"), o.Name,
					fmt.Sprintf("clashes with the variables forEach step %s keeps for its passes", step.Name)))
			}
		}
	}
	for i, other := range steps {
		if i == loop {
			continue
		}
		clash(base.Index(i), other.Name)
		clashVar(base.Index(i), other.Outputs)
		for j, sub := range other.SubSteps {
			clash(base.Index(i).Child("subSteps").Index(j), sub.Name)
			clashVar(base.Index(i).Child("subSteps").Index(j), sub.Outputs)
		}
	}
	return errs
}

// soleExpression reports whether a value is exactly one $( ) expression, as the
// engine reads it: whitespace around one is not text.
func soleExpression(v string) bool {
	parsed, err := template.Parse(v)
	if err != nil {
		return false
	}
	_, sole := parsed.SoleExpr()
	return sole
}
