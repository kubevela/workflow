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

package value

import (
	"encoding/json"
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/kubevela/pkg/cue/util"

	"github.com/stretchr/testify/require"
)

func TestValueFill(t *testing.T) {
	r := require.New(t)
	src := `
object: {
	x: int
    y: string
    z: {
       provider: string
       do: string
    }
}
`
	cuectx := cuecontext.New()
	testVal1 := cuectx.CompileString(src)

	expectedValString := `object: {
	x: 12
	y: "y_string"
	z: {
		provider: "kube"
		do:       "apply"
	}
}`
	res, err := FillRaw(testVal1, expectedValString, "")
	r.NoError(err)
	val2String, err := util.ToString(res)
	r.NoError(err)
	r.Equal(val2String, expectedValString)
}

func TestFieldPath(t *testing.T) {
	testCases := []struct {
		paths    []string
		expected cue.Path
	}{
		{
			paths:    []string{""},
			expected: cue.ParsePath(""),
		},
		{
			paths:    []string{`a.b`},
			expected: cue.ParsePath("a.b"),
		},
		{
			paths:    []string{`a[0]`},
			expected: cue.ParsePath("a[0]"),
		},
		{
			paths:    []string{`_a`},
			expected: cue.ParsePath("_a"),
		},
		{
			paths:    []string{`#a`},
			expected: cue.ParsePath("#a"),
		},
		{
			paths:    []string{`a`},
			expected: cue.ParsePath(`"a"`),
		},
		{
			paths:    []string{`"1"`},
			expected: cue.MakePath(cue.Str("1")),
		},
		{
			paths:    []string{`1`},
			expected: cue.MakePath(cue.Str("1")),
		},
		{
			paths:    []string{`1`, `"#a"`, `b`},
			expected: cue.ParsePath(`"1".#a["b"]`),
		},
	}
	for i, tc := range testCases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			r := require.New(t)
			fp := FieldPath(tc.paths...)
			r.Equal(tc.expected, fp)
		})
	}
}

// func TestValueFix(t *testing.T) {
// 	testCases := []struct {
// 		original string
// 		expected string
// 	}{
// 		{
// 			original: `
// parameter: test: _
// // comment
// y: {
// 	for k, v in parameter.test.p {
// 		"\(k)": v
// 	}
// }`,
// 			expected: `{
// 	parameter: {
// 		test: _
// 	}
// 	// comment
// 	y: {
// 		for k, v in *parameter.test.p | {} {
// 			"\(k)": v
// 		}
// 	}
// }`,
// 		},
// 	}
// 	for i, tc := range testCases {
// 		t.Run(fmt.Sprint(i), func(t *testing.T) {
// 			r := require.New(t)
// 			v, err := NewValue(tc.original, nil, "")
// 			r.NoError(err)
// 			b, err := format.Node(v.CueValue().Syntax(cue.Docs(true)))
// 			r.NoError(err)
// 			r.Equal(tc.expected, string(b))
// 		})
// 	}
// }

func TestSetByScript(t *testing.T) {
	testCases := []struct {
		name     string
		raw      string
		path     string
		v        string
		expected string
	}{
		{
			name:     "insert array",
			raw:      `a: ["hello"]`,
			path:     "a[0]",
			v:        `"world"`,
			expected: `a: ["world"]`},
		{
			name:     "insert array2",
			raw:      `a: ["hello"]`,
			path:     "a[1]",
			v:        `"world"`,
			expected: `a: ["hello", "world"]`},
		{
			name: "insert array3",
			raw:  `a: b: [{x: 100}]`,
			path: "a.b[0]",
			v:    `{name: "foo"}`,
			expected: `a: b: [{
	name: "foo"
}]`},
		{
			name:     "insert struct",
			raw:      `a: {b: "hello"}`,
			path:     "a.b",
			v:        `"world"`,
			expected: `a: b: "world"`},
		{
			name: "insert struct2",
			raw:  `a: {b: "hello"}, c: {d: "world"}`,
			path: "c.d",
			v:    `"hello"`,
			expected: `a: b: "hello"
c: d: "hello"`},
		{
			name: "insert array to array",
			raw: `
a: b: c: [{x: 100}, {x: 101}, {x: 102}]`,
			path: "a.b.c[0].value",
			v:    `"foo"`,
			expected: `a: b: c: [{
	x:     100
	value: "foo"
}, {
	x: 101
}, {
	x: 102
}]`,
		},
		{
			name: "insert nest array ",
			raw:  `a: b: [{x: y:[{name: "key"}]}]`,
			path: "a.b[0].x.y[0].value",
			v:    `"foo"`,
			expected: `a: b: [{
	x: y: [{
		name:  "key"
		value: "foo"
	}]
}]`,
		},
		{
			name: "insert without array",
			raw:  `a: b: [{x: y:[{name: "key"}]}]`,
			path: "a.c.x",
			v:    `"foo"`,
			expected: `a: {
	b: [{
		x: y: [{
			name: "key"
		}]
	}]
	c: x: "foo"
}`,
		},
		{
			name: "path with string index",
			raw:  `a: b: [{x: y:[{name: "key"}]}]`,
			path: "a.c[\"x\"]",
			v:    `"foo"`,
			expected: `a: {
	b: [{
		x: y: [{
			name: "key"
		}]
	}]
	c: x: "foo"
}`,
		},
	}

	for _, tCase := range testCases {
		r := require.New(t)
		cuectx := cuecontext.New()
		base := cuectx.CompileString(tCase.raw)
		new := cuectx.CompileString(tCase.v)
		res, err := SetValueByScript(base, new, tCase.path)
		r.NoError(err, tCase.name)
		s, err := util.ToString(res)
		r.NoError(err)
		r.Equal(s, tCase.expected, tCase.name)
	}
}

func TestUnmarshal(t *testing.T) {
	case1 := `
provider: "kube"
do: "apply"
`
	out := struct {
		Provider string `json:"provider"`
		Do       string `json:"do"`
	}{}

	r := require.New(t)
	cuectx := cuecontext.New()
	val := cuectx.CompileString(case1)
	err := UnmarshalTo(val, &out)
	r.NoError(err)
	r.Equal(out.Provider, "kube")
	r.Equal(out.Do, "apply")

	bt, err := val.MarshalJSON()
	r.NoError(err)
	expectedJson, err := json.Marshal(out)
	r.NoError(err)
	r.Equal(string(bt), string(expectedJson))

	caseIncomplete := `
provider: string
do: string
`
	val = cuectx.CompileString(caseIncomplete)
	r.NoError(err)
	err = UnmarshalTo(val, &out)
	r.Error(err)
}
