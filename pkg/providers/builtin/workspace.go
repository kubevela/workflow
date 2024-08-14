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
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"cuelang.org/go/cue/cuecontext"

	cuexruntime "github.com/kubevela/pkg/cue/cuex/runtime"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/errors"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
)

const (
	// ProviderName is provider name.
	ProviderName = "builtin"
	// ResumeTimeStamp is resume time stamp.
	ResumeTimeStamp = "resumeTimeStamp"
	// SuspendTimeStamp is suspend time stamp.
	SuspendTimeStamp = "suspendTimeStamp"
)

// VarVars .
type VarVars struct {
	Method string `json:"method"`
	Path   string `json:"path"`
	Value  any    `json:"value"`
}

// VarReturnVars
type VarReturnVars struct {
	Value any `json:"value"`
}

type VarReturns = providertypes.Returns[VarReturnVars]

// VarParams .
type VarParams = providertypes.Params[VarVars]

// DoVar get & put variable from context.
func DoVar(_ context.Context, params *VarParams) (*VarReturns, error) {
	wfCtx := params.RuntimeParams.WorkflowContext
	path := params.Params.Path

	switch params.Params.Method {
	case "Get":
		value, err := wfCtx.GetVar(strings.Split(path, ".")...)
		if err != nil {
			return nil, err
		}
		b, err := value.MarshalJSON()
		if err != nil {
			return nil, err
		}
		var v any
		if err := json.Unmarshal(b, &v); err != nil {
			return nil, err
		}
		return &VarReturns{
			Returns: VarReturnVars{
				Value: v,
			},
		}, nil
	case "Put":
		b, err := json.Marshal(params.Params.Value)
		if err != nil {
			return nil, err
		}
		if err := wfCtx.SetVar(cuecontext.New().CompileBytes(b), strings.Split(path, ".")...); err != nil {
			return nil, err
		}
		return nil, nil
	}
	return nil, nil
}

// ActionVars .
type ActionVars struct {
	Message string `json:"message,omitempty"`
}

// ActionParams .
type ActionParams = providertypes.Params[ActionVars]

// WaitVars .
type WaitVars struct {
	Continue bool `json:"continue"`
	ActionVars
}

// WaitParams .
type WaitParams = providertypes.Params[WaitVars]

// Wait let workflow wait.
func Wait(_ context.Context, params *WaitParams) (*any, error) {
	if params.Params.Continue {
		return nil, nil
	}
	params.Action.Wait(params.Params.Message)
	return nil, errors.GenericActionError(errors.ActionWait)
}

// Break let workflow terminate.
func Break(_ context.Context, params *ActionParams) (*any, error) {
	params.Action.Terminate(params.Params.Message)
	return nil, errors.GenericActionError(errors.ActionTerminate)
}

// Fail let the step fail, its status is failed and reason is Action
func Fail(_ context.Context, params *ActionParams) (*any, error) {
	params.Action.Fail(params.Params.Message)
	return nil, errors.GenericActionError(errors.ActionTerminate)
}

// SuspendVars .
type SuspendVars struct {
	Duration string `json:"duration,omitempty"`
	ActionVars
}

// SuspendParams .
type SuspendParams = providertypes.Params[SuspendVars]

// Suspend let the step suspend, its status is suspending and reason is Suspend
func Suspend(_ context.Context, params *SuspendParams) (*any, error) {
	pCtx := params.ProcessContext
	wfCtx := params.WorkflowContext
	act := params.Action
	stepID := fmt.Sprint(pCtx.GetData(model.ContextStepSessionID))
	timestamp := wfCtx.GetMutableValue(stepID, ResumeTimeStamp)

	var msg string
	if msg == "" {
		msg = fmt.Sprintf("Suspended by field %s", params.FieldLabel)
	}
	if timestamp != "" {
		t, err := time.Parse(time.RFC3339, timestamp)
		if err != nil {
			return nil, fmt.Errorf("failed to parse timestamp %s: %w", timestamp, err)
		}
		if time.Now().After(t) {
			act.Resume("")
			return nil, nil
		}
		act.Suspend(msg)
		return nil, errors.GenericActionError(errors.ActionSuspend)
	}
	if params.Params.Duration != "" {
		d, err := time.ParseDuration(params.Params.Duration)
		if err != nil {
			return nil, fmt.Errorf("failed to parse duration %s: %w", params.Params.Duration, err)
		}
		wfCtx.SetMutableValue(time.Now().Add(d).Format(time.RFC3339), stepID, ResumeTimeStamp)
	}
	if ts := wfCtx.GetMutableValue(stepID, params.FieldLabel, SuspendTimeStamp); ts != "" {
		if act.GetStatus().Phase == v1alpha1.WorkflowStepPhaseRunning {
			// if it is already suspended before and has been resumed, we should not suspend it again.
			return nil, nil
		}
	} else {
		wfCtx.SetMutableValue(time.Now().Format(time.RFC3339), stepID, params.FieldLabel, SuspendTimeStamp)
	}
	act.Suspend(msg)
	return nil, errors.GenericActionError(errors.ActionSuspend)
}

// Message writes message to step status, note that the message will be overwritten by the next message.
func Message(_ context.Context, params *ActionParams) (*any, error) {
	params.Action.Message(params.Params.Message)
	return nil, nil
}

//go:embed workspace.cue
var template string

// GetTemplate returns the cue template.
func GetTemplate() string {
	return template
}

// GetProviders returns the cue providers.
func GetProviders() map[string]cuexruntime.ProviderFn {
	return map[string]cuexruntime.ProviderFn{
		"wait":    providertypes.GenericProviderFn[WaitVars, any](Wait),
		"break":   providertypes.GenericProviderFn[ActionVars, any](Break),
		"fail":    providertypes.GenericProviderFn[ActionVars, any](Fail),
		"message": providertypes.GenericProviderFn[ActionVars, any](Message),
		"var":     providertypes.GenericProviderFn[VarVars, VarReturns](DoVar),
		"suspend": providertypes.GenericProviderFn[SuspendVars, any](Suspend),
	}
}
