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
	"cuelang.org/go/cue/format"
	cuejson "cuelang.org/go/pkg/encoding/json"
	"github.com/kubevela/workflow/pkg/cue/model/sets"

	"github.com/pkg/errors"
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
	testVal1, err := NewValue(src, nil, "")
	r.NoError(err)
	err = testVal1.FillObject(12, "object", "x")
	r.NoError(err)
	err = testVal1.FillObject("y_string", "object", "y")
	r.NoError(err)
	z, err := testVal1.MakeValue(`
z: {
	provider: string
	do: "apply"
}
`)
	r.NoError(err)
	err = z.FillObject("kube", "z", "provider")
	r.NoError(err)
	err = testVal1.FillObject(z, "object")
	r.NoError(err)
	val1String, err := testVal1.String()
	r.NoError(err)

	expectedValString := `object: {
	x: 12
	y: "y_string"
	z: {
		provider: "kube"
		do:       "apply"
	}
}
`
	r.Equal(val1String, expectedValString)

	testVal2, err := NewValue(src, nil, "")
	r.NoError(err)
	err = testVal2.FillRaw(expectedValString)
	r.NoError(err)
	val2String, err := testVal1.String()
	r.NoError(err)
	r.Equal(val2String, expectedValString)
}

func TestStepByFields(t *testing.T) {
	testCases := []struct {
		base     string
		expected string
	}{
		{base: `
step1: {}
step2: {prefix: step1.value}
step3: {prefix: step2.value}
step4: {prefix: step3.value}
if step4.value > 100 {
        step5: {prefix: step4.value}
} 
`,
			expected: `step1: {
	value: 100
}
step2: {
	prefix: 100
	value:  101
}
step3: {
	prefix: 101
	value:  102
}
step5: {
	prefix: 103
	value:  104
}
step4: {
	prefix: 102
	value:  103
}
`},

		{base: `
step1: {}
step2: {prefix: step1.value}
if step2.value > 100 {
        step2_3: {prefix: step2.value}
}
step3: {prefix: step2.value}
`,
			expected: `step1: {
	value: 100
}
step2: {
	prefix: 100
	value:  101
}
step2_3: {
	prefix: 101
	value:  102
}
step3: {
	prefix: 101
	value:  103
}
`},
	}

	for _, tCase := range testCases {
		r := require.New(t)
		val, err := NewValue(tCase.base, nil, "")
		r.NoError(err)
		number := 99
		err = val.StepByFields(func(_ string, in *Value) (bool, error) {
			number++
			return false, in.FillObject(map[string]interface{}{
				"value": number,
			})
		})
		r.NoError(err)
		str, err := val.String()
		r.NoError(err)
		r.Equal(str, tCase.expected)
	}

	r := require.New(t)
	caseSkip := `
step1: "1"
step2: "2"
step3: "3"
`
	val, err := NewValue(caseSkip, nil, "")
	r.NoError(err)
	inc := 0
	err = val.StepByFields(func(_ string, in *Value) (bool, error) {
		inc++
		s, err := in.CueValue().String()
		r.NoError(err)
		if s == "2" {
			return true, nil
		}
		return false, nil
	})
	r.NoError(err)
	r.Equal(inc, 2)

	inc = 0
	err = val.StepByFields(func(_ string, in *Value) (bool, error) {
		inc++
		s, err := in.CueValue().String()
		r.NoError(err)
		if s == "2" {
			return false, errors.New("mock error")
		}
		return false, nil
	})
	r.Error(err)
	r.Equal(inc, 2)

	inc = 0
	err = val.StepByFields(func(_ string, in *Value) (bool, error) {
		inc++
		s, err := in.CueValue().String()
		r.NoError(err)
		if s == "2" {
			v, err := NewValue("v: 33", nil, "")
			r.NoError(err)
			*in = *v
		}
		return false, nil
	})
	r.Error(err)
	r.Equal(inc, 2)
}

