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

package workspace

import (
	"encoding/json"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	"github.com/stretchr/testify/require"
)

func TestProvider_DoVar(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	p := &provider{}
	r := require.New(t)

	v, err := value.NewValue(`
method: "Put"
path: "clusterIP"
value: "1.1.1.1"
`, nil, "")
	r.NoError(err)
	err = p.DoVar(nil, wfCtx, v, &mockAction{})
	r.NoError(err)
	varV, err := wfCtx.GetVar("clusterIP")
	r.NoError(err)
	s, err := varV.CueValue().String()
	r.NoError(err)
	r.Equal(s, "1.1.1.1")

	v, err = value.NewValue(`
method: "Get"
path: "clusterIP"
`, nil, "")
	r.NoError(err)
	err = p.DoVar(nil, wfCtx, v, &mockAction{})
	r.NoError(err)
	varV, err = v.LookupValue("value")
	r.NoError(err)
	s, err = varV.CueValue().String()
	r.NoError(err)
	r.Equal(s, "1.1.1.1")

	errCases := []string{`
value: "1.1.1.1"
`, `
method: "Get"
`, `
path: "ClusterIP"
`, `
method: "Put"
path: "ClusterIP"
`}

	for _, tCase := range errCases {
		v, err = value.NewValue(tCase, nil, "")
		r.NoError(err)
		err = p.DoVar(nil, wfCtx, v, &mockAction{})
		r.Error(err)
	}
}

func TestProvider_Wait(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	p := &provider{}
	r := require.New(t)
	act := &mockAction{}
	v, err := value.NewValue(`
continue: 100!=100
message: "test log"
`, nil, "")
	r.NoError(err)
	err = p.Wait(nil, wfCtx, v, act)
	r.NoError(err)
	r.Equal(act.wait, true)
	r.Equal(act.msg, "test log")

	act = &mockAction{}
	v, err = value.NewValue(`
continue: 100==100
message: "not invalid"
`, nil, "")
	r.NoError(err)
	err = p.Wait(nil, wfCtx, v, act)
	r.NoError(err)
	r.Equal(act.wait, false)
	r.Equal(act.msg, "")

	act = &mockAction{}
	v, err = value.NewValue(`
continue: bool
message: string
`, nil, "")
	r.NoError(err)
	err = p.Wait(nil, wfCtx, v, act)
	r.NoError(err)
	r.Equal(act.wait, true)

	act = &mockAction{}
	v, err = value.NewValue(``, nil, "")
	r.NoError(err)
	err = p.Wait(nil, wfCtx, v, act)
	r.NoError(err)
	r.Equal(act.wait, true)
}

func TestProvider_Break(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	p := &provider{}
	r := require.New(t)
	act := &mockAction{}
	err := p.Break(nil, wfCtx, nil, act)
	r.NoError(err)
	r.Equal(act.terminate, true)

	act = &mockAction{}
	v, err := value.NewValue(`
message: "terminate"
`, nil, "")
	r.NoError(err)
	err = p.Break(nil, wfCtx, v, act)
	r.NoError(err)
	r.Equal(act.terminate, true)
	r.Equal(act.msg, "terminate")
}

func TestProvider_Fail(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	p := &provider{}
	r := require.New(t)
	act := &mockAction{}
	err := p.Fail(nil, wfCtx, nil, act)
	r.NoError(err)
	r.Equal(act.terminate, true)

	act = &mockAction{}
	v, err := value.NewValue(`
message: "fail"
`, nil, "")
	r.NoError(err)
	err = p.Fail(nil, wfCtx, v, act)
	r.NoError(err)
	r.Equal(act.terminate, true)
	r.Equal(act.msg, "fail")
}

func TestProvider_Message(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	p := &provider{}
	r := require.New(t)
	act := &mockAction{}
	v, err := value.NewValue(`
message: "test"
`, nil, "")
	r.NoError(err)
	err = p.Message(nil, wfCtx, nil, act)
	r.NoError(err)
	r.Equal(act.msg, "")
	err = p.Message(nil, wfCtx, v, act)
	r.NoError(err)
	r.Equal(act.msg, "test")
	err = p.Message(nil, wfCtx, nil, act)
	r.NoError(err)
	r.Equal(act.msg, "test")

	act = &mockAction{}
	v, err = value.NewValue(`
message: "fail"
`, nil, "")
	r.NoError(err)
	err = p.Fail(nil, wfCtx, v, act)
	r.NoError(err)
	r.Equal(act.msg, "fail")
}

type mockAction struct {
	suspend   bool
	terminate bool
	wait      bool
	msg       string
}

func (act *mockAction) Suspend(msg string) {
	act.suspend = true
	if msg != "" {
		act.msg = msg
	}
}

func (act *mockAction) Terminate(msg string) {
	act.terminate = true
	act.msg = msg
}

func (act *mockAction) Wait(msg string) {
	act.wait = true
	if msg != "" {
		act.msg = msg
	}
}

func (act *mockAction) Fail(msg string) {
	act.terminate = true
	if msg != "" {
		act.msg = msg
	}
}

func (act *mockAction) Message(msg string) {
	if msg != "" {
		act.msg = msg
	}
}

func newWorkflowContextForTest(t *testing.T) wfContext.Context {
	cm := corev1.ConfigMap{}
	r := require.New(t)
	testCaseJson, err := yaml.YAMLToJSON([]byte(testCaseYaml))
	r.NoError(err)
	err = json.Unmarshal(testCaseJson, &cm)
	r.NoError(err)

	wfCtx := new(wfContext.WorkflowContext)
	err = wfCtx.LoadFromConfigMap(cm)
	r.NoError(err)
	return wfCtx
}

var (
	testCaseYaml = `apiVersion: v1
data:
  test: ""
kind: ConfigMap
metadata:
  name: app-v1
`
)
