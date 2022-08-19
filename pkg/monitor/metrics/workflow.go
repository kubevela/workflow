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
)

var histogramBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.15, 0.2, 0.25, 0.3, 0.35, 0.4, 0.45, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0,
	1.25, 1.5, 1.75, 2.0, 2.5, 3.0, 3.5, 4.0, 4.5, 5, 6, 7, 8, 9, 10, 15, 20, 25, 30, 40, 50, 60}

var (
	//	WorkflowRunReconcileTimeHistogram report the reconciling time cost of workflow run controller with state transition recorded
	WorkflowRunReconcileTimeHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "workflowrun_reconcile_time_seconds",
		Help:        "workflow run reconcile duration distributions.",
		Buckets:     histogramBuckets,
		ConstLabels: prometheus.Labels{},
	}, []string{"begin_phase", "end_phase"})

	// GenerateTaskRunnerDurationHistogram report the generate task runners execution duration.
	GenerateTaskRunnersDurationHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "generate_task_runners_time_seconds",
		Help:        "generate task runners duration distributions.",
		Buckets:     histogramBuckets,
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
		Buckets:     histogramBuckets,
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

	// WorkflowStepDurationHistogram report the step execution duration.
	WorkflowRunStepDurationHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "workflowrun_step_duration_ms",
		Help:        "workflow run step latency distributions.",
		Buckets:     histogramBuckets,
		ConstLabels: prometheus.Labels{},
	}, []string{"controller", "step_type"})

	// ClientRequestHistogram report the client request execution duration.
	ClientRequestHistogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:        "workflow_client_request_time_seconds",
		Help:        "client request duration distributions for workflow",
		Buckets:     histogramBuckets,
		ConstLabels: prometheus.Labels{},
	}, []string{"verb", "Kind", "apiVersion", "unstructured"})
)

var collectorGroup = []prometheus.Collector{
	GenerateTaskRunnersDurationHistogram,
	WorkflowRunStepDurationHistogram,
	WorkflowRunReconcileTimeHistogram,
	WorkflowRunFinishedTimeHistogram,
	WorkflowRunInitializedCounter,
	WorkflowRunPhaseCounter,
	WorkflowRunStepPhaseGauge,
	ClientRequestHistogram,
}

func init() {
	for _, collector := range collectorGroup {
		if err := metrics.Registry.Register(collector); err != nil {
			klog.Error(err)
		}
	}
}
