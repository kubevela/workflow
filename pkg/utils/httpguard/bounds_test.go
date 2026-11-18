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
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRequestTimeout(t *testing.T) {
	r := require.New(t)
	got, err := RequestTimeout("")
	r.NoError(err)
	r.Equal(DefaultRequestTimeout, got)

	got, err = RequestTimeout("30s")
	r.NoError(err)
	r.Equal(30*time.Second, got)

	got, err = RequestTimeout(MaxRequestTimeout.String())
	r.NoError(err)
	r.Equal(MaxRequestTimeout, got)

	for _, refused := range []string{"0", "0s", "-1s", (MaxRequestTimeout + time.Second).String()} {
		_, err := RequestTimeout(refused)
		r.Error(err, refused)
		r.Contains(err.Error(), refused)
		r.Contains(err.Error(), MaxRequestTimeout.String())
	}

	_, err = RequestTimeout("soon")
	r.ErrorContains(err, `"soon"`)
}

func TestReadResponseBody(t *testing.T) {
	r := require.New(t)
	limit := MaxResponseBytes
	t.Cleanup(func() { MaxResponseBytes = limit })
	MaxResponseBytes = 8

	b, err := ReadResponseBody(strings.NewReader("12345678"))
	r.NoError(err)
	r.Equal("12345678", string(b))

	_, err = ReadResponseBody(strings.NewReader("123456789"))
	r.ErrorContains(err, "exceeds 8 bytes")
}

// A maximum below the default bounds requests that set no timeout too.
func TestDefaultTimeoutStaysWithinTheMaximum(t *testing.T) {
	max := MaxRequestTimeout
	t.Cleanup(func() { MaxRequestTimeout = max })

	require.Equal(t, DefaultRequestTimeout, DefaultTimeout())
	MaxRequestTimeout = time.Second
	require.Equal(t, time.Second, DefaultTimeout())
	got, err := RequestTimeout("")
	require.NoError(t, err)
	require.Equal(t, time.Second, got)
}

func TestReadResponseBodyAtTheLimits(t *testing.T) {
	limit := MaxResponseBytes
	t.Cleanup(func() { MaxResponseBytes = limit })

	MaxResponseBytes = math.MaxInt64
	b, err := ReadResponseBody(strings.NewReader("hello"))
	require.NoError(t, err)
	require.Equal(t, "hello", string(b), "the largest limit reads the body")

	MaxResponseBytes = -1
	_, err = ReadResponseBody(strings.NewReader("hello"))
	require.Error(t, err)
}

func TestCheckBounds(t *testing.T) {
	timeout, limit := MaxRequestTimeout, MaxResponseBytes
	t.Cleanup(func() { MaxRequestTimeout, MaxResponseBytes = timeout, limit })

	require.NoError(t, CheckBounds())
	MaxRequestTimeout = 0
	require.ErrorContains(t, CheckBounds(), "max-workflow-http-timeout")
	MaxRequestTimeout, MaxResponseBytes = timeout, 0
	require.ErrorContains(t, CheckBounds(), "max-workflow-http-response-bytes")
}
