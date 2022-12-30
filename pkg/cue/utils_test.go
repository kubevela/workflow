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

package cue

import (
	"testing"

	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/kubevela/workflow/pkg/cue/model/value"
)

func TestIntifyValues(t *testing.T) {
	testcases := map[string]struct {
		input  interface{}
		output interface{}
	}{
		"default case": {
			input:  "string",
			output: "string",
		},
		"float64": {
			input:  float64(1),
			output: 1,
		},
		"array": {
			input:  []interface{}{float64(1), float64(2)},
			output: []interface{}{1, 2},
		},
		"map": {
			input:  map[string]interface{}{"a": float64(1), "b": float64(2)},
			output: map[string]interface{}{"a": 1, "b": 2},
		},
	}
	for name, testcase := range testcases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			result := IntifyValues(testcase.input)
			r.Equal(testcase.output, result)
		})
	}
}

func TestFillUnstructuredObject(t *testing.T) {
	testcases := map[string]struct {
		obj  *unstructured.Unstructured
		json string
	}{
		"test unstructured object with nil value": {
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"metadata": map[string]interface{}{
								"creationTimestamp": nil,
							},
						},
					},
				},
			},
			json: `{"object":{"apiVersion":"apps/v1","kind":"Deployment","spec":{"template":{"metadata":{"creationTimestamp":null}}}}}`,
		},
		"test unstructured object without nil value": {
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"creationTimestamp": "2022-05-25T12:07:02Z",
					},
				},
			},
			json: `{"object":{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"creationTimestamp":"2022-05-25T12:07:02Z"}}}`,
		},
	}

	for name, testcase := range testcases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			value, err := value.NewValue("", nil, "")
			r.NoError(err)
			err = FillUnstructuredObject(value, testcase.obj, "object")
			r.NoError(err)
			json, err := value.CueValue().MarshalJSON()
			r.NoError(err)
			r.Equal(testcase.json, string(json))
		})
	}
}

func TestSubstituteUnstructuredObject(t *testing.T) {
	testcases := map[string]struct {
		obj  *unstructured.Unstructured
		json string
	}{
		"test unstructured object with nil value": {
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"spec": map[string]interface{}{
						"template": map[string]interface{}{
							"metadata": map[string]interface{}{
								"creationTimestamp": nil,
							},
						},
					},
				},
			},
			json: `{"object":{"apiVersion":"apps/v1","kind":"Deployment","spec":{"template":{"metadata":{"creationTimestamp":null}}}}}`,
		},
		"test unstructured object without nil value": {
			obj: &unstructured.Unstructured{
				Object: map[string]interface{}{
					"apiVersion": "apps/v1",
					"kind":       "Deployment",
					"metadata": map[string]interface{}{
						"creationTimestamp": "2022-05-25T12:07:02Z",
					},
				},
			},
			json: `{"object":{"apiVersion":"apps/v1","kind":"Deployment","metadata":{"creationTimestamp":"2022-05-25T12:07:02Z"}}}`,
		},
	}

	for name, testcase := range testcases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			value, err := value.NewValue(`object:{"test": "test"}`, nil, "")
			r.NoError(err)
			err = SetUnstructuredObject(value, testcase.obj, "object")
			r.NoError(err)
			json, err := value.CueValue().MarshalJSON()
			r.NoError(err)
			r.Equal(testcase.json, string(json))
		})
	}
}
