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

package http

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"cuelang.org/go/cue"
	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	monitorContext "github.com/kubevela/pkg/monitor/context"

	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	"github.com/kubevela/workflow/pkg/providers/http/ratelimiter"
	"github.com/kubevela/workflow/pkg/types"
)

const (
	// ProviderName is provider name for install.
	ProviderName = "http"
)

var (
	rateLimiter *ratelimiter.RateLimiter
)

func init() {
	rateLimiter = ratelimiter.NewRateLimiter(128)
}

type provider struct {
	cli client.Client
	ns  string
}

// Do process http request.
func (h *provider) Do(ctx monitorContext.Context, wfCtx wfContext.Context, v *value.Value, act types.Action) error {
	resp, err := h.runHTTP(ctx, v)
	if err != nil {
		return err
	}
	return v.FillObject(resp, "response")
}

func (h *provider) runHTTP(ctx monitorContext.Context, v *value.Value) (interface{}, error) {
	var (
		err             error
		method, u       string
		header, trailer http.Header
		r               io.Reader
	)
	defaultClient := &http.Client{
		Transport: http.DefaultTransport,
		Timeout:   time.Second * 3,
	}
	if timeout, err := v.GetString("request", "timeout"); err == nil && timeout != "" {
		duration, err := time.ParseDuration(timeout)
		if err != nil {
			return nil, err
		}
		defaultClient.Timeout = duration
	}
	if method, err = v.GetString("method"); err != nil {
		return nil, err
	}
	if u, err = v.GetString("url"); err != nil {
		return nil, err
	}
	if rl, err := v.LookupValue("request", "ratelimiter"); err == nil {
		limit, err := rl.GetInt64("limit")
		if err != nil {
			return nil, err
		}
		period, err := rl.GetString("period")
		if err != nil {
			return nil, err
		}
		duration, err := time.ParseDuration(period)
		if err != nil {
			return nil, err
		}
		if !rateLimiter.Allow(fmt.Sprintf("%s-%s", method, strings.Split(u, "?")[0]), int(limit), duration) {
			return nil, errors.New("request exceeds the rate limiter")
		}
	}
	if body, err := v.LookupValue("request", "body"); err == nil {
		r, err = body.CueValue().Reader()
		if err != nil {
			return nil, err
		}
	}
	if header, err = parseHeaders(v.CueValue(), "header"); err != nil {
		return nil, err
	}
	if trailer, err = parseHeaders(v.CueValue(), "trailer"); err != nil {
		return nil, err
	}
	if header == nil {
		header = map[string][]string{}
		header.Set("Content-Type", "application/json")
	}

	req, err := http.NewRequestWithContext(context.Background(), method, u, r)
	if err != nil {
		return nil, err
	}
	req.Header = header
	req.Trailer = trailer

	if tr, err := h.getTransport(ctx, v); err == nil && tr != nil {
		defaultClient.Transport = tr
	}

	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	// parse response body and headers
	return map[string]interface{}{
		"body":       string(b),
		"header":     resp.Header,
		"trailer":    resp.Trailer,
		"statusCode": resp.StatusCode,
	}, err
}

func (h *provider) getTransport(ctx monitorContext.Context, v *value.Value) (http.RoundTripper, error) {
	tlsConfig, err := v.LookupValue("tls_config")
	if err != nil {
		return nil, nil
	}
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"http/1.1"},
		},
	}

	secretName, err := tlsConfig.GetString("secret")
	if err != nil {
		return nil, err
	}
	objectKey := client.ObjectKey{
		Namespace: h.ns,
		Name:      secretName,
	}
	index := strings.Index(secretName, "/")
	if index > 0 {
		objectKey.Namespace = secretName[:index-1]
		objectKey.Name = secretName[index:]
	}
	secret := new(v1.Secret)
	if err := h.cli.Get(ctx, objectKey, secret); err != nil {
		return nil, err
	}
	if ca, ok := secret.Data["ca.crt"]; ok {
		caData, err := base64.StdEncoding.DecodeString(string(ca))
		if err != nil {
			return nil, err
		}
		pool := x509.NewCertPool()
		pool.AppendCertsFromPEM(caData)
		tr.TLSClientConfig.RootCAs = pool
	}
	var certData, keyData []byte
	if clientCert, ok := secret.Data["client.crt"]; ok {
		certData, err = base64.StdEncoding.DecodeString(string(clientCert))
		if err != nil {
			return nil, err
		}
	}
	if clientKey, ok := secret.Data["client.key"]; ok {
		keyData, err = base64.StdEncoding.DecodeString(string(clientKey))
		if err != nil {
			return nil, err
		}
	}
	cliCrt, err := tls.X509KeyPair(certData, keyData)
	if err != nil {
		return nil, errors.WithMessage(err, "parse client keypair")
	}
	tr.TLSClientConfig.Certificates = []tls.Certificate{cliCrt}
	return tr, nil
}

func parseHeaders(obj cue.Value, label string) (http.Header, error) {
	m := obj.LookupPath(value.FieldPath("request", label))
	if !m.Exists() {
		return nil, nil
	}
	iter, err := m.Fields()
	if err != nil {
		return nil, err
	}
	h := http.Header{}
	for iter.Next() {
		str, err := iter.Value().String()
		if err != nil {
			return nil, err
		}
		h.Add(iter.Label(), str)
	}
	return h, nil
}

// Install register handlers to provider discover.
func Install(p types.Providers, cli client.Client, ns string) {
	prd := &provider{
		cli: cli,
		ns:  ns,
	}
	p.Register(ProviderName, map[string]types.Handler{
		"do": prd.Do,
	})
}
