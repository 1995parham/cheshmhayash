{{/*
Expand the name of the chart.
*/}}
{{- define "natsie-chart.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Fully qualified app name. Truncated at 63 chars (DNS-1123 label).
*/}}
{{- define "natsie-chart.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Chart name + version, used by the helm.sh/chart label.
*/}}
{{- define "natsie-chart.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Recommended app.kubernetes.io/* labels. Apply to every rendered object.
*/}}
{{- define "natsie-chart.labels" -}}
helm.sh/chart: {{ include "natsie-chart.chart" . }}
{{ include "natsie-chart.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/component: bot
app.kubernetes.io/part-of: nats
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Immutable selector subset. Only name+instance — never include `version`
because Deployment selectors are immutable after creation.
*/}}
{{- define "natsie-chart.selectorLabels" -}}
app.kubernetes.io/name: {{ include "natsie-chart.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
ServiceAccount name in use.
*/}}
{{- define "natsie-chart.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "natsie-chart.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Container image reference (`repo:tag`). Tag falls back to .Chart.AppVersion.
*/}}
{{- define "natsie-chart.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end }}

{{/*
Names of chart-managed child objects.
*/}}
{{- define "natsie-chart.configMapName" -}}
{{- printf "%s-config" (include "natsie-chart.fullname" .) -}}
{{- end }}
{{- define "natsie-chart.secretName" -}}
{{- printf "%s-signing-key" (include "natsie-chart.fullname" .) -}}
{{- end }}
{{- define "natsie-chart.natsCtxSecretName" -}}
{{- printf "%s-nats-contexts" (include "natsie-chart.fullname" .) -}}
{{- end }}
{{- define "natsie-chart.pvcName" -}}
{{- printf "%s-state" (include "natsie-chart.fullname" .) -}}
{{- end }}

{{/*
True when the chart should create its own Signing-key Secret.
*/}}
{{- define "natsie-chart.hasManagedSigningKey" -}}
{{- if and .Values.bot.signingKey (not .Values.bot.existingSigningKeySecret.name) -}}
true
{{- end -}}
{{- end }}

{{/*
True when the chart should create the NATS contexts Secret from inline data.
*/}}
{{- define "natsie-chart.hasManagedNatsContexts" -}}
{{- if and .Values.natsContexts.inline (not .Values.natsContexts.existingSecret) -}}
true
{{- end -}}
{{- end }}

{{/*
Effective NATS contexts Secret name (inline → chart-managed, else existing).
*/}}
{{- define "natsie-chart.natsCtxSecretEffective" -}}
{{- if .Values.natsContexts.existingSecret -}}
{{- .Values.natsContexts.existingSecret -}}
{{- else -}}
{{- include "natsie-chart.natsCtxSecretName" . -}}
{{- end -}}
{{- end }}

{{/*
Render the natsie config.yaml. Sensitive values (signing_key, notify URLs
that reference Secrets) come in via env vars at runtime, so they don't
appear in the rendered ConfigMap.
*/}}
{{- define "natsie-chart.configYaml" -}}
defaults:
  min_pending: {{ .Values.defaults.minPending }}
  min_idle: {{ .Values.defaults.minIdle | quote }}
  format: {{ .Values.defaults.format | quote }}
{{- with .Values.contexts }}

contexts:
{{- range $name, $opts := . }}
  {{ $name }}:
    peer: {{ $opts.peer | quote }}
{{- end }}
{{- end }}

bot:
  store: "file://{{ .Values.bot.storePath }}"
  audit_log: {{ .Values.bot.auditLogPath | quote }}
  http:
    listen: ":{{ .Values.service.port }}"
{{- if .Values.bot.baseUrl }}
    base_url: {{ .Values.bot.baseUrl | quote }}
{{- end }}
{{- if and (not .Values.bot.notifySecret) .Values.bot.notify }}
  notify:
{{- range .Values.bot.notify }}
    - {{ . | quote }}
{{- end }}
{{- end }}
{{- with .Values.bot.schedules }}
  schedules:
{{- range . }}
    - name: {{ .name | quote }}
      cron: {{ .cron | quote }}
      context: {{ .context | quote }}
{{- if .peerContext }}
      peer_context: {{ .peerContext | quote }}
{{- end }}
{{- if .stream }}
      stream: {{ .stream | quote }}
{{- end }}
{{- if .minPending }}
      min_pending: {{ .minPending }}
{{- end }}
{{- if .minIdle }}
      min_idle: {{ .minIdle | quote }}
{{- end }}
{{- end }}
{{- end }}
{{- with .Values.bot.owners }}
  owners:
{{- range . }}
    - name: {{ .name | quote }}
{{- if .streams }}
      streams:
{{- range .streams }}
        - {{ . | quote }}
{{- end }}
{{- end }}
{{- if .consumerPrefix }}
      consumer_prefix:
{{- range .consumerPrefix }}
        - {{ . | quote }}
{{- end }}
{{- end }}
{{- if .notify }}
      notify:
{{- range .notify }}
        - {{ . | quote }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}
