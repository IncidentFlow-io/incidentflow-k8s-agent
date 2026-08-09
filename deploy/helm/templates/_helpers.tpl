{{- define "incidentflow-k8s-agent.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Avoid double-name when Release.Name already contains the chart name.
  release=incidentflow-k8s-agent, chart=incidentflow-k8s-agent → incidentflow-k8s-agent
  release=my-release,             chart=incidentflow-k8s-agent → my-release-incidentflow-k8s-agent
*/}}
{{- define "incidentflow-k8s-agent.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else if contains (include "incidentflow-k8s-agent.name" .) .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name (include "incidentflow-k8s-agent.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "incidentflow-k8s-agent.configmapName" -}}
{{- printf "%s-config" (include "incidentflow-k8s-agent.fullname" .) -}}
{{- end -}}

{{- define "incidentflow-k8s-agent.secretName" -}}
{{- default .Values.agent.credentialsSecretName .Values.credentialsSecretName -}}
{{- end -}}

{{- define "incidentflow-k8s-agent.bootstrapSecretName" -}}
{{- $legacy := .Values.agent.registrationTokenSecretName -}}
{{- default (printf "%s-bootstrap" (include "incidentflow-k8s-agent.fullname" .)) (default $legacy .Values.registrationTokenSecretName) -}}
{{- end -}}

{{- define "incidentflow-k8s-agent.registrationToken" -}}
{{- default .Values.agent.registrationToken .Values.registrationToken -}}
{{- end -}}

{{- define "incidentflow-k8s-agent.clusterName" -}}
{{- default .Release.Name (default .Values.agent.clusterName .Values.clusterName) -}}
{{- end -}}

{{- define "incidentflow-k8s-agent.labels" -}}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version | replace "+" "_" }}
app.kubernetes.io/name: {{ include "incidentflow-k8s-agent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "incidentflow-k8s-agent.selectorLabels" -}}
app.kubernetes.io/name: {{ include "incidentflow-k8s-agent.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
