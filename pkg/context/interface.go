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

package context

import (
	"context"

	"cuelang.org/go/cue"
	corev1 "k8s.io/api/core/v1"
)

// Context is workflow context interface
type Context interface {
	GetVar(paths ...string) (cue.Value, error)
	SetVar(v cue.Value, paths ...string) error
	GetStore() *corev1.ConfigMap
	GetMutableValue(path ...string) string
	SetMutableValue(data string, path ...string)
	DeleteMutableValue(paths ...string)
	IncreaseCountValueInMemory(paths ...string) int
	SetValueInMemory(data interface{}, paths ...string)
	GetValueInMemory(paths ...string) (interface{}, bool)
	DeleteValueInMemory(paths ...string)
	Commit(ctx context.Context) error
	StoreRef() *corev1.ObjectReference
}