func TestStepWithTag(t *testing.T) {
	testCases := []struct {
		base     string
		expected string
	}{
		{base: `
step1: {}
step2: {prefix: step1.value}
step3: {prefix: step2.value}
step4: {prefix: step3.value}
if step4.value > 100 {
	step5: {}
}
step5: {
	value:  *100|int
}
`,
			expected: `step1: {
	value: 100
} @step(1)
step2: {
	prefix: 100
	value:  101
} @step(2)
step3: {
	prefix: 101
	value:  102
} @step(3)
step4: {
	prefix: 102
	value:  103
} @step(4)
step5: {
	value: 104
} @step(5)
`}, {base: `
step1: {}
step2: {prefix: step1.value}
if step2.value > 100 {
	step2_3: {prefix: step2.value}
}
step3: {prefix: step2.value}
step4: {prefix: step3.value}
`,
			expected: `step1: {
	value: 100
} @step(1)
step2: {
	prefix: 100
	value:  101
} @step(2)
step3: {
	prefix: 101
	value:  103
} @step(4)
step2_3: {
	prefix: 101
	value:  102
} @step(3)
step4: {
	prefix: 103
	value:  104
} @step(5)
`}, {base: `
step2: {prefix: step1.value} @step(2)
step1: {} @step(1)
step3: {prefix: step2.value} @step(4)
if step2.value > 100 {
	step2_3: {prefix: step2.value} @step(3)
}
`,
			expected: `step2: {
	prefix: 100
	value:  101
} @step(2)
step1: {
	value: 100
} @step(1)
step2_3: {
	prefix: 101
	value:  102
} @step(3)
step3: {
	prefix: 101
	value:  103
} @step(4)
`},

		{base: `
step2: {prefix: step1.value} 
step1: {} @step(-1)
if step2.value > 100 {
	step2_3: {prefix: step2.value}
}
step3: {prefix: step2.value}
`,
			expected: `step2: {
	prefix: 100
	value:  101
} @step(1)
step1: {
	value: 100
} @step(-1)
step2_3: {
	prefix: 101
	value:  102
} @step(2)
step3: {
	prefix: 101
	value:  103
} @step(3)
`}}

	for i, tCase := range testCases {
		r := require.New(t)
		val, err := NewValue(tCase.base, nil, "", TagFieldOrder)
		r.NoError(err)
		number := 99
		err = val.StepByFields(func(name string, in *Value) (bool, error) {
			number++
			return false, in.FillObject(map[string]interface{}{
				"value": number,
			})
		})
		r.NoError(err)
		str, err := sets.ToString(val.CueValue())
		r.NoError(err)
		r.Equal(str, tCase.expected, fmt.Sprintf("testPatch for case(no:%d) %s", i, str))
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
	val, err := NewValue(case1, nil, "")
	r.NoError(err)
	err = val.UnmarshalTo(&out)
	r.NoError(err)
	r.Equal(out.Provider, "kube")
	r.Equal(out.Do, "apply")

	bt, err := val.CueValue().MarshalJSON()
	r.NoError(err)
	expectedJson, err := json.Marshal(out)
	r.NoError(err)
	r.Equal(string(bt), string(expectedJson))

	caseIncomplete := `
provider: string
do: string
`
	val, err = NewValue(caseIncomplete, nil, "")
	r.NoError(err)
	err = val.UnmarshalTo(&out)
	r.Error(err)
}

func TestStepByList(t *testing.T) {
	r := require.New(t)
	base := `[{step: 1},{step: 2}]`
	v, err := NewValue(base, nil, "")
	r.NoError(err)
	var i int64
	err = v.StepByList(func(name string, in *Value) (bool, error) {
		i++
		num, err := in.CueValue().LookupPath(FieldPath("step")).Int64()
		r.NoError(err)
		r.Equal(num, i)
		return false, nil
	})
	r.NoError(err)

	i = 0
	err = v.StepByList(func(_ string, _ *Value) (bool, error) {
		i++
		return true, nil
	})
	r.NoError(err)
	r.Equal(i, int64(1))

	i = 0
	err = v.StepByList(func(_ string, _ *Value) (bool, error) {
		i++
		return false, errors.New("mock error")
	})
	r.Equal(err.Error(), "mock error")
	r.Equal(i, int64(1))

	notListV, err := NewValue(`{}`, nil, "")
	r.NoError(err)
	err = notListV.StepByList(func(_ string, _ *Value) (bool, error) {
		return false, nil
	})
	r.Error(err)
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

func TestValueFix(t *testing.T) {
	testCases := []struct {
		original string
		expected string
	}{
		{
			original: `
parameter: test: _
// comment
y: {
	for k, v in parameter.test.p {
		"\(k)": v
	}
}`,
			expected: `{
	parameter: {
		test: _
	}
	// comment
	y: {
		for k, v in *parameter.test.p | {} {
			"\(k)": v
		}
	}
}`,
		},
	}
	for i, tc := range testCases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			r := require.New(t)
			v, err := NewValue(tc.original, nil, "")
			r.NoError(err)
			b, err := format.Node(v.CueValue().Syntax(cue.Docs(true)))
			r.NoError(err)
			r.Equal(tc.expected, string(b))
		})
	}
}

