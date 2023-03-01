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
	"sort"
	"time"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/robfig/cron/v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// recycleCronJob is the cron job to clean the completed workflow
type recycleCronJob struct {
	cli      client.Client
	duration time.Duration
	cron     string
	label    string
}

// NewRecycleCronJob returns a new recycleCronJob
func NewRecycleCronJob(cli client.Client, duration time.Duration, cron, label string) manager.Runnable {
	return &recycleCronJob{
		cli:      cli,
		duration: duration,
		label:    label,
		cron:     cron,
	}
}

func (r *recycleCronJob) Start(ctx context.Context) error {
	c, err := r.start(ctx)
	if err != nil {
		return err
	}
	defer c.Stop()
	<-ctx.Done()
	return nil
}

func (r *recycleCronJob) start(ctx context.Context) (*cron.Cron, error) {
	c := cron.New(cron.WithChain(
		cron.Recover(cron.DefaultLogger),
	))
	if _, err := c.AddFunc(r.cron, func() {
		err := retry.OnError(wait.Backoff{
			Steps:    3,
			Duration: 1 * time.Minute,
			Factor:   5.0,
			Jitter:   0.1,
		}, func(err error) bool {
			// always retry
			return true
		}, func() error {
			if err := r.run(ctx); err != nil {
				klog.Errorf("Failed to recycle workflow run: %v", err)
				return err
			}
			klog.Info("Recycle workflow run successfully")
			return nil
		})
		if err != nil {
			klog.Errorf("Failed to recycle workflow runs after 3 tries: %v", err)
		}
	}); err != nil {
		return nil, err
	}
	c.Start()
	return c, nil
}

func (r *recycleCronJob) run(ctx context.Context) error {
	runs := &v1alpha1.WorkflowRunList{}
	selector, err := metav1.LabelSelectorAsSelector(&metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{{Key: r.label, Operator: metav1.LabelSelectorOpExists}}})
	if err != nil {
		return err
	}
	listOpt := &client.ListOptions{LabelSelector: selector}
	if err := r.cli.List(ctx, runs, listOpt); err != nil {
		return err
	}
	sort.Sort(runs)
	items := make(map[string][]v1alpha1.WorkflowRun)
	for _, item := range runs.Items {
		if v, ok := item.Labels[r.label]; ok {
			items[v] = append(items[v], item)
		}
	}
	for _, l := range items {
		for i := 1; i < len(l); i++ {
			item := l[i]
			if item.Status.Finished && time.Since(item.Status.EndTime.Time) > r.duration {
				if err := r.cli.Delete(ctx, &item); err != nil {
					klog.Errorf("Failed to delete workflowRun %s/%s, error: %v", item.Namespace, item.Name, err)
				}
				klog.Info("Successfully recycled completed workflowRun %s/%s", item.Namespace, item.Name)
			}
		}
	}
	return nil
}
