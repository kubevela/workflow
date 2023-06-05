/*
 Copyright 2022. The KubeVela Authors.

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

package util

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/kubevela/pkg/cue/util"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/cue/process"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
)

func TestPatchK8sObject(t *testing.T) {
	ctx := context.Background()
	cuectx := cuecontext.New()
	testcases := map[string]struct {
		value       string
		expectedErr error
		patchResult string
	}{
		"test patch k8s object": {
			value: `
value: {
	apiVersion: "apps/v1"
	kind:       "Deployment"
	spec: template: metadata: {
		labels: {
			"oam.dev/name": "test"
		}
	}
}
patch: {
	spec: template: metadata: {
		labels: {
			"test-label": "true"
		}
	}
}`,
			expectedErr: nil,
			patchResult: `apiVersion: "apps/v1"
kind:       "Deployment"
spec: template: metadata: labels: {
	"oam.dev/name": "test"
	"test-label":   "true"
}`,
		},
		"test patch k8s object with patchKey": {
			value: `
value: {
	apiVersion: "apps/v1"
	kind:       "Deployment"
	spec: template: spec: {
		containers: [{
			name: "test"
		}]
	}
}
patch: {
	spec: template: spec: {
		// +patchKey=name
		containers: [{
			name: "test"
			env: [{
				name:  "test-env"
				value: "test-value"
			}]
		}]
	}
}`,
			expectedErr: nil,
			patchResult: `apiVersion: "apps/v1"
kind:       "Deployment"
spec: template: spec: containers: [{
	name: "test"
	env: [{
		name:  "test-env"
		value: "test-value"
	}]
}]`,
		},
		"test patch k8s object with patchStrategy": {
			value: `
value: {
	apiVersion: "apps/v1"
	kind:       "Deployment"
	spec: template: metadata: {
		name: "test-name"
	}
}
patch: {
	// +patchStrategy=retainKeys
	spec: template: metadata: {
		name: "test-patchStrategy"
	}
}
`,
			expectedErr: nil,
			patchResult: `apiVersion: "apps/v1"
kind:       "Deployment"
spec: template: metadata: name: "test-patchStrategy"`,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			res, err := PatchK8sObject(ctx, &providertypes.LegacyParams[cue.Value]{
				Params: cuectx.CompileString(tc.value),
			})
			if tc.expectedErr != nil {
				r.Equal(tc.expectedErr.Error(), err.Error())
				return
			}
			r.NoError(err)
			s, err := util.ToString(res.LookupPath(cue.ParsePath("result")))
			r.NoError(err)
			r.Equal(tc.patchResult, s)
		})
	}
}

func TestConvertString(t *testing.T) {
	ctx := context.Background()
	testCases := map[string]struct {
		from     []byte
		expected string
	}{
		"success": {
			from:     []byte("test"),
			expected: "test",
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			res, err := String(ctx, &StringParams{
				Params: StringVars{
					Byte: []byte(tc.from),
				},
			})
			r.NoError(err)
			r.Equal(tc.expected, res.String)
		})
	}
}

func TestLog(t *testing.T) {
	ctx := context.Background()
	wfCtx := newWorkflowContextForTest(t)
	pCtx := process.NewContext(process.ContextData{})
	pCtx.PushData(model.ContextStepName, "test-step")

	testCases := []struct {
		value       LogVars
		expected    string
		expectedErr string
	}{
		{
			value: LogVars{
				Data: "test",
			},
			expected: `{"test-step":{"data":true}}`,
		},
		{
			value: LogVars{
				Data: map[string]string{
					"message": "test",
				},
				Level: 3,
			},
			expected: `{"test-step":{"data":true}}`,
		},
		{
			value: LogVars{
				Data: map[string]string{
					"test": "",
				},
			},
			expected: `{"test-step":{"data":true}}`,
		},
		{
			value: LogVars{
				Source: &LogSource{
					URL: "https://kubevela.io",
				},
			},
			expected: `{"test-step":{"data":true,"source":{"url":"https://kubevela.io"}}}`,
		},
		{
			value: LogVars{
				Source: &LogSource{
					Resources: []Resource{
						{
							LabelSelector: map[string]string{
								"test": "test",
							},
						},
					},
				},
			},
			expected: `{"test-step":{"data":true,"source":{"url":"https://kubevela.io","resources":[{"labelSelector":{"test":"test"}}]}}}`,
		},
		{
			value: LogVars{
				Source: &LogSource{
					Resources: []Resource{
						{
							Name:      "test",
							Namespace: "test",
							Cluster:   "test",
						},
					},
				},
			},
			expected: `{"test-step":{"data":true,"source":{"url":"https://kubevela.io","resources":[{"name":"test","namespace":"test","cluster":"test"}]}}}`,
		},
		{
			value: LogVars{
				Source: &LogSource{
					URL: "https://kubevela.com",
				},
			},
			expected: `{"test-step":{"data":true,"source":{"url":"https://kubevela.com","resources":[{"name":"test","namespace":"test","cluster":"test"}]}}}`,
		},
	}
	for i, tc := range testCases {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			r := require.New(t)
			_, err := Log(ctx, &LogParams{
				Params: tc.value,
				RuntimeParams: providertypes.RuntimeParams{
					ProcessContext:  pCtx,
					WorkflowContext: wfCtx,
				},
			})
			if tc.expectedErr != "" {
				r.Contains(err.Error(), tc.expectedErr)
				return
			}
			r.NoError(err)
			if tc.expected != "" {
				config := wfCtx.GetMutableValue("logConfig")
				r.Equal(tc.expected, config)
			}
		})
	}
}

// func TestInstall(t *testing.T) {
// 	p := providers.NewProviders()
// 	pCtx := process.NewContext(process.ContextData{})
// 	pCtx.PushData(model.ContextStepName, "test-step")
// 	Install(p, pCtx)
// 	h, ok := p.GetHandler("util", "string")
// 	r := require.New(t)
// 	r.Equal(ok, true)
// 	r.Equal(h != nil, true)
// }

func newWorkflowContextForTest(t *testing.T) wfContext.Context {
	cm := corev1.ConfigMap{}
	r := require.New(t)
	testCaseJson, err := yaml.YAMLToJSON([]byte(testCaseYaml))
	r.NoError(err)
	err = json.Unmarshal(testCaseJson, &cm)
	r.NoError(err)

	wfCtx := new(wfContext.WorkflowContext)
	err = wfCtx.LoadFromConfigMap(context.Background(), cm)
	r.NoError(err)
	return wfCtx
}

var (
	testCaseYaml = `apiVersion: v1
data:
  logConfig: ""
kind: ConfigMap
metadata:
  name: app-v1
`
)
