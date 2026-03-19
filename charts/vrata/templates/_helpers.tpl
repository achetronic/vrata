{{- define "vrata.name" -}}
{{ .Chart.Name }}
{{- end -}}

{{- define "vrata.fullname" -}}
{{ .Release.Name }}-{{ .Chart.Name }}
{{- end -}}

{{- define "vrata.labels" -}}
app.kubernetes.io/name: {{ include "vrata.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ .Chart.Name }}-{{ .Chart.Version }}
{{- end -}}

{{- define "vrata.imageTag" -}}
{{ .Values.image.tag | default .Chart.AppVersion }}
{{- end -}}

{{- define "vrata.cpName" -}}
{{ include "vrata.fullname" . }}-cp
{{- end -}}

{{- define "vrata.proxyName" -}}
{{ include "vrata.fullname" . }}-proxy
{{- end -}}

{{/*
Returns "true" if Raft is configured in controlPlane.config.
Used to decide whether to expose the Raft port and generate the headless service.
*/}}
{{- define "vrata.raftEnabled" -}}
{{- if dig "controlPlane" "raft" "" .Values.controlPlane.config -}}
true
{{- end -}}
{{- end -}}

{{/*
Returns the Raft bind port from controlPlane.config, defaulting to 7000.
*/}}
{{- define "vrata.raftPort" -}}
{{- $bindAddr := dig "controlPlane" "raft" "bindAddress" ":7000" .Values.controlPlane.config -}}
{{- trimPrefix ":" $bindAddr -}}
{{- end -}}
