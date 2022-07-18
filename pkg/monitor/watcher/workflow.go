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

package watcher

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"k8s.io/client-go/tools/cache"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/monitor/metrics"
)

type workflowRunMetricsWatcher struct {
	mu               sync.Mutex
	phaseCounter     map[string]int
	stepPhaseCounter map[string]int
	phaseDirty       map[string]struct{}
	stepPhaseDirty   map[string]struct{}
	informer         ctrlcache.Informer
	stopCh           chan struct{}
}

func (watcher *workflowRunMetricsWatcher) getRun(obj interface{}) *v1alpha1.WorkflowRun {
	wr := &v1alpha1.WorkflowRun{}
	bs, _ := json.Marshal(obj)
	_ = json.Unmarshal(bs, wr)
	return wr
}

func (watcher *workflowRunMetricsWatcher) getPhase(phase string) string {
	if phase == "" {
		return "-"
	}
	return phase
}

func (watcher *workflowRunMetricsWatcher) inc(wr *v1alpha1.WorkflowRun, delta int) {
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	phase := watcher.getPhase(string(wr.Status.Phase))
	watcher.phaseCounter[phase] += delta
	watcher.phaseDirty[phase] = struct{}{}
	for _, step := range wr.Status.Steps {
		stepPhase := watcher.getPhase(string(step.Phase))
		key := fmt.Sprintf("%s/%s#%s", step.Type, stepPhase, step.Reason)
		watcher.stepPhaseCounter[key] += delta
		watcher.stepPhaseDirty[key] = struct{}{}
	}
}

func (watcher *workflowRunMetricsWatcher) report() {
	watcher.mu.Lock()
	defer watcher.mu.Unlock()
	for phase := range watcher.phaseDirty {
		metrics.WorkflowRunPhaseCounter.WithLabelValues(phase).Set(float64(watcher.phaseCounter[phase]))
	}
	for stepPhase := range watcher.stepPhaseDirty {
		metrics.WorkflowRunStepPhaseGauge.WithLabelValues(strings.Split(stepPhase, "/")[:2]...).Set(float64(watcher.stepPhaseCounter[stepPhase]))
	}
	watcher.phaseDirty = map[string]struct{}{}
	watcher.stepPhaseDirty = map[string]struct{}{}
}

func (watcher *workflowRunMetricsWatcher) run() {
	go func() {
		for {
			select {
			case <-watcher.stopCh:
				return
			default:
				time.Sleep(time.Second)
				watcher.report()
			}
		}
	}()
}

// StartWorkflowRunMetricsWatcher start metrics watcher for reporting workflow run stats
func StartWorkflowRunMetricsWatcher(informer ctrlcache.Informer) {
	watcher := &workflowRunMetricsWatcher{
		phaseCounter:     map[string]int{},
		stepPhaseCounter: map[string]int{},
		phaseDirty:       map[string]struct{}{},
		stepPhaseDirty:   map[string]struct{}{},
		informer:         informer,
		stopCh:           make(chan struct{}, 1),
	}
	watcher.informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) {
			wr := watcher.getRun(obj)
			watcher.inc(wr, 1)
		},
		UpdateFunc: func(oldObj, obj interface{}) {
			oldWr := watcher.getRun(oldObj)
			wr := watcher.getRun(obj)
			watcher.inc(oldWr, -1)
			watcher.inc(wr, 1)
		},
		DeleteFunc: func(obj interface{}) {
			wr := watcher.getRun(obj)
			watcher.inc(wr, -1)
		},
	})
	watcher.run()
}
