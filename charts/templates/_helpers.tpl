{{/*
Expand the name of the chart.
*/}}
{{- define "csi-driver-hostpath-on-steriod.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "csi-driver-hostpath-on-steriod.fullname" -}}
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
Create chart name and version as used by the chart label.
*/}}
{{- define "csi-driver-hostpath-on-steriod.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "csi-driver-hostpath-on-steriod.labels" -}}
helm.sh/chart: {{ include "csi-driver-hostpath-on-steriod.chart" . }}
{{ include "csi-driver-hostpath-on-steriod.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: {{ .Chart.Name }}
app.kubernetes.io/component: {{ .Values.component | default "csi-driver" }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "csi-driver-hostpath-on-steriod.selectorLabels" -}}
app.kubernetes.io/name: {{ include "csi-driver-hostpath-on-steriod.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
Create the name of the service account to use
*/}}
{{- define "csi-driver-hostpath-on-steriod.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "csi-driver-hostpath-on-steriod.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}


{{/*
CSI Driver name
*/}}
{{- define "csi-driver-hostpath-on-steriod.driverName" -}}
{{- default .Chart.Name .Values.driverName }}
{{- end }}