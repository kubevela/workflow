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

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"
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
	Context      *runtime.RawExtension            `json:"context,omitempty"`
	Mode         *oamv1alpha1.WorkflowExecuteMode `json:"mode,omitempty"`
	WorkflowSpec *oamv1alpha1.WorkflowSpec        `json:"workflowSpec,omitempty"`
	WorkflowRef  string                           `json:"workflowRef,omitempty"`
}

// WorkflowRunStatus record the status of workflow run
type WorkflowRunStatus struct {
	condition.ConditionedStatus `json:",inline"`

	Mode    oamv1alpha1.WorkflowExecuteMode `json:"mode"`
	Phase   WorkflowRunPhase                `json:"status"`
	Message string                          `json:"message,omitempty"`

	Suspend      bool   `json:"suspend"`
	SuspendState string `json:"suspendState,omitempty"`

	Terminated bool `json:"terminated"`
	Finished   bool `json:"finished"`

	ContextBackend *corev1.ObjectReference `json:"contextBackend,omitempty"`
	Steps          []WorkflowStepStatus    `json:"steps,omitempty"`

	StartTime metav1.Time `json:"startTime,omitempty"`
	EndTime   metav1.Time `json:"endTime,omitempty"`
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

const (
	// WorkflowModeDAG describes the DAG mode of workflow
	WorkflowModeDAG oamv1alpha1.WorkflowMode = "DAG"
	// WorkflowModeStep describes the step by step mode of workflow
	WorkflowModeStep oamv1alpha1.WorkflowMode = "StepByStep"
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
	// WorkflowStepPhaseSuspending will make the controller suspend the workflow.
	WorkflowStepPhaseSuspending WorkflowStepPhase = "suspending"
)
