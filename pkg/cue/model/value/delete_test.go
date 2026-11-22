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

package value

import (
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/require"

	"github.com/kubevela/workflow/pkg/cue/model/sets"
)

func TestDeletePath(t *testing.T) {
	testCases := map[string]struct {
		base    string
		paths   []string
		gone    []string
		present []string
	}{
		"top level field": {
			base:    `{a: "keep", b: "drop"}`,
			paths:   []string{"b"},
			gone:    []string{"drop"},
			present: []string{"keep"},
		},
		"nested field leaves its siblings and its parent": {
			base:    `{creds: {host: "keep", password: "drop"}}`,
			paths:   []string{"creds", "password"},
			gone:    []string{"drop"},
			present: []string{"keep", "creds"},
		},
		"whole subtree": {
			base:    `{creds: {host: "drop-too", password: "drop"}, other: "keep"}`,
			paths:   []string{"creds"},
			gone:    []string{"drop", "drop-too"},
			present: []string{"keep"},
		},
		"quoted label": {
			base:    `{"api-key": "drop", other: "keep"}`,
			paths:   []string{"api-key"},
			gone:    []string{"drop"},
			present: []string{"keep"},
		},
		"a path that is not there changes nothing": {
			base:    `{a: "keep"}`,
			paths:   []string{"nope"},
			present: []string{"keep"},
		},
		"a path through something that is not there changes nothing": {
			base:    `{a: "keep"}`,
			paths:   []string{"nope", "deeper"},
			present: []string{"keep"},
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			out, err := DeletePath(cuecontext.New().CompileString(tc.base), tc.paths...)
			r.NoError(err)
			s, err := sets.ToString(out)
			r.NoError(err)
			for _, want := range tc.present {
				r.Contains(s, want)
			}
			for _, unwanted := range tc.gone {
				r.NotContains(s, unwanted)
			}
			// The field itself is gone, not merely emptied.
			r.False(out.LookupPath(FieldPath(tc.paths...)).Exists() && len(tc.gone) > 0)
		})
	}
}

// Deleting nothing is not an error, so a caller need not special-case it.
func TestDeletePathWithNoPaths(t *testing.T) {
	r := require.New(t)
	in := cuecontext.New().CompileString(`{a: "keep"}`)
	out, err := DeletePath(in)
	r.NoError(err)
	r.True(out.LookupPath(FieldPath("a")).Exists())
}

// A value that is not concrete survives, which a decode through JSON would not
// allow. Step outputs are concrete, but vars are not required to be.
func TestDeletePathKeepsNonConcreteValues(t *testing.T) {
	r := require.New(t)
	out, err := DeletePath(cuecontext.New().CompileString(`{a: string, b: "drop"}`), "b")
	r.NoError(err)
	r.NoError(out.Err())
	r.True(out.LookupPath(FieldPath("a")).Exists())
	r.False(out.LookupPath(FieldPath("b")).Exists())
}
