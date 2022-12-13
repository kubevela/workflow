---
title: Built-in WorkflowStep Type
---

This documentation will walk through all the built-in workflow step types sorted alphabetically.

> It was generated automatically by [scripts](../../contributor/cli-ref-doc), please don't update manually, last updated at 2022-12-13T22:17:11+08:00.

## Apply-app

### Description

Apply application from data or ref to the cluster

### Examples (apply-app)

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

### Specification (apply-app)

<missing>

## Addon-Operation

### Description

Enable a KubeVela addon.

### Examples (addon-operation)

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: observability
  namespace: vela-system
spec:
  context:
    readConfig: true
  mode: 
  workflowSpec:
    steps:
      - name: Enable Prism
        type: addon-operation
        properties:
          addonName: vela-prism
      
      - name: Enable o11y
        type: addon-operation
        properties:
          addonName: o11y-definitions
          operation: enable
          args:
          - --override-definitions

      - name: Prepare Prometheus
        type: step-group
        subSteps: 
        - name: get-exist-prometheus
          type: list-config
          properties:
            template: prometheus-server
          outputs:
          - name: prometheus
            valueFrom: "output.configs"

        - name: prometheus-server
          inputs:
          - from: prometheus
            # TODO: Make it is not required
            parameterKey: configs
          if: "!context.readConfig || len(inputs.prometheus) == 0"
          type: addon-operation
          properties:
            addonName: prometheus-server
            operation: enable
            args:
            - memory=4096Mi
            - serviceType=LoadBalancer

      - name: Prepare Loki
        type: addon-operation
        properties:
          addonName: loki
          operation: enable
          args:
            - --version=v0.1.4
            - agent=vector
            - serviceType=LoadBalancer
            
      - name: Prepare Grafana
        type: step-group
        subSteps: 
        
        - name: get-exist-grafana
          type: list-config
          properties:
            template: grafana
          outputs:
          - name: grafana
            valueFrom: "output.configs"
        
        - name: Install Grafana & Init Dashboards
          inputs:
          - from: grafana
            parameterKey: configs
          if: "!context.readConfig || len(inputs.grafana) == 0"
          type: addon-operation
          properties:
            addonName: grafana
            operation: enable
            args:
              - serviceType=LoadBalancer
        
        - name: Init Dashboards
          inputs:
          - from: grafana
            parameterKey: configs
          if: "len(inputs.grafana) != 0"
          type: addon-operation
          properties:
            addonName: grafana
            operation: enable
            args:
              - install=false

      - name: Clean
        type: clean-jobs
  
      - name: print-message
        type: print-message-in-status
        properties:
          message: "All addons have been enabled successfully, you can use 'vela addon list' to check them."
```

### Specification (addon-operation)


 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 addonName | Specify the name of the addon. | string | true |  
 args | Specify addon enable args. | []string | false |  
 image | Specify the image. | string | false | oamdev/vela-cli:v1.6.4 
 operation | operation for the addon. | "enable" or "upgrade" or "disable" | false | enable 
 serviceAccountName | specify serviceAccountName want to use. | string | false | kubevela-vela-core 


## Apply-Deployment

### Description

Apply deployment with specified image and cmd.

### Examples (apply-deployment)

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: apply-deployment
  namespace: default
spec:
  workflowSpec:
    steps:
    - name: apply
      type: apply-deployment
      properties:
        image: nginx
```

### Specification (apply-deployment)


 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 image |  | string | true |  
 cmd |  | []string | false |  


## Apply-Object

### Description

Apply raw kubernetes objects for your workflow steps.

### Examples (apply-object)

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: apply-pvc
  namespace: default
spec:
  workflowSpec:
    steps:
      - name: apply-pvc
        type: apply-object
        properties:
          # Kubernetes native resources fields
          value:
            apiVersion: v1
            kind: PersistentVolumeClaim
            metadata:
              name: myclaim
              namespace: default
            spec:
              accessModes:
              - ReadWriteOnce
              resources:
                requests:
                  storage: 8Gi
              storageClassName: standard
          # the cluster you want to apply the resource to, default is the current cluster
          cluster: <your cluster name>
