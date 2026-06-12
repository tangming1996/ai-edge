{{/*
Expand the name of the chart.
*/}}
{{- define "ai-edge.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "ai-edge.fullname" -}}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "ai-edge.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "ai-edge.labels" -}}
helm.sh/chart: {{ include "ai-edge.chart" . }}
{{ include "ai-edge.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "ai-edge.selectorLabels" -}}
app.kubernetes.io/name: {{ include "ai-edge.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Image full name with tag.
If the repository already includes a registry (contains "/"),
global.imageRegistry is NOT prepended to avoid duplication.
*/}}
{{- define "ai-edge.image" -}}
{{- $globalReg := .root.Values.global.imageRegistry | default "" }}
{{- $repository := .image.repository }}
{{- $tag := .image.tag | default .root.Chart.AppVersion | toString }}
{{- if and $globalReg (not (contains "/" $repository)) }}
{{- printf "%s/%s:%s" $globalReg $repository $tag }}
{{- else if contains "/" $repository }}
{{- printf "%s:%s" $repository $tag }}
{{- else }}
{{- printf "%s:%s" $repository $tag }}
{{- end }}
{{- end }}

{{/*
DB host resolution:
  - if db.host is set, use it directly (external DB)
  - if postgresql.enabled, use the in-chart postgres FQDN
  - otherwise empty (must be provided)
*/}}
{{- define "ai-edge.dbHost" -}}
{{- if .Values.db.host }}
{{- .Values.db.host }}
{{- else if .Values.postgresql.enabled }}
{{- printf "%s-postgresql.%s.svc.cluster.local" .Release.Name .Release.Namespace }}
{{- else }}
{{- printf "" }}
{{- end }}
{{- end }}

{{/*
MinIO host resolution.
*/}}
{{- define "ai-edge.minioHost" -}}
{{- if .Values.minio.externalHost }}
{{- .Values.minio.externalHost }}
{{- else if .Values.minio.enabled }}
{{- printf "%s-minio.%s.svc.cluster.local" .Release.Name .Release.Namespace }}
{{- else }}
{{- printf "" }}
{{- end }}
{{- end }}

{{/*
Persistence volume claim spec helper.
*/}}
{{- define "ai-edge.pvc" -}}
{{- $storageClass := .storageClass | default .root.Values.global.storageClass | default "" -}}
{{- if $storageClass }}
storageClassName: {{ $storageClass }}
{{- end }}
resources:
  requests:
    storage: {{ .size | quote }}
{{- end }}
