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

package types

import (
	"context"

	"cuelang.org/go/cue"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/util/feature"
	"sigs.k8s.io/controller-runtime/pkg/client"

	monitorContext "github.com/kubevela/pkg/monitor/context"

	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	"github.com/kubevela/workflow/pkg/cue/packages"
	"github.com/kubevela/workflow/pkg/cue/process"
	"github.com/kubevela/workflow/pkg/features"
	"github.com/kubevela/workflow/pkg/tasks/template"
)

// WorkflowInstance is the instance for workflow engine to execute
type WorkflowInstance struct {
	WorkflowMeta
	OwnerInfo []metav1.OwnerReference
	Debug     bool
	Context   map[string]interface{}
	Mode      *v1alpha1.WorkflowExecuteMode
	Steps     []v1alpha1.WorkflowStep
	Status    v1alpha1.WorkflowRunStatus
}

// WorkflowMeta is the meta information for workflow instance
type WorkflowMeta struct {
	Name                 string
	Namespace            string
	Annotations          map[string]string
	Labels               map[string]string
	UID                  types.UID
	ChildOwnerReferences []metav1.OwnerReference
}

// TaskRunner is a task runner
type TaskRunner interface {
	Name() string
	Pending(ctx monitorContext.Context, wfCtx wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus)
	Run(ctx wfContext.Context, options *TaskRunOptions) (v1alpha1.StepStatus, *Operation, error)
}

// TaskDiscover is the interface to obtain the TaskGenerator
type TaskDiscover interface {
	GetTaskGenerator(ctx context.Context, name string) (TaskGenerator, error)
}

// Engine is the engine to run workflow
type Engine interface {
	Run(ctx monitorContext.Context, taskRunners []TaskRunner, dag bool) error
	GetStepStatus(stepName string) v1alpha1.WorkflowStepStatus
	GetCommonStepStatus(stepName string) v1alpha1.StepStatus
	SetParentRunner(name string)
	GetOperation() *Operation
}

// TaskRunOptions is the options for task run
type TaskRunOptions struct {
	Data          *value.Value
	PCtx          process.Context
	PreCheckHooks []TaskPreCheckHook
	PreStartHooks []TaskPreStartHook
	PostStopHooks []TaskPostStopHook
	GetTracer     func(id string, step v1alpha1.WorkflowStep) monitorContext.Context
	RunSteps      func(isDag bool, runners ...TaskRunner) (*v1alpha1.WorkflowRunStatus, error)
	Debug         func(step string, v *value.Value) error
	StepStatus    map[string]v1alpha1.StepStatus
	Engine        Engine
}

// PreCheckResult is the result of pre check.
type PreCheckResult struct {
	Skip    bool
	Timeout bool
}

// PreCheckOptions is the options for pre check.
type PreCheckOptions struct {
	PackageDiscover *packages.PackageDiscover
	BasicTemplate   string
	BasicValue      *value.Value
}

// StatusPatcher is the interface to patch status
type StatusPatcher func(ctx context.Context, status *v1alpha1.WorkflowRunStatus, isUpdate bool) error

// TaskPreCheckHook is the hook for pre check.
type TaskPreCheckHook func(step v1alpha1.WorkflowStep, options *PreCheckOptions) (*PreCheckResult, error)

// TaskPreStartHook run before task execution.
type TaskPreStartHook func(ctx wfContext.Context, paramValue *value.Value, step v1alpha1.WorkflowStep) error

// TaskPostStopHook  run after task execution.
type TaskPostStopHook func(ctx wfContext.Context, taskValue *value.Value, step v1alpha1.WorkflowStep, status v1alpha1.StepStatus, stepStatus map[string]v1alpha1.StepStatus) error

// Operation is workflow operation object.
type Operation struct {
	Suspend            bool
	Terminated         bool
	Waiting            bool
	Skip               bool
	FailedAfterRetries bool
}

