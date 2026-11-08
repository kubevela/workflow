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
	"fmt"
	"io"
	"math"
	"time"
)

// A workflow HTTP request runs inside a reconcile and holds one of the controller's few
// reconcile workers until it returns, so its duration and response size are bounded.
var (
	// DefaultRequestTimeout is the timeout of a request that sets none, capped at
	// MaxRequestTimeout; DefaultTimeout applies the cap.
	DefaultRequestTimeout = 3 * time.Second
	// MaxRequestTimeout is the longest timeout a request may set.
	MaxRequestTimeout = 60 * time.Second
	// MaxResponseBytes is the largest response body a request may read, bounding what the
	// controller holds in memory. A body within it can still be too large for the step's context
	// ConfigMap once its other variables are added, and fails the step when the context is saved.
	MaxResponseBytes int64 = 1 << 20
)

// CheckBounds refuses bounds a request could never meet, as the controller's flags set them.
func CheckBounds() error {
	if MaxRequestTimeout <= 0 {
		return fmt.Errorf("--max-workflow-http-timeout must be greater than 0, got %s", MaxRequestTimeout)
	}
	if MaxResponseBytes <= 0 {
		return fmt.Errorf("--max-workflow-http-response-bytes must be greater than 0, got %d", MaxResponseBytes)
	}
	return nil
}

// DefaultTimeout is the timeout of a request that sets none: DefaultRequestTimeout, or
// MaxRequestTimeout when that is lower.
func DefaultTimeout() time.Duration {
	if MaxRequestTimeout < DefaultRequestTimeout {
		return MaxRequestTimeout
	}
	return DefaultRequestTimeout
}

// RequestTimeout parses a request's timeout. Empty means DefaultTimeout; zero, negative
// and anything over MaxRequestTimeout are refused rather than taken as unbounded or clamped.
func RequestTimeout(s string) (time.Duration, error) {
	if s == "" {
		return DefaultTimeout(), nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("invalid timeout %q: %w", s, err)
	}
	if d <= 0 || d > MaxRequestTimeout {
		return 0, fmt.Errorf("timeout %s must be greater than 0 and at most %s", s, MaxRequestTimeout)
	}
	return d, nil
}

// ReadResponseBody reads a response body of at most MaxResponseBytes, and fails on a longer
// one rather than truncate it.
func ReadResponseBody(r io.Reader) ([]byte, error) {
	if MaxResponseBytes < 0 {
		return nil, fmt.Errorf("the response size limit must not be negative, got %d", MaxResponseBytes)
	}
	// One byte past the limit tells a body of exactly the limit from a longer one.
	limit := MaxResponseBytes
	if limit < math.MaxInt64 {
		limit++
	}
	b, err := io.ReadAll(io.LimitReader(r, limit))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > MaxResponseBytes {
		return nil, fmt.Errorf("response body exceeds %d bytes", MaxResponseBytes)
	}
	return b, nil
}
