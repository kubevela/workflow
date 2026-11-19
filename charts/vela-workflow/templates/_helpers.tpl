{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "kubevela.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "kubevela.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "kubevela.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "kubevela.labels" -}}
helm.sh/chart: {{ include "kubevela.chart" . }}
{{ include "kubevela.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{/*
Selector labels
*/}}
{{- define "kubevela.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kubevela.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Create the name of the service account to use
*/}}
{{- define "kubevela.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
    {{ default (include "kubevela.fullname" .) .Values.serviceAccount.name }}
{{- else -}}
    {{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}

{{/*
systemDefinitionNamespace value defaulter
*/}}
{{- define "systemDefinitionNamespace" -}}
{{- if .Values.systemDefinitionNamespace -}}
    {{ .Values.systemDefinitionNamespace }}
{{- else -}}
    {{ .Release.Namespace }}
{{- end -}}
{{- end -}}

{{/*
Returns "true" when vela-workflow manages its admission webhook serving cert.
*/}}
{{- define "kubevela.webhookRuntimeCerts" -}}
{{- if and .Values.admissionWebhooks.enabled .Values.admissionWebhooks.patch.enabled (not .Values.admissionWebhooks.certManager.enabled) -}}true{{- end -}}
{{- end -}}

{{/*
Returns the webhooks of an existing webhook configuration as JSON, keyed by
webhook name. Takes a dict of "kind" and "name".
*/}}
{{- define "kubevela.existingWebhooks" -}}
{{- $hooks := dict -}}
{{- $existing := lookup "admissionregistration.k8s.io/v1" .kind "" .name -}}
{{- if $existing -}}
{{- range $existing.webhooks -}}{{- $_ := set $hooks .name . -}}{{- end -}}
{{- end -}}
{{- toJson $hooks -}}
{{- end -}}

{{/*
Renders the caBundle for one webhook, preserving a valid live bundle on upgrade.
Runtime cert management leaves it empty until the controller patches it.
*/}}
{{- define "kubevela.webhookCABundle" -}}
{{- $ca := dig .name "clientConfig" "caBundle" "" .hooks -}}
{{- if include "kubevela.webhookRuntimeCerts" .root -}}
{{- if and $ca (ne $ca "Cg==") }}caBundle: {{ $ca }}{{ end -}}
{{- else -}}
caBundle: {{ default "Cg==" $ca }}
{{- end -}}
{{- end -}}

{{/*
Renders the failurePolicy. Runtime management starts new webhooks as Ignore and
the controller applies the requested policy after the server is listening.
*/}}
{{- define "kubevela.webhookFailurePolicy" -}}
{{- $live := dig .name "failurePolicy" "" .hooks -}}
{{- if and (include "kubevela.webhookRuntimeCerts" .root) $live -}}
failurePolicy: {{ $live }}
{{- else if .root.Values.admissionWebhooks.patch.enabled -}}
failurePolicy: Ignore
{{- else -}}
failurePolicy: {{ .policy }}
{{- end -}}
{{- end -}}

{{/*
Returns an index of the built-in definitions by kind.
*/}}
{{- define "kubevela.builtinDefinitions" -}}
{{- $index := dict -}}
{{- range $path, $_ := .Files.Glob "definitions/*.yaml" -}}
{{-   $def := tpl ($.Files.Get $path) $ | fromYaml -}}
{{-   $kind := $def.kind -}}
{{-   $entry := get $index $kind | default (dict "resource" (printf "%ss.core.oam.dev" (lower $kind)) "names" list) -}}
{{-   $_ := set $entry "names" (append $entry.names $def.metadata.name) -}}
{{-   $_ := set $index $kind $entry -}}
{{- end -}}
{{- toYaml $index -}}
{{- end -}}

{{/*
Names the ConfigMap holding built-in definitions of one kind.
*/}}
{{- define "kubevela.builtinDefinitionsConfigMap" -}}
{{- printf "%s-builtin-%ss" (include "kubevela.fullname" .root) (lower .kind) -}}
{{- end -}}