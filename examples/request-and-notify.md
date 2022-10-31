# Use Workflow for request and notify

> Note: You need to install the [notification step definition](https://github.com/kubevela/kubevela/blob/master/vela-templates/definitions/internal/workflowstep/notification.cue) and definitions in the [example directory](https://github.com/kubevela/workflow/tree/main/examples/definitions) first to run the example.

Apply the following workflow for request a specified URL first and then use the response as a message to your slack channel.

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: request-http
  namespace: default
spec:
  workflowSpec:
    steps:
    - name: request
      type: request
      properties:
        url: https://api.github.com/repos/kubevela/workflow
      outputs:
        - name: stars
          valueFrom: |
            import "strconv"
            "Current star count: " + strconv.FormatInt(response["stargazers_count"], 10)
    - name: notification
      type: notification
      inputs:
        - from: stars
          parameterKey: slack.message.text
      properties:
        slack:
          url:
            value: <your slack url>
    - name: failed-notification
      type: notification
      if: status.request.failed
      properties:
        slack:
          url:
            value: <your slack url>
          message:
            text: "Failed to request github"
            
```

Above workflow will send a request to the GitHub API and get the star count of the workflow repository as an output, then use the output as a message to send a notification to your Slack channel.

Apply the WorkflowRun, you can get a message from Slack like:

![slack-success](./static/slack-success.png)

If you change the `url` to an invalid one, you will get a failed notification:

![slack-failed](./static/slack-fail.png)