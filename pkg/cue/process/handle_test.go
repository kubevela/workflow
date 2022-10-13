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

package process

import (
	"testing"

	"cuelang.org/go/cue/cuecontext"
	"github.com/stretchr/testify/require"

	"github.com/kubevela/workflow/pkg/cue/model"
	"github.com/kubevela/workflow/pkg/cue/model/value"
)

func TestContext(t *testing.T) {
	baseTemplate := `
image: "myserver"
`

	r := require.New(t)
	inst := cuecontext.New().CompileString(baseTemplate)
	base, err := model.NewBase(inst)
	r.NoError(err)

	serviceTemplate := `
	apiVersion: "v1"
    kind:       "ConfigMap"
`

	svcInst := cuecontext.New().CompileString(serviceTemplate)

	svcIns, err := model.NewOther(svcInst)
	r.NoError(err)

	svcAux := Auxiliary{
		Ins:  svcIns,
		Name: "service",
	}

	svcAuxWithAbnormalName := Auxiliary{
		Ins:  svcIns,
		Name: "service-1",
	}

	targetParams := map[string]interface{}{
		"parameter1": "string",
		"parameter2": map[string]string{
			"key1": "value1",
			"key2": "value2",
		},
		"parameter3": []string{"item1", "item2"},
	}
	targetArbitraryData := map[string]interface{}{
		"int":    10,
		"string": "mytxt",
		"bool":   false,
		"map": map[string]interface{}{
			"key": "value",
		},
		"slice": []string{
			"str1", "str2", "str3",
		},
	}

	ctx := NewContext(ContextData{
		Name:           "myrun",
		Namespace:      "myns",
		WorkflowName:   "myworkflow",
		PublishVersion: "mypublishversion",
	})
	err = ctx.SetBase(base)
	r.NoError(err)
	err = ctx.AppendAuxiliaries(svcAux)
	r.NoError(err)
	err = ctx.AppendAuxiliaries(svcAuxWithAbnormalName)
	r.NoError(err)
	ctx.SetParameters(targetParams)
	ctx.PushData("arbitraryData", targetArbitraryData)
	get := ctx.GetData("arbitraryData")
	r.Equal(get, targetArbitraryData)

	c, err := ctx.BaseContextFile()
	r.NoError(err)
	ctxInst := cuecontext.New().CompileString(c)

	gName, err := ctxInst.LookupPath(value.FieldPath("context", model.ContextName)).String()
	r.Equal(nil, err)
	r.Equal("myrun", gName)

	myWorkflowName, err := ctxInst.LookupPath(value.FieldPath("context", model.ContextWorkflowName)).String()
	r.Equal(nil, err)
	r.Equal("myworkflow", myWorkflowName)

	myPublishVersion, err := ctxInst.LookupPath(value.FieldPath("context", model.ContextPublishVersion)).String()
	r.Equal(nil, err)
	r.Equal("mypublishversion", myPublishVersion)

	inputJs, err := ctxInst.LookupPath(value.FieldPath("context", model.OutputFieldName)).MarshalJSON()
	r.Equal(nil, err)
	r.Equal(`{"image":"myserver"}`, string(inputJs))

	outputsJs, err := ctxInst.LookupPath(value.FieldPath("context", model.OutputsFieldName, "service")).MarshalJSON()
	r.Equal(nil, err)
	r.Equal("{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\"}", string(outputsJs))

	outputsJs, err = ctxInst.LookupPath(value.FieldPath("context", model.OutputsFieldName, "service-1")).MarshalJSON()
	r.Equal(nil, err)
	r.Equal("{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\"}", string(outputsJs))

	ns, err := ctxInst.LookupPath(value.FieldPath("context", model.ContextNamespace)).String()
	r.Equal(nil, err)
	r.Equal("myns", ns)

	params, err := ctxInst.LookupPath(value.FieldPath("context", model.ParameterFieldName)).MarshalJSON()
	r.Equal(nil, err)
	r.Equal("{\"parameter1\":\"string\",\"parameter2\":{\"key1\":\"value1\",\"key2\":\"value2\"},\"parameter3\":[\"item1\",\"item2\"]}", string(params))

	arbitraryData, err := ctxInst.LookupPath(value.FieldPath("context", "arbitraryData")).MarshalJSON()
	r.Equal(nil, err)
	r.Equal("{\"bool\":false,\"int\":10,\"map\":{\"key\":\"value\"},\"slice\":[\"str1\",\"str2\",\"str3\"],\"string\":\"mytxt\"}", string(arbitraryData))
}