func TestValue(t *testing.T) {

	// Test NewValue with wrong cue format.
	caseError := `
provider: xxx
`
	r := require.New(t)
	val, err := NewValue(caseError, nil, "")
	r.NoError(err)
	r.Error(val.Error())

	val, err = NewValue(":", nil, "")
	r.Error(err)
	r.Equal(val == nil, true)

	// Test make error by Fill with wrong cue format.
	caseOk := `
provider: "kube"
do: "apply"
`
	val, err = NewValue(caseOk, nil, "")
	r.NoError(err)
	originCue := val.CueValue()

	_, err = val.MakeValue(caseError)
	r.Error(err)
	_, err = val.MakeValue(":")
	r.Error(err)
	_, err = val.MakeValue("test: _|_")
	r.Error(err)
	err = val.FillRaw(caseError)
	r.Error(err)
	r.Equal(originCue, val.CueValue())
	cv, err := NewValue(caseOk, nil, "")
	r.NoError(err)
	err = val.FillObject(cv)
	r.Error(err)
	r.Equal(originCue, val.CueValue())

	// Test make error by Fill with cue eval error.
	caseClose := `
close({provider: int})
`
	err = val.FillRaw(caseClose)
	r.Error(err)
	r.Equal(originCue, val.CueValue())
	cv, err = val.MakeValue(caseClose)
	r.NoError(err)
	err = val.FillObject(cv)
	r.NoError(err)
	r.Error(val.Error())

	_, err = val.LookupValue("abc")
	r.Error(err)

	providerValue, err := val.LookupValue("provider")
	r.NoError(err)
	err = providerValue.StepByFields(func(_ string, in *Value) (bool, error) {
		return false, nil
	})
	r.Error(err)

	openSt := `
#X: {...}
x: #X & {
   name: "xxx"
   age: 12
}
`
	val, err = NewValue(openSt, nil, "")
	r.NoError(err)
	x, _ := val.LookupValue("x")
	xs, _ := x.String()
	_, err = val.MakeValue(xs)
	r.NoError(err)
}

func TestLookupValue(t *testing.T) {
	testCases := []struct {
		name  string
		str   string
		paths []string
	}{
		{
			name: "def",
			str: `
#x: "v"
`,
			paths: []string{"#x"},
		},
		{
			name: "def in def",
			str: `
#x: {
	#y: "v"
}
`,
			paths: []string{"#x", "#y"},
		},
		{
			name: "num",
			str: `
"1": {
	"2": "v"
}
`,
			paths: []string{"1", "2"},
		},
		{
			name: "invalid",
			str: `
"a-b": {
	"b-c": "v"
}
`,
			paths: []string{"a-b", "b-c"},
		},
		{
			name: "concrete path",
			str: `
a: {
	"b-c": "v"
}
`,
			paths: []string{`a["b-c"]`},
		},
		{
			name: "concrete path with num",
			str: `
a: [
	{
		key: "v"
	}
]
`,
			paths: []string{`a[0].key`},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			r := require.New(t)
			v, err := NewValue(tc.str, nil, "")
			r.NoError(err)
			result, err := v.LookupValue(tc.paths...)
			r.NoError(err)
			r.NoError(result.Error())
			s, err := sets.ToString(result.v)
			r.Equal(s, `"v"
`)
			r.NoError(err)
		})
	}
}

func TestValueError(t *testing.T) {
	caseOk := `
provider: "kube"
do: "apply"
`
	r := require.New(t)
	val, err := NewValue(caseOk, nil, "")
	r.NoError(err)
	err = val.FillRaw(`
provider: "conflict"`)
	r.Error(err)

	val, err = NewValue(caseOk, nil, "")
	r.NoError(err)
	err = val.FillObject(map[string]string{
		"provider": "abc",
	})
	r.NoError(err)
	r.Error(val.Error())
}

