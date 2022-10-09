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
	"encoding/json"
	"fmt"
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
	err := cli.Create(ctx, &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workflow-test-context",
			Namespace: "default",
		},
		Data: map[string]string{
			"vars": `{"test-test": "test"}`,
		},
	})
	r := require.New(t)
	r.NoError(err)

	testCases := map[string]struct {
		name        string
		paths       string
		expected    string
		expectedErr string
	}{
		"not found": {
			name:        "not-found",
			expectedErr: "not found",
		},
		"found": {
			name:     "test",
			expected: "\"test-test\": \"test\"\n",
		},
		"found with path": {
			name:     "test",
			paths:    "test-test",
			expected: "\"test\"\n",
		},
		"path not found": {
			name:        "test",
			paths:       "not-found",
			expectedErr: "not exist",
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			v, err := GetDataFromContext(ctx, cli, tc.name, "default", tc.paths)
			if tc.expectedErr != "" {
				r.Contains(err.Error(), tc.expectedErr)
				return
			}
			r.NoError(err)
			s, err := sets.ToString(v.CueValue())
			r.NoError(err)
			r.Equal(tc.expected, s)
		})
	}
}

func TestGetStepLogConfig(t *testing.T) {
	cli := fake.NewFakeClientWithScheme(scheme.Scheme)
	ctx := context.Background()

	testCases := map[string]struct {
		name        string
		step        string
		config      string
		expected    string
		expectedErr string
	}{
		"not found": {
			name:        "not-found",
			config:      "not-found",
			expectedErr: "not found",
		},
		"no data": {
			name:        "test",
			step:        "step-test",
			config:      "",
			expectedErr: "no log config found",
		},
		"failed to marshal": {
			name:        "test",
			step:        "step-test",
			config:      "test",
			expectedErr: "invalid character",
		},
		"invalid config": {
			name:        "test",
			step:        "step-test",
			config:      `{"test": "test"}`,
			expectedErr: "cannot unmarshal string into Go value of type types.LogConfig",
		},
		"no config for step": {
			name:        "test",
			step:        "step-test",
			config:      `{"no-step": {}}`,
			expectedErr: "no log config found for step step-test",
		},
		"success": {
			name:     "test",
			step:     "step-test",
			config:   `{"step-test": {"data":true}}`,
			expected: `{"data":true}`,
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			cm := &corev1.ConfigMap{}
			if tc.config != "not-found" {
				cm = &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      fmt.Sprintf("workflow-%s-context", tc.name),
						Namespace: "default",
					},
					Data: map[string]string{
						"logConfig": tc.config,
					},
				}
				err := cli.Create(ctx, cm)
				r.NoError(err)
				defer func() {
					err = cli.Delete(ctx, cm)
					r.NoError(err)
				}()
			}
			v, err := GetLogConfigFromStep(ctx, cli, tc.name, "default", tc.step)
			if tc.expectedErr != "" {
				r.Contains(err.Error(), tc.expectedErr)
				return
			}
			r.NoError(err)
			b, err := json.Marshal(v)
			r.NoError(err)
			r.Equal(tc.expected, string(b))
		})
	}
}
