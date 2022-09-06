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

const (
	ReasonExecute  = "Execute"
	ReasonGenerate = "Generate"
)

const (
	MessageSuccessfully = "WorkflowRun finished successfully"
	// MessageTerminated is the message for terminated
	MessageTerminated = "WorkflowRun finished with termination"
	// MessageFailed is the message for failed
	MessageFailed = "WorkflowRun finished with failure"
	// MessageFailedGenerate is the message for failed to generate
	MessageFailedGenerate = "fail to generate workflow runners"
	MessageFailedExecute  = "fail to execute"
)
