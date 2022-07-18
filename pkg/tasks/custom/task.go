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

package custom

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"cuelang.org/go/cue"
	"github.com/pkg/errors"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/cue/model/sets"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	"github.com/kubevela/workflow/pkg/cue/packages"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/hooks"
	monitorContext "github.com/kubevela/workflow/pkg/monitor/context"
	"github.com/kubevela/workflow/pkg/types"
)

// LoadTaskTemplate gets the workflowStep definition from cluster and resolve it.
type LoadTaskTemplate func(ctx context.Context, name string) (string, error)

// TaskLoader is a client that get taskGenerator.
type TaskLoader struct {
	loadTemplate      func(ctx context.Context, name string) (string, error)
	pd                *packages.PackageDiscover
	handlers          types.Providers
	runOptionsProcess func(*types.TaskRunOptions)
	logLevel          int
}

// GetTaskGenerator get TaskGenerator by name.
func (t *TaskLoader) GetTaskGenerator(ctx context.Context, name string) (types.TaskGenerator, error) {
	templ, err := t.loadTemplate(ctx, name)
	if err != nil {
		return nil, err
	}
	return t.makeTaskGenerator(templ)
}

type taskRunner struct {
	name         string
	run          func(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error)
	checkPending func(ctx wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus)
}

// Name return step name.
func (tr *taskRunner) Name() string {
	return tr.name
}

// Run execute task.
func (tr *taskRunner) Run(ctx wfContext.Context, options *types.TaskRunOptions) (v1alpha1.StepStatus, *types.Operation, error) {
	return tr.run(ctx, options)
}

// Pending check task should be executed or not.
func (tr *taskRunner) Pending(ctx wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus) {
	return tr.checkPending(ctx, stepStatus)
}

