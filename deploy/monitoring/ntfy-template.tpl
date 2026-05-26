{{/* ntfy-template.tpl — Alertmanager Go templates for ntfy.sh notifications */}}
{{/* P1-C-04: used by alertmanager.yml receiver ntfy-caleb */}}

{{/* ── Title: alert name + firing/resolved status ────────────────────────── */}}
{{ define "ntfy.title" -}}
{{- if eq .Status "firing" -}}
[ALERT] {{ .CommonLabels.alertname }}
{{- else -}}
[RESOLVED] {{ .CommonLabels.alertname }}
{{- end -}}
{{- end }}

{{/* ── Priority: map severity → ntfy priority integer ────────────────────── */}}
{{/* ntfy priorities: 1=min 2=low 3=default 4=high 5=urgent               */}}
{{ define "ntfy.priority" -}}
{{- if eq .CommonLabels.severity "critical" -}}urgent
{{- else if eq .CommonLabels.severity "warning" -}}high
{{- else -}}default
{{- end -}}
{{- end }}

{{/* ── Tags: emoji tags for visual triage in ntfy app ────────────────────── */}}
{{ define "ntfy.tags" -}}
{{- if eq .Status "resolved" -}}white_check_mark,ironsight
{{- else if eq .CommonLabels.severity "critical" -}}rotating_light,ironsight
{{- else -}}warning,ironsight
{{- end -}}
{{- end }}

{{/* ── Click URL: runbook URL for the first alert in the group ───────────── */}}
{{ define "ntfy.click" -}}
{{- with index .Alerts 0 -}}
{{- .Annotations.runbook_url -}}
{{- end -}}
{{- end }}

{{/* ── Message body: full alert details ──────────────────────────────────── */}}
{{ define "ntfy.message" -}}
{{- if eq .Status "firing" -}}
FIRING {{ len .Alerts }} alert(s) — {{ .CommonLabels.alertname }}
{{- else -}}
RESOLVED — {{ .CommonLabels.alertname }}
{{- end }}

Severity: {{ .CommonLabels.severity | toUpper }}
{{- if .CommonLabels.route }}
Route: {{ .CommonLabels.route }}
{{- end }}
{{- if .CommonLabels.camera_id }}
Camera: {{ .CommonLabels.camera_id }}
{{- end }}

{{ range .Alerts -}}
Alert: {{ .Labels.alertname }}
  Status:  {{ .Status }}
  Summary: {{ .Annotations.summary }}
  {{ .Annotations.description | trimSpace }}
  {{- if .Annotations.runbook_url }}
  Runbook: {{ .Annotations.runbook_url }}
  {{- end }}
  Fired at: {{ .StartsAt.Format "2006-01-02 15:04:05 UTC" }}
  {{- if eq .Status "resolved" }}
  Resolved at: {{ .EndsAt.Format "2006-01-02 15:04:05 UTC" }}
  {{- end }}

{{ end -}}
{{- end }}
