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
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/pkg/errors"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"

	"github.com/kubevela/workflow/pkg/types"
)

// maxKeyLength bounds a joined key, which becomes part of step and variable names.
const maxKeyLength = 63

// keyPattern is what a joined key may hold. No '.', since inputs[].from splits a
// variable path on it, so a key holding one would read a different variable.
var keyPattern = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9_-]*[A-Za-z0-9])?$`)

// itemLabels returns each item's key, its forEach.key values joined with "-", or nil
// when the loop has no key and is named by index. A key must be valid in a step name,
// and every name the loop generates must belong to one item only: a key is never
// rewritten to make it fit, since two items rewritten to one name would share status.
func itemLabels(step oamv1alpha1.WorkflowStep, items []any) ([]string, error) {
	key := step.ForEach.Key
	if len(key) == 0 {
		return nil, nil
	}
	labels := make([]string, len(items))
	for i, item := range items {
		parts := make([]string, 0, len(key))
		for _, path := range key {
			v, ok := lookupItem(item, path)
			if !ok {
				return nil, errors.Errorf("item %d has no value at key %s", i, path)
			}
			part, ok := keyPart(v)
			if !ok {
				return nil, errors.Errorf("item %d: key %s must be a string or an integer, got %s", i, path, describeItem(v))
			}
			parts = append(parts, part)
		}
		label := strings.Join(parts, "-")
		if len(label) > maxKeyLength {
			return nil, errors.Errorf("item %d: key %q is over %d characters", i, label, maxKeyLength)
		}
		if !keyPattern.MatchString(label) {
			return nil, errors.Errorf("item %d: key %q may only hold letters, digits, '-' and '_', and start and end with a letter or digit", i, label)
		}
		labels[i] = label
	}

	seen := map[string]int{}
	for i, label := range labels {
		if j, ok := seen[label]; ok {
			return nil, errors.Errorf("items %d and %d have the same key %q; pick key fields that tell them apart", j, i, label)
		}
		seen[label] = i
	}
	owner := map[string]int{}
	for i, label := range labels {
		for _, name := range generatedNames(step, label) {
			if j, ok := owner[name]; ok {
				return nil, errors.Errorf("items %d and %d both name a step %s; pick key fields that tell them apart", j, i, name)
			}
			owner[name] = i
		}
	}
	return labels, nil
}

// generatedNames is every step and variable name the item labelled label takes.
func generatedNames(step oamv1alpha1.WorkflowStep, label string) []string {
	template, group := body(step)
	names := []string{types.IterationName(step.Name, label, eachRoot)}
	if !group {
		names = append(names, step.Name+"-"+label)
	}
	for _, sub := range template {
		if group {
			names = append(names, types.IterationName(step.Name, label, sub.Name))
		}
		for _, o := range sub.Outputs {
			names = append(names, types.IterationName(step.Name, label, o.Name))
		}
	}
	return names
}

// lookupItem reads the value at a dotted path in an item.
func lookupItem(item any, path string) (any, bool) {
	v := item
	for _, segment := range strings.Split(path, ".") {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		if v, ok = m[segment]; !ok {
			return nil, false
		}
	}
	return v, true
}

// keyPart renders a key value: a string as it is, an integer in decimal.
func keyPart(v any) (string, bool) {
	switch n := v.(type) {
	case string:
		return n, true
	case float64:
		if n != math.Trunc(n) || math.Abs(n) > 1<<53 {
			return "", false
		}
		return strconv.FormatInt(int64(n), 10), true
	case int:
		return strconv.Itoa(n), true
	case int64:
		return strconv.FormatInt(n, 10), true
	}
	return "", false
}
