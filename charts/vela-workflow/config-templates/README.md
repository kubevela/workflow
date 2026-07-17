# Workflow HTTP denylist config

Apply the system config template, then create a denylist ConfigMap in the
workflow controller namespace:

```bash
vela config-template apply \
  -f charts/vela-workflow/config-templates/workflow-http-deny.cue

vela config create workflow-http-deny \
  --template workflow-http-deny \
  --namespace vela-system \
  'denyHosts={metadata.google.internal,*.example.com}' \
  'denyCIDRs={10.0.0.0/8,169.254.169.254}'
```

Configure the controller to load that ConfigMap:

```bash
helm upgrade workflow kubevela/vela-workflow \
  --namespace vela-system \
  --reuse-values \
  --set workflow.httpDeny.configMapName=workflow-http-deny
```

The Helm chart does not install this config template or ConfigMap
automatically. Creating a raw ConfigMap with the `denyHosts` and `denyCIDRs`
data keys remains supported.
