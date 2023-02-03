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

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"github.com/kubevela/workflow/api/condition"
)

// +kubebuilder:object:root=true

// WorkflowRun is the Schema for the workflowRun API
// +kubebuilder:storageversion
// +kubebuilder:resource:categories={oam},shortName={wr}
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="PHASE",type=string,JSONPath=`.status.status`
// +kubebuilder:printcolumn:name="AGE",type=date,JSONPath=".metadata.creationTimestamp"
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type WorkflowRun struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`
	Spec              WorkflowRunSpec   `json:"spec,omitempty"`
	Status            WorkflowRunStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// WorkflowRunList contains a list of WorkflowRun
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type WorkflowRunList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []WorkflowRun `json:"items"`
}

func (w WorkflowRunList) Len() int {
	return len(w.Items)
}

func (w WorkflowRunList) Swap(i, j int) {
	w.Items[i], w.Items[j] = w.Items[j], w.Items[i]
}

func (w WorkflowRunList) Less(i, j int) bool {
	if !w.Items[i].Status.Finished && !w.Items[j].Status.Finished {
		return w.Items[i].CreationTimestamp.After(w.Items[j].CreationTimestamp.Time)
	}
	if !w.Items[i].Status.EndTime.IsZero() && !w.Items[j].Status.EndTime.IsZero() {
		return w.Items[i].Status.EndTime.After(w.Items[j].Status.EndTime.Time)
	}
	return !w.Items[i].Status.EndTime.IsZero()
}

// WorkflowRunSpec is the spec for the WorkflowRun
type WorkflowRunSpec struct {
	// +kubebuilder:pruning:PreserveUnknownFields
	Context      *runtime.RawExtension `json:"context,omitempty"`
	Mode         *WorkflowExecuteMode  `json:"mode,omitempty"`
	WorkflowSpec *WorkflowSpec         `json:"workflowSpec,omitempty"`
	WorkflowRef  string                `json:"workflowRef,omitempty"`
}

// WorkflowRunStatus record the status of workflow run
type WorkflowRunStatus struct {
	condition.ConditionedStatus `json:",inline"`

	Mode    WorkflowExecuteMode `json:"mode"`
	Phase   WorkflowRunPhase    `json:"status"`
	Message string              `json:"message,omitempty"`

	Suspend      bool   `json:"suspend"`
	SuspendState string `json:"suspendState,omitempty"`

	Terminated bool `json:"terminated"`
	Finished   bool `json:"finished"`

	ContextBackend *corev1.ObjectReference `json:"contextBackend,omitempty"`
	Steps          []WorkflowStepStatus    `json:"steps,omitempty"`

	StartTime metav1.Time `json:"startTime,omitempty"`
	EndTime   metav1.Time `json:"endTime,omitempty"`
}

// WorkflowSpec defines workflow steps and other attributes
type WorkflowSpec struct {
	Steps []WorkflowStep `json:"steps,omitempty"`
}

// WorkflowExecuteMode defines the mode of workflow execution
type WorkflowExecuteMode struct {
	// Steps is the mode of workflow steps execution
	Steps WorkflowMode `json:"steps,omitempty"`
	// SubSteps is the mode of workflow sub steps execution
	SubSteps WorkflowMode `json:"subSteps,omitempty"`
}

// WorkflowRunPhase is a label for the condition of a WorkflowRun at the current time
type WorkflowRunPhase string

const (
	// WorkflowStateInitializing means the workflow run is initializing
	WorkflowStateInitializing WorkflowRunPhase = "initializing"
	// WorkflowStateExecuting means the workflow run is executing
	WorkflowStateExecuting WorkflowRunPhase = "executing"
	// WorkflowStateSuspending means the workflow run is suspending
	WorkflowStateSuspending WorkflowRunPhase = "suspending"
	// WorkflowStateTerminated means the workflow run is terminated
	WorkflowStateTerminated WorkflowRunPhase = "terminated"
	// WorkflowStateFailed means the workflow run is failed
	WorkflowStateFailed WorkflowRunPhase = "failed"
	// WorkflowStateSucceeded means the workflow run is succeeded
	WorkflowStateSucceeded WorkflowRunPhase = "succeeded"
	// WorkflowStateSkipped means the workflow run is skipped
	WorkflowStateSkipped WorkflowRunPhase = "skipped"
)

// +kubebuilder:object:root=true

// Workflow is the Schema for the workflow API
// +kubebuilder:storageversion
// +kubebuilder:resource:categories={oam}
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Workflow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Mode         *WorkflowExecuteMode `json:"mode,omitempty"`
	WorkflowSpec `json:",inline"`
}

// +kubebuilder:object:root=true

// WorkflowList contains a list of Workflow
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type WorkflowList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Workflow `json:"items"`
}

// WorkflowStep defines how to execute a workflow step.
type WorkflowStep struct {
	WorkflowStepBase `json:",inline"`
	SubSteps         []WorkflowStepBase `json:"subSteps,omitempty"`
}

// WorkflowStepMeta contains the meta data of a workflow step
type WorkflowStepMeta struct {
	Alias string `json:"alias,omitempty"`
}

// WorkflowStepBase defines the workflow step base
type WorkflowStepBase struct {
	// Name is the unique name of the workflow step.
	Name string `json:"name,omitempty"`
	// Type is the type of the workflow step.
	Type string `json:"type"`
	// Meta is the meta data of the workflow step.
	Meta *WorkflowStepMeta `json:"meta,omitempty"`
	// If is the if condition of the step
	If string `json:"if,omitempty"`
	// Timeout is the timeout of the step
	Timeout string `json:"timeout,omitempty"`
	// DependsOn is the dependency of the step
	DependsOn []string `json:"dependsOn,omitempty"`
	// Inputs is the inputs of the step
	Inputs StepInputs `json:"inputs,omitempty"`
	// Outputs is the outputs of the step
	Outputs StepOutputs `json:"outputs,omitempty"`

	// Properties is the properties of the step
	// +kubebuilder:pruning:PreserveUnknownFields
	Properties *runtime.RawExtension `json:"properties,omitempty"`
}

// WorkflowMode describes the mode of workflow
type WorkflowMode string

const (
	// WorkflowModeDAG describes the DAG mode of workflow
	WorkflowModeDAG WorkflowMode = "DAG"
	// WorkflowModeStep describes the step by step mode of workflow
	WorkflowModeStep WorkflowMode = "StepByStep"
)

// StepStatus record the base status of workflow step, which could be workflow step or subStep
type StepStatus struct {
	ID    string            `json:"id"`
	Name  string            `json:"name,omitempty"`
	Type  string            `json:"type,omitempty"`
	Phase WorkflowStepPhase `json:"phase,omitempty"`
	// A human readable message indicating details about why the workflowStep is in this state.
	Message string `json:"message,omitempty"`
	// A brief CamelCase message indicating details about why the workflowStep is in this state.
	Reason string `json:"reason,omitempty"`
	// FirstExecuteTime is the first time this step execution.
	FirstExecuteTime metav1.Time `json:"firstExecuteTime,omitempty"`
	// LastExecuteTime is the last time this step execution.
	LastExecuteTime metav1.Time `json:"lastExecuteTime,omitempty"`
}

// WorkflowStepStatus record the status of a workflow step, include step status and subStep status
type WorkflowStepStatus struct {
	StepStatus     `json:",inline"`
	SubStepsStatus []StepStatus `json:"subSteps,omitempty"`
}

// SetConditions set condition to workflow run
func (wr *WorkflowRun) SetConditions(c ...condition.Condition) {
	wr.Status.SetConditions(c...)
}

// GetCondition get condition by given condition type
func (wr *WorkflowRun) GetCondition(t condition.ConditionType) condition.Condition {
	return wr.Status.GetCondition(t)
}

// WorkflowRunConditionType is a valid condition type for a WorkflowRun
const WorkflowRunConditionType string = "WorkflowRun"

// WorkflowStepPhase describes the phase of a workflow step.
type WorkflowStepPhase string

const (
	// WorkflowStepPhaseSucceeded will make the controller run the next step.
	WorkflowStepPhaseSucceeded WorkflowStepPhase = "succeeded"
	// WorkflowStepPhaseFailed will report error in `message`.
	WorkflowStepPhaseFailed WorkflowStepPhase = "failed"
	// WorkflowStepPhaseSkipped will make the controller skip the step.
	WorkflowStepPhaseSkipped WorkflowStepPhase = "skipped"
	// WorkflowStepPhaseRunning will make the controller continue the workflow.
	WorkflowStepPhaseRunning WorkflowStepPhase = "running"
	// WorkflowStepPhasePending will make the controller wait for the step to run.
	WorkflowStepPhasePending WorkflowStepPhase = "pending"
)

// StepOutputs defines output variable of WorkflowStep
type StepOutputs []OutputItem

// StepInputs defines variable input of WorkflowStep
type StepInputs []InputItem

// InputItem defines an input variable of WorkflowStep
type InputItem struct {
	ParameterKey string `json:"parameterKey,omitempty"`
	From         string `json:"from"`
}

// OutputItem defines an output variable of WorkflowStep
type OutputItem struct {
	ValueFrom string `json:"valueFrom"`
	Name      string `json:"name"`
}
