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
	"github.com/stretchr/testify/require"
)

func TestGetPackages(t *testing.T) {
	r := require.New(t)
	pkg, err := GetPackages()
	r.NoError(err)
	cuectx := cuecontext.New()
	file, err := parser.ParseFile(builtinPackageName, pkg[builtinPackageName])
	r.NoError(err)
	_ = cuectx.BuildFile(file)

	file, err = parser.ParseFile("-", `
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
}
