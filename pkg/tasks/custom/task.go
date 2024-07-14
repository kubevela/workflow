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
	"cuelang.org/go/cue/cuecontext"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kubevela/pkg/cue/cuex"
	"github.com/kubevela/pkg/cue/util"
	monitorContext "github.com/kubevela/pkg/monitor/context"
	"github.com/kubevela/pkg/util/slices"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/hooks"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
	"github.com/kubevela/workflow/pkg/types"
)

// LoadTaskTemplate gets the workflowStep definition from cluster and resolve it.
type LoadTaskTemplate func(ctx context.Context, name string) (string, error)

// TaskLoader is a client that get taskGenerator.
type TaskLoader struct {
	loadTemplate      func(ctx context.Context, name string) (string, error)
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
	checkPending func(ctx monitorContext.Context, wfCtx wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus)
	fillContext  func(ctx monitorContext.Context, processCtx process.Context) types.ContextDataResetter
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
func (tr *taskRunner) Pending(ctx monitorContext.Context, wfCtx wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus) {
	return tr.checkPending(ctx, wfCtx, stepStatus)
}

func (tr *taskRunner) FillContextData(ctx monitorContext.Context, processCtx process.Context) types.ContextDataResetter {
	return tr.fillContext(ctx, processCtx)
}

// nolint:gocyclo
func (t *TaskLoader) makeTaskGenerator(templ string) (types.TaskGenerator, error) {
	return func(wfStep v1alpha1.WorkflowStep, genOpt *types.TaskGeneratorOptions) (types.TaskRunner, error) {

		initialStatus := v1alpha1.StepStatus{
			Name:  wfStep.Name,
			Type:  wfStep.Type,
			Phase: v1alpha1.WorkflowStepPhaseSucceeded,
		}
		exec := &executor{
			wfStatus:   initialStatus,
			stepStatus: initialStatus,
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

		tRunner := new(taskRunner)
		tRunner.name = wfStep.Name
		tRunner.checkPending = func(ctx monitorContext.Context, wfCtx wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus) {
			options := &types.TaskRunOptions{}
			if t.runOptionsProcess != nil {
				t.runOptionsProcess(options)
			}

			resetter := tRunner.fillContext(ctx, options.PCtx)
			defer resetter(options.PCtx)
			basicVal, _ := MakeBasicValue(ctx, options.Compiler, wfStep.Properties, options.PCtx)

			return CheckPending(wfCtx, wfStep, exec.wfStatus.ID, stepStatus, basicVal)
		}
		tRunner.fillContext = func(ctx monitorContext.Context, processCtx process.Context) types.ContextDataResetter {
			metas := []process.StepMetaKV{
				process.WithName(wfStep.Name),
				process.WithSessionID(exec.wfStatus.ID),
				process.WithSpanID(ctx.GetID()),
			}
			manager := process.NewStepRunTimeMeta()
			manager.Fill(processCtx, metas)
			return func(processCtx process.Context) {
				manager.Remove(processCtx, slices.Map(metas,
					func(t process.StepMetaKV) string {
						return t.Key
					}),
				)
			}
		}
		tRunner.run = func(wfCtx wfContext.Context, options *types.TaskRunOptions) (stepStatus v1alpha1.StepStatus, operations *types.Operation, rErr error) {
			if options.GetTracer == nil {
				options.GetTracer = func(id string, step v1alpha1.WorkflowStep) monitorContext.Context { //nolint:revive,unused
					return monitorContext.NewTraceContext(context.Background(), "")
				}
			}
			tracer := options.GetTracer(exec.wfStatus.ID, wfStep).AddTag("step_name", wfStep.Name, "step_type", wfStep.Type)
			tracer.V(t.logLevel)
			exec.tracer = tracer
			defer func() {
				tracer.Commit(string(exec.status().Phase))
			}()

			if t.runOptionsProcess != nil {
				t.runOptionsProcess(options)
			}
			resetter := tRunner.fillContext(tracer, options.PCtx)
			defer resetter(options.PCtx)

			ctx := providertypes.WithRuntimeParams(tracer.GetContext(), providertypes.RuntimeParams{
				WorkflowContext: wfCtx,
				ProcessContext:  options.PCtx,
				Action:          exec,
			})

			basicVal, err := MakeBasicValue(tracer, options.Compiler, wfStep.Properties, options.PCtx)
			if err != nil {
				tracer.Error(err, "make context parameter")
				return v1alpha1.StepStatus{}, nil, errors.WithMessage(err, "make context parameter")
			}

			var taskv cue.Value
			defer func() {
				if r := recover(); r != nil {
					exec.err(wfCtx, false, fmt.Errorf("invalid cue task for evaluation: %v", r), types.StatusReasonRendering)
					stepStatus = exec.status()
					operations = exec.operation()
					return
				}
				if taskv == (cue.Value{}) {
					taskv = basicVal.FillPath(cue.ParsePath(""), templ)
				}
				if options.Debug != nil {
					if err := options.Debug(exec.wfStatus.ID, taskv); err != nil {
						tracer.Error(err, "failed to debug")
					}
				}
				for _, hook := range options.PostStopHooks {
					if err := hook(wfCtx, taskv, wfStep, exec.status(), options.StepStatus); err != nil {
						exec.wfStatus.Message = err.Error()
						stepStatus = exec.status()
						operations = exec.operation()
						return
					}
				}
			}()

			for _, hook := range options.PreCheckHooks {
				result, err := hook(wfStep, &types.PreCheckOptions{BasicValue: basicVal})
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

			for _, hook := range options.PreStartHooks {
				if basicVal, err = hook(wfCtx, basicVal, wfStep); err != nil {
					tracer.Error(err, "do preStartHook")
					exec.err(wfCtx, false, err, types.StatusReasonInput)
					return exec.status(), exec.operation(), nil
				}
			}

			if status, ok := options.StepStatus[wfStep.Name]; ok {
				exec.stepStatus = status
			}
			basicTempl, err := util.ToString(basicVal)
			if err != nil {
				exec.err(wfCtx, false, err, types.StatusReasonRendering)
				return exec.status(), exec.operation(), nil
			}
			taskv, err = options.Compiler.CompileString(ctx, strings.Join([]string{templ, basicTempl}, "\n"))
			if err != nil {
				// resolve the action break error
				if resolvedErr := ResolveActionBreak(err); resolvedErr != nil {
					tracer.Error(resolvedErr, "do steps")
					exec.err(wfCtx, true, resolvedErr, types.StatusReasonExecute)
					return exec.status(), exec.operation(), nil
				}
			}

			if exec.stepStatus.Phase == v1alpha1.WorkflowStepPhaseSucceeded && taskv.Err() != nil {
				tracer.Error(taskv.Err(), "do steps")
				exec.err(wfCtx, true, taskv.Err(), types.StatusReasonExecute)
				return exec.status(), exec.operation(), nil
			}

			return exec.status(), exec.operation(), nil
		}
		return tRunner, nil
	}, nil
}

// ValidateIfValue validates the if value
func ValidateIfValue(ctx wfContext.Context, step v1alpha1.WorkflowStep, stepStatus map[string]v1alpha1.StepStatus, basicVal cue.Value) (bool, error) {
	s, _ := util.ToString(basicVal)
	template := fmt.Sprintf("if: %s\n%s\n%s\n%s", step.If, getInputsTemplate(ctx, step, basicVal), buildValueForStatus(ctx, stepStatus), s)
	v := cuecontext.New().CompileString(template).LookupPath(cue.ParsePath("if"))
	if v.Err() != nil {
		return false, errors.WithMessage(v.Err(), "invalid if value")
	}
	check, err := v.Bool()
	if err != nil {
		return false, err
	}
	return check, nil
}

func buildValueForStatus(_ wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) string {
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
	b, _ := json.Marshal(statusMap)
	return fmt.Sprintf("status: %s", string(b))
}

// MakeBasicValue makes basic value
func MakeBasicValue(ctx monitorContext.Context, compiler *cuex.Compiler, properties *runtime.RawExtension, pCtx process.Context) (cue.Value, error) {
	// use default compiler to compile the basic value without providers
	v, err := compiler.CompileStringWithOptions(ctx, getContextTemplate(pCtx), cuex.WithExtraData(
		model.ParameterFieldName, properties,
	), cuex.DisableResolveProviderFunctions{})
	if err != nil {
		return cue.Value{}, err
	}
	if v.Err() != nil {
		return cue.Value{}, v.Err()
	}
	return v, nil
}

func getContextTemplate(pCtx process.Context) string {
	var contextTempl string
	if pCtx == nil {
		return contextTempl
	}
	c, err := pCtx.BaseContextFile()
	if err != nil {
		return ""
	}
	return c
}

func getInputsTemplate(ctx wfContext.Context, step v1alpha1.WorkflowStep, basicVal cue.Value) string {
	var inputsTempl string
	for _, input := range step.Inputs {
		inputValue, err := ctx.GetVar(input.From)
		if err != nil {
			inputValue = basicVal.LookupPath(value.FieldPath(input.From))
			if !inputValue.Exists() {
				continue
			}
		}
		s, err := util.ToString(inputValue)
		if err != nil {
			continue
		}
		inputsTempl += fmt.Sprintf("\n\"%s\": {\n%s\n}", input.From, s)
	}
	return fmt.Sprintf("inputs: {%s\n}", inputsTempl)
}

// NewTaskLoader create a tasks loader.
func NewTaskLoader(lt LoadTaskTemplate, logLevel int, pCtx process.Context, compiler *cuex.Compiler) *TaskLoader {
	return &TaskLoader{
		loadTemplate: lt,
		runOptionsProcess: func(options *types.TaskRunOptions) {
			if len(options.PreStartHooks) == 0 {
				options.PreStartHooks = append(options.PreStartHooks, hooks.Input)
			}
			if len(options.PostStopHooks) == 0 {
				options.PostStopHooks = append(options.PostStopHooks, hooks.Output)
			}
			options.PCtx = pCtx
			options.Compiler = compiler
		},
		logLevel: logLevel,
	}
}

// CheckPending checks whether to pending task run
func CheckPending(ctx wfContext.Context, step v1alpha1.WorkflowStep, id string, stepStatus map[string]v1alpha1.StepStatus, basicValue cue.Value) (bool, v1alpha1.StepStatus) {
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
			if v := basicValue.LookupPath(cue.ParsePath(input.From)); !v.Exists() {
				return true, pStatus
			}
		}
	}
	return false, v1alpha1.StepStatus{}
}