```

### Specification (apply-object)


 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 value | Specify Kubernetes native resource object to be applied. | map[string]_ | true |  
 cluster | The cluster you want to apply the resource to, default is the current control plane cluster. | string | false | empty 


## Apply-Terraform-Config

### Description

Apply terraform configuration in the step.

### Examples (apply-terraform-config)

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: apply-terraform-resource
  namespace: default
spec:
  workflowSpec:
    steps:
    - name: provider
      type: apply-terraform-provider
      properties:
        type: alibaba
        name: my-alibaba-provider
        accessKey: <accessKey>
        secretKey: <secretKey>
        region: cn-hangzhou
    - name: configuration
      type: apply-terraform-config
      properties:
        source:
          path: alibaba/cs/dedicated-kubernetes
          remote: https://github.com/FogDong/terraform-modules
        providerRef:
          name: my-alibaba-provider
        writeConnectionSecretToRef:
            name: my-terraform-secret
            namespace: vela-system
        variable:
          name: regular-check-ack
          new_nat_gateway: true
          vpc_name: "tf-k8s-vpc-regular-check"
          vpc_cidr: "10.0.0.0/8"
          vswitch_name_prefix: "tf-k8s-vsw-regualr-check"
          vswitch_cidrs: [ "10.1.0.0/16", "10.2.0.0/16", "10.3.0.0/16" ]
          k8s_name_prefix: "tf-k8s-regular-check"
          k8s_version: 1.24.6-aliyun.1
          k8s_pod_cidr: "192.168.5.0/24"
          k8s_service_cidr: "192.168.2.0/24"
          k8s_worker_number: 2
          cpu_core_count: 4
          memory_size: 8
          tags:
            created_by: "Terraform-of-KubeVela"
            created_from: "module-tf-alicloud-ecs-instance"
```