// nolint:gocyclo
func (t *TaskLoader) makeTaskGenerator(templ string) (types.TaskGenerator, error) {
	return func(wfStep v1alpha1.WorkflowStep, genOpt *types.TaskGeneratorOptions) (types.TaskRunner, error) {

		exec := &executor{
			handlers: t.handlers,
			wfStatus: v1alpha1.StepStatus{
				Name:  wfStep.Name,
				Type:  wfStep.Type,
				Phase: v1alpha1.WorkflowStepPhaseSucceeded,
			},
		}

		var err error

		if genOpt != nil {
			exec.wfStatus.ID = genOpt.ID
			if genOpt.StepConvertor != nil {
				wfStep, err = genOpt.StepConvertor(wfStep)
				if err != nil {
					return nil, errors.WithMessage(err, "convert step")
				}
			}
		}

		params := map[string]interface{}{}

		if wfStep.Properties != nil && len(wfStep.Properties.Raw) > 0 {
			bt, err := wfStep.Properties.MarshalJSON()
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(bt, &params); err != nil {
				return nil, err
			}
		}

		tRunner := new(taskRunner)
		tRunner.name = wfStep.Name
		tRunner.checkPending = func(ctx wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus) {
			return CheckPending(ctx, wfStep, exec.wfStatus.ID, stepStatus)
		}
		tRunner.run = func(ctx wfContext.Context, options *types.TaskRunOptions) (stepStatus v1alpha1.StepStatus, operations *types.Operation, rErr error) {
			if options.GetTracer == nil {
				options.GetTracer = func(id string, step v1alpha1.WorkflowStep) monitorContext.Context {
					return monitorContext.NewTraceContext(context.Background(), "")
				}
			}
			tracer := options.GetTracer(exec.wfStatus.ID, wfStep).AddTag("step_name", wfStep.Name, "step_type", wfStep.Type)
			tracer.V(t.logLevel)
			defer func() {
				tracer.Commit(string(exec.status().Phase))
			}()

			if t.runOptionsProcess != nil {
				t.runOptionsProcess(options)
			}

			var taskv *value.Value
			var err error
			var paramFile string

			defer func() {
				if taskv == nil {
					taskv, err = convertTemplate(ctx, t.pd, strings.Join([]string{templ, paramFile}, "\n"), wfStep.Name, exec.wfStatus.ID, options.PCtx)
					if err != nil {
						return
					}
				}
				for _, hook := range options.PostStopHooks {
					if err := hook(ctx, taskv, wfStep, exec.status(), options.StepStatus); err != nil {
						exec.wfStatus.Message = err.Error()
						stepStatus = exec.status()
						operations = exec.operation()
						return
					}
				}
			}()

			for _, hook := range options.PreCheckHooks {
				result, err := hook(wfStep, &types.PreCheckOptions{
					PackageDiscover: t.pd,
					ProcessContext:  options.PCtx,
				})
				if err != nil {
					tracer.Error(err, "do preCheckHook")
					exec.Skip(fmt.Sprintf("pre check error: %s", err.Error()))
					return exec.status(), exec.operation(), nil
				}
				if result.Skip {
					exec.Skip("")
					return exec.status(), exec.operation(), nil
				}
				if result.Timeout {
					exec.timeout("")
				}
			}

			paramsValue, err := ctx.MakeParameter(params)
			if err != nil {
				tracer.Error(err, "make parameter")
				return v1alpha1.StepStatus{}, nil, errors.WithMessage(err, "make parameter")
			}

			for _, hook := range options.PreStartHooks {
				if err := hook(ctx, paramsValue, wfStep); err != nil {
					tracer.Error(err, "do preStartHook")
					return v1alpha1.StepStatus{}, nil, errors.WithMessage(err, "do preStartHook")
				}
			}

			if err := paramsValue.Error(); err != nil {
				exec.err(ctx, false, err, types.StatusReasonParameter)
				return exec.status(), exec.operation(), nil
			}

			paramFile = model.ParameterFieldName + ": {}\n"
			if params != nil {
				ps, err := paramsValue.String()
				if err != nil {
					return v1alpha1.StepStatus{}, nil, errors.WithMessage(err, "params encode")
				}
				paramFile = fmt.Sprintf(model.ParameterFieldName+": {%s}\n", ps)
			}

			taskv, err = convertTemplate(ctx, t.pd, strings.Join([]string{templ, paramFile}, "\n"), wfStep.Name, exec.wfStatus.ID, options.PCtx)
			if err != nil {
				exec.err(ctx, false, err, types.StatusReasonRendering)
				return exec.status(), exec.operation(), nil
			}

			exec.tracer = tracer
			if debugLog(taskv) {
				exec.printStep("workflowStepStart", "workflow", "", taskv)
				defer exec.printStep("workflowStepEnd", "workflow", "", taskv)
			}
			if options.Debug != nil {
				defer func() {
					if err := options.Debug(exec.wfStatus.Name, taskv); err != nil {
						tracer.Error(err, "failed to debug")
					}
				}()
			}
			if err := exec.doSteps(tracer, ctx, taskv); err != nil {
				tracer.Error(err, "do steps")
				exec.err(ctx, true, err, types.StatusReasonExecute)
				return exec.status(), exec.operation(), nil
			}

			return exec.status(), exec.operation(), nil
		}
		return tRunner, nil
	}, nil
}

// ValidateIfValue validates the if value
func ValidateIfValue(ctx wfContext.Context, step v1alpha1.WorkflowStep, stepStatus map[string]v1alpha1.StepStatus, options *types.PreCheckOptions) (bool, error) {
	var pd *packages.PackageDiscover
	var pCtx process.Context
	if options != nil {
		pd = options.PackageDiscover
		pCtx = options.ProcessContext
	}

	template := fmt.Sprintf("if: %s", step.If)
	value, err := buildValueForStatus(ctx, step, pd, template, stepStatus, pCtx)
	if err != nil {
		return false, errors.WithMessage(err, "invalid if value")
	}
	check, err := value.GetBool("if")
	if err != nil {
		return false, err
	}
	return check, nil
}

