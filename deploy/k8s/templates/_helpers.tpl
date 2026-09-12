{{/*
Helper: 资源名
*/}}
{{- define "kubeui.name" -}}
{{- default .Chart.Name .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- end -}}
