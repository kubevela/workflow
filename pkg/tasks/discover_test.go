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

package tasks

import (
	"context"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"

	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/tasks/builtin"
	"github.com/kubevela/workflow/pkg/tasks/custom"
	"github.com/kubevela/workflow/pkg/types"
)

func TestDiscover(t *testing.T) {
	r := require.New(t)
	makeErr := func(name string) error {
		return errors.Errorf("template %s not found", name)
	}

	loadTemplate := func(ctx context.Context, name string) (string, error) {
		switch name {
		case "foo":
			return "", nil
		case "crazy":
			return "", nil
		default:
			return "", makeErr(name)
		}
	}
	pCtx := process.NewContext(process.ContextData{
		Namespace: "default",
	})
	discover := &taskDiscover{
		builtin: map[string]types.TaskGenerator{
			"stepGroup": builtin.StepGroup,
		},
		customTaskDiscover: custom.NewTaskLoader(loadTemplate, 0, pCtx, nil),
	}

	_, err := discover.GetTaskGenerator(context.Background(), "stepGroup")
	r.NoError(err)
	_, err = discover.GetTaskGenerator(context.Background(), "foo")
	r.NoError(err)
	_, err = discover.GetTaskGenerator(context.Background(), "crazy")
	r.NoError(err)
	_, err = discover.GetTaskGenerator(context.Background(), "fly")
	r.Equal(err.Error(), makeErr("fly").Error())

}
