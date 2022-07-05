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

	"cuelang.org/go/cue"
	"github.com/stretchr/testify/require"

	"github.com/kubevela/workflow/pkg/cue/model"
)

func TestContext(t *testing.T) {
	baseTemplate := `
image: "myserver"
`
	re := require.New(t)
	var r cue.Runtime
	inst, err := r.Compile("-", baseTemplate)
	re.NoError(err)
	base, err := model.NewBase(inst.Value())
	re.NoError(err)

	serviceTemplate := `
	apiVersion: "v1"
    kind:       "ConfigMap"
`

	svcInst, err := r.Compile("-", serviceTemplate)
	re.NoError(err)

	svcIns, err := model.NewOther(svcInst.Value())
	re.NoError(err)

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
	re.NoError(err)
	err = ctx.AppendAuxiliaries(svcAux)
	re.NoError(err)
	err = ctx.AppendAuxiliaries(svcAuxWithAbnormalName)
	re.NoError(err)
	ctx.SetParameters(targetParams)
	ctx.PushData("arbitraryData", targetArbitraryData)

	c, err := ctx.ExtendedContextFile()
	re.NoError(err)
	ctxInst, err := r.Compile("-", c)
	re.NoError(err)

	gName, err := ctxInst.Lookup("context", model.ContextName).String()
	re.NoError(err)
	re.Equal("myrun", gName)

	myWorkflowName, err := ctxInst.Lookup("context", model.ContextWorkflowName).String()
	re.NoError(err)
	re.Equal("myworkflow", myWorkflowName)

	myPublishVersion, err := ctxInst.Lookup("context", model.ContextPublishVersion).String()
	re.NoError(err)
	re.Equal("mypublishversion", myPublishVersion)

	inputJs, err := ctxInst.Lookup("context", model.OutputFieldName).MarshalJSON()
	re.NoError(err)
	re.Equal(`{"image":"myserver"}`, string(inputJs))

	outputsJs, err := ctxInst.Lookup("context", model.OutputsFieldName, "service").MarshalJSON()
	re.NoError(err)
	re.Equal("{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\"}", string(outputsJs))

	outputsJs, err = ctxInst.Lookup("context", model.OutputsFieldName, "service-1").MarshalJSON()
	re.NoError(err)
	re.Equal("{\"apiVersion\":\"v1\",\"kind\":\"ConfigMap\"}", string(outputsJs))

	ns, err := ctxInst.Lookup("context", model.ContextNamespace).String()
	re.NoError(err)
	re.Equal("myns", ns)

	params, err := ctxInst.Lookup("context", model.ParameterFieldName).MarshalJSON()
	re.NoError(err)
	re.Equal("{\"parameter1\":\"string\",\"parameter2\":{\"key1\":\"value1\",\"key2\":\"value2\"},\"parameter3\":[\"item1\",\"item2\"]}", string(params))

	arbitraryData, err := ctxInst.Lookup("context", "arbitraryData").MarshalJSON()
	re.NoError(err)
	re.Equal("{\"bool\":false,\"string\":\"mytxt\",\"int\":10,\"map\":{\"key\":\"value\"},\"slice\":[\"str1\",\"str2\",\"str3\"]}", string(arbitraryData))
}
