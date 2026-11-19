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

package main

import (
	"context"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kubevela/workflow/pkg/utils/servingcert"
	webhookcerts "github.com/kubevela/workflow/pkg/webhook/certs"
)

const testWebhookConfigName = "vela-workflow-admission"

func TestWebhookTLSOptions(t *testing.T) {
	opts, err := webhookTLSOptions("vela-system/"+testWebhookConfigName, testWebhookConfigName, "Fail")
	require.NoError(t, err)
	require.Equal(t, webhookcerts.Options{
		SecretNamespace:   "vela-system",
		SecretName:        testWebhookConfigName,
		ConfigurationName: testWebhookConfigName,
		FailurePolicy:     admissionregistrationv1.Fail,
	}, opts)

	tests := []struct {
		name          string
		secret        string
		configuration string
		policy        string
	}{
		{name: "secret without namespace", secret: testWebhookConfigName, configuration: testWebhookConfigName, policy: "Fail"},
		{name: "secret with extra path", secret: "vela-system/secret/extra", configuration: testWebhookConfigName, policy: "Fail"},
		{name: "no webhook configuration", secret: "vela-system/secret", policy: "Fail"},
		{name: "unknown failure policy", secret: "vela-system/secret", configuration: testWebhookConfigName, policy: "Sometimes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := webhookTLSOptions(tt.secret, tt.configuration, tt.policy)
			require.Error(t, err)
		})
	}
}

func TestSetupRuntimeWebhookTLSDisabled(t *testing.T) {
	c, opts, err := setupRuntimeWebhookTLS(context.Background(), nil, false, "", "", "", "")
	require.NoError(t, err)
	require.Nil(t, c)
	require.Empty(t, opts)
}

func TestWebhookServingCert(t *testing.T) {
	ctx := context.Background()
	c := fake.NewClientBuilder().WithScheme(scheme).WithObjects(&admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{Name: testWebhookConfigName},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{{
			Name:          "validating.core.oam.dev.v1alpha1.workflowruns",
			FailurePolicy: ptr.To(admissionregistrationv1.Ignore),
			ClientConfig: admissionregistrationv1.WebhookClientConfig{
				Service: &admissionregistrationv1.ServiceReference{Namespace: "vela-system", Name: "vela-workflow-webhook"},
			},
		}},
	}).Build()
	opts := webhookcerts.Options{
		SecretNamespace:   "vela-system",
		SecretName:        testWebhookConfigName,
		ConfigurationName: testWebhookConfigName,
		FailurePolicy:     admissionregistrationv1.Fail,
	}
	certDir := filepath.Join(t.TempDir(), "serving-certs")

	getWebhook := func() admissionregistrationv1.ValidatingWebhook {
		v := &admissionregistrationv1.ValidatingWebhookConfiguration{}
		require.NoError(t, c.Get(ctx, client.ObjectKey{Name: testWebhookConfigName}, v))
		return v.Webhooks[0]
	}

	require.NoError(t, ensureWebhookTLS(ctx, c, opts, certDir, false))
	crt, err := os.ReadFile(filepath.Join(certDir, servingcert.CertKey))
	require.NoError(t, err)
	key, err := os.ReadFile(filepath.Join(certDir, servingcert.KeyKey))
	require.NoError(t, err)
	require.NoError(t, servingcert.Check(map[string][]byte{
		servingcert.CAKey: getWebhook().ClientConfig.CABundle, servingcert.CertKey: crt, servingcert.KeyKey: key,
	}, []string{"vela-workflow-webhook.vela-system.svc"}, time.Now()))
	require.Equal(t, admissionregistrationv1.Ignore, *getWebhook().FailurePolicy)

	origRetry, origResync := startupRetryInterval, servingCertResyncPeriod
	startupRetryInterval = 5 * time.Millisecond
	servingCertResyncPeriod = 20 * time.Millisecond
	t.Cleanup(func() {
		startupRetryInterval = origRetry
		servingCertResyncPeriod = origResync
	})
	var serving atomic.Bool
	resyncCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		resyncWebhookTLS(resyncCtx, c, opts, certDir, serving.Load)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	require.Never(t, func() bool {
		return *getWebhook().FailurePolicy == admissionregistrationv1.Fail
	}, 50*time.Millisecond, 5*time.Millisecond)

	serving.Store(true)
	require.Eventually(t, func() bool {
		return *getWebhook().FailurePolicy == admissionregistrationv1.Fail
	}, time.Second, 5*time.Millisecond)
}
