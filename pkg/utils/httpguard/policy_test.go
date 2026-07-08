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

package httpguard

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultPolicy_blocksMetadataAndLinkLocal(t *testing.T) {
	policy := DefaultPolicy()

	blocked := []string{
		"169.254.169.254",
		"169.254.0.1",
		"fd00:ec2::254",
		"100.100.100.200",
	}
	for _, raw := range blocked {
		ip := net.ParseIP(raw)
		require.NotNil(t, ip, raw)
		assert.True(t, policy.Blocked(ip), "expected %s blocked", raw)
	}

	allowed := []string{
		"127.0.0.1",
		"10.0.0.1",
		"192.168.1.1",
		"8.8.8.8",
	}
	for _, raw := range allowed {
		ip := net.ParseIP(raw)
		require.NotNil(t, ip, raw)
		assert.False(t, policy.Blocked(ip), "expected %s allowed", raw)
	}
}

func TestSecureTransport_blocksLinkLocalDial(t *testing.T) {
	client := &http.Client{
		Transport: SecureTransport(http.DefaultTransport.(*http.Transport).Clone(), DefaultPolicy()),
	}
	_, err := client.Get("http://169.254.169.254/latest/meta-data/")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked SSRF target")
}

func TestSecureTransport_allowsLoopback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))
	defer ts.Close()

	client := &http.Client{
		Transport: SecureTransport(http.DefaultTransport.(*http.Transport).Clone(), DefaultPolicy()),
	}
	resp, err := client.Get(ts.URL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestSecureTransport_redirectRevalidated(t *testing.T) {
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://169.254.169.254/latest/meta-data/", http.StatusFound)
	}))
	defer redirector.Close()

	client := &http.Client{
		Transport: SecureTransport(http.DefaultTransport.(*http.Transport).Clone(), DefaultPolicy()),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return nil
		},
	}
	_, err := client.Get(redirector.URL)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "blocked SSRF target")
}
