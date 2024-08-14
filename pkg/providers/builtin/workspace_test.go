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

package builtin

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/yaml"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/errors"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
)

func TestProvider_DoVar(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	r := require.New(t)
	ctx := context.Background()

	_, err := DoVar(ctx, &VarParams{
		Params: VarVars{
			Method: "Put",
			Path:   "clusterIP",
			Value:  "1.1.1.1",
		},
		RuntimeParams: providertypes.RuntimeParams{
			WorkflowContext: wfCtx,
		},
	})
	r.NoError(err)
	varV, err := wfCtx.GetVar("clusterIP")
	r.NoError(err)
	s, err := varV.String()
	r.NoError(err)
	r.Equal(s, "1.1.1.1")

	res, err := DoVar(ctx, &VarParams{
		Params: VarVars{
			Method: "Get",
			Path:   "clusterIP",
		},
		RuntimeParams: providertypes.RuntimeParams{
			WorkflowContext: wfCtx,
		},
	})
	r.NoError(err)
	r.Equal(res.Returns.Value, "1.1.1.1")
}

func TestProvider_Wait(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)
	act := &mockAction{}

	_, err := Wait(ctx, &WaitParams{
		Params: WaitVars{
			Continue: false,
			ActionVars: ActionVars{
				Message: "test log",
			},
		},
		RuntimeParams: providertypes.RuntimeParams{
			Action: act,
		},
	})
	_, ok := err.(errors.GenericActionError)
	r.Equal(ok, true)
	r.Equal(act.wait, true)
	r.Equal(act.msg, "test log")

	act = &mockAction{}
	_, err = Wait(ctx, &WaitParams{
		Params: WaitVars{
			Continue: true,
			ActionVars: ActionVars{
				Message: "omit msg",
			},
		},
		RuntimeParams: providertypes.RuntimeParams{
			Action: act,
		},
	})
	r.NoError(err)
	r.Equal(act.wait, false)
	r.Equal(act.msg, "")
}

func TestProvider_Break(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)
	act := &mockAction{}
	_, err := Break(ctx, &ActionParams{
		RuntimeParams: providertypes.RuntimeParams{
			Action: act,
		},
	})
	_, ok := err.(errors.GenericActionError)
	r.Equal(ok, true)
	r.Equal(act.terminate, true)

	act = &mockAction{}
	_, err = Break(ctx, &ActionParams{
		Params: ActionVars{
			Message: "terminate",
		},
		RuntimeParams: providertypes.RuntimeParams{
			Action: act,
		},
	})
	_, ok = err.(errors.GenericActionError)
	r.Equal(ok, true)
	r.Equal(act.terminate, true)
	r.Equal(act.msg, "terminate")
}

func TestProvider_Suspend(t *testing.T) {
	wfCtx := newWorkflowContextForTest(t)
	ctx := context.Background()
	pCtx := process.NewContext(process.ContextData{})
	pCtx.PushData(model.ContextStepSessionID, "test-id")
	r := require.New(t)
	act := &mockAction{}

	params := &SuspendParams{
		Params: SuspendVars{
			Duration: "1s",
		},
		RuntimeParams: providertypes.RuntimeParams{
			Action:          act,
			WorkflowContext: wfCtx,
			ProcessContext:  pCtx,
		},
	}
	_, err := Suspend(ctx, params)
	_, ok := err.(errors.GenericActionError)
	r.Equal(ok, true)
	r.Equal(act.suspend, true)
	r.Equal(act.msg, "Suspended by field ")
	// test second time to check if the suspend is resumed in 1s
	_, err = Suspend(ctx, params)
	_, ok = err.(errors.GenericActionError)
	r.Equal(ok, true)
	r.Equal(act.suspend, true)
	time.Sleep(time.Second)
	_, err = Suspend(ctx, params)
	r.NoError(err)
	r.Equal(act.suspend, false)
}

func TestProvider_Fail(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)
	act := &mockAction{}
	_, err := Fail(ctx, &ActionParams{
		RuntimeParams: providertypes.RuntimeParams{
			Action: act,
		},
	})
	_, ok := err.(errors.GenericActionError)
	r.Equal(ok, true)
	r.Equal(act.terminate, true)

	act = &mockAction{}
	_, err = Fail(ctx, &ActionParams{
		Params: ActionVars{
			Message: "fail",
		},
		RuntimeParams: providertypes.RuntimeParams{
			Action: act,
		},
	})
	_, ok = err.(errors.GenericActionError)
	r.Equal(ok, true)
	r.Equal(act.terminate, true)
	r.Equal(act.msg, "fail")
}

func TestProvider_Message(t *testing.T) {
	ctx := context.Background()
	r := require.New(t)
	act := &mockAction{}
	_, err := Message(ctx, &ActionParams{
		Params: ActionVars{
			Message: "test",
		},
		RuntimeParams: providertypes.RuntimeParams{
			Action: act,
		},
	})
	r.NoError(err)
	r.Equal(act.msg, "test")
}

type mockAction struct {
	suspend   bool
	terminate bool
	wait      bool
	msg       string
}

func (act *mockAction) GetStatus() v1alpha1.StepStatus {
	return v1alpha1.StepStatus{}
}

func (act *mockAction) Suspend(msg string) {
	act.suspend = true
	if msg != "" {
		act.msg = msg
	}
}

func (act *mockAction) Resume(msg string) {
	act.suspend = false
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
	err = wfCtx.LoadFromConfigMap(context.Background(), cm)
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
