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

package controllers

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/utils"
)

var _ = Describe("Test Backup", func() {
	ctx := context.Background()
	namespace := "test-ns"

	wrTemplate := &v1alpha1.WorkflowRun{
		TypeMeta: metav1.TypeMeta{
			Kind:       "WorkflowRun",
			APIVersion: "core.oam.dev/v1alpha1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "wr",
			Namespace: namespace,
		},
		Spec: v1alpha1.WorkflowRunSpec{
			WorkflowSpec: &v1alpha1.WorkflowSpec{
				Steps: []v1alpha1.WorkflowStep{
					{
						WorkflowStepBase: v1alpha1.WorkflowStepBase{
							Name: "step-1",
							Type: "suspend",
						},
					},
				},
			},
		},
	}
	testDefinitions := []string{"test-apply", "apply-object", "failed-render"}

	BeforeEach(func() {
		setupNamespace(ctx, namespace)
		setupTestDefinitions(ctx, testDefinitions, namespace)
		By("[TEST] Set up definitions before integration test")
	})

	AfterEach(func() {
		Expect(k8sClient.DeleteAllOf(ctx, &v1alpha1.WorkflowRun{}, client.InNamespace(namespace))).Should(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &appsv1.Deployment{}, client.InNamespace(namespace))).Should(Succeed())
		Expect(k8sClient.DeleteAllOf(ctx, &corev1.ConfigMap{}, client.InNamespace(namespace))).Should(Succeed())
	})

	It("workflow not finished with finished strategy", func() {
		backupReconciler := &BackupReconciler{
			Client: k8sClient,
			Scheme: testScheme,
			BackupArgs: BackupArgs{
				BackupStrategy: StrategyBackupFinishedRecord,
				CleanOnBackup:  true,
			},
		}
		wr := wrTemplate.DeepCopy()
		wr.Name = "not-finished"
		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())

		tryReconcileBackup(backupReconciler, wr.Name, wr.Namespace)
		wrObj := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}, wrObj)).Should(BeNil())
	})

	It("workflow finished with finished strategy", func() {
		backupReconciler := &BackupReconciler{
			Client: k8sClient,
			Scheme: testScheme,
			BackupArgs: BackupArgs{
				BackupStrategy: StrategyBackupFinishedRecord,
				CleanOnBackup:  true,
			},
		}
		wr := wrTemplate.DeepCopy()
		wr.Name = "finished"
		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())
		wr.Status.Finished = true
		Expect(k8sClient.Status().Update(ctx, wr)).Should(BeNil())

		tryReconcileBackup(backupReconciler, wr.Name, wr.Namespace)
		wrObj := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}, wrObj)).Should(utils.NotFoundMatcher{})
	})

	It("no strategy specified", func() {
		backupReconciler := &BackupReconciler{
			Client: k8sClient,
			Scheme: testScheme,
		}
		wr := wrTemplate.DeepCopy()
		wr.Name = "no-strategy"
		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())
		wr.Status.Finished = true
		Expect(k8sClient.Status().Update(ctx, wr)).Should(BeNil())

		err := reconcileBackupWithReturn(backupReconciler, wr.Name, wr.Namespace)
		Expect(err).ShouldNot(BeNil())
	})

	It("workflow latest failed with failed strategy", func() {
		backupReconciler := &BackupReconciler{
			Client: k8sClient,
			Scheme: testScheme,
			BackupArgs: BackupArgs{
				BackupStrategy: StrategyBackupFinishedRecord,
				IgnoreStrategy: StrategyIgnoreLatestFailed,
				CleanOnBackup:  true,
			},
		}
		wr := wrTemplate.DeepCopy()
		wr.Name = "latest-failed"
		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())
		wr.Status.Finished = true
		wr.Status.Phase = v1alpha1.WorkflowStateFailed
		Expect(k8sClient.Status().Update(ctx, wr)).Should(BeNil())

		tryReconcileBackup(backupReconciler, wr.Name, wr.Namespace)
		wrObj := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}, wrObj)).Should(BeNil())
	})

	It("workflow latest failed with failed strategy", func() {
		backupReconciler := &BackupReconciler{
			Client: k8sClient,
			Scheme: testScheme,
			BackupArgs: BackupArgs{
				BackupStrategy: StrategyBackupFinishedRecord,
				IgnoreStrategy: StrategyIgnoreLatestFailed,
				CleanOnBackup:  true,
			},
		}
		wr := wrTemplate.DeepCopy()
		wr.Name = "failed-wr"
		Expect(k8sClient.Create(ctx, wr)).Should(BeNil())
		wr.Status.Finished = true
		wr.Status.Phase = v1alpha1.WorkflowStateFailed
		wr.Status.EndTime = metav1.Now()
		Expect(k8sClient.Status().Update(ctx, wr)).Should(BeNil())

		wr2 := wrTemplate.DeepCopy()
		wr2.Name = "failed-wr2"
		Expect(k8sClient.Create(ctx, wr2)).Should(BeNil())
		wr2.Status.Finished = true
		wr2.Status.Phase = v1alpha1.WorkflowStateFailed
		Expect(k8sClient.Status().Update(ctx, wr2)).Should(BeNil())

		latest := wrTemplate.DeepCopy()
		latest.Name = "latest-failed"
		Expect(k8sClient.Create(ctx, latest)).Should(BeNil())
		latest.Status.Finished = true
		latest.Status.Phase = v1alpha1.WorkflowStateFailed
		latest.Status.EndTime.Time = wr.Status.EndTime.Add(time.Hour)
		Expect(k8sClient.Status().Update(ctx, latest)).Should(BeNil())

		tryReconcileBackup(backupReconciler, wr.Name, wr.Namespace)
		wrObj := &v1alpha1.WorkflowRun{}
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr.Name,
			Namespace: wr.Namespace,
		}, wrObj)).Should(utils.NotFoundMatcher{})

		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      wr2.Name,
			Namespace: wr2.Namespace,
		}, wrObj)).Should(utils.NotFoundMatcher{})

		tryReconcileBackup(backupReconciler, wr2.Name, wr2.Namespace)

		tryReconcileBackup(backupReconciler, latest.Name, latest.Namespace)
		Expect(k8sClient.Get(ctx, client.ObjectKey{
			Name:      latest.Name,
			Namespace: latest.Namespace,
		}, wrObj)).Should(BeNil())
	})

})

func reconcileBackupWithReturn(r *BackupReconciler, name, ns string) error {
	wrKey := client.ObjectKey{
		Name:      name,
		Namespace: ns,
	}
	_, err := r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: wrKey})
	return err
}

func tryReconcileBackup(r *BackupReconciler, name, ns string) {
	wrKey := client.ObjectKey{
		Name:      name,
		Namespace: ns,
	}
	_, err := r.Reconcile(context.TODO(), reconcile.Request{NamespacedName: wrKey})
	if err != nil {
		By(fmt.Sprintf("reconcile err: %+v ", err))
	}
	Expect(err).Should(BeNil())
}
