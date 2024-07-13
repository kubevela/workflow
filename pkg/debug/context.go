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

package debug

import (
	"context"
	"fmt"

	"cuelang.org/go/cue"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kubevela/pkg/cue/util"
	"github.com/kubevela/pkg/util/singleton"
	wfTypes "github.com/kubevela/workflow/pkg/types"
)

// ContextImpl is workflow debug context interface
type ContextImpl interface {
	Set(v cue.Value) error
}

// Context is debug context.
type Context struct {
	instance *wfTypes.WorkflowInstance
	id       string
}

// Set sets debug content into context
func (d *Context) Set(v cue.Value) error {
	data, err := util.ToString(v)
	if err != nil {
		return err
	}
	err = setStore(context.Background(), d.instance, d.id, data)
	if err != nil {
		return err
	}

	return nil
}

func setStore(ctx context.Context, instance *wfTypes.WorkflowInstance, id, data string) error {
	cm := &corev1.ConfigMap{}
	cli := singleton.KubeClient.Get()
	if err := cli.Get(ctx, types.NamespacedName{
		Namespace: instance.Namespace,
		Name:      GenerateContextName(instance.Name, id, string(instance.UID)),
	}, cm); err != nil {
		if errors.IsNotFound(err) {
			cm.Name = GenerateContextName(instance.Name, id, string(instance.UID))
			cm.Namespace = instance.Namespace
			cm.Data = map[string]string{
				"debug": data,
			}
			cm.Labels = map[string]string{}
			cm.SetOwnerReferences(instance.ChildOwnerReferences)
			return cli.Create(ctx, cm)
		}
		return err
	}
	cm.Data = map[string]string{
		"debug": data,
	}

	return cli.Update(ctx, cm)
}

// NewContext new workflow context without initialize data.
func NewContext(instance *wfTypes.WorkflowInstance, id string) ContextImpl {
	return &Context{
		instance: instance,
		id:       id,
	}
}

// GenerateContextName generate context name
func GenerateContextName(name, id, suffix string) string {
	if len(suffix) > 5 {
		suffix = suffix[len(suffix)-5:]
	}
	return fmt.Sprintf("%s-%s-debug-%s", name, id, suffix)
}
