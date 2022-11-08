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

package debug

import (
	"context"
	"testing"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/workflow/pkg/cue/model/value"
	"github.com/kubevela/workflow/pkg/types"
)

func TestSetContext(t *testing.T) {
	r := require.New(t)
	cli := newCliForTest(&corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name: GenerateContextName("test", "step1", "123456"),
		},
		Data: map[string]string{
			"debug": "test",
		},
	})
	// test update
	debugCtx := NewContext(cli, &types.WorkflowInstance{
		WorkflowMeta: types.WorkflowMeta{
			Name: "test",
		},
	}, "step1")
	v, err := value.NewValue(`
test: test
`, nil, "")
	r.NoError(err)
	err = debugCtx.Set(v)
	r.NoError(err)
	// test create
	debugCtx = NewContext(cli, &types.WorkflowInstance{
		WorkflowMeta: types.WorkflowMeta{
			Name: "test",
		},
	}, "step2")
	v, err = value.NewValue(`
test: test
`, nil, "")
	r.NoError(err)
	err = debugCtx.Set(v)
	r.NoError(err)
}

func newCliForTest(wfCm *corev1.ConfigMap) *test.MockClient {
	return &test.MockClient{
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			o, ok := obj.(*corev1.ConfigMap)
			if ok {
				switch key.Name {
				case GenerateContextName("test", "step1", "123456"):
					if wfCm != nil {
						*o = *wfCm
						return nil
					}
				default:
					return kerrors.NewNotFound(corev1.Resource("configMap"), o.Name)
				}
			}
			return kerrors.NewNotFound(corev1.Resource("configMap"), key.Name)
		},
		MockUpdate: func(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
			o, ok := obj.(*corev1.ConfigMap)
			if ok {
				if wfCm == nil {
					return kerrors.NewNotFound(corev1.Resource("configMap"), o.Name)
				}
				*wfCm = *o
			}
			return nil
		},
		MockCreate: func(ctx context.Context, obj client.Object, opts ...client.CreateOption) error {
			return nil
		},
	}
}
