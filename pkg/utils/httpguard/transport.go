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
	"context"
	"net"
	"net/http"
	"syscall"
	"time"
)

// SecureTransport returns a copy of base with dial-time SSRF validation applied.
// Every connection, including redirect follows, re-enters the dial hook. Proxy
// settings and any DialTLS or DialTLSContext hook on base are dropped: the TLS
// handshake runs over the guarded DialContext.
func SecureTransport(base *http.Transport, policy Policy) *http.Transport {
	if base == nil {
		base = defaultTransport()
	} else {
		base = base.Clone()
	}
	// Do not inherit environment proxy settings: proxied dials only validate the
	// proxy address, which would bypass destination SSRF checks on hostnames.
	base.Proxy = nil
	base.ProxyConnectHeader = nil

	// Same bounds as http.DefaultTransport's dialer.
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		Control:   controlFunc(policy),
	}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if err := policy.BlockedAddress(address); err != nil {
			return nil, err
		}
		return dialer.DialContext(ctx, network, address)
	}
	// A custom TLS dialer resolves and connects on its own, so the resolved
	// address never reaches controlFunc. Drop both hooks: net/http then runs the
	// TLS handshake over the guarded DialContext.
	base.DialTLS = nil //nolint:staticcheck // cleared on purpose, see above
	base.DialTLSContext = nil
	return base
}

// defaultTransport clones http.DefaultTransport, or builds the same settings
// when a caller has replaced it with another RoundTripper.
func defaultTransport() *http.Transport {
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		return t.Clone()
	}
	return &http.Transport{
		ForceAttemptHTTP2:     true,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

func controlFunc(policy Policy) func(network, address string, _ syscall.RawConn) error {
	return func(_ string, address string, _ syscall.RawConn) error {
		return policy.BlockedAddress(address)
	}
}