func buildValueForStatus(ctx wfContext.Context, step v1alpha1.WorkflowStep, pd *packages.PackageDiscover, template string, stepStatus map[string]v1alpha1.StepStatus, pCtx process.Context) (*value.Value, error) {
	contextTempl := getContextTemplate(ctx, step.Name, "", pCtx)
	inputsTempl := getInputsTemplate(ctx, step)
	statusTemplate := "\n"
	statusMap := make(map[string]interface{})
	for name, ss := range stepStatus {
		abbrStatus := struct {
			v1alpha1.StepStatus `json:",inline"`
			Failed              bool `json:"failed"`
			Succeeded           bool `json:"succeeded"`
			Skipped             bool `json:"skipped"`
			Timeout             bool `json:"timeout"`
			FailedAfterRetries  bool `json:"failedAfterRetries"`
			Terminate           bool `json:"terminate"`
		}{
			StepStatus:         ss,
			Failed:             ss.Phase == v1alpha1.WorkflowStepPhaseFailed,
			Succeeded:          ss.Phase == v1alpha1.WorkflowStepPhaseSucceeded,
			Skipped:            ss.Phase == v1alpha1.WorkflowStepPhaseSkipped,
			Timeout:            ss.Reason == types.StatusReasonTimeout,
			FailedAfterRetries: ss.Reason == types.StatusReasonFailedAfterRetries,
			Terminate:          ss.Reason == types.StatusReasonTerminate,
		}
		statusMap[name] = abbrStatus
	}
	status, err := json.Marshal(statusMap)
	if err != nil {
		return nil, err
	}
	statusTemplate += fmt.Sprintf("status: %s\n", status)
	statusTemplate += contextTempl
	statusTemplate += "\n" + inputsTempl
	return value.NewValue(template+"\n"+statusTemplate, pd, "")
}

func convertTemplate(ctx wfContext.Context, pd *packages.PackageDiscover, templ, step, id string, pCtx process.Context) (*value.Value, error) {
	contextTempl := getContextTemplate(ctx, step, id, pCtx)
	return value.NewValue(templ+contextTempl, pd, "", value.ProcessScript, value.TagFieldOrder)
}

// MakeValueForContext makes context value
func MakeValueForContext(ctx wfContext.Context, pd *packages.PackageDiscover, step, id string, pCtx process.Context) (*value.Value, error) {
	contextTempl := getContextTemplate(ctx, step, id, pCtx)
	return value.NewValue(contextTempl, pd, "")
}

func getContextTemplate(ctx wfContext.Context, step, id string, pCtx process.Context) string {
	var contextTempl string
	meta, _ := ctx.GetVar(types.ContextKeyMetadata)
	if meta != nil {
		ms, err := meta.String()
		if err != nil {
			return ""
		}
		contextTempl = fmt.Sprintf("\ncontext: {%s}\ncontext: stepSessionID: \"%s\"", ms, id)
	}
	if pCtx == nil {
		return ""
	}
	pCtx.PushData(model.ContextStepSessionID, id)
	pCtx.PushData(model.ContextStepName, step)
	c, err := pCtx.ExtendedContextFile()
	if err != nil {
		return ""
	}
	contextTempl += "\n" + c
	return contextTempl
}

func getInputsTemplate(ctx wfContext.Context, step v1alpha1.WorkflowStep) string {
	var inputsTempl string
	for _, input := range step.Inputs {
		inputValue, err := ctx.GetVar(strings.Split(input.From, ".")...)
		if err != nil {
			continue
		}
		s, err := inputValue.String()
		if err != nil {
			continue
		}
		inputsTempl += fmt.Sprintf("\ninputs: \"%s\": %s", input.From, s)
	}
	return inputsTempl
}

type executor struct {
	handlers types.Providers

	wfStatus           v1alpha1.StepStatus
	suspend            bool
	terminated         bool
	failedAfterRetries bool
	wait               bool
	skip               bool

	tracer monitorContext.Context
}

