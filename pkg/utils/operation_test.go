/*
Copyright 2021 The KubeVela Authors.

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
	"context"
	"testing"

	"github.com/kubevela/workflow/pkg/cue/model/sets"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestGetWorkflowContextData(t *testing.T) {
	cli := fake.NewFakeClientWithScheme(scheme.Scheme)
	ctx := context.Background()
	r := require.New(t)

	// test not found
	_, err := GetDataFromContext(ctx, cli, "not-found", "default")
	r.Error(err)

	// test found
	err = cli.Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workflow-test-context",
			Namespace: "default",
		},
		Data: map[string]string{
			"vars": `{"test-test": "test"}`,
		},
	})
	r.NoError(err)
	v, err := GetDataFromContext(ctx, cli, "test", "default")
	r.NoError(err)
	s, err := sets.ToString(v.CueValue())
	r.NoError(err)
	r.Equal(s, "\"test-test\": \"test\"\n")

	// test found with path
	v, err = GetDataFromContext(ctx, cli, "test", "default", "test-test")
	r.NoError(err)
	s, err = sets.ToString(v.CueValue())
	r.NoError(err)
	r.Equal(s, "\"test\"\n")
}
