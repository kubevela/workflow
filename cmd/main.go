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

package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"strings"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	flag "github.com/spf13/pflag"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apiserver/pkg/util/feature"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/controllers"
	ctrlClient "github.com/kubevela/workflow/pkg/client"
	"github.com/kubevela/workflow/pkg/cue/packages"
	"github.com/kubevela/workflow/pkg/monitor/watcher"
	"github.com/kubevela/workflow/pkg/types"
	"github.com/kubevela/workflow/version"
	//+kubebuilder:scaffold:imports
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var qps float64
	var burst int
	var webhookPort int
	var leaderElectionResourceLock string
	var leaseDuration time.Duration
	var renewDeadline time.Duration
	var retryPeriod time.Duration
	var pprofAddr string
	var controllerArgs controllers.Args

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&leaderElectionResourceLock, "leader-election-resource-lock", "configmapsleases", "The resource lock to use for leader election")
	flag.DurationVar(&leaseDuration, "leader-election-lease-duration", 15*time.Second,
		"The duration that non-leader candidates will wait to force acquire leadership")
	flag.DurationVar(&renewDeadline, "leader-election-renew-deadline", 10*time.Second,
		"The duration that the acting controlplane will retry refreshing leadership before giving up")
	flag.DurationVar(&retryPeriod, "leader-election-retry-period", 2*time.Second,
		"The duration the LeaderElector clients should wait between tries of actions")
	flag.IntVar(&webhookPort, "webhook-port", 9443, "admission webhook listen address")
	flag.IntVar(&controllerArgs.ConcurrentReconciles, "concurrent-reconciles", 4, "concurrent-reconciles is the concurrent reconcile number of the controller. The default value is 4")
	flag.Float64Var(&qps, "kube-api-qps", 50, "the qps for reconcile clients. Low qps may lead to low throughput. High qps may give stress to api-server. Raise this value if concurrent-reconciles is set to be high.")
	flag.IntVar(&burst, "kube-api-burst", 100, "the burst for reconcile clients. Recommend setting it qps*2.")
	flag.StringVar(&pprofAddr, "pprof-addr", "", "The address for pprof to use while exporting profiling results. The default value is empty which means do not expose it. Set it to address like :6666 to expose it.")
	flag.IntVar(&types.MaxWorkflowWaitBackoffTime, "max-workflow-wait-backoff-time", 60, "Set the max workflow wait backoff time, default is 60")
	flag.IntVar(&types.MaxWorkflowFailedBackoffTime, "max-workflow-failed-backoff-time", 300, "Set the max workflow wait backoff time, default is 300")
	flag.IntVar(&types.MaxWorkflowStepErrorRetryTimes, "max-workflow-step-error-retry-times", 10, "Set the max workflow step error retry times, default is 10")
	feature.DefaultMutableFeatureGate.AddFlag(flag.CommandLine)

	flag.Parse()

	if pprofAddr != "" {
		// Start pprof server if enabled
		mux := http.NewServeMux()
		mux.HandleFunc("/debug/pprof/", pprof.Index)
		mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
		pprofServer := http.Server{
			Addr:    pprofAddr,
			Handler: mux,
		}
		klog.InfoS("Starting debug HTTP server", "addr", pprofServer.Addr)

		go func() {
			go func() {
				ctx := context.Background()
				<-ctx.Done()

				ctx, cancelFunc := context.WithTimeout(context.Background(), 60*time.Minute)
				defer cancelFunc()

				if err := pprofServer.Shutdown(ctx); err != nil {
					klog.Error(err, "Failed to shutdown debug HTTP server")
				}
			}()

			if err := pprofServer.ListenAndServe(); !errors.Is(http.ErrServerClosed, err) {
				klog.Error(err, "Failed to start debug HTTP server")
				panic(err)
			}
		}()
	}

	ctrl.SetLogger(klogr.New())

	klog.InfoS("KubeVela Workflow information", "version", version.VelaVersion, "revision", version.GitRevision)

	restConfig := ctrl.GetConfigOrDie()
	restConfig.QPS = float32(qps)
	restConfig.Burst = burst
	klog.InfoS("Kubernetes Config Loaded",
		"QPS", restConfig.QPS,
		"Burst", restConfig.Burst,
	)

	leaderElectionID := fmt.Sprintf("workflow-%s", strings.ToLower(strings.ReplaceAll(version.VelaVersion, ".", "-")))
	mgr, err := ctrl.NewManager(restConfig, ctrl.Options{
		Scheme:                     scheme,
		MetricsBindAddress:         metricsAddr,
		Port:                       webhookPort,
		HealthProbeBindAddress:     probeAddr,
		LeaderElection:             enableLeaderElection,
		LeaderElectionID:           leaderElectionID,
		LeaderElectionResourceLock: leaderElectionResourceLock,
		LeaseDuration:              &leaseDuration,
		RenewDeadline:              &renewDeadline,
		RetryPeriod:                &retryPeriod,
		NewClient:                  ctrlClient.DefaultNewControllerClient,
	})
	if err != nil {
		klog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	pd, err := packages.NewPackageDiscover(mgr.GetConfig())
	if err != nil {
		klog.Error(err, "Failed to create CRD discovery for CUE package client")
		if !packages.IsCUEParseErr(err) {
			os.Exit(1)
		}
	}

	if err = (&controllers.WorkflowRunReconciler{
		Client:          mgr.GetClient(),
		Scheme:          mgr.GetScheme(),
		PackageDiscover: pd,
		Recorder:        event.NewAPIRecorder(mgr.GetEventRecorderFor("WorkflowRun")),
		Args:            controllerArgs,
	}).SetupWithManager(mgr); err != nil {
		klog.Error(err, "unable to create controller", "controller", "WorkflowRun")
		os.Exit(1)
	}
	//+kubebuilder:scaffold:builder

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		klog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		klog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	klog.Info("Start the vela workflow monitor")
	informer, err := mgr.GetCache().GetInformer(context.Background(), &v1alpha1.WorkflowRun{})
	if err != nil {
		klog.ErrorS(err, "Unable to get informer for application")
	}
	watcher.StartWorkflowRunMetricsWatcher(informer)

	klog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		klog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