// TaskGenerator will generate taskRunner.
type TaskGenerator func(wfStep v1alpha1.WorkflowStep, options *TaskGeneratorOptions) (TaskRunner, error)

// TaskGeneratorOptions is the options for generate task.
type TaskGeneratorOptions struct {
	ID                 string
	PrePhase           v1alpha1.WorkflowStepPhase
	StepConvertor      func(step v1alpha1.WorkflowStep) (v1alpha1.WorkflowStep, error)
	SubTaskRunners     []TaskRunner
	SubStepExecuteMode v1alpha1.WorkflowMode
	PackageDiscover    *packages.PackageDiscover
	ProcessContext     process.Context
}

// Handler is provider's processing method.
type Handler func(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act Action) error

// Providers is provider discover interface.
type Providers interface {
	GetHandler(provider, name string) (Handler, bool)
	Register(provider string, m map[string]Handler)
}

// StepGeneratorOptions is the options for generate step.
type StepGeneratorOptions struct {
	Providers       Providers
	PackageDiscover *packages.PackageDiscover
	ProcessCtx      process.Context
	TemplateLoader  template.Loader
	Client          client.Client
	StepConvertor   map[string]func(step v1alpha1.WorkflowStep) (v1alpha1.WorkflowStep, error)
	LogLevel        int
}

// Action is that workflow provider can do.
type Action interface {
	Suspend(message string)
	Terminate(message string)
	Wait(message string)
	Fail(message string)
	Message(message string)
}

// Parameter defines a parameter for cli from capability template
type Parameter struct {
	Name     string      `json:"name"`
	Short    string      `json:"short,omitempty"`
	Required bool        `json:"required,omitempty"`
	Default  interface{} `json:"default,omitempty"`
	Usage    string      `json:"usage,omitempty"`
	Ignore   bool        `json:"ignore,omitempty"`
	Type     cue.Kind    `json:"type,omitempty"`
	Alias    string      `json:"alias,omitempty"`
	JSONType string      `json:"jsonType,omitempty"`
}

// Resource is the log resources
type Resource struct {
	Name          string            `json:"name,omitempty"`
	Namespace     string            `json:"namespace,omitempty"`
	Cluster       string            `json:"cluster,omitempty"`
	LabelSelector map[string]string `json:"labelSelector,omitempty"`
}

// LogSource is the source of the log
type LogSource struct {
	URL       string     `json:"url,omitempty"`
	Resources []Resource `json:"resources,omitempty"`
}

// LogConfig is the config of the log
type LogConfig struct {
	Data   bool       `json:"data,omitempty"`
	Source *LogSource `json:"source,omitempty"`
}

const (
	// ContextPrefixFailedTimes is the prefix that refer to the failed times of the step in workflow context config map.
	ContextPrefixFailedTimes = "failed_times"
	// ContextPrefixBackoffTimes is the prefix that refer to the backoff times in workflow context config map.
	ContextPrefixBackoffTimes = "backoff_times"
	// ContextPrefixBackoffReason is the prefix that refer to the current backoff reason in workflow context config map
	ContextPrefixBackoffReason = "backoff_reason"
	// ContextKeyLastExecuteTime is the key that refer to the last execute time in workflow context config map.
	ContextKeyLastExecuteTime = "last_execute_time"
	// ContextKeyNextExecuteTime is the key that refer to the next execute time in workflow context config map.
	ContextKeyNextExecuteTime = "next_execute_time"
	// ContextKeyLogConfig is key for log config.
	ContextKeyLogConfig = "logConfig"
)

const (
	// WorkflowStepTypeSuspend type suspend
	WorkflowStepTypeSuspend = "suspend"
	// WorkflowStepTypeApplyComponent type apply-component
	WorkflowStepTypeApplyComponent = "apply-component"
	// WorkflowStepTypeBuiltinApplyComponent type builtin-apply-component
	WorkflowStepTypeBuiltinApplyComponent = "builtin-apply-component"
	// WorkflowStepTypeStepGroup type step-group
	WorkflowStepTypeStepGroup = "step-group"
)

