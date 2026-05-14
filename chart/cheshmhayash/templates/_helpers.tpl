{{/*
Expand the name of the chart.
*/}}
{{- define "cheshmhayash.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Fully qualified app name. Truncated at 63 chars because some Kubernetes
name fields are limited to that length (DNS-1123 label).
*/}}
{{- define "cheshmhayash.fullname" -}}
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
{{- define "cheshmhayash.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Recommended app.kubernetes.io/* labels. Apply to every rendered object.
*/}}
{{- define "cheshmhayash.labels" -}}
helm.sh/chart: {{ include "cheshmhayash.chart" . }}
{{ include "cheshmhayash.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/component: dashboard
app.kubernetes.io/part-of: nats
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Immutable selector subset. Only name+instance — never include `version`
because Deployment selectors are immutable after creation.
*/}}
{{- define "cheshmhayash.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cheshmhayash.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
ServiceAccount name in use.
*/}}
{{- define "cheshmhayash.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "cheshmhayash.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Container image reference (`repo:tag`). Tag falls back to .Chart.AppVersion.
*/}}
{{- define "cheshmhayash.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end }}

{{/*
Name of the ConfigMap holding settings.toml.
*/}}
{{- define "cheshmhayash.configMapName" -}}
{{- printf "%s-config" (include "cheshmhayash.fullname" .) -}}
{{- end }}

{{/*
Name of the chart-managed Secret (only created when at least one cluster
declares `auth.userPassword`).
*/}}
{{- define "cheshmhayash.secretName" -}}
{{- printf "%s-auth" (include "cheshmhayash.fullname" .) -}}
{{- end }}

{{/*
True when any cluster declares a chart-managed userPassword.
*/}}
{{- define "cheshmhayash.hasManagedAuth" -}}
{{- $managed := false -}}
{{- range $i, $c := .Values.clusters -}}
{{- if and $c.auth $c.auth.userPassword -}}
{{- $managed = true -}}
{{- end -}}
{{- end -}}
{{- if $managed }}true{{ end }}
{{- end }}

{{/*
Render settings.toml. Passwords live in env vars (CHESHMHAYASH__NATS__i__PASSWORD)
so they never appear in the ConfigMap; `creds_file` paths point at the
projected Secret mount.
*/}}
{{- define "cheshmhayash.settingsToml" -}}
[server]
host = "{{ .Values.server.host }}"
port = {{ .Values.server.port }}

{{ range $i, $c := .Values.clusters }}
[[nats]]
name = "{{ $c.name }}"
url = "{{ $c.url }}"
{{- if $c.requestTimeoutMs }}
request_timeout_ms = {{ $c.requestTimeoutMs }}
{{- end }}
{{- if $c.discoveryTimeoutMs }}
discovery_timeout_ms = {{ $c.discoveryTimeoutMs }}
{{- end }}
{{- if and $c.auth $c.auth.credsFileSecret }}
creds_file = "/etc/cheshmhayash/creds/{{ $c.name }}.creds"
{{- end }}
{{- if and $c.auth $c.auth.userPassword $c.auth.userPassword.user }}
user = "{{ $c.auth.userPassword.user }}"
{{- end }}
{{ end }}
{{- end }}
