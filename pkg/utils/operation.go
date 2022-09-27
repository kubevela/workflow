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

package utils

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/client"

	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model/value"
)

// GetDataFromContext get data from workflow context
func GetDataFromContext(ctx context.Context, cli client.Client, name, ns string, paths ...string) (*value.Value, error) {
	wfCtx, err := wfContext.LoadContext(cli, ns, name)
	if err != nil {
		return nil, err
	}
	v, err := wfCtx.GetVar(paths...)
	if err != nil {
		return nil, err
	}
	if v.Error() != nil {
		return nil, v.Error()
	}
	return v, nil
}
