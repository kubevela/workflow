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
	"fmt"
	"strings"
	"time"

	monitorContext "github.com/kubevela/pkg/monitor/context"
	"sigs.k8s.io/kind/pkg/errors"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/types"
)

const (
	// ProviderName is provider name.
	ProviderName = "builtin"
	// ResumeTimeStamp is resume time stamp.
	ResumeTimeStamp = "resumeTimeStamp"
	// SuspendTimeStamp is suspend time stamp.
	SuspendTimeStamp = "suspendTimeStamp"
)

type provider struct {
	pCtx process.Context
}

// DoVar get & put variable from context.
func (h *provider) DoVar(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error { //nolint:revive,unused
	methodV, err := v.Field("method")
	if err != nil {
		return err
	}
	method, err := methodV.String()
	if err != nil {
		return err
	}

	pathV, err := v.Field("path")
	if err != nil {
		return err
	}
	path, err := pathV.String()
	if err != nil {
		return err
	}

	switch method {
	case "Get":
		value, err := wfCtx.GetVar(strings.Split(path, ".")...)
		if err != nil {
			return err
		}
		raw, err := value.String()
		if err != nil {
			return err
		}
		return v.FillRaw(raw, "value")
	case "Put":
		value, err := v.LookupValue("value")
		if err != nil {
			return err
		}
		return wfCtx.SetVar(value, strings.Split(path, ".")...)
	}
	return nil
}

// Wait let workflow wait.
func (h *provider) Wait(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error { //nolint:revive,unused
	cv := v.CueValue()
	if cv.Exists() {
		ret := cv.LookupPath(value.FieldPath("continue"))
		if ret.Exists() {
			isContinue, err := ret.Bool()
			if err == nil && isContinue {
				return nil
			}
		}
	}
	msg, _ := v.GetString("message")
	act.Wait(msg)
	return nil
}

// Break let workflow terminate.
func (h *provider) Break(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error { //nolint:revive,unused
	var msg string
	if v != nil {
		msg, _ = v.GetString("message")
	}
	act.Terminate(msg)
	return nil
}

// Fail let the step fail, its status is failed and reason is Action
func (h *provider) Fail(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error { //nolint:revive,unused
	var msg string
	if v != nil {
		msg, _ = v.GetString("message")
	}
	act.Fail(msg)
	return nil
}

// Suspend let the step suspend, its status is suspending and reason is Suspend
func (h *provider) Suspend(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error { //nolint:revive,unused
	stepID := fmt.Sprint(h.pCtx.GetData(model.ContextStepSessionID))
	timestamp := wfCtx.GetMutableValue(stepID, ResumeTimeStamp)
	var msg string
	if v != nil {
		msg, _ = v.GetString("message")
		if msg == "" {
			msg = fmt.Sprintf("Suspended by field %s", v.FieldName())
		}
	}
	if timestamp != "" {
		t, err := time.Parse(time.RFC3339, timestamp)
		if err != nil {
			return errors.Wrap(err, "failed to parse timestamp")
		}
		if time.Now().After(t) {
			act.Resume("")
			return nil
		}
		act.Suspend(msg)
		return nil
	}
	if v != nil {
		d, _ := v.LookupValue("duration")
		if d != nil && d.CueValue().Exists() {
			dur, err := d.CueValue().String()
			if err != nil {
				return err
			}
			duration, err := time.ParseDuration(dur)
			if err != nil {
				return errors.Wrap(err, "failed to parse duration")
			}
			wfCtx.SetMutableValue(time.Now().Add(duration).Format(time.RFC3339), stepID, ResumeTimeStamp)
		}
	}
	if ts := wfCtx.GetMutableValue(stepID, v.FieldName(), SuspendTimeStamp); ts != "" {
		if act.GetStatus().Phase == v1alpha1.WorkflowStepPhaseRunning {
			// if it is already suspended before and has been resumed, we should not suspend it again.
			return nil
		}
	} else {
		wfCtx.SetMutableValue(time.Now().Format(time.RFC3339), stepID, v.FieldName(), SuspendTimeStamp)
	}
	act.Suspend(msg)
	return nil
}

// Message writes message to step status, note that the message will be overwritten by the next message.
func (h *provider) Message(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error { //nolint:revive,unused
	var msg string
	if v != nil {
		msg, _ = v.GetString("message")
	}
	act.Message(msg)
	return nil
}

// Install register handler to provider discover.
func Install(p types.Providers, pCtx process.Context) {
	prd := &provider{
		pCtx: pCtx,
	}
	p.Register(ProviderName, map[string]types.Handler{
		"wait":    prd.Wait,
		"break":   prd.Break,
		"fail":    prd.Fail,
		"message": prd.Message,
		"var":     prd.DoVar,
		"suspend": prd.Suspend,
	})
}
