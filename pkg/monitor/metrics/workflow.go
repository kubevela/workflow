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

package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/metrics"

	velametrics "github.com/kubevela/pkg/monitor/metrics"
)

var (
	// WorkflowRunReconcileTimeHistogram report the reconciling time cost of workflow run controller with state transition recorded
	WorkflowRunReconcileTimeHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "workflowrun_reconcile_time_seconds",
		Help:        "workflow run reconcile duration distributions.",
		Buckets:     velametrics.FineGrainedBuckets,
		ConstLabels: prometheus.Labels{},
	}, []string{"begin_phase", "end_phase"})

	// GenerateTaskRunnersDurationHistogram report the generate task runners execution duration.
	GenerateTaskRunnersDurationHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "generate_task_runners_time_seconds",
		Help:        "generate task runners duration distributions.",
		Buckets:     velametrics.FineGrainedBuckets,
		ConstLabels: prometheus.Labels{},
	}, []string{"controller"})

	// WorkflowRunPhaseCounter report the number of workflow run phase
	WorkflowRunPhaseCounter = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "workflowrun_phase_number",
		Help: "workflow run phase number",
	}, []string{"phase"})

	// WorkflowRunFinishedTimeHistogram report the time for finished workflow run
	WorkflowRunFinishedTimeHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "workflowrun_finished_time_seconds",
		Help:        "workflow run finished time distributions.",
		Buckets:     velametrics.FineGrainedBuckets,
		ConstLabels: prometheus.Labels{},
	}, []string{"phase"})

	// WorkflowRunInitializedCounter report the workflow run initialize execute number.
	WorkflowRunInitializedCounter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "workflowrun_initialized_num",
		Help: "workflow run initialize times",
	}, []string{})

	// WorkflowRunStepPhaseGauge report the number of workflow run step state
	WorkflowRunStepPhaseGauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "workflowrun_step_phase_number",
		Help: "workflow step phase number",
	}, []string{"step_type", "phase"})

	// WorkflowRunStepDurationHistogram report the step execution duration.
	WorkflowRunStepDurationHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "workflowrun_step_duration_ms",
		Help:        "workflow run step latency distributions.",
		Buckets:     velametrics.FineGrainedBuckets,
		ConstLabels: prometheus.Labels{},
	}, []string{"controller", "step_type"})
)

var collectorGroup = []prometheus.Collector{
	GenerateTaskRunnersDurationHistogram,
	WorkflowRunStepDurationHistogram,
	WorkflowRunReconcileTimeHistogram,
	WorkflowRunFinishedTimeHistogram,
	WorkflowRunInitializedCounter,
	WorkflowRunPhaseCounter,
	WorkflowRunStepPhaseGauge,
}

func init() {
	for _, collector := range collectorGroup {
		if err := metrics.Registry.Register(collector); err != nil {
			klog.Error(err)
		}
	}
}
