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

package http

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kubevela/workflow/pkg/utils/httpguard"
)

// boundsServer serves /slow after a delay and /big as a body one byte over the limit.
func boundsServer(t *testing.T, delay time.Duration) *httptest.Server {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/slow":
			select {
			case <-time.After(delay):
			case <-release:
			case <-r.Context().Done():
			}
			_, _ = w.Write([]byte("late"))
		case "/big":
			_, _ = w.Write([]byte(strings.Repeat("x", int(httpguard.MaxResponseBytes)+1)))
		}
	}))
	t.Cleanup(func() {
		close(release)
		srv.Close()
	})
	return srv
}

func do(ctx context.Context, url, timeout string) (string, error) {
	res, err := Do(ctx, &DoParams{Params: RequestVars{Method: "GET", URL: url, Request: &Request{Timeout: timeout}}})
	if err != nil {
		return "", err
	}
	return res.Returns.Body, nil
}

func TestHttpDoRefusesAnUnboundedTimeout(t *testing.T) {
	srv := boundsServer(t, time.Minute)
	for _, timeout := range []string{"0", "-1s", "61s"} {
		_, err := do(context.Background(), srv.URL+"/slow", timeout)
		require.ErrorContains(t, err, fmt.Sprintf("timeout %s must be greater than 0", timeout))
	}
}

func TestHttpDoStopsWithItsContext(t *testing.T) {
	srv := boundsServer(t, time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := do(ctx, srv.URL+"/slow", "30s")
	require.Error(t, err)
	require.Less(t, time.Since(start), 5*time.Second)
}

func TestHttpDoRefusesAnOversizedBody(t *testing.T) {
	srv := boundsServer(t, 0)
	_, err := do(context.Background(), srv.URL+"/big", "")
	require.ErrorContains(t, err, "exceeds")
}
