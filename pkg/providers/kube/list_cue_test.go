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

package kube

import (
	"context"

	"cuelang.org/go/cue"
	"cuelang.org/go/cue/cuecontext"
	"github.com/kubevela/pkg/util/singleton"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// callThroughCUE calls a function the way a template does: unified with
// this package's CUE, then marshalled into the provider function.
func callThroughCUE(fn, call string) cue.Value {
	singleton.KubeClient.Set(k8sClient)
	v := cuecontext.New().CompileString(GetTemplate() + "\ncall: " + call)
	Expect(v.Err()).ToNot(HaveOccurred())
	out, err := GetProviders()[fn].Call(context.Background(), v.LookupPath(cue.ParsePath("call")))
	Expect(err).ToNot(HaveOccurred())
	return out
}

var _ = Describe("#List, called as its schema declares", func() {
	BeforeEach(func() {
		for _, name := range []string{"listed-1", "listed-2"} {
			Expect(client.IgnoreAlreadyExists(k8sClient.Create(context.Background(), &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: "default", Labels: map[string]string{"listed-via": "cue"}},
			}))).To(Succeed())
		}
	})

	It("lists $params.resource, filtered", func() {
		out := callThroughCUE("list", `#List & {$params: {
			resource: {apiVersion: "v1", kind: "ConfigMap"}
			filter: {namespace: "default", matchingLabels: "listed-via": "cue"}
		}}`)
		items, err := out.LookupPath(cue.ParsePath("$returns.values.items")).List()
		Expect(err).ToNot(HaveOccurred())
		n := 0
		for items.Next() {
			n++
		}
		Expect(n).To(Equal(2))
	})

	It("lists without a filter", func() {
		out := callThroughCUE("list", `#List & {$params: resource: {apiVersion: "v1", kind: "ConfigMap"}}`)
		Expect(out.LookupPath(cue.ParsePath("$returns.err")).Exists()).To(BeFalse())
		Expect(out.LookupPath(cue.ParsePath("$returns.values.items")).Exists()).To(BeTrue())
	})
})