func TestField(t *testing.T) {
	caseSrc := `
name: "foo"
#name: "fly"
#age: 100
bottom: _|_
`
	r := require.New(t)
	val, err := NewValue(caseSrc, nil, "")
	r.NoError(err)

	name, err := val.Field("name")
	r.NoError(err)
	nameValue, err := name.String()
	r.NoError(err)
	r.Equal(nameValue, "foo")

	dname, err := val.Field("#name")
	r.NoError(err)
	nameValue, err = dname.String()
	r.NoError(err)
	r.Equal(nameValue, "fly")

	_, err = val.Field("age")
	r.Error(err)

	_, err = val.Field("bottom")
	r.Error(err)
}

func TestProcessScript(t *testing.T) {
	testCases := []struct {
		src    string
		expect string
		err    string
	}{
		{
			src: `parameter: {
 check: "status==\"ready\""
}

wait: {
 status: "ready"
 continue: script(parameter.check)
}`,
			expect: `parameter: {
	check: "status==\"ready\""
}
wait: {
	status:   "ready"
	continue: true
}
`,
		},
		{
			src: `parameter: {
 check: "status==\"ready\""
}

wait: {
 status: "ready"
 continue: script("")
}`,
			expect: ``,
			err:    "script parameter error",
		},
		{
			src: `parameter: {
 check: "status=\"ready\""
}

wait: {
 status: "ready"
 continue: script(parameter.check)
}`,
			expect: ``,
			err:    "script value(status=\"ready\") is invalid CueLang",
		},
	}

	for _, tCase := range testCases {
		r := require.New(t)
		v, err := NewValue(tCase.src, nil, "", ProcessScript)
		if tCase.err != "" {
			r.Equal(err.Error(), tCase.err)
			continue
		}
		r.NoError(err)
		s, err := v.String()
		r.NoError(err)
		r.Equal(s, tCase.expect)
	}
}

func TestLookupByScript(t *testing.T) {
	testCases := []struct {
		src    string
		script string
		expect string
	}{
		{
			src: `
traits: {
	ingress: {
		// +patchKey=name
		test: [{name: "main", image: "busybox"}]
	}
}
`,
			script: `traits["ingress"]`,
			expect: `// +patchKey=name
test: [{
	name:  "main"
	image: "busybox"
}]
`,
		},
		{
			src: `
apply: containers: [{name: "main", image: "busybox"}]
`,
			script: `apply.containers[0].image`,
			expect: `"busybox"
`,
		},
		{
			src: `
apply: workload: name: "main"
`,
			script: `
apply.workload.name`,
			expect: `"main"
`,
		},
		{
			src: `
apply: arr: ["abc","def"]
`,
			script: `
import "strings"
strings.Join(apply.arr,".")+"$"`,
			expect: `"abc.def$"
`,
		},
	}

	for _, tCase := range testCases {
		r := require.New(t)
		srcV, err := NewValue(tCase.src, nil, "")
		r.NoError(err)
		v, err := srcV.LookupByScript(tCase.script)
		r.NoError(err)
		result, _ := v.String()
		r.Equal(tCase.expect, result)
	}

	errorCases := []struct {
		src    string
		script string
		err    string
	}{
		{
			src: `
   op: string 
   op: "help"
`,
			script: `op(1`,
			err:    "parse script: expected ')', found 'EOF'",
		},
		{
			src: `
   op: string 
   op: "help"
`,
			script: `oss`,
			err:    "failed to lookup value: var(path=oss) not exist",
		},
	}

	for _, tCase := range errorCases {
		r := require.New(t)
		srcV, err := NewValue(tCase.src, nil, "")
		r.NoError(err)
		_, err = srcV.LookupByScript(tCase.script)
		r.Error(err, tCase.err)
		r.Equal(err.Error(), tCase.err)
	}
}

func TestGet(t *testing.T) {
	caseOk := `
strKey: "xxx"
intKey: 100
boolKey: true
`
	r := require.New(t)
	val, err := NewValue(caseOk, nil, "")
	r.NoError(err)

	str, err := val.GetString("strKey")
	r.NoError(err)
	r.Equal(str, "xxx")
	// err case
	_, err = val.GetInt64("strKey")
	r.Error(err)

	intv, err := val.GetInt64("intKey")
	r.NoError(err)
	r.Equal(intv, int64(100))
	// err case
	_, err = val.GetBool("intKey")
	r.Error(err)

	ok, err := val.GetBool("boolKey")
	r.NoError(err)
	r.Equal(ok, true)
	// err case
	_, err = val.GetString("boolKey")
	r.Error(err)
}

