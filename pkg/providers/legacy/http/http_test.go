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

	"github.com/kubevela/workflow/pkg/providers/legacy/http/ratelimiter"
	"github.com/kubevela/workflow/pkg/providers/legacy/http/testdata"
	"github.com/kubevela/workflow/pkg/providers/types"
)

func TestHttpDo(t *testing.T) {
	shutdown := make(chan struct{})
	runMockServer(shutdown)
	defer func() {
		close(shutdown)
	}()
	ctx := context.Background()

	testCases := map[string]struct {
		request     RequestVars
		expected    ResponseVars
		expectedErr string
	}{
		"hello": {
			request: RequestVars{
				Method: "GET",
				URL:    "http://127.0.0.1:1229/hello",
				Request: &Request{
					Timeout: "2s",
				},
			},
			expected: ResponseVars{
				Body:       "hello",
				StatusCode: 200,
			},
		},

		"echo": {
			request: RequestVars{
				Method: "POST",
				URL:    "http://127.0.0.1:1229/echo",
				Request: &Request{
					Body: "I am vela",
					Header: map[string]string{
						"Content-Type": "text/plain; charset=utf-8",
					},
				},
			},
			expected: ResponseVars{
				Body:       "I am vela",
				StatusCode: 200,
			},
		},
		"json": {
			request: RequestVars{
				Method: "POST",
				URL:    "http://127.0.0.1:1229/echo",
				Request: &Request{
					Body: `{"name":"foo","score":100}`,
					Header: map[string]string{
						"Content-Type": "text/plain; charset=utf-8",
					},
				},
			},
			expected: ResponseVars{
				Body:       `{"name":"foo","score":100}`,
				StatusCode: 200,
			},
		},
		"timeout": {
			request: RequestVars{
				Method: "GET",
				URL:    "http://127.0.0.1:1229/timeout",
				Request: &Request{
					Timeout: "1s",
				},
			},
			expected: ResponseVars{
				Body:       `{"name":"foo","score":100}`,
				StatusCode: 200,
			},
			expectedErr: "context deadline exceeded",
		},
		"not-timeout": {
			request: RequestVars{
				Method: "GET",
				URL:    "http://127.0.0.1:1229/timeout",
				Request: &Request{
					Timeout: "3s",
				},
			},
			expected: ResponseVars{
				Body:       "hello",
				StatusCode: 200,
			},
		},
		"notfound": {
			request: RequestVars{
				Method: "GET",
				URL:    "http://127.0.0.1:1229/notfound",
				Request: &Request{
					Timeout: "1s",
				},
			},
			expected: ResponseVars{
				StatusCode: 404,
			},
		},
	}

	for tName, tc := range testCases {
		r := require.New(t)
		res, err := Do(ctx, &DoParams{
			Params: tc.request,
		})
		if tc.expectedErr != "" {
			r.Error(err)
			r.Contains(err.Error(), tc.expectedErr)
			continue
		}
		r.NoError(err, tName)
		r.Equal(res.Body, tc.expected.Body, tName)
		r.Equal(res.StatusCode, tc.expected.StatusCode, tName)
	}

	// test ratelimiter
	rateLimiter = ratelimiter.NewRateLimiter(1)
	limiterTestCases := []struct {
		request     RequestVars
		expectedErr string
	}{
		{
			request: RequestVars{
				Method: "GET",
				URL:    "http://127.0.0.1:1229/hello",
				Request: &Request{
					RateLimiter: &RateLimiter{
						Limit:  1,
						Period: "1m",
					},
				},
			},
		},
		{
			request: RequestVars{
				Method: "GET",
				URL:    "http://127.0.0.1:1229/hello?query=1",
				Request: &Request{
					RateLimiter: &RateLimiter{
						Limit:  1,
						Period: "1m",
					},
				},
			},
			expectedErr: "request exceeds the rate limiter",
		},
		{
			request: RequestVars{
				Method: "GET",
				URL:    "http://127.0.0.1:1229/echo",
				Request: &Request{
					RateLimiter: &RateLimiter{
						Limit:  1,
						Period: "1m",
					},
				},
			},
		},
		{
			request: RequestVars{
				Method: "GET",
				URL:    "http://127.0.0.1:1229/hello?query=2",
				Request: &Request{
					RateLimiter: &RateLimiter{
						Limit:  1,
						Period: "1m",
					},
				},
			},
		},
	}

	for tName, tc := range limiterTestCases {
		r := require.New(t)
		_, err := Do(ctx, &DoParams{
			Params: tc.request,
		})
		if tc.expectedErr != "" {
			r.Error(err)
			r.Contains(err.Error(), tc.expectedErr)
			continue
		}
		r.NoError(err, tName)
	}
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
	http.HandleFunc("/notfound", func(w http.ResponseWriter, req *http.Request) {
		w.WriteHeader(404)
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
	ctx := context.Background()
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
	_, err := Do(ctx, &DoParams{
		Params: RequestVars{
			Method: "GET",
			URL:    "https://127.0.0.1:8443/api/v1/token?val=test-token",
			TLSConfig: &TLSConfig{
				Secret:    "certs",
				Namespace: "default",
			},
		},
		RuntimeParams: types.RuntimeParams{
			KubeClient: cli,
		},
	})
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
