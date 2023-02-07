<div style="text-align: center">
  <p align="center">
    <img src="https://raw.githubusercontent.com/kubevela/kubevela.io/main/docs/resources/KubeVela-03.png">
    <br><br>
    <i>Make shipping applications more enjoyable.</i>
  </p>
</div>

[![Go Report Card](https://goreportcard.com/badge/github.com/kubevela/workflow)](https://goreportcard.com/report/github.com/kubevela/workflow)
[![codecov](https://codecov.io/gh/kubevela/workflow/branch/main/graph/badge.svg)](https://codecov.io/gh/kubevela/workflow)
[![LICENSE](https://img.shields.io/github/license/kubevela/workflow.svg?style=flat-square)](/LICENSE)
[![Total alerts](https://img.shields.io/lgtm/alerts/g/kubevela/workflow.svg?logo=lgtm&logoWidth=18)](https://lgtm.com/projects/g/kubevela/workflow/alerts/)

# KubeVela Workflow helm chart

## TL;DR

```bash
helm repo add kubevela https://charts.kubevela.net/core
helm repo update
helm install --create-namespace -n vela-system workflow kubevela/vela-workflow --wait
```

## Prerequisites

- Kubernetes >= v1.19 && < v1.22
  
## Parameters

### Core parameters

| Name                                         | Description                                                                                                           | Value   |
| -------------------------------------------- | --------------------------------------------------------------------------------------------------------------------- | ------- |
| `systemDefinitionNamespace`                  | System definition namespace, if unspecified, will use built-in variable `.Release.Namespace`.                         | `nil`   |
| `concurrentReconciles`                       | concurrentReconciles is the concurrent reconcile number of the controller                                             | `4`     |
| `ignoreWorkflowWithoutControllerRequirement` | will determine whether to process the workflowrun without 'workflowrun.oam.dev/controller-version-require' annotation | `false` |


### KubeVela workflow parameters

| Name                                   | Description                                                                                                                                                                            | Value   |
| -------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------- |
| `workflow.enableSuspendOnFailure`      | Enable the capability of suspend an failed workflow automatically                                                                                                                      | `false` |
| `workflow.enablePatchStatusAtOnce`     | Enable the capability of patch status at once                                                                                                                                          | `false` |
| `workflow.enableWatchEventListener`    | Enable the capability of watch event listener for a faster reconcile, note that you need to install [kube-trigger](https://github.com/kubevela/kube-trigger) first to use this feature | `false` |
| `workflow.backoff.maxTime.waitState`   | The max backoff time of workflow in a wait condition                                                                                                                                   | `60`    |
| `workflow.backoff.maxTime.failedState` | The max backoff time of workflow in a failed condition                                                                                                                                 | `300`   |
| `workflow.step.errorRetryTimes`        | The max retry times of a failed workflow step                                                                                                                                          | `10`    |


### KubeVela workflow backup parameters

| Name                           | Description                                    | Value                      |
| ------------------------------ | ---------------------------------------------- | -------------------------- |
| `backup.enabled`               | Enable backup workflow record                  | `false`                    |
| `backup.strategy`              | The backup strategy for workflow record        | `BackupFinishedRecord`     |
| `backup.ignoreStrategy`        | The ignore strategy for backup                 | `IgnoreLatestFailedRecord` |
| `backup.cleanOnBackup`         | Enable auto clean after backup workflow record | `false`                    |
| `backup.groupByLabel`          | The label used to group workflow record        | `""`                       |
| `backup.persistType`           | The persist type for workflow record           | `""`                       |
| `backup.configSecretName`      | The secret name of backup config               | `backup-config`            |
| `backup.configSecretNamespace` | The secret name of backup config namespace     | `vela-system`              |


### KubeVela Workflow controller parameters

| Name                        | Description                          | Value                  |
| --------------------------- | ------------------------------------ | ---------------------- |
| `replicaCount`              | Workflow controller replica count    | `1`                    |
| `imageRegistry`             | Image registry                       | `""`                   |
| `image.repository`          | Image repository                     | `oamdev/vela-workflow` |
| `image.tag`                 | Image tag                            | `latest`               |
| `image.pullPolicy`          | Image pull policy                    | `Always`               |
| `resources.limits.cpu`      | Workflow controller's cpu limit      | `500m`                 |
| `resources.limits.memory`   | Workflow controller's memory limit   | `1Gi`                  |
| `resources.requests.cpu`    | Workflow controller's cpu request    | `50m`                  |
| `resources.requests.memory` | Workflow controller's memory request | `20Mi`                 |
| `webhookService.type`       | KubeVela webhook service type        | `ClusterIP`            |
| `webhookService.port`       | KubeVela webhook service port        | `9443`                 |
| `healthCheck.port`          | KubeVela health check port           | `9440`                 |


### Common parameters

| Name                         | Description                                                                                                                | Value           |
| ---------------------------- | -------------------------------------------------------------------------------------------------------------------------- | --------------- |
| `imagePullSecrets`           | Image pull secrets                                                                                                         | `[]`            |
| `nameOverride`               | Override name                                                                                                              | `""`            |
| `fullnameOverride`           | Fullname override                                                                                                          | `""`            |
| `serviceAccount.create`      | Specifies whether a service account should be created                                                                      | `true`          |
| `serviceAccount.annotations` | Annotations to add to the service account                                                                                  | `{}`            |
| `serviceAccount.name`        | The name of the service account to use. If not set and create is true, a name is generated using the fullname template     | `nil`           |
| `nodeSelector`               | Node selector                                                                                                              | `{}`            |
| `tolerations`                | Tolerations                                                                                                                | `[]`            |
| `affinity`                   | Affinity                                                                                                                   | `{}`            |
| `rbac.create`                | Specifies whether a RBAC role should be created                                                                            | `true`          |
| `logDebug`                   | Enable debug logs for development purpose                                                                                  | `false`         |
| `logFilePath`                | If non-empty, write log files in this path                                                                                 | `""`            |
| `logFileMaxSize`             | Defines the maximum size a log file can grow to. Unit is megabytes. If the value is 0, the maximum file size is unlimited. | `1024`          |
| `kubeClient.qps`             | The qps for reconcile clients, default is 50                                                                               | `500`           |
| `kubeClient.burst`           | The burst for reconcile clients, default is 100                                                                            | `1000`          |
| `kubeClient.userAgent`       | The user agent of the client, default is vela-workflow                                                                     | `vela-workflow` |


## Uninstallation

### Helm CLI

```shell
$ helm uninstall -n vela-system workflow
```
