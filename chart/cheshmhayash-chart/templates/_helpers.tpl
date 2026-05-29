{{/*
Expand the name of the chart.
*/}}
{{- define "cheshmhayash-chart.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Fully qualified app name. Truncated at 63 chars because some Kubernetes
name fields are limited to that length (DNS-1123 label).
*/}}
{{- define "cheshmhayash-chart.fullname" -}}
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
{{- define "cheshmhayash-chart.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Recommended app.kubernetes.io/* labels. Apply to every rendered object.
*/}}
{{- define "cheshmhayash-chart.labels" -}}
helm.sh/chart: {{ include "cheshmhayash-chart.chart" . }}
{{ include "cheshmhayash-chart.selectorLabels" . }}
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
{{- define "cheshmhayash-chart.selectorLabels" -}}
app.kubernetes.io/name: {{ include "cheshmhayash-chart.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
ServiceAccount name in use.
*/}}
{{- define "cheshmhayash-chart.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "cheshmhayash-chart.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Container image reference (`repo:tag`). Tag falls back to .Chart.AppVersion.
*/}}
{{- define "cheshmhayash-chart.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s:%s" .Values.image.repository $tag -}}
{{- end }}

{{/*
Name of the ConfigMap holding settings.toml.
*/}}
{{- define "cheshmhayash-chart.configMapName" -}}
{{- printf "%s-config" (include "cheshmhayash-chart.fullname" .) -}}
{{- end }}

{{/*
Name of the chart-managed Secret (only created when at least one cluster
declares `auth.userPassword`).
*/}}
{{- define "cheshmhayash-chart.secretName" -}}
{{- printf "%s-auth" (include "cheshmhayash-chart.fullname" .) -}}
{{- end }}

{{/*
True when any cluster declares a chart-managed userPassword.
*/}}
{{- define "cheshmhayash-chart.hasManagedAuth" -}}
{{- $managed := false -}}
{{- range $i, $c := .Values.clusters -}}
{{- if and $c.auth $c.auth.userPassword -}}
{{- $managed = true -}}
{{- end -}}
{{- end -}}
{{- if $managed }}true{{ end }}
{{- end }}

{{/*
True when the chart needs to create a Secret entry for the OIDC client
secret, the session signing secret, or one of the inline MCP keys.
*/}}
{{- define "cheshmhayash-chart.hasManagedAppAuth" -}}
{{- $managed := false -}}
{{- if and .Values.auth.enabled .Values.auth.oidc.clientSecret -}}{{- $managed = true -}}{{- end -}}
{{- if and .Values.auth.enabled .Values.auth.session.secret -}}{{- $managed = true -}}{{- end -}}
{{- range $i, $k := .Values.auth.mcpKeys -}}
{{- if $k.value -}}{{- $managed = true -}}{{- end -}}
{{- end -}}
{{- if $managed }}true{{ end }}
{{- end }}

{{/*
True when the chart needs *any* Secret object — either cluster userPassword
or any of the app-auth (OIDC/session/MCP) inline values.
*/}}
{{- define "cheshmhayash-chart.hasSecret" -}}
{{- if or (eq (include "cheshmhayash-chart.hasManagedAuth" .) "true") (eq (include "cheshmhayash-chart.hasManagedAppAuth" .) "true") -}}
true
{{- end -}}
{{- end }}

{{/*
Render settings.toml. Passwords live in env vars (CHESHMHAYASH__NATS__i__PASSWORD)
so they never appear in the ConfigMap; `creds_file` paths point at the
projected Secret mount.
*/}}
{{- define "cheshmhayash-chart.settingsToml" -}}
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
{{- range $i, $n := .Values.notify }}
[[notify]]
provider = "{{ $n.provider }}"
{{- if $n.url }}
url = "{{ $n.url }}"
{{- end }}
{{- if $n.channel }}
channel = "{{ $n.channel }}"
{{- end }}
{{- if $n.username }}
username = "{{ $n.username }}"
{{- end }}
{{ end }}
{{- if .Values.auth.enabled }}

[auth]
enabled = true

[auth.oidc]
issuer = "{{ .Values.auth.oidc.issuer }}"
client_id = "{{ .Values.auth.oidc.clientId }}"
redirect_url = "{{ .Values.auth.oidc.redirectUrl }}"
{{- with .Values.auth.oidc.scopes }}
scopes = [{{ range $i, $s := . }}{{ if $i }}, {{ end }}"{{ $s }}"{{ end }}]
{{- end }}

[auth.access]
{{- with .Values.auth.access.allowedEmails }}
allowed_emails = [{{ range $i, $s := . }}{{ if $i }}, {{ end }}"{{ $s }}"{{ end }}]
{{- end }}
{{- with .Values.auth.access.allowedDomains }}
allowed_domains = [{{ range $i, $s := . }}{{ if $i }}, {{ end }}"{{ $s }}"{{ end }}]
{{- end }}
{{- with .Values.auth.access.allowedGroups }}
allowed_groups = [{{ range $i, $s := . }}{{ if $i }}, {{ end }}"{{ $s }}"{{ end }}]
{{- end }}
{{- if .Values.auth.access.groupsClaim }}
groups_claim = "{{ .Values.auth.access.groupsClaim }}"
{{- end }}
{{- if or .Values.auth.access.admin.allowedEmails .Values.auth.access.admin.allowedDomains .Values.auth.access.admin.allowedGroups }}

[auth.access.admin]
{{- with .Values.auth.access.admin.allowedEmails }}
allowed_emails = [{{ range $i, $s := . }}{{ if $i }}, {{ end }}"{{ $s }}"{{ end }}]
{{- end }}
{{- with .Values.auth.access.admin.allowedDomains }}
allowed_domains = [{{ range $i, $s := . }}{{ if $i }}, {{ end }}"{{ $s }}"{{ end }}]
{{- end }}
{{- with .Values.auth.access.admin.allowedGroups }}
allowed_groups = [{{ range $i, $s := . }}{{ if $i }}, {{ end }}"{{ $s }}"{{ end }}]
{{- end }}
{{- end }}

[auth.session]
ttl_seconds = {{ .Values.auth.session.ttlSeconds }}
cookie_name = "{{ .Values.auth.session.cookieName }}"
secure = {{ .Values.auth.session.secure }}
{{- end }}
{{- range $i, $k := .Values.auth.mcpKeys }}

[[auth.mcp_keys]]
name = "{{ $k.name }}"
{{- end }}
{{- end }}
