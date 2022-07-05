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

package providers

import (
	"sync"

	"github.com/kubevela/workflow/pkg/types"
)

type providers struct {
	l sync.RWMutex
	m map[string]map[string]types.Handler
}

// GetHandler get handler by provider name and handle name.
func (p *providers) GetHandler(providerName, handleName string) (types.Handler, bool) {
	p.l.RLock()
	defer p.l.RUnlock()
	provider, ok := p.m[providerName]
	if !ok {
		return nil, false
	}
	h, ok := provider[handleName]
	return h, ok
}

// Register install provider.
func (p *providers) Register(provider string, m map[string]types.Handler) {
	p.l.Lock()
	defer p.l.Unlock()
	p.m[provider] = m
}

// NewProviders will create provider discover.
func NewProviders() types.Providers {
	return &providers{m: map[string]map[string]types.Handler{}}
}
