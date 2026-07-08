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
)

// SecureTransport returns a copy of base with dial-time SSRF validation applied.
// Every connection, including redirect follows, re-enters the dial hook.
func SecureTransport(base *http.Transport, policy Policy) *http.Transport {
	if base == nil {
		base = http.DefaultTransport.(*http.Transport).Clone()
	} else {
		base = base.Clone()
	}

	securedDial := func(ctx context.Context, network, address string) (net.Conn, error) {
		if err := policy.BlockedAddress(address); err != nil {
			return nil, err
		}
		dialer := &net.Dialer{
			Control: controlFunc(policy),
		}
		return dialer.DialContext(ctx, network, address)
	}

	existingDialTLS := base.DialTLSContext
	base.DialContext = securedDial
	base.DialTLS = nil
	if existingDialTLS != nil {
		base.DialTLSContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			if err := policy.BlockedAddress(addr); err != nil {
				return nil, err
			}
			return existingDialTLS(ctx, network, addr)
		}
	}
	return base
}

func controlFunc(policy Policy) func(network, address string, _ syscall.RawConn) error {
	return func(_ string, address string, _ syscall.RawConn) error {
		return policy.BlockedAddress(address)
	}
}
