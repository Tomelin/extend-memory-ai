{{/* Chart name. */}}
{{- define "extended-memory.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Fully qualified resource name. */}}
{{- define "extended-memory.fullname" -}}
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

{{/* Chart label. */}}
{{- define "extended-memory.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Common resource labels. */}}
{{- define "extended-memory.labels" -}}
helm.sh/chart: {{ include "extended-memory.chart" . }}
{{ include "extended-memory.selectorLabels" . }}
app.kubernetes.io/part-of: extend-memory-ai
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/* Stable selector labels. */}}
{{- define "extended-memory.selectorLabels" -}}
app.kubernetes.io/name: {{ include "extended-memory.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/* HTTPRoute name. */}}
{{- define "extended-memory.httpRouteName" -}}
{{- default (include "extended-memory.fullname" .) .Values.httpRoute.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Purge CronJob name. */}}
{{- define "extended-memory.purgeName" -}}
{{- default (printf "%s-purge" (include "extended-memory.fullname" .)) .Values.purge.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/* Secret containing database and OpenTelemetry credentials. */}}
{{- define "extended-memory.secretName" -}}
{{- if .Values.secret.create }}
{{- include "extended-memory.fullname" . }}
{{- else -}}
{{- required "secret.existingSecret is required when secret.create=false" .Values.secret.existingSecret }}
{{- end }}
{{- end }}
