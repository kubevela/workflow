/*
 Copyright 2022. The KubeVela Authors.

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

package util

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"

	"cuelang.org/go/cue"
	"k8s.io/klog/v2"

	cuexruntime "github.com/kubevela/pkg/cue/cuex/runtime"
	utilruntime "github.com/kubevela/pkg/util/runtime"

	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	"github.com/kubevela/workflow/pkg/cue/process"
	providertypes "github.com/kubevela/workflow/pkg/providers/types"
	"github.com/kubevela/workflow/pkg/types"
)

// PatchVars is the vars for patch
type PatchVars struct {
	Resource cue.Value `json:"value"`
	Patch    cue.Value `json:"patch"`
}

// PatchK8sObject patch k8s object
func PatchK8sObject(_ context.Context, params *providertypes.Params[cue.Value]) (cue.Value, error) {
	base, err := model.NewBase(params.Params.LookupPath(cue.ParsePath("value")))
	if err != nil {
		return cue.Value{}, err
	}
	if err = base.Unify(params.Params.LookupPath(cue.ParsePath("patch"))); err != nil {
		return params.Params.FillPath(cue.ParsePath("err"), err.Error()), nil
	}

	workload, err := base.Compile()
	if err != nil {
		return params.Params.FillPath(cue.ParsePath("err"), err.Error()), nil
	}
	return params.Params.FillPath(value.FieldPath("$returns", "result"), params.Params.Context().CompileBytes(workload)), nil
}

// StringVars .
type StringVars struct {
	Byte []byte `json:"bt"`
}

// StringReturnVars .
type StringReturnVars struct {
	String string `json:"str"`
}

// StringParams .
type StringParams = providertypes.Params[StringVars]

// StringReturns .
type StringReturns = providertypes.Returns[StringReturnVars]

// String convert byte to string
func String(_ context.Context, params *StringParams) (*StringReturns, error) {
	return &StringReturns{
		Returns: StringReturnVars{
			String: string(params.Params.Byte),
		},
	}, nil
}

// Resource is the log resources
type Resource struct {
	Name          string            `json:"name,omitempty"`
	Namespace     string            `json:"namespace,omitempty"`
	Cluster       string            `json:"cluster,omitempty"`
	LabelSelector map[string]string `json:"labelSelector,omitempty"`
}

// LogSource is the source of the log
type LogSource struct {
	URL       string     `json:"url,omitempty"`
	Resources []Resource `json:"resources,omitempty"`
}

// LogConfig is the config of the log
type LogConfig struct {
	Data   bool       `json:"data,omitempty"`
	Source *LogSource `json:"source,omitempty"`
}

// LogVars is the vars for log
type LogVars struct {
	Data   any        `json:"data,omitempty"`
	Level  int        `json:"level"`
	Source *LogSource `json:"source,omitempty"`
}

// LogParams .
type LogParams = providertypes.Params[LogVars]

// Log print cue value in log
func Log(ctx context.Context, params *LogParams) (*any, error) {
	pCtx := params.ProcessContext
	stepName := fmt.Sprint(pCtx.GetData(model.ContextStepName))
	wfCtx := params.WorkflowContext
	config := make(map[string]LogConfig)
	c := wfCtx.GetMutableValue(types.ContextKeyLogConfig)
	if c != "" {
		if err := json.Unmarshal([]byte(c), &config); err != nil {
			return nil, err
		}
	}

	stepConfig := config[stepName]
	data := params.Params.Data
	if !utilruntime.IsNil(data) {
		stepConfig.Data = true
		if err := printDataInLog(ctx, data, params.Params.Level, pCtx); err != nil {
			return nil, err
		}
	}
	if source := params.Params.Source; source != nil {
		if stepConfig.Source == nil {
			stepConfig.Source = &LogSource{}
		}
		if source.URL != "" {
			stepConfig.Source.URL = source.URL
		}
		if len(source.Resources) != 0 {
			stepConfig.Source.Resources = source.Resources
		}
	}
	config[stepName] = stepConfig
	b, err := json.Marshal(config)
	if err != nil {
		return nil, err
	}
	wfCtx.SetMutableValue(string(b), types.ContextKeyLogConfig)
	return nil, nil
}

func printDataInLog(_ context.Context, data any, level int, pCtx process.Context) error {
	var message string
	switch v := data.(type) {
	case string:
		message = v
	default:
		b, err := json.Marshal(data)
		if err != nil {
			return err
		}
		message = string(b)
	}
	klog.V(klog.Level(level)).InfoS(message,
		model.ContextName, fmt.Sprint(pCtx.GetData(model.ContextName)),
		model.ContextNamespace, fmt.Sprint(pCtx.GetData(model.ContextNamespace)),
		model.ContextStepName, fmt.Sprint(pCtx.GetData(model.ContextStepName)),
		model.ContextStepSessionID, fmt.Sprint(pCtx.GetData(model.ContextStepSessionID)),
	)
	return nil
}

//go:embed util.cue
var template string

// GetTemplate return the template
func GetTemplate() string {
	return template
}

// GetProviders return the provider
func GetProviders() map[string]cuexruntime.ProviderFn {
	return map[string]cuexruntime.ProviderFn{
		"patch-k8s-object": providertypes.NativeProviderFn(PatchK8sObject),
		"string":           providertypes.GenericProviderFn[StringVars, StringReturns](String),
		"log":              providertypes.GenericProviderFn[LogVars, any](Log),
	}
}
