# Kubernetes Job Orchestration

Kubevela provides a simple way to orchestrate Jobs in Kubernetes. With the built-in !(`apply-job`)[../charts/vela-core/templates/definitions/apply-job.yaml] workflow step, you can easily create a workflow that runs a series of Jobs in order.

## Run Jobs in Order

For example, following is a workflow that runs three Jobs in order, the first two is in a group that runs in parallel, and the third one runs after the first two are done.

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: batch-jobs
  namespace: default
spec:
  mode:
    steps: StepByStep
    subSteps: DAG
  workflowSpec:
    steps:
      - name: group
        type: step-group
        subSteps:
          - name: job1
            type: apply-job
            properties:
              value:
                metadata:
                  name: job1
                  namespace: default
                spec:
                  completions: 2
                  parallelism: 1
                  template:
                    spec:
                      containers:
                      - command:
                        - echo
                        - hello world
                        image: bash
                        name: mytask
            - name: job2
              type: apply-job
              properties:
                ...
        - name: job3
          if: steps.job2.status.succeeded > 1
          type: apply-job
          properties:
            ...
```

### Inside `apply-job`

The `apply-job` workflow step is a wrapper of Kubernetes Job. It takes the same parameters as a Kubernetes Job, and creates a Job in the cluster. The workflow step will wait until the Job is done before moving on to the next step. If the Job fails, the workflow will be marked as failed.

The `apply-job` type is written in CUE, and the definition is as follows:

```cue
import (
	"vela/op"
)

"apply-job": {
	type: "workflow-step"
	annotations: {
		"category": "Resource Management"
	}
	labels: {}
	description: "Apply job"
}
template: {
	// apply the job
	apply: op.#Apply & {
		value:   parameter.value
		cluster: parameter.cluster
	}

	// fail the step if the job fails
	if apply.status.failed > 0 {
		fail: op.#Fail & {
			message: "Job failed"
		}
	}

	// wait the job to be ready
	wait: op.#ConditionalWait & {
		continue: apply.status.succeeded == apply.spec.completions
	}

	parameter: {
		// +usage=Specify Kubernetes job object to be applied
		value: {
			apiVersion: "batch/v1"
			kind:       "Job"
			...
		}
		// +usage=The cluster you want to apply the resource to, default is the current control plane cluster
		cluster: *"" | string
	}
}
```

## Customize the Job for Machine Learning Training

If you want to customize the Job for training with some specific machine learning framework, you can customize your own workflow step. For example, if you want to use PyTorch to train a model, you can create a workflow step that creates a PyTorch Job.


```cue
import (
	"vela/op"
)

"apply-pytorch-job": {
	type: "workflow-step"
	annotations: {
		"category": "Resource Management"
	}
	labels: {}
	description: "Apply pytorch job"
}
template: {
  // customize the job with pytorch config
  job: {
    parameter.value
    spec: template: spec: {
      containers: [{
        // pytorch config
        env: [{...}]
      }]
    }
  }
	// apply the job
	apply: op.#Apply & {
		value:   job
		cluster: parameter.cluster
	}

  // create the service for pytorch job
  service: op.#Apply & {...}

	// fail the step if the job fails
	if apply.status.failed > 0 {
		fail: op.#Fail & {
			message: "Job failed"
		}
	}

	// wait the job to be ready
	wait: op.#ConditionalWait & {
		continue: apply.status.succeeded == apply.spec.completions
	}

	parameter: {
		// +usage=Specify Kubernetes job object to be applied
		value: {
			apiVersion: "batch/v1"
			kind:       "Job"
			...
		}
		// +usage=The cluster you want to apply the resource to, default is the current control plane cluster
		cluster: *"" | string
    service: *true | bool
    config: {
      ...
    }
	}
}
```