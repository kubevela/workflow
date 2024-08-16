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
	_ "embed"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/pkg/errors"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	cuexruntime "github.com/kubevela/pkg/cue/cuex/runtime"

	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/providers/legacy/http/ratelimiter"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
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

// Request .
type Request struct {
	Timeout     string            `json:"timeout,omitempty"`
	Body        string            `json:"body,omitempty"`
	Header      map[string]string `json:"header,omitempty"`
	Trailer     map[string]string `json:"trailer,omitempty"`
	RateLimiter *RateLimiter      `json:"rateLimiter,omitempty"`
}

// RateLimiter .
type RateLimiter struct {
	Limit  int    `json:"limit"`
	Period string `json:"period"`
}

// TLSConfig .
type TLSConfig struct {
	Secret    string `json:"secret"`
	Namespace string `json:"namespace,omitempty"`
}

// RequestVars is the vars for http request
type RequestVars struct {
	Method    string     `json:"method"`
	URL       string     `json:"url"`
	Request   *Request   `json:"request,omitempty"`
	TLSConfig *TLSConfig `json:"tls_config,omitempty"`
}

// ResponseVars is the vars for http response
type ResponseVars struct {
	Body       string      `json:"body"`
	Header     http.Header `json:"header,omitempty"`
	Trailer    http.Header `json:"trailer,omitempty"`
	StatusCode int         `json:"statusCode"`
}

// DoParams is the params for http request
type DoParams = providertypes.LegacyParams[RequestVars]

// Do process http request.
func Do(ctx context.Context, params *DoParams) (*ResponseVars, error) {
	return runHTTP(ctx, params)
}

func runHTTP(ctx context.Context, params *DoParams) (*ResponseVars, error) {
	var (
		err             error
		header, trailer http.Header
		reader          io.Reader
	)
	defaultClient := &http.Client{
		Transport: http.DefaultTransport,
		Timeout:   time.Second * 3,
	}
	method := params.Params.Method
	url := params.Params.URL
	if request := params.Params.Request; request != nil {
		if request.Timeout != "" {
			timeout, err := time.ParseDuration(request.Timeout)
			if err != nil {
				return nil, fmt.Errorf("invalid timeout %s: %w", timeout, err)
			}
			defaultClient.Timeout = timeout
		}
		if request.RateLimiter != nil {
			period, err := time.ParseDuration(request.RateLimiter.Period)
			if err != nil {
				return nil, fmt.Errorf("invalid period %s: %w", period, err)
			}
			if !rateLimiter.Allow(fmt.Sprintf("%s-%s", method, strings.Split(url, "?")[0]), request.RateLimiter.Limit, period) {
				return nil, errors.New("request exceeds the rate limiter")
			}
		}
		reader = strings.NewReader(request.Body)
		header = parseHeaders(request.Header)
		trailer = parseHeaders(request.Trailer)
	}
	if len(header) == 0 {
		header = map[string][]string{}
		header.Set("Content-Type", "application/json")
	}

	req, err := http.NewRequestWithContext(context.Background(), method, url, reader)
	if err != nil {
		return nil, err
	}
	req.Header = header
	req.Trailer = trailer

	if params.Params.TLSConfig != nil {
		if params.Params.TLSConfig.Namespace == "" {
			params.Params.TLSConfig.Namespace = fmt.Sprint(params.ProcessContext.GetData(model.ContextNamespace))
		}
		if tr, err := getTransport(ctx, params.KubeClient, params.Params.TLSConfig.Secret, params.Params.TLSConfig.Namespace); err == nil && tr != nil {
			defaultClient.Transport = tr
		}
	}

	resp, err := defaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	//nolint:errcheck
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	// parse response body and headers
	return &ResponseVars{
		Body:       string(b),
		Header:     resp.Header,
		Trailer:    resp.Trailer,
		StatusCode: resp.StatusCode,
	}, nil
}

func getTransport(ctx context.Context, cli client.Client, secretName, ns string) (http.RoundTripper, error) {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{
			NextProtos: []string{"http/1.1"},
		},
	}
	objectKey := client.ObjectKey{
		Namespace: ns,
		Name:      secretName,
	}
	index := strings.Index(secretName, "/")
	if index > 0 {
		objectKey.Namespace = secretName[:index-1]
		objectKey.Name = secretName[index:]
	}
	secret := new(v1.Secret)
	if err := cli.Get(ctx, objectKey, secret); err != nil {
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
	var err error
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

func parseHeaders(obj map[string]string) http.Header {
	h := http.Header{}
	for k, v := range obj {
		h.Add(k, v)
	}
	return h
}

//go:embed http.cue
var template string

// GetTemplate returns the cue template
func GetTemplate() string {
	return template
}

// GetProviders returns the providers
func GetProviders() map[string]cuexruntime.ProviderFn {
	return map[string]cuexruntime.ProviderFn{
		"do": providertypes.LegacyGenericProviderFn[RequestVars, ResponseVars](Do),
	}
}