// Suspend let workflow pause.
func (exec *executor) Suspend(message string) {
	exec.suspend = true
	exec.wfStatus.Phase = v1alpha1.WorkflowStepPhaseSucceeded
	exec.wfStatus.Message = message
	exec.wfStatus.Reason = types.StatusReasonSuspend
}

// Terminate let workflow terminate.
func (exec *executor) Terminate(message string) {
	exec.terminated = true
	exec.wfStatus.Phase = v1alpha1.WorkflowStepPhaseSucceeded
	exec.wfStatus.Message = message
	exec.wfStatus.Reason = types.StatusReasonTerminate
}

// Wait let workflow wait.
func (exec *executor) Wait(message string) {
	exec.wait = true
	if exec.wfStatus.Phase != v1alpha1.WorkflowStepPhaseFailed {
		exec.wfStatus.Phase = v1alpha1.WorkflowStepPhaseRunning
		exec.wfStatus.Reason = types.StatusReasonWait
		exec.wfStatus.Message = message
	}
}

// Fail let the step fail, its status is failed and reason is Action
func (exec *executor) Fail(message string) {
	exec.terminated = true
	exec.wfStatus.Phase = v1alpha1.WorkflowStepPhaseFailed
	exec.wfStatus.Reason = types.StatusReasonAction
	exec.wfStatus.Message = message
}

func (exec *executor) Skip(message string) {
	exec.skip = true
	exec.wfStatus.Phase = v1alpha1.WorkflowStepPhaseSkipped
	exec.wfStatus.Reason = types.StatusReasonSkip
	exec.wfStatus.Message = message
}

func (exec *executor) timeout(message string) {
	exec.terminated = true
	exec.wfStatus.Phase = v1alpha1.WorkflowStepPhaseFailed
	exec.wfStatus.Reason = types.StatusReasonTimeout
	exec.wfStatus.Message = message
}

func (exec *executor) err(ctx wfContext.Context, wait bool, err error, reason string) {
	exec.wait = wait
	exec.wfStatus.Phase = v1alpha1.WorkflowStepPhaseFailed
	exec.wfStatus.Message = err.Error()
	if exec.wfStatus.Reason == "" {
		exec.wfStatus.Reason = reason
	}
	exec.checkErrorTimes(ctx)
}

func (exec *executor) checkErrorTimes(ctx wfContext.Context) {
	times := ctx.IncreaseCountValueInMemory(types.ContextPrefixFailedTimes, exec.wfStatus.ID)
	if times >= types.MaxWorkflowStepErrorRetryTimes {
		exec.wait = false
		exec.failedAfterRetries = true
		exec.wfStatus.Reason = types.StatusReasonFailedAfterRetries
	}
}

func (exec *executor) operation() *types.Operation {
	return &types.Operation{
		Suspend:            exec.suspend,
		Terminated:         exec.terminated,
		Waiting:            exec.wait,
		Skip:               exec.skip,
		FailedAfterRetries: exec.failedAfterRetries,
	}
}

func (exec *executor) status() v1alpha1.StepStatus {
	return exec.wfStatus
}

func (exec *executor) printStep(phase string, provider string, do string, v *value.Value) {
	msg, _ := v.String()
	exec.tracer.Info("cue eval: "+msg, "phase", phase, "provider", provider, "do", do)
}

// Handle process task-step value by provider and do.
func (exec *executor) Handle(ctx monitorContext.Context, wfCtx wfContext.Context, provider string, do string, v *value.Value) error {
	if debugLog(v) {
		exec.printStep("stepStart", provider, do, v)
		defer exec.printStep("stepEnd", provider, do, v)
	}
	h, exist := exec.handlers.GetHandler(provider, do)
	if !exist {
		return errors.Errorf("handler not found")
	}
	return h(ctx, wfCtx, v, exec)
}

