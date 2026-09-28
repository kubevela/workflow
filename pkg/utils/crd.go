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

package utils

import (
	"context"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CRDDeclaresField reports whether every served version of the installed CRD declares
// the field at path in its schema. "[]" in path steps into an array's items.
//
// The API server prunes a field its CRD does not declare, without an error, so a
// controller newer than its CRDs loses fields silently. Read as unstructured so the
// caller's scheme need not know CustomResourceDefinition.
func CRDDeclaresField(ctx context.Context, cli client.Reader, crdName string, path ...string) (bool, error) {
	crd := &unstructured.Unstructured{}
	crd.SetGroupVersionKind(schema.GroupVersionKind{Group: "apiextensions.k8s.io", Version: "v1", Kind: "CustomResourceDefinition"})
	if err := cli.Get(ctx, client.ObjectKey{Name: crdName}, crd); err != nil {
		return false, err
	}
	versions, _, _ := unstructured.NestedSlice(crd.Object, "spec", "versions")
	for _, v := range versions {
		version, ok := v.(map[string]any)
		if !ok || version["served"] != true {
			continue
		}
		node, _, _ := unstructured.NestedMap(version, "schema", "openAPIV3Schema")
		for _, segment := range path {
			if segment == "[]" {
				node, _, _ = unstructured.NestedMap(node, "items")
			} else {
				node, _, _ = unstructured.NestedMap(node, "properties", segment)
			}
			if node == nil {
				return false, nil
			}
		}
	}
	return true, nil
}

// WarnIfCRDLacksForEach logs when the installed CRD predates the forEach step field, so
// that a looped step running once is explained in the controller's log. It never fails
// startup: the check is advice, and a controller may lack permission to read CRDs.
func WarnIfCRDLacksForEach(ctx context.Context, cli client.Reader, crdName string, stepsPath ...string) {
	ok, err := CRDDeclaresField(ctx, cli, crdName, append(append([]string{}, stepsPath...), "[]", "forEach")...)
	if err != nil {
		klog.V(2).InfoS("could not check the installed CRD for forEach", "crd", crdName, "err", err)
		return
	}
	if !ok {
		klog.Warningf("The installed %s CRD does not declare forEach on workflow steps, so the API server drops it "+
			"and a looped step runs once. Apply the CRDs shipped with this version; helm upgrade does not update them.", crdName)
	}
}
