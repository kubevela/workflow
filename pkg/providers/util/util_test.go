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
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubevela/workflow/pkg/cue/model/value"
	monitorContext "github.com/kubevela/workflow/pkg/monitor/context"
	"github.com/kubevela/workflow/pkg/providers"
)

func TestPatchK8sObject(t *testing.T) {
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
}
`,
			expectedErr: nil,
			patchResult: `
apiVersion: "apps/v1"
kind:       "Deployment"
spec: template: metadata: {
	labels: {
		"oam.dev/name": "test"
		"test-label":   "true"
	}
}
`,
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
}
`,
			expectedErr: nil,
			patchResult: `
apiVersion: "apps/v1"
kind:       "Deployment"
spec: template: spec: {
	containers: [{
		name: "test"
		env: [{
			name:  "test-env"
			value: "test-value"
		}]
	}]
}
`,
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
			patchResult: `
apiVersion: "apps/v1"
kind:       "Deployment"
spec: template: metadata: {
	name: "test-patchStrategy"
}
`,
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			v, err := value.NewValue(tc.value, nil, "")
			r.NoError(err)
			prd := &provider{}
			err = prd.PatchK8sObject(nil, nil, v, nil)
			if tc.expectedErr != nil {
				r.Equal(tc.expectedErr.Error(), err.Error())
				return
			}
			r.NoError(err)
			result, err := v.LookupValue("result")
			r.NoError(err)
			var patchResult map[string]interface{}
			r.NoError(result.UnmarshalTo(&patchResult))
			var expectResult map[string]interface{}
			resultValue, err := value.NewValue(tc.patchResult, nil, "")
			r.NoError(err)
			r.NoError(resultValue.UnmarshalTo(&expectResult))
			r.Equal(expectResult, patchResult)
		})
	}
}

func TestConvertString(t *testing.T) {
	testCases := map[string]struct {
		from        string
		expected    string
		expectedErr error
	}{
		"success": {
			from:     `bt: 'test'`,
			expected: "test",
		},
		"fail": {
			from:        `bt: 123`,
			expectedErr: errors.New("bt: cannot use value 123 (type int) as string|bytes"),
		},
	}

	for name, tc := range testCases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			v, err := value.NewValue(tc.from, nil, "")
			r.NoError(err)
			prd := &provider{}
			err = prd.String(nil, nil, v, nil)
			if tc.expectedErr != nil {
				r.Equal(tc.expectedErr.Error(), err.Error())
				return
			}
			r.NoError(err)
			expected, err := v.LookupValue("str")
			r.NoError(err)
			ret, err := expected.CueValue().String()
			r.NoError(err)
			r.Equal(ret, tc.expected)
		})
	}
}

func TestLog(t *testing.T) {
	r := require.New(t)
	v, err := value.NewValue(`
data: "test"
`, nil, "")
	r.NoError(err)
	logCtx := monitorContext.NewTraceContext(context.Background(), "")
	prd := &provider{}
	err = prd.Log(logCtx, nil, v, nil)
	r.NoError(err)
}

func TestInstall(t *testing.T) {
	p := providers.NewProviders()
	Install(p)
	h, ok := p.GetHandler("util", "string")
	r := require.New(t)
	r.Equal(ok, true)
	r.Equal(h != nil, true)
}
