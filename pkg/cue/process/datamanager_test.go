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

package process

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubevela/workflow/pkg/cue/model"
)

func TestWithLoop(t *testing.T) {
	r := require.New(t)
	ctx := NewContext(ContextData{})
	meta := WithLoop("b", 1)
	manager := NewStepRunTimeMeta()
	manager.Fill(ctx, []StepMetaKV{meta})
	r.Equal(map[string]interface{}{"item": "b", "index": 1}, ctx.GetData(model.ContextLoop))
	manager.Remove(ctx, []string{meta.Key})
	r.Nil(ctx.GetData(model.ContextLoop))
}
