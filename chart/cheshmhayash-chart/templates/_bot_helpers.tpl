{{/*
Operations bot (`cheshmhayash bot serve`, formerly natsie). Every value
lives under `bot:`; the image, pull secrets, service account and security
contexts are the dashboard's.
*/}}
{{- define "cheshmhayash-chart.bot.fullname" -}}
{{- printf "%s-bot" (include "cheshmhayash-chart.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{/*
Immutable selector subset for the bot, distinct from the dashboard and MCP
selectors.
*/}}
{{- define "cheshmhayash-chart.bot.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cheshmhayash-chart.name" . }}-bot
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Recommended labels for the bot (component: bot).
*/}}
{{- define "cheshmhayash-chart.bot.labels" -}}
helm.sh/chart: {{ include "cheshmhayash-chart.chart" . }}
{{ include "cheshmhayash-chart.bot.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/component: bot
app.kubernetes.io/part-of: nats
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{- define "cheshmhayash-chart.bot.serviceAccountName" -}}
{{- include "cheshmhayash-chart.serviceAccountName" . -}}
{{- end }}

{{- define "cheshmhayash-chart.bot.image" -}}
{{- include "cheshmhayash-chart.image" . -}}
{{- end }}

{{/*
Names of chart-managed child objects.
*/}}
{{- define "cheshmhayash-chart.bot.configMapName" -}}
{{- printf "%s-config" (include "cheshmhayash-chart.bot.fullname" .) -}}
{{- end }}
{{- define "cheshmhayash-chart.bot.secretName" -}}
{{- printf "%s-signing-key" (include "cheshmhayash-chart.bot.fullname" .) -}}
{{- end }}
{{- define "cheshmhayash-chart.bot.natsCtxSecretName" -}}
{{- printf "%s-nats-contexts" (include "cheshmhayash-chart.bot.fullname" .) -}}
{{- end }}
{{- define "cheshmhayash-chart.bot.pvcName" -}}
{{- printf "%s-state" (include "cheshmhayash-chart.bot.fullname" .) -}}
{{- end }}

{{/*
True when the chart should create its own Signing-key Secret.
*/}}
{{- define "cheshmhayash-chart.bot.hasManagedSigningKey" -}}
{{- if and .Values.bot.signingKey (not .Values.bot.existingSigningKeySecret.name) -}}
true
{{- end -}}
{{- end }}

{{/*
True when the chart should create its own Mattermost-token Secret.
*/}}
{{- define "cheshmhayash-chart.bot.hasManagedMattermostToken" -}}
{{- if and .Values.bot.mattermost.enabled .Values.bot.mattermost.token (not .Values.bot.mattermost.existingTokenSecret.name) -}}
true
{{- end -}}
{{- end }}

{{/*
Mattermost-token Secret name (chart-managed) — only meaningful when
hasManagedMattermostToken is true.
*/}}
{{- define "cheshmhayash-chart.bot.mattermostTokenSecretName" -}}
{{- printf "%s-mattermost-token" (include "cheshmhayash-chart.bot.fullname" .) -}}
{{- end }}

{{/*
True when the chart should create the NATS contexts Secret from inline data.
*/}}
{{- define "cheshmhayash-chart.bot.hasManagedNatsContexts" -}}
{{- if and .Values.bot.natsContexts.inline (not .Values.bot.natsContexts.existingSecret) -}}
true
{{- end -}}
{{- end }}

{{/*
Effective NATS contexts Secret name (inline → chart-managed, else existing).
*/}}
{{- define "cheshmhayash-chart.bot.natsCtxSecretEffective" -}}
{{- if .Values.bot.natsContexts.existingSecret -}}
{{- .Values.bot.natsContexts.existingSecret -}}
{{- else -}}
{{- include "cheshmhayash-chart.bot.natsCtxSecretName" . -}}
{{- end -}}
{{- end }}

{{/*
Render the cheshmhayash config.yaml. Sensitive values (signing_key, notify URLs
that reference Secrets) come in via env vars at runtime, so they don't
appear in the rendered ConfigMap.
*/}}
{{- define "cheshmhayash-chart.bot.configYaml" -}}
{{- with .Values.bot.protect }}
{{- if or .metadataKeys .patterns }}
protect:
{{- with .metadataKeys }}
  metadata_keys:
{{- range . }}
    - {{ . | quote }}
{{- end }}
{{- end }}
{{- with .patterns }}
  patterns:
{{- range . }}
    - {{ if .stream }}stream: {{ .stream | quote }}{{ end }}{{ if and .stream .consumer }}
      {{ end }}{{ if .consumer }}consumer: {{ .consumer | quote }}{{ end }}
{{- end }}
{{- end }}
{{- end }}
{{- end }}

defaults:
  min_pending: {{ .Values.bot.defaults.minPending }}
  min_idle: {{ .Values.bot.defaults.minIdle | quote }}
  format: {{ .Values.bot.defaults.format | quote }}
{{- with .Values.bot.contexts }}

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
    listen: ":{{ .Values.bot.service.port }}"
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
{{- if .kind }}
      kind: {{ .kind | quote }}
{{- end }}
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
{{- if .autoDelete }}
      auto_delete: true
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
{{- if .Values.bot.mattermost.enabled }}
  mattermost:
    enabled: true
    server: {{ .Values.bot.mattermost.server | quote }}
    team: {{ .Values.bot.mattermost.team | quote }}
    channel: {{ .Values.bot.mattermost.channel | quote }}
    trigger: {{ .Values.bot.mattermost.trigger | quote }}
{{- end }}
{{- end }}
