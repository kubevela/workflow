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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apiserver/pkg/util/feature"

	"github.com/kubevela/pkg/cel"
	"github.com/kubevela/pkg/cue/cuex"
	monitorContext "github.com/kubevela/pkg/monitor/context"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
	"github.com/kubevela/workflow/api/v1alpha1"
	wfContext "github.com/kubevela/workflow/pkg/context"
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
	Mode      *oamv1alpha1.WorkflowExecuteMode
	Steps     []oamv1alpha1.WorkflowStep
	// InferredDependsOn is, by step name, the steps each step's expressions read
	// that it waits on: the entries of its dependsOn the reads added, and the
	// earlier steps a workflow run in order puts first without one.
	InferredDependsOn map[string][]string
	Status            v1alpha1.WorkflowRunStatus
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

// ContextDataResetter reset process.Context after the step is finished
type ContextDataResetter func(processCtx process.Context)

// TaskRunner is a task runner
type TaskRunner interface {
	Name() string
	Pending(ctx monitorContext.Context, wfCtx wfContext.Context, stepStatus map[string]v1alpha1.StepStatus) (bool, v1alpha1.StepStatus)
	Run(ctx wfContext.Context, options *TaskRunOptions) (v1alpha1.StepStatus, *Operation, error)
	FillContextData(ctx monitorContext.Context, processCtx process.Context) ContextDataResetter
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
	// SetDependsOn records the dependsOn of a step generated at run time, which the
	// workflow spec does not list.
	SetDependsOn(name string, dependsOn []string)
}

// TaskRunOptions is the options for task run
type TaskRunOptions struct {
	PCtx          process.Context
	PreCheckHooks []TaskPreCheckHook
	PreStartHooks []TaskPreStartHook
	PostStopHooks []TaskPostStopHook
	GetTracer     func(id string, step oamv1alpha1.WorkflowStep) monitorContext.Context
	RunSteps      func(isDag bool, runners ...TaskRunner) (*v1alpha1.WorkflowRunStatus, error)
	Debug         func(step string, v cue.Value) error
	StepStatus    map[string]v1alpha1.StepStatus
	Engine        Engine
	Compiler      *cuex.Compiler
}

// PreCheckResult is the result of pre check.
type PreCheckResult struct {
	Skip    bool
	Timeout bool
	// Wait holds the step back, with Message saying why: its $( ) if reads
	// something still to be answered.
	Wait bool
	// Fail fails the step, with Message saying why: its $( ) if cannot be
	// evaluated in a run that does not read expressions.
	Fail    bool
	Message string
}

// PreCheckOptions is the options for pre check.
type PreCheckOptions struct {
	BasicValue cue.Value
	// Condition evaluates the step's $( ) if, when its run resolves expressions;
	// nil otherwise, and an if that is one is then CUE, as it always was.
	Condition func() (bool, error)
}

// StatusPatcher is the interface to patch status
type StatusPatcher func(ctx context.Context, status *v1alpha1.WorkflowRunStatus, isUpdate bool) error

// TaskPreCheckHook is the hook for pre check.
type TaskPreCheckHook func(step oamv1alpha1.WorkflowStep, options *PreCheckOptions) (*PreCheckResult, error)

// TaskPreStartHook run before task execution.
type TaskPreStartHook func(ctx wfContext.Context, paramValue cue.Value, step oamv1alpha1.WorkflowStep) (cue.Value, error)

// TaskPostStopHook  run after task execution.
type TaskPostStopHook func(ctx wfContext.Context, taskValue cue.Value, step oamv1alpha1.WorkflowStep, status v1alpha1.StepStatus, stepStatus map[string]v1alpha1.StepStatus) error

// Operation is workflow operation object.
type Operation struct {
	Suspend            bool
	Terminated         bool
	Waiting            bool
	Skip               bool
	FailedAfterRetries bool
}

// TaskGenerator will generate taskRunner.
type TaskGenerator func(wfStep oamv1alpha1.WorkflowStep, options *TaskGeneratorOptions) (TaskRunner, error)

// TaskGeneratorOptions is the options for generate task.
type TaskGeneratorOptions struct {
	ID                 string
	PrePhase           v1alpha1.WorkflowStepPhase
	StepConvertor      func(step oamv1alpha1.WorkflowStep) (oamv1alpha1.WorkflowStep, error)
	SubTaskRunners     []TaskRunner
	SubStepExecuteMode oamv1alpha1.WorkflowMode
	ProcessContext     process.Context
	// SubTaskGenerator builds a runner for a sub-step that only exists at run time,
	// such as one iteration of a for-each body.
	SubTaskGenerator func(step oamv1alpha1.WorkflowStepBase, id string) (TaskRunner, error)
	// Expressions resolves the $( ) expressions in the step's properties when it
	// runs; nil leaves them as written.
	Expressions PropertyEvaluator
}

// IterationName is what the pass of a forEach loop over a step-group for the item
// labelled label calls its body step, or a body output, name: <loop>-<label>-<name>.
// A loop over one step names its pass <loop>-<label>. The label is the item's key,
// or its index when the loop has none.
func IterationName(loop, label, name string) string {
	return loop + "-" + label + "-" + name
}

// PropertyEvaluator resolves the $( ) expressions in a step's properties. An
// error that is cel.ErrNotReady means the step waits and tries again.
type PropertyEvaluator interface {
	Properties(ctx context.Context, step ExpressionStep, properties *runtime.RawExtension, wfCtx wfContext.Context,
		pCtx process.Context, status map[string]v1alpha1.StepStatus) (*runtime.RawExtension, error)
	// Condition evaluates a step's $( ) if.
	Condition(ctx context.Context, step ExpressionStep, cond string, wfCtx wfContext.Context,
		pCtx process.Context, status map[string]v1alpha1.StepStatus) (bool, error)
}

// ExpressionStep is the step whose expressions are resolved: its name as it runs,
// so an iteration's step has its own, and its type.
type ExpressionStep struct {
	Name string
	Type string
}

// ExpressionOptions turns on $( ) expressions in step properties, forEach items
// and if conditions. The workflow answers context, steps and each; a host adds
// roots of its own with a resolver for each.
type ExpressionOptions struct {
	Roots     []cel.Root
	Resolvers map[string]cel.Resolver
}

// StepGeneratorOptions is the options for generate step.
type StepGeneratorOptions struct {
	ProcessCtx     process.Context
	TemplateLoader template.Loader
	StepConvertor  map[string]func(step oamv1alpha1.WorkflowStep) (oamv1alpha1.WorkflowStep, error)
	LogLevel       int
	Compiler       *cuex.Compiler
	// Expressions turns on $( ) expressions in step properties; nil leaves them
	// as written.
	Expressions *ExpressionOptions
}

// Action is that workflow provider can do.
type Action interface {
	Suspend(message string)
	Resume(message string)
	Terminate(message string)
	Wait(message string)
	Fail(message string)
	Message(message string)
	GetStatus() v1alpha1.StepStatus
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
	// MaxForEachItems caps the iterations of a forEach step, since every iteration is
	// recorded in the run's status object.
	MaxForEachItems = 50
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
	// AnnotationCelExpressions set to "true" opts a WorkflowRun in to $( )
	// expressions, where the EnableCelExpressions feature gate is on.
	AnnotationCelExpressions = "workflowrun.oam.dev/cel-expressions"
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
