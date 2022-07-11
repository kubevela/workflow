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
	"github.com/stretchr/testify/require"
)

func TestGetPackages(t *testing.T) {
	re := require.New(t)
	pkgs, err := GetPackages()
	re.NoError(err)
	var r cue.Runtime
	for path, content := range pkgs {
		_, err := r.Compile(path, content)
		re.NoError(err)
	}

	builder := &build.Instance{}
	err = builder.AddFile("-", `
import "vela/custom"
out: custom.context`)
	re.NoError(err)
	err = AddImportsFor(builder, "context: id: \"xxx\"")
	re.NoError(err)

	insts := cue.Build([]*build.Instance{builder})
	re.Equal(len(insts), 1)
	str, err := insts[0].Lookup("out", "id").String()
	re.NoError(err)
	re.Equal(str, "xxx")
}
