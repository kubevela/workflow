<h1 align="center">KubeVela Workflow</h1>

[![Go Report Card](https://goreportcard.com/badge/github.com/kubevela/workflow)](https://goreportcard.com/report/github.com/kubevela/workflow)
[![codecov](https://codecov.io/gh/kubevela/workflow/branch/main/graph/badge.svg)](https://codecov.io/gh/kubevela/workflow)
[![LICENSE](https://img.shields.io/github/license/kubevela/workflow.svg?style=flat-square)](/LICENSE)
[![Total alerts](https://img.shields.io/lgtm/alerts/g/kubevela/workflow.svg?logo=lgtm&logoWidth=18)](https://lgtm.com/projects/g/kubevela/workflow/alerts/)

<br/>
<br/>
<h2 align="center">Table of Contents</h2>

* [Why use KubeVela Workflow?](#why-use-kubevela-workflow)
* [Quick Start](#quick-start)
* [Installation](#installation)
* [Features](#features)
* [How to write custom steps?](#how-to-write-custom-steps)
* [How can KubeVela Workflow be used?](#how-can-kubevela-workflow-be-used)
* [Contributing](#contributing)

<br/>
<br/>

<h2 align="center">Why use KubeVela Workflow</h2>

<h1 align="center"><a href="https://kubevela.io/docs/end-user/pipeline/workflowrun"><img src="https://static.kubevela.net/images/1.6/workflow-arch.png" alt="workflow arch" align="center" width="700px" /></a></h1>

🌬️ **Lightweight Workflow Engine**: KubeVela Workflow won't create a pod or job for process control. Instead, everything can be done in steps and there will be no redundant resource consumption.

✨ **Flexible, Extensible and Programmable**: Every step has a type, and all the types are based on the [CUE](https://cuelang.org/) language, which means if you want to customize a new step type, you just need to write CUE codes and no need to compile or build anything, KubeVela Workflow will evaluate these codes.

💪 **Rich built-in capabilities**: You can control the process with conditional judgement, inputs/outputs, timeout, etc. You can also use the built-in step types to do some common tasks, such as `deploy resources`, `suspend`, `notification`, `step-group` and more!

🔐 **Safe execution with schema checksum checking**: Every step will be checked with the schema, which means you can't run a step with a wrong parameter. This will ensure the safety of the workflow execution.

<h2 align="center">Quick Start</h2>

After installation, you can either run a WorkflowRun directly or from a Workflow Template. Every step in the workflow should have a type and some parameters, in which defines how this step works. You can use the [built-in step type definitions](./examples/built-in-workflow-def.md) or [write your own custom step types](#how-to-write-custom-steps).

> Please checkout the [WorkflowRun Specification](https://kubevela.io/docs/next/end-user/pipeline/workflowrun#workflowrun) and [WorkflowRun Status](https://kubevela.io/docs/next/end-user/pipeline/workflowrun#status) for more details.

### Run a WorkflowRun directly

Please refer to the following examples:

- [Control the delivery process of multiple resources(e.g. your Applications)](./examples/multiple-apps.md)
- [Request a specified URL and then use the response as a message to notify](./examples/request-and-notify.md)
- [Automatically initialize the environment with terraform](./examples/initialize-env.md)

### Run a WorkflowRun from a Workflow Template

Please refer to the following examples:

- [Run the Workflow Template with different context to control the process](./examples/run-with-template.md)

<h2 align="center">Installation</h2>

### Install Workflow

#### Helm

```shell
helm repo add kubevela https://kubevela.github.io/charts
helm repo update
helm install --create-namespace -n vela-system vela-workflow kubevela/vela-workflow
```

#### KubeVela Addon

If you have installed KubeVela, you can install Workflow with the KubeVela Addon:

```shell
vela addon enable vela-workflow
```

### Install Vela CLI(Optional)

Please checkout: [Install Vela CLI](https://kubevela.io/docs/installation/kubernetes#install-vela-cli)

> Note that if you installed Workflow using KubeVela Addon, then the definitions in the addon will be installed automatically.

<h2 align="center">Features</h2>

- [Operate WorkflowRun](https://kubevela.io/docs/next/end-user/pipeline/workflowrun#operate-workflowrun)
- [Suspend and Resume](https://kubevela.io/docs/next/end-user/pipeline/workflowrun#suspend-and-resume)
- [Sub Steps](https://kubevela.io/docs/next/end-user/pipeline/workflowrun#sub-steps)
- [Dependency](https://kubevela.io/docs/next/end-user/pipeline/workflowrun#dependency)
- [Data Passing](https://kubevela.io/docs/next/end-user/pipeline/workflowrun#data-passing)
- [Timeout](https://kubevela.io/docs/next/end-user/pipeline/workflowrun#timeout)
- [If Conditions](https://kubevela.io/docs/next/end-user/pipeline/workflowrun#if-conditions)
- [Custom Context Data](https://kubevela.io/docs/next/end-user/pipeline/workflowrun#custom-context-data)
- [Built-in Context Data](https://kubevela.io/docs/next/end-user/pipeline/workflowrun#built-in-context-data)

<h2 align="center">How to write custom steps</h2>

If you're not familiar with CUE, please checkout the [CUE documentation](https://kubevela.io/docs/platform-engineers/cue/basic) first.

You can customize your steps with CUE and some [built-in operations](https://kubevela.io/docs/platform-engineers/workflow/cue-actions). Please checkout the [tutorial](https://kubevela.io/docs/platform-engineers/workflow/workflow) for more details.

> Note that you cannot use the [application operations](https://kubevela.io/docs/next/platform-engineers/workflow/cue-actions#application-operations) since there're no application data like components/traits/policy in the WorkflowRun.

<h2 align="center">How can KubeVela Workflow be used</h2>

During the evolution of the [OAM](https://oam.dev/) and [KubeVela project](https://github.com/kubevela/kubevela), **workflow**, as an important part to control the delivery process, has gradually matured. Therefore, we separated the workflow code from the KubeVela repository to make it standalone. As a general workflow engine, it can be used directly or as an SDK by other projects.

### As a standalone workflow engine

Unlike the workflow in the KubeVela Application, this workflow will only be executed once, and will **not keep reconciliation**, **no garbage collection** when the workflow object deleted or updated. You can use it for **one-time** operations like:

- Glue and orchestrate operations, such as control the deploy process of multiple resources(e.g. your Applications), scale up/down, read-notify processes, or the sequence between http requests.
- Orchestrate delivery process without day-2 management, just deploy. The most common use case is to initialize your infrastructure for some environment.

### As an SDK

You can use KubeVela Workflow as an SDK to integrate it into your project. For example, the KubeVela Project use it to control the process of application delivery.

You just need to initialize a workflow instance and generate all the task runners with the instance, then execute the task runners. Please check out the example in [Workflow](https://github.com/kubevela/workflow/blob/main/controllers/workflowrun_controller.go#L101) or [KubeVela](https://github.com/kubevela/kubevela/blob/master/pkg/controller/core.oam.dev/v1alpha2/application/application_controller.go#L197).

<h2 align="center">Contributing</h2>

Check out [CONTRIBUTING](https://kubevela.io/docs/contributor/overview) to see how to develop with KubeVela Workflow.