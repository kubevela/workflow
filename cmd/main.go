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
	goflag "flag"
	"fmt"
	"io"
	"net/http"
	"net/http/pprof"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	"github.com/kubevela/pkg/controller/sharding"
	flag "github.com/spf13/pflag"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/apiserver/pkg/util/feature"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/klog/v2"
	"k8s.io/klog/v2/klogr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"

	triggerv1alpha1 "github.com/kubevela/kube-trigger/api/v1alpha1"
	velaclient "github.com/kubevela/pkg/controller/client"
	"github.com/kubevela/pkg/multicluster"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/controllers"
	"github.com/kubevela/workflow/pkg/backup"
	"github.com/kubevela/workflow/pkg/common"
	"github.com/kubevela/workflow/pkg/cue/packages"
	"github.com/kubevela/workflow/pkg/features"
	"github.com/kubevela/workflow/pkg/monitor/watcher"
	"github.com/kubevela/workflow/pkg/types"
	"github.com/kubevela/workflow/pkg/webhook"
	"github.com/kubevela/workflow/version"
	//+kubebuilder:scaffold:imports
)

var (
	scheme             = runtime.NewScheme()
	waitSecretTimeout  = 90 * time.Second
	waitSecretInterval = 2 * time.Second
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(v1alpha1.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func main() {
	var metricsAddr, logFilePath, probeAddr, pprofAddr, leaderElectionResourceLock, userAgent, certDir string
	var backupStrategy, backupIgnoreStrategy, backupPersistType, groupByLabel, backupConfigSecretName, backupConfigSecretNamespace string
	var enableLeaderElection, useWebhook, logDebug, backupCleanOnBackup bool
	var qps float64
	var logFileMaxSize uint64
	var burst, webhookPort int
	var leaseDuration, renewDeadline, retryPeriod time.Duration
	var controllerArgs controllers.Args

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.StringVar(&logFilePath, "log-file-path", "", "The file to write logs to.")
	flag.Uint64Var(&logFileMaxSize, "log-file-max-size", 1024, "Defines the maximum size a log file can grow to, Unit is megabytes.")
	flag.BoolVar(&logDebug, "log-debug", false, "Enable debug logs for development purpose")
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
	flag.BoolVar(&useWebhook, "use-webhook", false, "Enable Admission Webhook")
	flag.StringVar(&certDir, "webhook-cert-dir", "/k8s-webhook-server/serving-certs", "Admission webhook cert/key dir.")
	flag.IntVar(&webhookPort, "webhook-port", 9443, "admission webhook listen address")
	flag.IntVar(&controllerArgs.ConcurrentReconciles, "concurrent-reconciles", 4, "concurrent-reconciles is the concurrent reconcile number of the controller. The default value is 4")
	flag.BoolVar(&controllerArgs.IgnoreWorkflowWithoutControllerRequirement, "ignore-workflow-without-controller-requirement", false, "If true, workflow controller will not process the workflowrun without 'workflowrun.oam.dev/controller-version-require' annotation")
	flag.Float64Var(&qps, "kube-api-qps", 50, "the qps for reconcile clients. Low qps may lead to low throughput. High qps may give stress to api-server. Raise this value if concurrent-reconciles is set to be high.")
	flag.IntVar(&burst, "kube-api-burst", 100, "the burst for reconcile clients. Recommend setting it qps*2.")
	flag.StringVar(&userAgent, "user-agent", "vela-workflow", "the user agent of the client.")
	flag.StringVar(&pprofAddr, "pprof-addr", "", "The address for pprof to use while exporting profiling results. The default value is empty which means do not expose it. Set it to address like :6666 to expose it.")
	flag.IntVar(&types.MaxWorkflowWaitBackoffTime, "max-workflow-wait-backoff-time", 60, "Set the max workflow wait backoff time, default is 60")
	flag.IntVar(&types.MaxWorkflowFailedBackoffTime, "max-workflow-failed-backoff-time", 300, "Set the max workflow wait backoff time, default is 300")
	flag.IntVar(&types.MaxWorkflowStepErrorRetryTimes, "max-workflow-step-error-retry-times", 10, "Set the max workflow step error retry times, default is 10")
	flag.StringVar(&backupStrategy, "backup-strategy", "BackupFinishedRecord", "Set the strategy for backup workflow records, default is RemainLatestFailedRecord")
	flag.StringVar(&backupIgnoreStrategy, "backup-ignore-strategy", "", "Set the strategy for ignore backup workflow records, default is IgnoreLatestFailedRecord")
	flag.StringVar(&backupPersistType, "backup-persist-type", "", "Set the persist type for backup workflow records, default is empty")
	flag.StringVar(&groupByLabel, "backup-group-by-label", "", "Set the label for group by, default is empty")
	flag.BoolVar(&backupCleanOnBackup, "backup-clean-on-backup", false, "Set the auto clean for backup workflow records, default is false")
	flag.StringVar(&backupConfigSecretName, "backup-config-secret-name", "backup-config", "Set the secret name for backup workflow configs, default is backup-config")
	flag.StringVar(&backupConfigSecretNamespace, "backup-config-secret-namespace", "vela-system", "Set the secret namespace for backup workflow configs, default is backup-config")
	multicluster.AddClusterGatewayClientFlags(flag.CommandLine)
	feature.DefaultMutableFeatureGate.AddFlag(flag.CommandLine)
	sharding.AddControllerFlags(flag.CommandLine)

	// setup logging
	klog.InitFlags(nil)
	flag.CommandLine.AddGoFlagSet(goflag.CommandLine)
	flag.Parse()
	if logDebug {
		_ = flag.Set("v", strconv.Itoa(int(common.LogDebug)))
	}

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

	if logFilePath != "" {
		_ = flag.Set("logtostderr", "false")
		_ = flag.Set("log_file", logFilePath)
		_ = flag.Set("log_file_max_size", strconv.FormatUint(logFileMaxSize, 10))
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
	restConfig.UserAgent = userAgent

	if feature.DefaultMutableFeatureGate.Enabled(features.EnableWatchEventListener) {
		utilruntime.Must(triggerv1alpha1.AddToScheme(scheme))
	}

	leaderElectionID := fmt.Sprintf("workflow-%s", strings.ToLower(strings.ReplaceAll(version.VelaVersion, ".", "-")))
	leaderElectionID += sharding.GetShardIDSuffix()
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
		NewClient:                  velaclient.DefaultNewControllerClient,
		NewCache:                   sharding.BuildCache(scheme, &v1alpha1.WorkflowRun{}),
		CertDir:                    certDir,
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
	controllerArgs.PackageDiscover = pd

	if useWebhook {
		klog.InfoS("Enable webhook", "server port", strconv.Itoa(webhookPort))
		webhook.Register(mgr, controllerArgs)
		if err := waitWebhookSecretVolume(certDir, waitSecretTimeout, waitSecretInterval); err != nil {
			klog.ErrorS(err, "Unable to get webhook secret")
			os.Exit(1)
		}
	}

	kubeClient := mgr.GetClient()

	if err = (&controllers.WorkflowRunReconciler{
		Client:            kubeClient,
		Scheme:            mgr.GetScheme(),
		Recorder:          event.NewAPIRecorder(mgr.GetEventRecorderFor("WorkflowRun")),
		ControllerVersion: version.VelaVersion,
		Args:              controllerArgs,
	}).SetupWithManager(mgr); err != nil {
		klog.Error(err, "unable to create controller", "controller", "WorkflowRun")
		os.Exit(1)
	}

	if feature.DefaultMutableFeatureGate.Enabled(features.EnableBackupWorkflowRecord) {
		if backupPersistType == "" {
			klog.Warning("Backup persist type is empty, workflow record won't be persisted")
		}
		configSecret := &corev1.Secret{}
		reader := mgr.GetAPIReader()
		if err := reader.Get(context.Background(), client.ObjectKey{
			Name:      backupConfigSecretName,
			Namespace: backupConfigSecretNamespace,
		}, configSecret); err != nil && !kerrors.IsNotFound(err) {
			klog.Error(err, "unable to find secret")
			os.Exit(1)
		}
		persister, err := backup.NewPersister(configSecret.Data, backupPersistType)
		if err != nil {
			klog.Error(err, "unable to create persister")
			os.Exit(1)
		}
		if err = (&controllers.BackupReconciler{
			Client:            kubeClient,
			Scheme:            mgr.GetScheme(),
			ControllerVersion: version.VelaVersion,
			BackupArgs: controllers.BackupArgs{
				BackupStrategy: backupStrategy,
				IgnoreStrategy: backupIgnoreStrategy,
				CleanOnBackup:  backupCleanOnBackup,
				GroupByLabel:   groupByLabel,
				Persister:      persister,
			},
			Args: controllerArgs,
		}).SetupWithManager(mgr); err != nil {
			klog.Error(err, "unable to create controller", "controller", "backup")
			os.Exit(1)
		}
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

	if logFilePath != "" {
		klog.Flush()
	}
	klog.Info("Safely stops Program...")
}

// waitWebhookSecretVolume waits for webhook secret ready to avoid mgr running crash
func waitWebhookSecretVolume(certDir string, timeout, interval time.Duration) error {
	start := time.Now()
	for {
		time.Sleep(interval)
		if time.Since(start) > timeout {
			return fmt.Errorf("getting webhook secret timeout after %s", timeout.String())
		}
		klog.InfoS("Wait webhook secret", "time consumed(second)", int64(time.Since(start).Seconds()),
			"timeout(second)", int64(timeout.Seconds()))
		if _, err := os.Stat(certDir); !os.IsNotExist(err) {
			ready := func() bool {
				f, err := os.Open(filepath.Clean(certDir))
				if err != nil {
					return false
				}
				defer func() {
					if err := f.Close(); err != nil {
						klog.Error(err, "Failed to close file")
					}
				}()
				// check if dir is empty
				if _, err := f.Readdir(1); errors.Is(err, io.EOF) {
					return false
				}
				// check if secret files are empty
				err = filepath.Walk(certDir, func(path string, info os.FileInfo, err error) error {
					// even Cert dir is created, cert files are still empty for a while
					if info.Size() == 0 {
						return errors.New("secret is not ready")
					}
					return nil
				})
				if err == nil {
					klog.InfoS("Webhook secret is ready", "time consumed(second)",
						int64(time.Since(start).Seconds()))
					return true
				}
				return false
			}()
			if ready {
				return nil
			}
		}
	}
}
