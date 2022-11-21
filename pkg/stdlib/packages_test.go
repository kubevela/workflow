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

package stdlib

import (
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/build"
	"cuelang.org/go/cue/cuecontext"
	"cuelang.org/go/cue/parser"
	"github.com/kubevela/workflow/pkg/cue/model/sets"
	"github.com/stretchr/testify/require"
)

func TestGetPackages(t *testing.T) {
	//test vela/op
	r := require.New(t)
	pkg, err := GetPackages()
	r.NoError(err)
	cuectx := cuecontext.New()
	_ = cuectx.BuildFile(pkg[builtinPackageName])

	file, err := parser.ParseFile("-", `
import "vela/custom"
out: custom.context`)
	r.NoError(err)
	builder := &build.Instance{}
	err = builder.AddSyntax(file)
	r.NoError(err)
	err = AddImportsFor(builder, "context: id: \"xxx\"")
	r.NoError(err)

	inst := cuectx.BuildInstance(builder)
	str, err := inst.LookupPath(cue.ParsePath("out.id")).String()
	r.NoError(err)
	r.Equal(str, "xxx")

	//test vela/op/v1
	testVersion := "vela/op/v1"
	cuectx1 := cuecontext.New()
	_ = cuectx1.BuildFile(pkg[testVersion])

	file, err = parser.ParseFile("-", `
import "vela/op/v1"
out: v1.#Break & {
	message: "break"
}
`)
	r.NoError(err)
	builder1 := &build.Instance{}
	err = builder1.AddSyntax(file)
	r.NoError(err)
	err = AddImportsFor(builder1, "")
	r.NoError(err)
	inst1 := cuectx1.BuildInstance(builder1)
	str1, err := inst1.LookupPath(cue.ParsePath("out.message")).String()
	r.NoError(err)
	r.Equal(str1, "break")
}

func TestSetupBuiltinImports(t *testing.T) {
	//test vela/op
	r := require.New(t)
	err := SetupBuiltinImports(map[string]string{"vela/op": "test: kube.#Apply"})
	r.NoError(err)
	file, err := parser.ParseFile("-", `
import "vela/op"
step: op.#Steps
apply: op.test`)
	r.NoError(err)
	builder := &build.Instance{}
	err = builder.AddSyntax(file)
	r.NoError(err)
	err = AddImportsFor(builder, "")
	r.NoError(err)

	cuectx := cuecontext.New()
	inst := cuectx.BuildInstance(builder)
	r.NoError(inst.Value().Err())
	v := inst.LookupPath(cue.ParsePath("step"))
	r.NoError(err)
	r.NoError(v.Err())
	s, err := sets.ToString(v)
	r.NoError(err)
	r.Equal(s, "#do: \"steps\"\n")
	v = inst.LookupPath(cue.ParsePath("apply"))
	r.NoError(err)
	r.NoError(v.Err())
	s, err = sets.ToString(v)
	r.NoError(err)
	r.Contains(s, "apply")
}