func TestImports(t *testing.T) {
	cont := `
context: stepSessionID: "3w9qkdgn5w"`
	v, err := NewValue(`
import (
	"vela/custom"
)

id: custom.context.stepSessionID 

`+cont, nil, cont)
	r := require.New(t)
	r.NoError(err)
	id, err := v.GetString("id")
	r.NoError(err)
	r.Equal(id, "3w9qkdgn5w")
}

func TestOpenCompleteValue(t *testing.T) {
	v, err := NewValue(`
x: 10
y: "100"
`, nil, "")
	r := require.New(t)
	r.NoError(err)
	err = v.OpenCompleteValue()
	r.NoError(err)
	s, err := v.String()
	r.NoError(err)
	r.Equal(s, `x: *10 | _
y: *"100" | _
`)
}

func TestFillByScript(t *testing.T) {
	testCases := []struct {
		name     string
		raw      string
		path     string
		v        string
		expected string
	}{
		{
			name: "insert array",
			raw:  `a: ["hello"]`,
			path: "a[1]",
			v:    `"world"`,
			expected: `a: ["hello", "world", ...]
`},
		{
			name: "insert array",
			raw:  `a: b: [{x: 100},...]`,
			path: "a.b[1]",
			v:    `{name: "foo"}`,
			expected: `a: {
	b: [{
		x: 100
	}, {
		name: "foo"
	}, ...]
}
`},
		{
			name: "insert array to array",
			raw: `
a: b: c: [{x: 100}, {x: 101}, {x: 102}]`,
			path: "a.b.c[0].value",
			v:    `"foo"`,
			expected: `a: {
	b: {
		c: [{
			x:     100
			value: "foo"
		}, {
			x: 101
		}, {
			x: 102
		}, ...]
	}
}
`,
		},
		{
			name: "insert nest array ",
			raw:  `a: b: [{x: y:[{name: "key"}]}]`,
			path: "a.b[0].x.y[0].value",
			v:    `"foo"`,
			expected: `a: {
	b: [{
		x: {
			y: [{
				name:  "key"
				value: "foo"
			}, ...]
		}
	}, ...]
}
`,
		},
		{
			name: "insert without array",
			raw:  `a: b: [{x: y:[{name: "key"}]}]`,
			path: "a.c.x",
			v:    `"foo"`,
			expected: `a: {
	b: [{
		x: {
			y: [{
				name: "key"
			}, ...]
		}
	}, ...]
	c: {
		x: "foo"
	}
}
`,
		},
		{
			name: "path with string index",
			raw:  `a: b: [{x: y:[{name: "key"}]}]`,
			path: "a.c[\"x\"]",
			v:    `"foo"`,
			expected: `a: {
	b: [{
		x: {
			y: [{
				name: "key"
			}, ...]
		}
	}, ...]
	c: {
		x: "foo"
	}
}
`,
		},
	}

	for _, tCase := range testCases {
		r := require.New(t)
		v, err := NewValue(tCase.raw, nil, "")
		r.NoError(err)
		val, err := v.MakeValue(tCase.v)
		r.NoError(err)
		err = v.FillValueByScript(val, tCase.path)
		r.NoError(err)
		s, err := v.String()
		r.NoError(err)
		r.Equal(s, tCase.expected, tCase.name)
	}

	errCases := []struct {
		name string
		raw  string
		path string
		v    string
		err  string
	}{
		{
			name: "invalid path",
			raw:  `a: b: [{x: 100},...]`,
			path: "a.b[1]+1",
			v:    `{name: "foo"}`,
			err:  "invalid path: invalid label a.b[1]+1 ",
		},
		{
			name: "invalid path [float]",
			raw:  `a: b: [{x: 100},...]`,
			path: "a.b[0.1]",
			v:    `{name: "foo"}`,
			err:  "invalid path: invalid literal 0.1",
		},
		{
			name: "conflict merge",
			raw:  `a: b: [{x: y:[{name: "key"}]}]`,
			path: "a.b[0].x.y[0].name",
			v:    `"foo"`,
			err:  "a.b.0.x.y.0.name: conflicting values \"foo\" and \"key\"",
		},
	}

	for _, errCase := range errCases {
		r := require.New(t)
		v, err := NewValue(errCase.raw, nil, "")
		r.NoError(err)
		errV, err := v.MakeValue(errCase.v)
		r.NoError(err)
		err = v.FillValueByScript(errV, errCase.path)
		r.Equal(errCase.err, err.Error())
	}
}