func (exec *executor) doSteps(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value) error {
	do := OpTpy(v)
	if do != "" && do != "steps" {
		provider := opProvider(v)
		if err := exec.Handle(ctx, wfCtx, provider, do, v); err != nil {
			return errors.WithMessagef(err, "run step(provider=%s,do=%s)", provider, do)
		}
		return nil
	}
	return v.StepByFields(func(fieldName string, in *value.Value) (bool, error) {
		if in.CueValue().IncompleteKind() == cue.BottomKind {
			errInfo, err := sets.ToString(in.CueValue())
			if err != nil {
				errInfo = "value is _|_"
			}
			return true, errors.New(errInfo + "(bottom kind)")
		}
		if retErr := in.CueValue().Err(); retErr != nil {
			errInfo, err := sets.ToString(in.CueValue())
			if err == nil {
				retErr = errors.WithMessage(retErr, errInfo)
			}
			return false, retErr
		}

		if isStepList(fieldName) {
			return false, in.StepByList(func(name string, item *value.Value) (bool, error) {
				do := OpTpy(item)
				if do == "" {
					return false, nil
				}
				return false, exec.doSteps(ctx, wfCtx, item)
			})
		}
		do := OpTpy(in)
		if do == "" {
			return false, nil
		}
		if do == "steps" {
			if err := exec.doSteps(ctx, wfCtx, in); err != nil {
				return false, err
			}
		} else {
			provider := opProvider(in)
			if err := exec.Handle(ctx, wfCtx, provider, do, in); err != nil {
				return false, errors.WithMessagef(err, "run step(provider=%s,do=%s)", provider, do)
			}
		}

		if exec.suspend || exec.terminated || exec.wait {
			return true, nil
		}
		return false, nil
	})
}

func isStepList(fieldName string) bool {
	if fieldName == "#up" {
		return true
	}
	return strings.HasPrefix(fieldName, "#up_")
}

func debugLog(v *value.Value) bool {
	debug, _ := v.CueValue().LookupDef("#debug").Bool()
	return debug
}

// OpTpy get label do
func OpTpy(v *value.Value) string {
	return getLabel(v, "#do")
}

func opProvider(v *value.Value) string {
	provider := getLabel(v, "#provider")
	if provider == "" {
		provider = "builtin"
	}
	return provider
}

func getLabel(v *value.Value, label string) string {
	do, err := v.Field(label)
	if err == nil && do.Exists() {
		if str, err := do.String(); err == nil {
			return str
		}
	}
	return ""
}

// NewTaskLoader create a tasks loader.
func NewTaskLoader(lt LoadTaskTemplate, pkgDiscover *packages.PackageDiscover, handlers types.Providers, logLevel int, pCtx process.Context) *TaskLoader {
	return &TaskLoader{
		loadTemplate: lt,
		pd:           pkgDiscover,
		handlers:     handlers,
		runOptionsProcess: func(options *types.TaskRunOptions) {
			if len(options.PreStartHooks) == 0 {
				options.PreStartHooks = append(options.PreStartHooks, hooks.Input)
			}
			if len(options.PostStopHooks) == 0 {
				options.PostStopHooks = append(options.PostStopHooks, hooks.Output)
			}
			options.PCtx = pCtx
		},
		logLevel: logLevel,
	}
}

// CheckPending checks whether to pending task run
func CheckPending(ctx wfContext.Context, step v1alpha1.WorkflowStep, id string, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus) {
	pStatus := v1alpha1.StepStatus{
		Phase: v1alpha1.WorkflowStepPhasePending,
		Type:  step.Type,
		ID:    id,
		Name:  step.Name,
	}
	for _, depend := range step.DependsOn {
		pStatus.Message = fmt.Sprintf("Pending on DependsOn: %s", depend)
		if status, ok := stepStatus[depend]; ok {
			if !types.IsStepFinish(status.Phase, status.Reason) {
				return true, pStatus
			}
		} else {
			return true, pStatus
		}
	}
	for _, input := range step.Inputs {
		pStatus.Message = fmt.Sprintf("Pending on Input: %s", input.From)
		if _, err := ctx.GetVar(strings.Split(input.From, ".")...); err != nil {
			return true, pStatus
		}
	}
	return false, v1alpha1.StepStatus{}
}
