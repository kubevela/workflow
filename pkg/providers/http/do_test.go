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
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/crossplane/crossplane-runtime/pkg/test"
	"github.com/stretchr/testify/require"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kubevela/workflow/pkg/cue/model/value"
	monitorContext "github.com/kubevela/workflow/pkg/monitor/context"
	"github.com/kubevela/workflow/pkg/providers"
	"github.com/kubevela/workflow/pkg/providers/http/ratelimiter"
	"github.com/kubevela/workflow/pkg/providers/http/testdata"
)

func TestHttpDo(t *testing.T) {
	shutdown := make(chan struct{})
	runMockServer(shutdown)
	defer func() {
		close(shutdown)
	}()
	ctx := monitorContext.NewTraceContext(context.Background(), "")
	baseTemplate := `
		url: string
		request?: close({
			timeout?: string
			body?:    string
			header?:  [string]: string
			trailer?: [string]: string
			ratelimiter?: {
				limit: int
				period: string
			}
		})
		response: close({
			body: string
			header?:  [string]: [...string]
			trailer?: [string]: [...string]
		})
`
	testCases := map[string]struct {
		request      string
		expectedBody string
		expectedErr  string
	}{
		"hello": {
			request: baseTemplate + `
method: "GET"
url: "http://127.0.0.1:1229/hello"
request: {
	timeout: "2s"
}`,
			expectedBody: `hello`,
		},

		"echo": {
			request: baseTemplate + `
method: "POST"
url: "http://127.0.0.1:1229/echo"
request:{ 
   body: "I am vela" 
   header: "Content-Type": "text/plain; charset=utf-8"
}`,
			expectedBody: `I am vela`,
		},
		"json": {
			request: `
import ("encoding/json")
foo: {
	name: "foo"
	score: 100
}

method: "POST"
url: "http://127.0.0.1:1229/echo"
request:{ 
   body: json.Marshal(foo)
   header: "Content-Type": "application/json; charset=utf-8"
}` + baseTemplate,
			expectedBody: `{"name":"foo","score":100}`,
		},
		"timeout": {
			request: baseTemplate + `
method: "GET"
url: "http://127.0.0.1:1229/timeout"
request: {
	timeout: "1s"
}`,
			expectedErr: "context deadline exceeded",
		},
		"not-timeout": {
			request: baseTemplate + `
method: "GET"
url: "http://127.0.0.1:1229/timeout"
request: {
	timeout: "3s"
}`,
			expectedBody: `hello`,
		},
		"invalid-timeout": {
			request: baseTemplate + `
method: "GET"
url: "http://127.0.0.1:1229/timeout"
request: {
	timeout: "test"
}`,
			expectedErr: "invalid duration",
		},
	}

	for tName, tCase := range testCases {
		r := require.New(t)
		v, err := value.NewValue(tCase.request, nil, "")
		r.NoError(err, tName)
		prd := &provider{}
		err = prd.Do(ctx, nil, v, nil)
		if tCase.expectedErr != "" {
			r.Error(err)
			r.Contains(err.Error(), tCase.expectedErr)
			continue
		}
		r.NoError(err, tName)
		body, err := v.LookupValue("response", "body")
		r.NoError(err, tName)
		ret, err := body.CueValue().String()
		r.NoError(err)
		r.Equal(ret, tCase.expectedBody, tName)
	}

	// test ratelimiter
	rateLimiter = ratelimiter.NewRateLimiter(1)
	limiterTestCases := []struct {
		request     string
		expectedErr string
	}{
		{
			request: baseTemplate + `
method: "GET"
url: "http://127.0.0.1:1229/hello"
request: {
	ratelimiter: {
		limit: 1
		period: "1m"
	}
}`},
		{
			request: baseTemplate + `
method: "GET"
url: "http://127.0.0.1:1229/hello?query=1"
request: {
	ratelimiter: {
		limit: 1
		period: "1m"
	}
}`,
			expectedErr: "request exceeds the rate limiter",
		},
		{
			request: baseTemplate + `
method: "GET"
url: "http://127.0.0.1:1229/echo"
request: {
	ratelimiter: {
		limit: 1
		period: "1m"
	}
}`,
		},
		{
			request: baseTemplate + `
method: "GET"
url: "http://127.0.0.1:1229/hello?query=2"
request: {
	ratelimiter: {
		limit: 1
		period: "1m"
	}
}`,
		},
	}

	for tName, tCase := range limiterTestCases {
		r := require.New(t)
		v, err := value.NewValue(tCase.request, nil, "")
		r.NoError(err, tName)
		prd := &provider{}
		err = prd.Do(ctx, nil, v, nil)
		if tCase.expectedErr != "" {
			r.Error(err)
			r.Contains(err.Error(), tCase.expectedErr)
			continue
		}
		r.NoError(err, tName)
	}
}

