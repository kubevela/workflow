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
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/kubevela/pkg/multicluster"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	wfContext "github.com/kubevela/workflow/pkg/context"
	"github.com/kubevela/workflow/pkg/cue/model/value"
	"github.com/kubevela/workflow/pkg/types"
)

// GetDataFromContext get data from workflow context
func GetDataFromContext(ctx context.Context, cli client.Client, ctxName, name, ns string, paths ...string) (*value.Value, error) {
	wfCtx, err := wfContext.LoadContext(cli, ns, name, ctxName)
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

// GetLogConfigFromStep get log config from step
func GetLogConfigFromStep(ctx context.Context, cli client.Client, ctxName, name, ns, step string) (*types.LogConfig, error) {
	wfCtx, err := wfContext.LoadContext(cli, ns, name, ctxName)
	if err != nil {
		return nil, err
	}
	config := make(map[string]types.LogConfig)
	c := wfCtx.GetMutableValue(types.ContextKeyLogConfig)
	if c == "" {
		return nil, fmt.Errorf("no log config found")
	}

	if err := json.Unmarshal([]byte(c), &config); err != nil {
		return nil, err
	}

	stepConfig, ok := config[step]
	if !ok {
		return nil, fmt.Errorf("no log config found for step %s", step)
	}
	return &stepConfig, nil
}

// GetPodListFromResources get pod list from resources
func GetPodListFromResources(ctx context.Context, cli client.Client, resources []types.Resource) ([]corev1.Pod, error) {
	pods := make([]corev1.Pod, 0)
	for _, resource := range resources {
		cliCtx := multicluster.WithCluster(ctx, resource.Cluster)
		if resource.LabelSelector != nil {
			labels := &metav1.LabelSelector{
				MatchLabels: resource.LabelSelector,
			}
			selector, err := metav1.LabelSelectorAsSelector(labels)
			if err != nil {
				return nil, err
			}
			var podList corev1.PodList
			err = cli.List(cliCtx, &podList, &client.ListOptions{
				LabelSelector: selector,
			})
			if err != nil {
				return nil, err
			}
			pods = append(pods, podList.Items...)
			continue
		}
		if resource.Namespace == "" {
			resource.Namespace = metav1.NamespaceDefault
		}
		var pod corev1.Pod
		err := cli.Get(cliCtx, client.ObjectKey{Namespace: resource.Namespace, Name: resource.Name}, &pod)
		if err != nil {
			return nil, err
		}
		pods = append(pods, pod)
	}
	if len(pods) == 0 {
		return nil, fmt.Errorf("no pod found")
	}
	return pods, nil
}

// GetLogsFromURL get logs from url
func GetLogsFromURL(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

// GetLogsFromPod get logs from pod
func GetLogsFromPod(ctx context.Context, clientSet kubernetes.Interface, cli client.Client, podName, ns, cluster string, opts *corev1.PodLogOptions) (io.ReadCloser, error) {
	cliCtx := multicluster.WithCluster(ctx, cluster)
	req := clientSet.CoreV1().Pods(ns).GetLogs(podName, opts)
	readCloser, err := req.Stream(cliCtx)
	if err != nil {
		return nil, err
	}
	return readCloser, nil
}
