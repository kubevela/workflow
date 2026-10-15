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

package expressions

import (
	"context"
	"strings"
	"testing"

	"github.com/kubevela/pkg/cel"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/types"
)

// stored is a workflow context's mutable values, as its ConfigMap keeps them.
type stored struct {
	wfVars
	data map[string]string
}

func (s *stored) GetMutableValue(paths ...string) string { return s.data[strings.Join(paths, ".")] }
func (s *stored) SetMutableValue(v string, paths ...string) {
	s.data[strings.Join(paths, ".")] = v
}

func TestAHostResolverIsGivenTheRun(t *testing.T) {
	var seen RunScope
	var found bool
	ev, err := NewEvaluator(types.ExpressionOptions{
		Roots: []cel.Root{{Name: "source"}},
		Resolvers: map[string]cel.Resolver{"source": cel.ResolverFunc(func(ctx context.Context, _ []cel.Read) (interface{}, error) {
			seen, found = RunFrom(ctx)
			seen.Store.SetMutableValue("kept", "host-key")
			return map[string]interface{}{"cfg": "v"}, nil
		})},
	}, nil, "run-1")
	require.NoError(t, err)
	wfCtx := &stored{data: map[string]string{}}
	_, err = ev.Properties(context.Background(), types.ExpressionStep{Name: "notify-0", Type: "notification"},
		&runtime.RawExtension{Raw: []byte(`{"x":"$(source.cfg)"}`)}, wfCtx,
		process.NewContext(process.ContextData{}), nil)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, RunScope{Run: "run-1", Step: "notify-0", StepType: "notification", Store: wfCtx}, seen)
	require.Equal(t, "kept", wfCtx.data["host-key"], "values a host keeps last as long as the run's context")

	_, found = RunFrom(context.Background())
	require.False(t, found)
}
