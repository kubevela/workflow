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

package model

import (
	"fmt"
	"testing"

	"cuelang.org/go/cue"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

func TestGetCompileError(t *testing.T) {
	testcases := []struct {
		src     string
		wantErr bool
		errInfo string
	}{{
		src: ` env: [{
	name:  "HELLO"
	value: "_A_|_B_|_C_"
}]`,
		wantErr: false,
		errInfo: "",
	}, {
		src: ` env: [{
	name:  conflicting
	value:  _|_ // conflicting values "ENV_LEVEL" and "JAVA_TOOL_OPTIONS"
}]`,
		wantErr: true,
		errInfo: "_|_ // conflicting values \"ENV_LEVEL\" and \"JAVA_TOOL_OPTIONS\"",
	}, {
		src: ` env: [{
	name:  conflicting-1
	value:  _|_ // conflicting values "ENV_LEVEL" and "JAVA_TOOL_OPTIONS"
	},{
	name:  conflicting-2
	value:  _|_ // conflicting values "HELLO" and "WORLD"
}]`,
		wantErr: true,
		errInfo: "_|_ // conflicting values \"ENV_LEVEL\" and \"JAVA_TOOL_OPTIONS\"," +
			"_|_ // conflicting values \"HELLO\" and \"WORLD\"",
	}}
	for _, tt := range testcases {
		r := require.New(t)
		errInfo, contains := IndexMatchLine(tt.src, "_|_")
		r.Equal(tt.wantErr, contains)
		r.Equal(tt.errInfo, errInfo)
	}

}

func TestInstance(t *testing.T) {

	testCases := []struct {
		src string
		gvk schema.GroupVersionKind
	}{{
		src: `apiVersion: "apps/v1"
kind: "Deployment"
metadata: name: "test"
`,
		gvk: schema.GroupVersionKind{
			Group:   "apps",
			Version: "v1",
			Kind:    "Deployment",
		}},
	}

	for _, v := range testCases {
		var r cue.Runtime
		inst, err := r.Compile("-", v.src)
		if err != nil {
			t.Error(err)
			return
		}
		base, err := NewBase(inst.Value())
		if err != nil {
			t.Error(err)
			return
		}
		baseObj, err := base.Unstructured()
		if err != nil {
			t.Error(err)
			return
		}
		re := require.New(t)
		re.Equal(v.gvk, baseObj.GetObjectKind().GroupVersionKind())
		re.Equal(true, base.IsBase())

		other, err := NewOther(inst.Value())
		if err != nil {
			t.Error(err)
			return
		}
		otherObj, err := other.Unstructured()
		if err != nil {
			t.Error(err)
			return
		}

		re.Equal(v.gvk, otherObj.GetObjectKind().GroupVersionKind())
		re.Equal(false, other.IsBase())
	}
}

func TestIncompleteError(t *testing.T) {
	base := `parameter: {
	name: string
	// +usage=Which image would you like to use for your service
	// +short=i
	image: string
	// +usage=Which port do you want customer traffic sent to
	// +short=p
	port: *8080 | int
	env: [...{
		name:  string
		value: string
	}]
	cpu?: string
}
output: {
	apiVersion: "apps/v1"
	kind:       "Deployment"
	metadata: name: parameter.name
	spec: {
		selector:
			matchLabels:
				app: parameter.name
		template: {
			metadata:
				labels:
					app: parameter.name
			spec: containers: [{
				image: parameter.image
				name:  parameter.name
				env:   parameter.env
				ports: [{
					containerPort: parameter.port
					protocol:      "TCP"
					name:          "default"
				}]
				if parameter["cpu"] != _|_ {
					resources: {
						limits:
							cpu: parameter.cpu
						requests:
							cpu: parameter.cpu
					}
				}
			}]
	}
	}
}
`

	var r cue.Runtime
	re := require.New(t)
	inst, err := r.Compile("-", base)
	re.NoError(err)
	newbase, err := NewBase(inst.Value())
	re.NoError(err)
	data, err := newbase.Unstructured()
	re.Error(err)
	var expnil *unstructured.Unstructured
	re.Equal(expnil, data)
}

func TestError(t *testing.T) {
	ins := &instance{
		v: ``,
	}
	r := require.New(t)
	_, err := ins.Unstructured()
	r.Equal(err.Error(), "Object 'Kind' is missing in '{}'")
	ins = &instance{
		v: `
apiVersion: "apps/v1"
kind:       "Deployment"
metadata: name: parameter.name
`,
	}
	_, err = ins.Unstructured()
	r.Equal(err.Error(), fmt.Sprintf(`failed to have the workload/trait unstructured: metadata.name: reference "%s" not found`, ParameterFieldName))
	ins = &instance{
		v: `
apiVersion: "apps/v1"
kind:       "Deployment"
metadata: name: "abc"
`,
	}
	obj, err := ins.Unstructured()
	r.Equal(err, nil)
	r.Equal(obj, &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "apps/v1",
			"kind":       "Deployment",
			"metadata": map[string]interface{}{
				"name": "abc",
			},
		},
	})

	ins = &instance{
		v: `
apiVersion: "source.toolkit.fluxcd.io/v1beta1"
metadata: {
	name: "grafana"
}
kind: "HelmRepository"
spec: {
	url:      string
	interval: *"5m" | string
}`,
	}
	o, err := ins.Unstructured()
	r.Nil(o)
	r.NotNil(err)
}
