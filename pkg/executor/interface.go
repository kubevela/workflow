/*Copyright 2022 The KubeVela Authors.

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

package executor

import (
	"time"

	"github.com/kubevela/workflow/api/v1alpha1"
	monitorContext "github.com/kubevela/workflow/pkg/monitor/context"
	"github.com/kubevela/workflow/pkg/types"
)

// Workflow is used to execute the workflow steps of Application.
type WorkflowExecutor interface {
	// ExecuteSteps executes the steps
	ExecuteRunners(ctx monitorContext.Context, taskRunners []types.TaskRunner) (state v1alpha1.WorkflowRunPhase, err error)

	// GetBackoffWaitTime returns the wait time for next retry.
	GetBackoffWaitTime() time.Duration

	GetSuspendBackoffWaitTime() time.Duration
}
