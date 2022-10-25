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

package utils

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientfake "k8s.io/client-go/kubernetes/fake"

	"github.com/kubevela/workflow/pkg/cue/model/sets"
	"github.com/kubevela/workflow/pkg/types"
)

func TestGetWorkflowContextData(t *testing.T) {
	ctx := context.Background()
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "workflow-test-context",
			Namespace: "default",
		},
		Data: map[string]string{
			"vars": `{"test-test": "test"}`,
		},
	}
	err := cli.Create(ctx, cm)
	r := require.New(t)
	r.NoError(err)
	defer func() {
		err = cli.Delete(ctx, cm)
		r.NoError(err)
	}()

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
			name:     "workflow-test-context",
			expected: "\"test-test\": \"test\"\n",
		},
		"found with path": {
			name:     "workflow-test-context",
			paths:    "test-test",
			expected: "\"test\"\n",
		},
		"path not found": {
			name:        "workflow-test-context",
			paths:       "not-found",
			expectedErr: "not exist",
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			v, err := GetDataFromContext(ctx, cli, tc.name, tc.name, "default", tc.paths)
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
			name:        "workflow-test-context",
			step:        "step-test",
			config:      "",
			expectedErr: "no log config found",
		},
		"failed to marshal": {
			name:        "workflow-test-context",
			step:        "step-test",
			config:      "test",
			expectedErr: "invalid character",
		},
		"invalid config": {
			name:        "workflow-test-context",
			step:        "step-test",
			config:      `{"test": "test"}`,
			expectedErr: "cannot unmarshal string into Go value of type types.LogConfig",
		},
		"no config for step": {
			name:        "workflow-test-context",
			step:        "step-test",
			config:      `{"no-step": {}}`,
			expectedErr: "no log config found for step step-test",
		},
		"success": {
			name:     "workflow-test-context",
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
						Name:      tc.name,
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
			v, err := GetLogConfigFromStep(ctx, cli, tc.name, tc.name, "default", tc.step)
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

func TestGetPodListFromResources(t *testing.T) {
	ctx := context.Background()
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-test",
			Namespace: "default",
			Labels: map[string]string{
				"test-label": "test",
			},
		},
	}
	err := cli.Create(ctx, pod)
	r := require.New(t)
	r.NoError(err)
	defer func() {
		err = cli.Delete(ctx, pod)
		r.NoError(err)
	}()

	testCases := map[string]struct {
		name        string
		step        string
		resources   []types.Resource
		expected    string
		expectedErr string
	}{
		"not found": {
			name: "not-found",
			resources: []types.Resource{
				{
					Name: "not-found",
				},
			},
			expectedErr: "not found",
		},
		"not found with label": {
			name: "not-found",
			resources: []types.Resource{
				{
					LabelSelector: map[string]string{
						"test-label": "not-found",
					},
				},
			},
			expectedErr: "no pod found",
		},
		"found with name": {
			name: "not-found",
			resources: []types.Resource{
				{
					Name: "pod-test",
				},
			},
			expected: "pod-test",
		},
		"found with label": {
			name: "not-found",
			resources: []types.Resource{
				{
					LabelSelector: map[string]string{
						"test-label": "test",
					},
				},
			},
			expected: "pod-test",
		},
	}
	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			pods, err := GetPodListFromResources(ctx, cli, tc.resources)
			if tc.expectedErr != "" {
				r.Contains(err.Error(), tc.expectedErr)
				return
			}
			r.NoError(err)
			r.Equal(tc.expected, pods[0].Name)
		})
	}
}

func TestGetLogsFromURL(t *testing.T) {
	r := require.New(t)
	_, err := GetLogsFromURL(context.Background(), "https://kubevela.io")
	r.NoError(err)
}

func TestGetLogsFromPod(t *testing.T) {
	r := require.New(t)
	clientSet := clientfake.NewSimpleClientset()
	ctx := context.Background()
	err := cli.Create(ctx, &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "pod-test",
			Namespace: "default",
			Labels: map[string]string{
				"test-label": "test",
			},
		},
	})
	r.NoError(err)
	_, err = GetLogsFromPod(ctx, clientSet, cli, "pod-test", "default", "", nil)
	r.NoError(err)
}
