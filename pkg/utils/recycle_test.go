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

package utils

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubevela/workflow/api/v1alpha1"
)

func TestRecycleCronJob(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	r := require.New(t)
	for i := 1; i < 7; i++ {
		run := &v1alpha1.WorkflowRun{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("workflow-test-%d", i),
				Namespace: "default",
			},
		}
		if i%5 != 0 {
			run.Labels = map[string]string{
				"pipeline.oam.dev/name": "test",
			}
		}
		err := cli.Create(ctx, run)
		r.NoError(err)
		run.Status.Finished = i%6 != 0
		run.Status.EndTime = metav1.Time{Time: time.Now().AddDate(0, 0, -i)}
		err = cli.Status().Update(ctx, run)
		r.NoError(err)
		defer cli.Delete(ctx, run)
	}

	runner := NewRecycleCronJob(cli, time.Hour, "@every 1s", "pipeline.oam.dev/name")
	err := runner.Start(ctx)
	r.NoError(err)
	runs := &v1alpha1.WorkflowRunList{}
	err = cli.List(ctx, runs)
	r.NoError(err)
	r.Equal(3, len(runs.Items))
}
