# Automatically initialize the environment with terraform

You can use Workflow together with Terraform to initialize your environment automatically.

> Note: please make sure that you have enabled the KubeVela Terraform Addon first: 
> ```bash
> vela addon enable terraform
> # supported: terraform-alibaba/terraform-aws/terraform-azure/terraform-baidu/terraform-ec/terraform-gcp/terraform-tencent/terraform-ucloud
> vela addon enable terraform-<cloud name>
> ```

For example, use the cloud provider to create a cluster first, then add this cluster to the management of kubevela workflow. After that, deploy a configmap in the newly created cluster to initialize the environment. Let's take Alibaba Cloud Kubernetes cluster as an example:

Apply the following YAML:

```yaml
apiVersion: core.oam.dev/v1alpha1
kind: WorkflowRun
metadata:
  name: apply-terraform-resource
  namespace: default
spec:
  workflowSpec:
    steps:
    # initialize the terraform provider with credential first
    - name: provider
      type: apply-terraform-provider
      properties:
        type: alibaba
        name: my-alibaba-provider
        accessKey: <accessKey>
        secretKey: <secretKey>
        region: cn-hangzhou
    # create a ACK cluster with terraform
    - name: configuration
      type: apply-terraform-config
      properties:
        source:
          # you can choose to use remote tf or specify the hcl directly
          # hcl: <your hcl>
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
    # add the newly created cluster to the management of kubevela workflow with vela cli
    - name: add-cluster
      type: vela-cli
      properties:
        storage:
          secret:
            - name: secret-mount
              secretName: my-terraform-secret
              mountPath: /kubeconfig/ack
        command:
          - vela
          - cluster
          - join
          - /kubeconfig/ack/KUBECONFIG
          - --name=ack
    # clean the execution job
    - name: clean-cli-jobs
      type: clean-jobs
      if: always
      properties:
        labelSelector:
          "workflow.oam.dev/step-name": apply-terraform-resource-add-cluster
    # apply the configmap in the created cluster
    - name: distribute-config
      type: apply-object
      properties:
        cluster: ack
        value:
          apiVersion: v1
          kind: ConfigMap
          metadata:
            name: my-cm
            namespace: default
          data:
            test-key: test-value
```

In the workflow, the first step will create a terraform provider with your credentials, after that, it will create the terraform resource. Then, it will throw a job to execute vela command -- add the cluster to the management of vela, and clean the job after it is finished. Finally, it will create a config map in the created cluster.
