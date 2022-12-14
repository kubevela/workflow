# Run your workflow with template

You can also create a Workflow Template and run it with a WorkflowRun with different context.

Apply the following Workflow Template first:

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: Workflow
metadata:
  name: deploy-template
  namespace: default
steps:
  - name: apply
    type: apply-deployment
    if: context.env == "dev"
    properties:
      image: nginx
  - name: apply-test
    type: apply-deployment
    if: context.env == "test"
    properties:
      image: crccheck/hello-world
```

Apply the following WorkflowRun:

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: deploy-run
  namespace: default
spec:
  context:
    env: dev
  workflowRef: deploy-template
```

If you apply the WorkflowRun with `dev` in `context.env`, then you'll get a deployment with `nginx` image. If you change the `env` to `test`, you'll get a deployment with `crccheck/hello-world` image.