func TestSetByScript(t *testing.T) {
	testCases := []struct {
		name     string
		raw      string
		path     string
		v        string
		expected string
	}{
		{
			name: "insert array",
			raw:  `a: ["hello"]`,
			path: "a[0]",
			v:    `"world"`,
			expected: `a: ["world"]
`},
		{
			name: "insert array2",
			raw:  `a: ["hello"]`,
			path: "a[1]",
			v:    `"world"`,
			expected: `a: ["hello", "world"]
`},
		{
			name: "insert array3",
			raw:  `a: b: [{x: 100}]`,
			path: "a.b[0]",
			v:    `{name: "foo"}`,
			expected: `a: {
	b: [{
		name: "foo"
	}]
}
`},
		{
			name: "insert struct",
			raw:  `a: {b: "hello"}`,
			path: "a.b",
			v:    `"world"`,
			expected: `a: {
	b: "world"
}
`},
		{
			name: "insert struct2",
			raw:  `a: {b: "hello"}, c: {d: "world"}`,
			path: "c.d",
			v:    `"hello"`,
			expected: `a: {
	b: "hello"
}
c: {
	d: "hello"
}
`},
		{
			name: "insert array to array",
			raw: `
a: b: c: [{x: 100}, {x: 101}, {x: 102}]`,
			path: "a.b.c[0].value",
			v:    `"foo"`,
			expected: `a: {
	b: {
		c: [{
			x:     100
			value: "foo"
		}, {
			x: 101
		}, {
			x: 102
		}]
	}
}
`,
		},
		{
			name: "insert nest array ",
			raw:  `a: b: [{x: y:[{name: "key"}]}]`,
			path: "a.b[0].x.y[0].value",
			v:    `"foo"`,
			expected: `a: {
	b: [{
		x: {
			y: [{
				name:  "key"
				value: "foo"
			}]
		}
	}]
}
`,
		},
		{
			name: "insert without array",
			raw:  `a: b: [{x: y:[{name: "key"}]}]`,
			path: "a.c.x",
			v:    `"foo"`,
			expected: `a: {
	b: [{
		x: {
			y: [{
				name: "key"
			}]
		}
	}]
	c: {
		x: "foo"
	}
}
`,
		},
		{
			name: "path with string index",
			raw:  `a: b: [{x: y:[{name: "key"}]}]`,
			path: "a.c[\"x\"]",
			v:    `"foo"`,
			expected: `a: {
	b: [{
		x: {
			y: [{
				name: "key"
			}]
		}
	}]
	c: {
		x: "foo"
	}
}
`,
		},
	}

	for _, tCase := range testCases {
		r := require.New(t)
		v, err := NewValue(tCase.raw, nil, "")
		r.NoError(err)
		val, err := v.MakeValue(tCase.v)
		r.NoError(err)
		err = v.SetValueByScript(val, tCase.path)
		r.NoError(err, tCase.name)
		s, err := v.String()
		r.NoError(err)
		r.Equal(s, tCase.expected, tCase.name)
	}
}

func TestSubstituteInStruct(t *testing.T) {
	base := `
value: {
	a: 1
}
`
	r := require.New(t)
	val, err := NewValue(base, nil, "")
	r.NoError(err)
	expr, err := cuejson.Unmarshal([]byte(`{"b": 2}`))
	r.NoError(err)
	err = val.SubstituteInStruct(expr, "value")
	r.NoError(err)
	s, err := val.String()
	r.NoError(err)
	r.Equal(s, `value: {
	b: 2
}
`)
	err = val.SubstituteInStruct(expr, "notfound")
	r.Error(err)

	errBase := `1`
	val1, err := NewValue(errBase, nil, "")
	r.NoError(err)
	err = val1.SubstituteInStruct(expr, "value")
	r.Error(err)
}
