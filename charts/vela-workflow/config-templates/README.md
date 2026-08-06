# Workflow HTTP denylist config

The controller discovers every ConfigMap labeled
`config.oam.dev/type=workflow-http-deny` and unions their entries on top of an
immutable builtin floor (link-local + cloud metadata) compiled into the binary.
The Helm chart installs the ConfigTemplate and a labeled default ConfigMap with
the same entries for Day 1; ConfigMaps can only add denies, not remove the floor.

Create additional denylists with the ConfigTemplate. The controller unions all
matching ConfigMaps in its namespace:

```bash
vela config create team-http-deny \
  --template workflow-http-deny \
  --namespace vela-system \
  'denyHosts={*.example.com}' \
  'denyCIDRs={10.0.0.0/8}'
```

Disable discovery or the shipped default ConfigMap if needed. An empty
`configTemplateName` also skips installing the ConfigTemplate / default
ConfigMap (avoids invalid `config-template-` names):

```bash
--set workflow.httpDeny.configTemplateName=""
--set workflow.httpDeny.defaultConfig.enabled=false
```

Raw ConfigMaps remain supported when they carry:

```yaml
metadata:
  labels:
    config.oam.dev/type: workflow-http-deny
data:
  denyHosts: |
    blocked.example
  denyCIDRs: |
    10.0.0.0/8
```