### Specification (apply-terraform-config)


 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 source | specify the source of the terraform configuration. | [type-option-1](#type-option-1-apply-terraform-config) or [type-option-2](#type-option-2-apply-terraform-config) | true |  
 deleteResource | whether to delete resource. | bool | false | true 
 variable | the variable in the configuration. | map[string]_ | true |  
 writeConnectionSecretToRef | this specifies the namespace and name of a secret to which any connection details for this managed resource should be written. | [writeConnectionSecretToRef](#writeconnectionsecrettoref-apply-terraform-config) | false |  
 providerRef | providerRef specifies the reference to Provider. | [providerRef](#providerref-apply-terraform-config) | false |  
 region | region is cloud provider's region. It will override the region in the region field of providerRef. | string | false |  
 jobEnv | the envs for job. | map[string]_ | false |  
 forceDelete |  | bool | false | false 


#### type-option-1 (apply-terraform-config)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 hcl | directly specify the hcl of the terraform configuration. | string | true |  


#### type-option-2 (apply-terraform-config)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 remote | specify the remote url of the terraform configuration. | string | false | https://github.com/kubevela-contrib/terraform-modules.git 
 path | specify the path of the terraform configuration. | string | false |  


#### writeConnectionSecretToRef (apply-terraform-config)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 name |  | string | true |  
 namespace |  | _&#124;_ | true |  


#### providerRef (apply-terraform-config)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 name |  | string | true |  
 namespace |  | _&#124;_ | true |  


## Apply-Terraform-Provider

### Description

Apply terraform provider config.

### Examples (apply-terraform-provider)

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: apply-terraform-provider
  namespace: default
spec:
  workflowSpec:
    steps:
    - name: provider
      type: apply-terraform-provider
      properties:
        type: alibaba
        name: my-alibaba-provider
        accessKey: <accessKey>
        secretKey: <secretKey>
        region: cn-hangzhou
```

### Specification (apply-terraform-provider)


 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
  |  | [AlibabaProvider](#alibabaprovider-apply-terraform-provider) or [AWSProvider](#awsprovider-apply-terraform-provider) or [AzureProvider](#azureprovider-apply-terraform-provider) or [BaiduProvider](#baiduprovider-apply-terraform-provider) or [ECProvider](#ecprovider-apply-terraform-provider) or [GCPProvider](#gcpprovider-apply-terraform-provider) or [TencentProvider](#tencentprovider-apply-terraform-provider) or [UCloudProvider](#ucloudprovider-apply-terraform-provider) | false |  


#### AlibabaProvider (apply-terraform-provider)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 type |  | string | true |  
 accessKey |  | string | true |  
 secretKey |  | string | true |  
 name |  | string | false | alibaba-provider 
 region |  | string | true |  


#### AWSProvider (apply-terraform-provider)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 token |  | string | false | empty 
 type |  | string | true |  
 accessKey |  | string | true |  
 secretKey |  | string | true |  
 name |  | string | false | aws-provider 
 region |  | string | true |  


#### AzureProvider (apply-terraform-provider)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 subscriptionID |  | string | true |  
 tenantID |  | string | true |  
 clientID |  | string | true |  
 clientSecret |  | string | true |  
 name |  | string | false | azure-provider 


#### BaiduProvider (apply-terraform-provider)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 type |  | string | true |  
 accessKey |  | string | true |  
 secretKey |  | string | true |  
 name |  | string | false | baidu-provider 
 region |  | string | true |  


#### ECProvider (apply-terraform-provider)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 type |  | string | true |  
 apiKey |  | string | false | empty 
 name |  | string | true |  


#### GCPProvider (apply-terraform-provider)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 credentials |  | string | true |  
 region |  | string | true |  
 project |  | string | true |  
 type |  | string | true |  
 name |  | string | false | gcp-provider 


#### TencentProvider (apply-terraform-provider)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 secretID |  | string | true |  
 secretKey |  | string | true |  
 region |  | string | true |  
 type |  | string | true |  
 name |  | string | false | tencent-provider 


#### UCloudProvider (apply-terraform-provider)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 publicKey |  | string | true |  
 privateKey |  | string | true |  
 projectID |  | string | true |  
 region |  | string | true |  
 type |  | string | true |  
 name |  | string | false | ucloud-provider 


## Clean-Jobs

### Description

clean workflow run applied jobs.

### Specification (clean-jobs)


 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 labelselector |  | map[string]_ | false |  


## Export2config

### Description

Export data to specified Kubernetes ConfigMap in your workflow.

### Examples (export2config)

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: export2config
  namespace: default
spec:
  workflowSpec:
    steps:
      - name: export-config
        type: export2config
        properties:
          configName: my-configmap
          data:
            testkey: |
              testvalue
              value-line-2
```

### Specification (export2config)


 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 configName | Specify the name of the config map. | string | true |  
 namespace | Specify the namespace of the config map. | string | false |  
 data | Specify the data of config map. | struct | true |  
 cluster | Specify the cluster of the config map. | string | false | empty 


## Export2secret

### Description

Export data to Kubernetes Secret in your workflow.

### Examples (export2secret)

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: export-secret
  namespace: default
spec:
  workflowSpec:
    steps:
      - name: export-secret
        type: export2secret
        properties:
          secretName: my-secret
          data:
            testkey: |
              testvalue
              value-line-2
```

### Specification (export2secret)


 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 secretName | Specify the name of the secret. | string | true |  
 namespace | Specify the namespace of the secret. | string | false |  
 type | Specify the type of the secret. | string | false |  
 data | Specify the data of secret. | struct | true |  
 cluster | Specify the cluster of the secret. | string | false | empty 


## Notification

### Description

Send notifications to Email, DingTalk, Slack, Lark or webhook in your workflow.

### Examples (notification)

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: noti-workflow
  namespace: default
spec:
  workflowSpec:
    steps:
      - name: dingtalk-message
        type: notification
        properties:
          dingding:
            # the DingTalk webhook address, please refer to: https://developers.dingtalk.com/document/robots/custom-robot-access
            url: 
              value: <url>
            message:
              msgtype: text
              text:
                context: Workflow starting...
      - name: slack-message
        type: notification
        properties:
          slack:
            # the Slack webhook address, please refer to: https://api.slack.com/messaging/webhooks
            url:
              secretRef:
                name: <secret-key>
                key: <secret-value>
            message:
              text: Workflow ended.
          lark:
            url:
              value: <lark-url>
            message:
              msg_type: "text"
              content: "{\"text\":\" Hello KubeVela\"}"
          email:
            from:
              address: <sender-email-address>
              alias: <sender-alias>
              password:
                # secretRef:
                #   name: <secret-name>
                #   key: <secret-key>
                value: <sender-password>
              host: <email host like smtp.gmail.com>
              port: <email port, optional, default to 587>
            to:
              - kubevela1@gmail.com
              - kubevela2@gmail.com
            content:
              subject: test-subject
              body: test-body
```

### Specification (notification)


 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 lark | Please fulfill its url and message if you want to send Lark messages. | [lark](#lark-notification) | false |  
 dingding | Please fulfill its url and message if you want to send DingTalk messages. | [dingding](#dingding-notification) | false |  
 slack | Please fulfill its url and message if you want to send Slack messages. | [slack](#slack-notification) | false |  
 email | Please fulfill its from, to and content if you want to send email. | [email](#email-notification) | false |  


#### lark (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 url | Specify the the lark url, you can either sepcify it in value or use secretRef. | [type-option-1](#type-option-1-notification) or [type-option-2](#type-option-2-notification) | true |  
 message | Specify the message that you want to sent, refer to [Lark messaging](https://open.feishu.cn/document/ukTMukTMukTM/ucTM5YjL3ETO24yNxkjN#8b0f2a1b). | [message](#message-notification) | true |  


##### type-option-1 (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 value | the url address content in string. | string | true |  


##### type-option-2 (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 secretRef |  | [secretRef](#secretref-notification) | true |  


##### secretRef (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 name | name is the name of the secret. | string | true |  
 key | key is the key in the secret. | string | true |  


##### message (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 msg_type | msg_type can be text, post, image, interactive, share_chat, share_user, audio, media, file, sticker. | string | true |  
 content | content should be json encode string. | string | true |  


#### dingding (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 url | Specify the the dingding url, you can either sepcify it in value or use secretRef. | [type-option-1](#type-option-1-notification) or [type-option-2](#type-option-2-notification) | true |  
 message | Specify the message that you want to sent, refer to [dingtalk messaging](https://developers.dingtalk.com/document/robots/custom-robot-access/title-72m-8ag-pqw). | [message](#message-notification) | true |  


##### type-option-1 (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 value | the url address content in string. | string | true |  


##### type-option-2 (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 secretRef |  | [secretRef](#secretref-notification) | true |  


##### secretRef (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 name | name is the name of the secret. | string | true |  
 key | key is the key in the secret. | string | true |  


##### message (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 text | Specify the message content of dingtalk notification. | null | false |  
 msgtype | msgType can be text, link, mardown, actionCard, feedCard. | "text" or "link" or "markdown" or "actionCard" or "feedCard" | false | text 
 link |  | null | false |  
 markdown |  | null | false |  
 at |  | null | false |  
 actionCard |  | null | false |  
 feedCard |  | null | false |  


#### slack (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 url | Specify the the slack url, you can either sepcify it in value or use secretRef. | [type-option-1](#type-option-1-notification) or [type-option-2](#type-option-2-notification) | true |  
 message | Specify the message that you want to sent, refer to [slack messaging](https://api.slack.com/reference/messaging/payload). | [message](#message-notification) | true |  


##### type-option-1 (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 value | the url address content in string. | string | true |  


##### type-option-2 (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 secretRef |  | [secretRef](#secretref-notification) | true |  


##### secretRef (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 name | name is the name of the secret. | string | true |  
 key | key is the key in the secret. | string | true |  


##### message (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 text | Specify the message text for slack notification. | string | true |  
 blocks |  | null | false |  
 attachments |  | null | false |  
 thread_ts |  | string | false |  
 mrkdwn | Specify the message text format in markdown for slack notification. | bool | false | true 


#### email (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 from | Specify the email info that you want to send from. | [from](#from-notification) | true |  
 to | Specify the email address that you want to send to. | []string | true |  
 content | Specify the content of the email. | [content](#content-notification) | true |  


##### from (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 address | Specify the email address that you want to send from. | string | true |  
 alias | The alias is the email alias to show after sending the email. | string | false |  
 password | Specify the password of the email, you can either sepcify it in value or use secretRef. | [type-option-1](#type-option-1-notification) or [type-option-2](#type-option-2-notification) | true |  
 host | Specify the host of your email. | string | true |  
 port | Specify the port of the email host, default to 587. | int | false | 587 


##### type-option-1 (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 value | the password content in string. | string | true |  


##### type-option-2 (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 secretRef |  | [secretRef](#secretref-notification) | true |  


##### secretRef (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 name | name is the name of the secret. | string | true |  
 key | key is the key in the secret. | string | true |  


##### content (notification)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 subject | Specify the subject of the email. | string | true |  
 body | Specify the context body of the email. | string | true |  


## Print-Message-In-Status

### Description

print message in workflow step status.

### Examples (print-message-in-status)

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: print-message-in-status
  namespace: default
spec:
  workflowSpec:
    steps:
      - name: message
        type: print-message-in-status
        properties:
          message: "hello message"
```

### Specification (print-message-in-status)


 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 message |  | string | true |  


## Read-App

### Description

Read application from the cluster.

### Specification (read-app)


 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 name |  | string | true |  
 namespace |  | _&#124;_ | true |  


## Read-Object

### Description

Read Kubernetes objects from cluster for your workflow steps.

### Examples (read-object)

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: read-object
  namespace: default
spec:
  workflowSpec:
    steps:
    - name: read-object
      type: read-object
      outputs:
        - name: cpu
          valueFrom: output.value.data["cpu"]
        - name: memory
          valueFrom: output.value.data["memory"]
      properties:
        apiVersion: v1
        kind: ConfigMap
        name: my-cm-name
        cluster: <your cluster name
```

### Specification (read-object)


 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 apiVersion | Specify the apiVersion of the object, defaults to 'core.oam.dev/v1beta1'. | string | false |  
 kind | Specify the kind of the object, defaults to Application. | string | false |  
 name | Specify the name of the object. | string | true |  
 namespace | The namespace of the resource you want to read. | string | false | default 
 cluster | The cluster you want to apply the resource to, default is the current control plane cluster. | string | false | empty 


## Request

### Description

Send request to the url.

### Examples (request)

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
        url: https://api.github.com/repos/kubevela/notfound
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

### Specification (request)


 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 url |  | string | true |  
 method |  | "GET" or "POST" or "PUT" or "DELETE" | false | GET 
 body |  | map[string]_ | false |  
 header |  | map[string]string | false |  


## Step-Group

### Description

A special step that you can declare 'subSteps' in it, 'subSteps' is an array containing any step type whose valid parameters do not include the `step-group` step type itself. The sub steps were executed in parallel.

### Examples (step-group)

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: example-group
  namespace: default
spec:
  workflowSpec:
    steps:
      - name: step
        type: step-group
        subSteps:
          - name: apply-sub-step1
            type: apply-deployment
            properties:
              image: nginx
          - name: apply-sub-step2
            type: apply-deployment
            properties:
              image: nginx
```

### Specification (step-group)
This capability has no arguments.

## Suspend

### Description

Suspend the current workflow, it can be resumed by 'vela workflow resume' command.

### Examples (suspend)

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: suspend-example
  namespace: default
spec:
  workflowSpec:
    steps:
      - name: slack-message
        type: notification
        properties:
          slack:
            url:
              value: <your-slack-url>
            # the Slack webhook address, please refer to: https://api.slack.com/messaging/webhooks
            message:
              text: Ready to apply the application, ask the administrator to approve and resume the workflow.
      - name: manual-approval
        type: suspend
        # properties:
        #   duration: "30s"
      - name: nginx-server
        type: apply-deployment
        properties:
          image: nginx
```

### Specification (suspend)


 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 duration | Specify the wait duration time to resume workflow such as "30s", "1min" or "2m15s". | string | false |  


## Vela-Cli

### Description

Run a vela command.

### Specification (vela-cli)


 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 addonName | Specify the name of the addon. | string | true |  
 command | Specify the vela command. | []string | true |  
 image | Specify the image. | string | false | oamdev/vela-cli:v1.6.4 
 serviceAccountName | specify serviceAccountName want to use. | string | false | kubevela-vela-core 
 storage |  | [storage](#storage-vela-cli) | false |  


#### storage (vela-cli)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 secret | Mount Secret type storage. | [[]secret](#secret-vela-cli) | false |  


##### secret (vela-cli)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 name |  | string | true |  
 mountPath |  | string | true |  
 subPath |  | string | false |  
 defaultMode |  | int | false | 420 
 secretName |  | string | true |  
 items |  | [[]items](#items-vela-cli) | false |  


##### items (vela-cli)

 Name | Description | Type | Required | Default 
 ---- | ----------- | ---- | -------- | ------- 
 key |  | string | true |  
 path |  | string | true |  
 mode |  | int | false | 511 


