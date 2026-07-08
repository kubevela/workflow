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

// SecureTransport wires SSRF address validation into transport dialing.
func SecureTransport(base *http.Transport, policy Policy) *http.Transport {
	if base == nil {
		base = http.DefaultTransport.(*http.Transport).Clone()
	}
	dialer := &net.Dialer{}
	dialer.Control = func(_, address string, _ syscall.RawConn) error {
		return policy.BlockedAddress(address)
	}
	base.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		if err := policy.BlockedAddress(address); err != nil {
			return nil, err
		}
		return dialer.DialContext(ctx, network, address)
	}
	return base
}
