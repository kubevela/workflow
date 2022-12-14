# Control the delivery process of multiple applications

Apply the following workflow to control the delivery process of multiple applications:

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: apply-applications
  namespace: default
  annotations:
    workflowrun.oam.dev/debug: "true"
spec:
  workflowSpec:
    steps:
      - name: check-app-exist
        type: read-app
        properties:
          name: webservice-app
      - name: apply-app1
        type: apply-app
        if: status["check-app-exist"].message == "Application not found"
        properties:
          data:
            apiVersion: core.oam.dev/v1beta1
            kind: Application
            metadata:
              name: webservice-app
            spec:
              components:
                - name: express-server
                  type: webservice
                  properties:
                    image: crccheck/hello-world
                    ports:
                      - port: 8000
      - name: suspend
        type: suspend
        timeout: 24h
      - name: apply-app2
        type: apply-app
        properties:
          ref:
            name: my-app
            key: application
            type: configMap
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: my-app
  namespace: default
data:
  application: |
    apiVersion: core.oam.dev/v1beta1
    kind: Application
    metadata:
      name: webservice-app2
    spec:
      components:
        - name: express-server2
          type: webservice
          properties:
            image: crccheck/hello-world
            ports:
              - port: 8000
```

Above workflow will first try to read the Application called `webservice-app` from the cluster, if the Application is not found, this step's status message will be `Application not found`. The second step will deploy the `webservice-app` Application if `webservice-app` is not exist in the cluster. After that, the `suspend` step will suspend the delivery process for manual approve.

```
$ kubectl get wr
NAME                 PHASE        AGE
apply-applications   suspending   2s
```

You can use `vela workflow resume` to resume this workflow, note that if the workflow has not been resumed in 24 hours, the workflow will failed of timeout:

```
vela workflow resume apply-applications
```

After the workflow is resumed, it will deploy an another Application from the data in ConfigMap with key `application`.
