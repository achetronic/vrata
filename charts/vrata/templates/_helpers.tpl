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
