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
	"fmt"
	"strings"
	"time"

	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	webhookcerts "github.com/kubevela/workflow/pkg/webhook/certs"
)

var (
	servingCertResyncPeriod = time.Minute
	startupRetryInterval    = 2 * time.Second
	startupRetryTimeout     = 2 * time.Minute
)

func retryStartup(ctx context.Context, name string, fn func(context.Context) error) error {
	var lastErr error
	err := wait.PollImmediateWithContext(ctx, startupRetryInterval, startupRetryTimeout, func(ctx context.Context) (bool, error) {
		lastErr = fn(ctx)
		if lastErr != nil {
			klog.InfoS("Waiting to set up serving cert", "for", name, "reason", lastErr.Error())
		}
		return lastErr == nil, nil
	})
	if lastErr != nil {
		return lastErr
	}
	return err
}

func webhookTLSOptions(secretRef, configurationName, failurePolicy string) (webhookcerts.Options, error) {
	namespace, name, err := parseNamespacedName("webhook-tls-secret", secretRef)
	if err != nil {
		return webhookcerts.Options{}, err
	}
	if configurationName == "" {
		return webhookcerts.Options{}, fmt.Errorf("--webhook-configuration is required with --webhook-tls-secret")
	}
	policy := admissionregistrationv1.FailurePolicyType(failurePolicy)
	if policy != "" && policy != admissionregistrationv1.Fail && policy != admissionregistrationv1.Ignore {
		return webhookcerts.Options{}, fmt.Errorf("invalid --webhook-failure-policy %q, want Fail or Ignore", failurePolicy)
	}
	return webhookcerts.Options{
		SecretNamespace:   namespace,
		SecretName:        name,
		ConfigurationName: configurationName,
		FailurePolicy:     policy,
	}, nil
}

func setupRuntimeWebhookTLS(ctx context.Context, restConfig *rest.Config, useWebhook bool,
	secretRef, configurationName, failurePolicy, certDir string) (client.Client, webhookcerts.Options, error) {
	if !useWebhook || secretRef == "" {
		return nil, webhookcerts.Options{}, nil
	}
	opts, err := webhookTLSOptions(secretRef, configurationName, failurePolicy)
	if err != nil {
		return nil, webhookcerts.Options{}, err
	}
	c, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, webhookcerts.Options{}, fmt.Errorf("failed to create client for webhook TLS setup: %w", err)
	}
	if err = retryStartup(ctx, "webhook TLS", func(ctx context.Context) error {
		return ensureWebhookTLS(ctx, c, opts, certDir, false)
	}); err != nil {
		return nil, webhookcerts.Options{}, fmt.Errorf("failed to set up webhook TLS: %w", err)
	}
	return c, opts, nil
}

func ensureWebhookTLS(ctx context.Context, c client.Client, opts webhookcerts.Options, certDir string, applyPolicy bool) error {
	if !applyPolicy {
		opts.FailurePolicy = ""
	}
	res, err := webhookcerts.Ensure(ctx, c, opts)
	if err != nil {
		return err
	}
	changed, err := webhookcerts.WriteFiles(certDir, res)
	if err != nil {
		return err
	}
	if changed {
		klog.InfoS("Wrote the webhook serving cert", "secret", opts.SecretNamespace+"/"+opts.SecretName, "certDir", certDir)
	}
	return nil
}

func resyncWebhookTLS(ctx context.Context, c client.Client, opts webhookcerts.Options, certDir string, serving func() bool) {
	policySet := false
	for {
		period := servingCertResyncPeriod
		if !policySet {
			period = startupRetryInterval
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(period):
		}
		up := serving()
		if err := ensureWebhookTLS(ctx, c, opts, certDir, up); err != nil {
			klog.ErrorS(err, "Failed to resync the webhook serving cert")
			continue
		}
		if up && !policySet {
			policySet = true
			if opts.FailurePolicy != "" {
				klog.InfoS("Webhook server is serving, set the webhook failurePolicy", "failurePolicy", opts.FailurePolicy)
			}
		}
	}
}

func parseNamespacedName(flagName, ref string) (namespace, name string, err error) {
	namespace, name, ok := strings.Cut(ref, "/")
	if !ok || namespace == "" || name == "" || strings.Contains(name, "/") {
		return "", "", fmt.Errorf("invalid --%s %q, want <namespace>/<name>", flagName, ref)
	}
	return namespace, name, nil
}
