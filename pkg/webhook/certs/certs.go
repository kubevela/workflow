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

// Package certs sets up the serving cert of the vela-workflow admission webhooks.
package certs

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"

	"github.com/pkg/errors"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/workflow/pkg/utils/servingcert"
)

// Options configures Ensure.
type Options struct {
	SecretNamespace   string
	SecretName        string
	ConfigurationName string
	FailurePolicy     admissionregistrationv1.FailurePolicyType
}

// Ensure keeps the serving cert Secret and webhook configuration in sync.
func Ensure(ctx context.Context, c client.Client, opts Options) (*servingcert.Result, error) {
	configs, err := getConfigurations(ctx, c, opts.ConfigurationName)
	if err != nil {
		return nil, err
	}
	svc, err := webhookService(configs, opts.ConfigurationName)
	if err != nil {
		return nil, err
	}
	res, err := servingcert.EnsureSecret(ctx, c, opts.SecretNamespace, opts.SecretName,
		servingcert.Service{Namespace: svc.Namespace, Name: svc.Name})
	if err != nil {
		return nil, err
	}
	for _, obj := range configs {
		if err = patchConfiguration(ctx, c, obj, res.CABundle, opts.FailurePolicy); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// WriteFiles writes the serving cert and key where the webhook server reads them.
func WriteFiles(dir string, res *servingcert.Result) (bool, error) {
	changed := false
	for name, data := range map[string][]byte{servingcert.CertKey: res.Cert, servingcert.KeyKey: res.Key} {
		wrote, err := servingcert.WriteFileIfChanged(filepath.Join(dir, name), data)
		if err != nil {
			return changed, errors.Wrap(err, "failed to write webhook cert")
		}
		changed = changed || wrote
	}
	return changed, nil
}

func getConfigurations(ctx context.Context, c client.Client, name string) ([]client.Object, error) {
	var configs []client.Object
	for _, obj := range []client.Object{
		&admissionregistrationv1.MutatingWebhookConfiguration{},
		&admissionregistrationv1.ValidatingWebhookConfiguration{},
	} {
		err := c.Get(ctx, types.NamespacedName{Name: name}, obj)
		if apierrors.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get %T %s", obj, name)
		}
		configs = append(configs, obj)
	}
	return configs, nil
}

func webhookService(configs []client.Object, name string) (*admissionregistrationv1.ServiceReference, error) {
	for _, obj := range configs {
		switch o := obj.(type) {
		case *admissionregistrationv1.MutatingWebhookConfiguration:
			for _, w := range o.Webhooks {
				if w.ClientConfig.Service != nil {
					return w.ClientConfig.Service, nil
				}
			}
		case *admissionregistrationv1.ValidatingWebhookConfiguration:
			for _, w := range o.Webhooks {
				if w.ClientConfig.Service != nil {
					return w.ClientConfig.Service, nil
				}
			}
		}
	}
	return nil, fmt.Errorf("no webhook with a service found in the webhook configurations named %s", name)
}

func patchWebhook(cc *admissionregistrationv1.WebhookClientConfig, failurePolicy **admissionregistrationv1.FailurePolicyType,
	ca []byte, policy admissionregistrationv1.FailurePolicyType) bool {
	changed := false
	if !bytes.Equal(cc.CABundle, ca) {
		cc.CABundle = ca
		changed = true
	}
	if policy != "" && (*failurePolicy == nil || **failurePolicy != policy) {
		policyCopy := policy
		*failurePolicy = &policyCopy
		changed = true
	}
	return changed
}

func patchConfiguration(ctx context.Context, c client.Client, obj client.Object, ca []byte,
	policy admissionregistrationv1.FailurePolicyType) error {
	err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		if err := c.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			return client.IgnoreNotFound(err)
		}
		changed := false
		switch o := obj.(type) {
		case *admissionregistrationv1.MutatingWebhookConfiguration:
			for i := range o.Webhooks {
				changed = patchWebhook(&o.Webhooks[i].ClientConfig, &o.Webhooks[i].FailurePolicy, ca, policy) || changed
			}
		case *admissionregistrationv1.ValidatingWebhookConfiguration:
			for i := range o.Webhooks {
				changed = patchWebhook(&o.Webhooks[i].ClientConfig, &o.Webhooks[i].FailurePolicy, ca, policy) || changed
			}
		}
		if !changed {
			return nil
		}
		klog.InfoS("Updating webhook configuration", "kind", fmt.Sprintf("%T", obj), "name", obj.GetName(), "failurePolicy", policy)
		return c.Update(ctx, obj)
	})
	return errors.Wrapf(err, "failed to patch %T %s", obj, obj.GetName())
}
