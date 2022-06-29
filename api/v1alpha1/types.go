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
)

// +kubebuilder:object:root=true

// WorkflowRun is the Schema for the workflowRun API
// +kubebuilder:storageversion
// +kubebuilder:resource:categories={oam}
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

type WorkflowRunSpec struct {
	Mode             *WorkflowExecuteMode `json:"mode,omitempty"`
	WorkflowSpec     *WorkflowSpec        `json:"workflowSpec,omitempty"`
	WorkflowTemplate string               `json:"workflowTemplate,omitempty"`
}

// WorkflowRunStatus record the status of workflow run
type WorkflowRunStatus struct {
	Mode    WorkflowExecuteMode `json:"mode"`
	Message string              `json:"message,omitempty"`

	Suspend      bool   `json:"suspend"`
	SuspendState string `json:"suspendState,omitempty"`

	Terminated bool `json:"terminated"`
	Finished   bool `json:"finished"`

	ContextBackend *corev1.ObjectReference `json:"contextBackend,omitempty"`
	Steps          []WorkflowStepStatus    `json:"steps,omitempty"`

	StartTime metav1.Time `json:"startTime,omitempty"`
}

// Workflow defines workflow steps and other attributes
type WorkflowSpec struct {
	Steps []WorkflowStep `json:"steps,omitempty"`
}

// WorkflowExecuteMode defines the mode of workflow execution
type WorkflowExecuteMode struct {
	Steps    WorkflowMode `json:"steps,omitempty"`
	SubSteps WorkflowMode `json:"subSteps,omitempty"`
}

// +kubebuilder:object:root=true

// Workflow is the Schema for the workflow API
// +kubebuilder:storageversion
// +kubebuilder:resource:categories={oam}
// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
type Workflow struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

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
	// Name is the unique name of the workflow step.
	Name string `json:"name"`

	Type string `json:"type"`

	Meta *WorkflowStepMeta `json:"meta,omitempty"`

	// +kubebuilder:pruning:PreserveUnknownFields
	Properties *runtime.RawExtension `json:"properties,omitempty"`

	SubSteps []WorkflowSubStep `json:"subSteps,omitempty"`

	If string `json:"if,omitempty"`

	Timeout string `json:"timeout,omitempty"`

	DependsOn []string `json:"dependsOn,omitempty"`

	Inputs StepInputs `json:"inputs,omitempty"`

	Outputs StepOutputs `json:"outputs,omitempty"`
}

// WorkflowStepMeta contains the meta data of a workflow step
type WorkflowStepMeta struct {
	Alias string `json:"alias,omitempty"`
}

// WorkflowSubStep defines how to execute a workflow subStep.
type WorkflowSubStep struct {
	// Name is the unique name of the workflow step.
	Name string `json:"name"`

	Type string `json:"type"`

	Meta *WorkflowStepMeta `json:"meta,omitempty"`

	// +kubebuilder:pruning:PreserveUnknownFields
	Properties *runtime.RawExtension `json:"properties,omitempty"`

	If string `json:"if,omitempty"`

	Timeout string `json:"timeout,omitempty"`

	DependsOn []string `json:"dependsOn,omitempty"`

	Inputs StepInputs `json:"inputs,omitempty"`

	Outputs StepOutputs `json:"outputs,omitempty"`
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
	SubStepsStatus []WorkflowSubStepStatus `json:"subSteps,omitempty"`
}

// WorkflowSubStepStatus record the status of a workflow subStep
type WorkflowSubStepStatus struct {
	StepStatus `json:",inline"`
}

// WorkflowStepPhase describes the phase of a workflow step.
type WorkflowStepPhase string

const (
	// WorkflowStepPhaseSucceeded will make the controller run the next step.
	WorkflowStepPhaseSucceeded WorkflowStepPhase = "succeeded"
	// WorkflowStepPhaseFailed will report error in `message`.
	WorkflowStepPhaseFailed WorkflowStepPhase = "failed"
	// WorkflowStepPhaseSkipped will make the controller skip the step.
	WorkflowStepPhaseSkipped WorkflowStepPhase = "skipped"
	// WorkflowStepPhaseStopped will make the controller stop the workflow.
	WorkflowStepPhaseStopped WorkflowStepPhase = "stopped"
	// WorkflowStepPhaseRunning will make the controller continue the workflow.
	WorkflowStepPhaseRunning WorkflowStepPhase = "running"
)

// StepOutputs defines output variable of WorkflowStep
type StepOutputs []outputItem

// StepInputs defines variable input of WorkflowStep
type StepInputs []inputItem

type inputItem struct {
	ParameterKey string `json:"parameterKey"`
	From         string `json:"from"`
}

type outputItem struct {
	ValueFrom string `json:"valueFrom"`
	Name      string `json:"name"`
}
