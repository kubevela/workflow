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

package utils

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func crdWithSteps(name string, stepProps map[string]any, served ...bool) *unstructured.Unstructured {
	var versions []any
	for i, s := range served {
		versions = append(versions, map[string]any{
			"name":   fmt.Sprintf("v%d", i+1),
			"served": s,
			"schema": map[string]any{"openAPIV3Schema": map[string]any{
				"properties": map[string]any{"spec": map[string]any{
					"properties": map[string]any{"steps": map[string]any{
						"type":  "array",
						"items": map[string]any{"properties": stepProps},
					}},
				}},
			}},
		})
	}
	u := &unstructured.Unstructured{Object: map[string]any{"spec": map[string]any{"versions": versions}}}
	u.SetAPIVersion("apiextensions.k8s.io/v1")
	u.SetKind("CustomResourceDefinition")
	u.SetName(name)
	return u
}

// dropStepProps replaces one version's step properties.
func dropStepProps(crd *unstructured.Unstructured, version int, props map[string]any) *unstructured.Unstructured {
	versions := crd.Object["spec"].(map[string]any)["versions"].([]any)
	steps := versions[version].(map[string]any)["schema"].(map[string]any)["openAPIV3Schema"].(map[string]any)["properties"].(map[string]any)["spec"].(map[string]any)["properties"].(map[string]any)["steps"].(map[string]any)
	steps["items"] = map[string]any{"properties": props}
	return crd
}

func TestCRDDeclaresField(t *testing.T) {
	withForEach := map[string]any{"name": map[string]any{}, "forEach": map[string]any{}}
	without := map[string]any{"name": map[string]any{}}
	cases := map[string]struct {
		crd  *unstructured.Unstructured
		want bool
	}{
		"declared":                         {crd: crdWithSteps("runs.example.io", withForEach, true), want: true},
		"missing":                          {crd: crdWithSteps("runs.example.io", without, true)},
		"declared in every served version": {crd: crdWithSteps("runs.example.io", withForEach, true, true), want: true},
		"missing from an unserved version": {crd: dropStepProps(crdWithSteps("runs.example.io", withForEach, true, false), 1, without), want: true},
		"missing from a served version":    {crd: dropStepProps(crdWithSteps("runs.example.io", withForEach, true, true), 1, without)},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			cli := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).WithObjects(tc.crd).Build()
			got, err := CRDDeclaresField(context.Background(), cli, "runs.example.io", "spec", "steps", "[]", "forEach")
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}

	t.Run("no such CRD", func(t *testing.T) {
		cli := fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build()
		_, err := CRDDeclaresField(context.Background(), cli, "runs.example.io", "spec")
		require.Error(t, err)
	})
}

// failingReader fails every read, as a client without permission to get CRDs does.
type failingReader struct{ client.Reader }

func (failingReader) Get(context.Context, client.ObjectKey, client.Object, ...client.GetOption) error {
	return errors.New("forbidden")
}

func TestWarnIfCRDLacksForEach(t *testing.T) {
	var buf bytes.Buffer
	klog.LogToStderr(false)
	klog.SetOutput(&buf)
	defer klog.LogToStderr(true)
	warned := func(cli client.Reader) bool {
		buf.Reset()
		WarnIfCRDLacksForEach(context.Background(), cli, "runs.example.io", "spec", "steps")
		klog.Flush()
		return bytes.Contains(buf.Bytes(), []byte("does not declare forEach"))
	}
	crd := func(props map[string]any) client.Reader {
		return fake.NewClientBuilder().WithScheme(runtime.NewScheme()).WithObjects(crdWithSteps("runs.example.io", props, true)).Build()
	}

	require.True(t, warned(crd(map[string]any{"name": map[string]any{}})), "a CRD without forEach is warned about")
	require.False(t, warned(crd(map[string]any{"forEach": map[string]any{}})))
	require.False(t, warned(failingReader{}), "a CRD that cannot be read is not warned about, nor fails startup")
}