const (
	// LabelWorkflowRunName is the label key for workflow run name
	LabelWorkflowRunName = "workflowrun.oam.dev/name"
	// LabelWorkflowRunNamespace is the label key for workflow run namespace
	LabelWorkflowRunNamespace = "workflowrun.oam.dev/namespace"
)

var (
	// MaxWorkflowStepErrorRetryTimes is the max retry times of the failed workflow step.
	MaxWorkflowStepErrorRetryTimes = 10
	// MaxWorkflowWaitBackoffTime is the max time to wait before reconcile wait workflow again
	MaxWorkflowWaitBackoffTime = 60
	// MaxWorkflowFailedBackoffTime is the max time to wait before reconcile failed workflow again
	MaxWorkflowFailedBackoffTime = 300
)

const (
	// StatusReasonWait is the reason of the workflow progress condition which is Wait.
	StatusReasonWait = "Wait"
	// StatusReasonSkip is the reason of the workflow progress condition which is Skip.
	StatusReasonSkip = "Skip"
	// StatusReasonRendering is the reason of the workflow progress condition which is Rendering.
	StatusReasonRendering = "Rendering"
	// StatusReasonExecute is the reason of the workflow progress condition which is Execute.
	StatusReasonExecute = "Execute"
	// StatusReasonSuspend is the reason of the workflow progress condition which is Suspend.
	StatusReasonSuspend = "Suspend"
	// StatusReasonTerminate is the reason of the workflow progress condition which is Terminate.
	StatusReasonTerminate = "Terminate"
	// StatusReasonParameter is the reason of the workflow progress condition which is ProcessParameter.
	StatusReasonParameter = "ProcessParameter"
	// StatusReasonInput is the reason of the workflow progress condition which is Input.
	StatusReasonInput = "Input"
	// StatusReasonOutput is the reason of the workflow progress condition which is Output.
	StatusReasonOutput = "Output"
	// StatusReasonFailedAfterRetries is the reason of the workflow progress condition which is FailedAfterRetries.
	StatusReasonFailedAfterRetries = "FailedAfterRetries"
	// StatusReasonTimeout is the reason of the workflow progress condition which is Timeout.
	StatusReasonTimeout = "Timeout"
	// StatusReasonAction is the reason of the workflow progress condition which is Action.
	StatusReasonAction = "Action"
)

const (
	// MessageSuspendFailedAfterRetries is the message of failed after retries
	MessageSuspendFailedAfterRetries = "The workflow suspends automatically because the failed times of steps have reached the limit"
)

const (
	// AnnotationWorkflowRunDebug is the annotation for debug
	AnnotationWorkflowRunDebug = "workflowrun.oam.dev/debug"
	// AnnotationControllerRequirement indicates the controller version that can process the workflow run
	AnnotationControllerRequirement = "workflowrun.oam.dev/controller-version-require"
)

// IsStepFinish will decide whether step is finish.
func IsStepFinish(phase v1alpha1.WorkflowStepPhase, reason string) bool {
	if feature.DefaultMutableFeatureGate.Enabled(features.EnableSuspendOnFailure) {
		return phase == v1alpha1.WorkflowStepPhaseSucceeded
	}
	switch phase {
	case v1alpha1.WorkflowStepPhaseFailed:
		return reason != "" && reason != StatusReasonExecute
	case v1alpha1.WorkflowStepPhaseSkipped:
		return true
	case v1alpha1.WorkflowStepPhaseSucceeded:
		return true
	default:
		return false
	}
}

// SetNamespaceInCtx set namespace in context.
func SetNamespaceInCtx(ctx context.Context, namespace string) context.Context {
	if namespace == "" {
		// compatible with some webhook handlers that maybe receive empty string as app namespace which means `default` namespace
		namespace = "default"
	}
	ctx = context.WithValue(ctx, template.DefinitionNamespace, namespace)
	return ctx
}
