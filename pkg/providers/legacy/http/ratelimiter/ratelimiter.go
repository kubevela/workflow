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

package ratelimiter

import (
	"time"

	"github.com/golang/groupcache/lru"
	"golang.org/x/time/rate"
)

// RateLimiter is the rate limiter.
type RateLimiter struct {
	store *lru.Cache
}

// NewRateLimiter returns a new rate limiter.
func NewRateLimiter(length int) *RateLimiter {
	store := lru.New(length)
	store.Clear()
	return &RateLimiter{store: store}
}

// Allow returns true if the operation is allowed.
func (rl *RateLimiter) Allow(id string, limit int, duration time.Duration) bool {
	if l, ok := rl.store.Get(id); ok {
		limiter := l.(*rate.Limiter)
		if limiter.Limit() == rate.Every(duration) && limiter.Burst() == limit {
			return limiter.Allow()
		}
	}
	limiter := rate.NewLimiter(rate.Every(duration), limit)
	rl.store.Add(id, limiter)
	return limiter.Allow()
}
