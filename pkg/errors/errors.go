/*
Copyright 2023 The KubeVela Authors.

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

package errors

// ActionType is the type of action
type ActionType string

const (
	// ActionSuspend is the action type of suspend
	ActionSuspend ActionType = "suspend"
	// ActionTerminate is the action type of terminate
	ActionTerminate ActionType = "terminate"
	// ActionWait is the action type of wait
	ActionWait ActionType = "wait"
)

// GenericActionError is the error type of action
type GenericActionError ActionType

func (e GenericActionError) Error() string {
	return ""
}
