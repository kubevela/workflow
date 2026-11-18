/*
Copyright 2026 The KubeVela Authors.

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
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilfeature "k8s.io/apiserver/pkg/util/feature"
	featuregatetesting "k8s.io/component-base/featuregate/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	oamv1alpha1 "github.com/kubevela/pkg/apis/oam/v1alpha1"

	"github.com/kubevela/workflow/api/v1alpha1"
	"github.com/kubevela/workflow/pkg/features"
	"github.com/kubevela/workflow/pkg/utils"
)

// A full reconcile of a started WorkflowRun where another writer changes it while a side-effect
// step is in flight: the step's request is sent once, and a user's terminate stops the run.
var _ = Describe("Side-effect steps run once when the WorkflowRun changes mid-step", func() {
	ctx := context.Background()
	namespace := "runs-once"

	BeforeEach(func() {
		setupNamespace(ctx, namespace)
		setupTestDefinitions(ctx, []string{"side-effect"}, namespace)
	})
	AfterEach(func() {
		Expect(k8sClient.DeleteAllOf(ctx, &v1alpha1.WorkflowRun{}, client.InNamespace(namespace))).Should(Succeed())
	})

	// run creates a run of steps and reconciles it until it finishes. mid acts on the run while the
	// first request is being served; it returns the requests per step.
	run := func(name string, steps []string, mid func(key client.ObjectKey)) (*v1alpha1.WorkflowRun, map[string]int) {
		key := client.ObjectKey{Namespace: namespace, Name: name}
		var mu sync.Mutex
		sent := map[string]int{}
		var once sync.Once
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			defer GinkgoRecover()
			body, _ := io.ReadAll(req.Body)
			mu.Lock()
			sent[string(body)]++
			mu.Unlock()
			once.Do(func() { mid(key) })
			_, _ = w.Write([]byte(`{"ok":true}`))
		}))
		DeferCleanup(server.Close)

		props := fmt.Sprintf(`{"url":%q}`, server.URL)
		var wfSteps []oamv1alpha1.WorkflowStep
		for _, n := range steps {
			step := oamv1alpha1.WorkflowStep{WorkflowStepBase: oamv1alpha1.WorkflowStepBase{Name: n, Type: "side-effect", Properties: &runtime.RawExtension{Raw: []byte(props)}}}
			if strings.HasPrefix(n, "suspend:") {
				step.Name, step.Type = strings.TrimPrefix(n, "suspend:"), "suspend"
				step.Properties = &runtime.RawExtension{Raw: []byte(`{"duration":"1s"}`)}
			}
			wfSteps = append(wfSteps, step)
		}
		wr := &v1alpha1.WorkflowRun{
			ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
			Spec:       v1alpha1.WorkflowRunSpec{WorkflowSpec: &oamv1alpha1.WorkflowSpec{Steps: wfSteps}},
		}
		Expect(k8sClient.Create(ctx, wr)).Should(Succeed())
		// A run that has started, as one the controller has already reconciled.
		wr.Status.StartTime = metav1.Now()
		wr.Status.Mode = oamv1alpha1.WorkflowExecuteMode{Steps: v1alpha1.WorkflowModeStep, SubSteps: v1alpha1.WorkflowModeDAG}
		Expect(k8sClient.Status().Update(ctx, wr)).Should(Succeed())

		r := withRunsOnce(reconciler)
		got := &v1alpha1.WorkflowRun{}
		for i := 0; i < 50; i++ {
			_, _ = r.Reconcile(ctx, reconcile.Request{NamespacedName: key})
			Expect(k8sClient.Get(ctx, key, got)).Should(Succeed())
			if got.Status.Finished {
				break
			}
			time.Sleep(200 * time.Millisecond)
		}
		mu.Lock()
		defer mu.Unlock()
		return got, sent
	}

	for _, atOnce := range []bool{false, true} {
		atOnce := atOnce
		gate := fmt.Sprintf("EnablePatchStatusAtOnce=%v", atOnce)

		It("stops the run on a terminate mid-request and sends no request twice, "+gate, func() {
			featuregatetesting.SetFeatureGateDuringTest(GinkgoT(), utilfeature.DefaultFeatureGate, features.EnablePatchStatusAtOnce, atOnce)
			steps := []string{"suspend:warm-up", "slow-call", "suspend:hold", "notify-after"}
			got, sent := run(fmt.Sprintf("terminate-mid-step-%v", atOnce), steps, func(key client.ObjectKey) {
				live := &v1alpha1.WorkflowRun{}
				Expect(k8sClient.Get(ctx, key, live)).Should(Succeed())
				Expect(utils.TerminateWorkflow(ctx, k8sClient, live)).Should(Succeed())
			})
			Expect(sent["slow-call"]).Should(Equal(1), "the in-flight request is sent once")
			Expect(sent["notify-after"]).Should(BeZero(), "no step after the terminate runs")
			Expect(got.Status.Terminated).Should(BeTrue(), "the user's terminate stands")
			Expect(got.Status.Finished).Should(BeTrue(), "the run ends")
			Expect(got.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateTerminated), "the run reports terminated")
		})

		It("sends each request once when the run is annotated mid-step, "+gate, func() {
			featuregatetesting.SetFeatureGateDuringTest(GinkgoT(), utilfeature.DefaultFeatureGate, features.EnablePatchStatusAtOnce, atOnce)
			got, sent := run(fmt.Sprintf("annotate-mid-step-%v", atOnce), []string{"notify", "notify-again"}, func(key client.ObjectKey) {
				live := &v1alpha1.WorkflowRun{}
				Expect(k8sClient.Get(ctx, key, live)).Should(Succeed())
				live.Annotations = map[string]string{"touched-by": "kubectl"}
				Expect(k8sClient.Update(ctx, live)).Should(Succeed())
			})
			Expect(got.Status.Phase).Should(BeEquivalentTo(v1alpha1.WorkflowStateSucceeded))
			Expect(sent).Should(Equal(map[string]int{"notify": 1, "notify-again": 1}), "each request is sent once")
		})
	}
})
