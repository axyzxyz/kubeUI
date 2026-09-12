{{/*
Helper: 资源名
*/}}
{{- define "v911.name" -}}
{{- default .Chart.Name .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- end -}}
