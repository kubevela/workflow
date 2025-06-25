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
	"path/filepath"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/event"
	cuexv1alpha1 "github.com/kubevela/pkg/apis/cue/v1alpha1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/dynamic/fake"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kubevela/pkg/util/singleton"
	"github.com/kubevela/workflow/api/v1alpha1"
	//+kubebuilder:scaffold:imports
)

// These tests use Ginkgo (BDD-style Go testing framework). Refer to
// http://onsi.github.io/ginkgo/ to learn more about Ginkgo.

var cfg *rest.Config
var k8sClient client.Client
var testEnv *envtest.Environment
var reconciler *WorkflowRunReconciler
var testScheme *runtime.Scheme
var recorder = NewFakeRecorder(10000)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)

	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func(ctx SpecContext) {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "charts", "vela-workflow", "crds")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	// cfg is defined in this file globally.
	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	testScheme = scheme.Scheme
	err = v1alpha1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())
	err = cuexv1alpha1.AddToScheme(testScheme)
	Expect(err).NotTo(HaveOccurred())

	//+kubebuilder:scaffold:scheme

	k8sClient, err = client.New(cfg, client.Options{Scheme: testScheme})
	Expect(err).NotTo(HaveOccurred())
	Expect(k8sClient).NotTo(BeNil())
	singleton.KubeClient.Set(k8sClient)
	fakeDynamicClient := fake.NewSimpleDynamicClient(testScheme)
	singleton.DynamicClient.Set(fakeDynamicClient)

	reconciler = &WorkflowRunReconciler{
		Client:   k8sClient,
		Scheme:   testScheme,
		Recorder: event.NewAPIRecorder(recorder),
	}
}, NodeTimeout(1*time.Minute))

var _ = AfterSuite(func() {
	By("tearing down the test environment")
	err := testEnv.Stop()
	Expect(err).NotTo(HaveOccurred())
})

type FakeRecorder struct {
	Events  chan string
	Message map[string][]*Events
}

type Events struct {
	Name      string
	Namespace string
	EventType string
	Reason    string
	Message   string
}

func (f *FakeRecorder) Event(object runtime.Object, eventtype, reason, message string) {
	if f.Events != nil {
		objectMeta, err := meta.Accessor(object)
		if err != nil {
			return
		}

		event := &Events{
			Name:      objectMeta.GetName(),
			Namespace: objectMeta.GetNamespace(),
			EventType: eventtype,
			Reason:    reason,
			Message:   message,
		}

		records, ok := f.Message[objectMeta.GetName()]
		if !ok {
			f.Message[objectMeta.GetName()] = []*Events{event}
			return
		}

		records = append(records, event)
		f.Message[objectMeta.GetName()] = records

	}
}

func (f *FakeRecorder) Eventf(object runtime.Object, eventtype, reason, messageFmt string, args ...interface{}) {
	f.Event(object, eventtype, reason, messageFmt)
}

func (f *FakeRecorder) AnnotatedEventf(object runtime.Object, annotations map[string]string, eventtype, reason, messageFmt string, args ...interface{}) {
	f.Eventf(object, eventtype, reason, messageFmt, args...)
}

func (f *FakeRecorder) GetEventsWithName(name string) ([]*Events, error) {
	records, ok := f.Message[name]
	if !ok {
		return nil, errors.New("not found events")
	}

	return records, nil
}

// NewFakeRecorder creates new fake event recorder with event channel with
// buffer of given size.
func NewFakeRecorder(bufferSize int) *FakeRecorder {
	return &FakeRecorder{
		Events:  make(chan string, bufferSize),
		Message: make(map[string][]*Events),
	}
}
