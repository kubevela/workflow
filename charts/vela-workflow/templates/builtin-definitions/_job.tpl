{{/*
Returns builtinDefinitions.job as YAML, filled with defaults for upgrades that
use --reuse-values from a release that predates these values.
*/}}
{{- define "kubevela.builtinDefinitionsJobValues" -}}
{{- $defaults := dict "image" (dict "repository" "alpine/k8s" "tag" "1.31.13" "pullPolicy" "IfNotPresent") "rolloutTimeoutSeconds" 300 "applyRetries" 6 -}}
{{- $set := index (.Values.builtinDefinitions | default dict) "job" | default dict -}}
{{- toYaml (mergeOverwrite $defaults (deepCopy $set)) -}}
{{- end -}}

{{/*
Renders a hook Job that runs files/builtin-definitions/<action>.sh.
Takes a dict of "root", "action", "hook", and "weight".
*/}}
{{- define "kubevela.builtinDefinitionsJob" -}}
{{- $root := .root -}}
{{- $job := include "kubevela.builtinDefinitionsJobValues" $root | fromYaml -}}
{{- $index := include "kubevela.builtinDefinitions" $root | fromYaml -}}
{{- $refs := list -}}
{{- range $kind := keys $index | sortAlpha -}}
{{-   $entry := get $index $kind -}}
{{-   range $name := $entry.names -}}
{{-     $refs = append $refs (printf "%s/%s" $entry.resource $name) -}}
{{-   end -}}
{{- end -}}
{{- $suffix := printf "-builtin-definitions-%s" .action -}}
{{- $jobName := printf "%s%s" (include "kubevela.fullname" $root | trunc (sub 63 (len $suffix) | int) | trimSuffix "-") $suffix -}}
{{- $appLabel := printf "%s%s" (include "kubevela.name" $root | trunc (sub 63 (len $suffix) | int) | trimSuffix "-") $suffix -}}
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ $jobName }}
  namespace: {{ $root.Release.Namespace }}
  annotations:
    "helm.sh/hook": {{ .hook }}
    "helm.sh/hook-weight": {{ .weight | quote }}
    "helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded
  labels:
    app: {{ $appLabel }}
    {{- include "kubevela.labels" $root | nindent 4 }}
spec:
  backoffLimit: 1
  template:
    metadata:
      # Selector labels would make this Job pod a webhook Service endpoint.
      labels:
        app: {{ $appLabel }}
    spec:
      {{- with $root.Values.imagePullSecrets }}
      imagePullSecrets:
      {{- toYaml . | nindent 8 }}
      {{- end }}
      restartPolicy: Never
      serviceAccountName: {{ include "kubevela.serviceAccountName" $root }}
      containers:
        - name: {{ .action }}
          image: {{ $root.Values.imageRegistry }}{{ $job.image.repository }}:{{ $job.image.tag }}
          imagePullPolicy: {{ $job.image.pullPolicy }}
          command:
            - /bin/sh
            - -c
            - |
              {{- $root.Files.Get (printf "files/builtin-definitions/%s.sh" .action) | nindent 14 }}
          env:
            - name: HOME
              value: /tmp
            - name: RELEASE_NAME
              value: {{ $root.Release.Name | quote }}
            - name: DEFINITION_NAMESPACE
              value: {{ include "systemDefinitionNamespace" $root | trim | quote }}
            - name: CONTROLLER_NAMESPACE
              value: {{ $root.Release.Namespace | quote }}
            - name: CONTROLLER_DEPLOYMENT
              value: {{ include "kubevela.fullname" $root | quote }}
            - name: ROLLOUT_TIMEOUT_SECONDS
              value: {{ $job.rolloutTimeoutSeconds | quote }}
            - name: APPLY_RETRIES
              value: {{ $job.applyRetries | quote }}
            - name: BUILTIN_DEFINITIONS
              value: {{ join " " $refs | quote }}
            - name: WEBHOOK_CONFIGURATION
              value: {{ ternary (printf "%s-admission" (include "kubevela.fullname" $root)) "" (and $root.Values.admissionWebhooks.enabled $root.Values.admissionWebhooks.certManager.enabled) | quote }}
          volumeMounts:
            - name: definitions
              mountPath: /definitions
              readOnly: true
      volumes:
        # The pre-upgrade Job runs before this release's ConfigMaps exist.
        - name: definitions
          projected:
            sources:
              {{- range $kind := keys $index | sortAlpha }}
              - configMap:
                  name: {{ include "kubevela.builtinDefinitionsConfigMap" (dict "kind" $kind "root" $root) }}
                  optional: true
              {{- end }}
      {{- with $root.Values.nodeSelector }}
      nodeSelector:
      {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with $root.Values.affinity }}
      affinity:
      {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with $root.Values.tolerations }}
      tolerations:
      {{- toYaml . | nindent 8 }}
      {{- end }}
      securityContext:
        runAsGroup: 2000
        runAsNonRoot: true
        runAsUser: 2000
{{- end -}}
