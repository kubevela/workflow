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
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kubevela/workflow/pkg/types"
)

func TestProvers(t *testing.T) {
	p := NewProviders()
	r := require.New(t)
	p.Register("test", map[string]types.Handler{
		"foo":   nil,
		"crazy": nil,
	})

	_, found := p.GetHandler("test", "foo")
	r.Equal(found, true)
	_, found = p.GetHandler("test", "crazy")
	r.Equal(found, true)
	_, found = p.GetHandler("staging", "crazy")
	r.Equal(found, false)
	_, found = p.GetHandler("test", "fly")
	r.Equal(found, false)
}