func TestInstall(t *testing.T) {
	r := require.New(t)
	p := providers.NewProviders()
	Install(p, nil, "")
	h, ok := p.GetHandler("http", "do")
	r.Equal(ok, true)
	r.Equal(h != nil, true)
}

func runMockServer(shutdown chan struct{}) {
	http.HandleFunc("/timeout", func(w http.ResponseWriter, req *http.Request) {
		time.Sleep(time.Second * 2)
		_, _ = w.Write([]byte("hello"))
	})
	http.HandleFunc("/hello", func(w http.ResponseWriter, req *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})
	http.HandleFunc("/echo", func(w http.ResponseWriter, req *http.Request) {
		bt, _ := io.ReadAll(req.Body)
		_, _ = w.Write(bt)
	})
	srv := &http.Server{Addr: ":1229"}
	go srv.ListenAndServe() //nolint
	go func() {
		<-shutdown
		srv.Close()
	}()

	client := &http.Client{}
	// wait server started.
	for {
		time.Sleep(time.Millisecond * 300)
		req, _ := http.NewRequest("GET", "http://127.0.0.1:1229/hello", nil)
		_, err := client.Do(req)
		if err == nil {
			break
		}
	}
}

func TestHTTPSDo(t *testing.T) {
	ctx := monitorContext.NewTraceContext(context.Background(), "")
	s := newMockHttpsServer()
	defer s.Close()
	cli := &test.MockClient{
		MockGet: func(ctx context.Context, key client.ObjectKey, obj client.Object) error {
			secret := obj.(*v1.Secret)
			*secret = v1.Secret{
				Data: map[string][]byte{
					"ca.crt":     []byte(testdata.MockCerts.Ca),
					"client.crt": []byte(testdata.MockCerts.ClientCrt),
					"client.key": []byte(testdata.MockCerts.ClientKey),
				},
			}
			return nil
		},
	}
	r := require.New(t)
	v, err := value.NewValue(`
method: "GET"
url: "https://127.0.0.1:8443/api/v1/token?val=test-token"
`, nil, "")
	r.NoError(err)
	r.NoError(v.FillObject("certs", "tls_config", "secret"))
	prd := &provider{cli, "default"}
	err = prd.Do(ctx, nil, v, nil)
	r.NoError(err)
}

func newMockHttpsServer() *httptest.Server {
	ts := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			fmt.Printf("Expected 'GET' request, got '%s'", r.Method)
		}
		if r.URL.EscapedPath() != "/api/v1/token" {
			fmt.Printf("Expected request to '/person', got '%s'", r.URL.EscapedPath())
		}
		_ = r.ParseForm()
		token := r.Form.Get("val")
		tokenBytes, _ := json.Marshal(map[string]interface{}{"token": token})

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(tokenBytes)
	}))
	l, _ := net.Listen("tcp", "127.0.0.1:8443")
	ts.Listener.Close()
	ts.Listener = l

	decode := func(in string) []byte {
		out, _ := base64.StdEncoding.DecodeString(in)
		return out
	}

	pool := x509.NewCertPool()
	pool.AppendCertsFromPEM(decode(testdata.MockCerts.Ca))
	cert, _ := tls.X509KeyPair(decode(testdata.MockCerts.ServerCrt), decode(testdata.MockCerts.ServerKey))
	ts.TLS = &tls.Config{
		ClientCAs:    pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{"http/1.1"},
	}
	ts.StartTLS()
	return ts
}
