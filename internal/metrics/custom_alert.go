package metrics

import "github.com/prometheus/client_golang/prometheus"

// ── Discrete app-emitted alert counter ───────────────────────────────────────

// AppAlertTotal counts discrete one-shot application events that are not
// naturally scrapeable via standard gauges or counters (e.g. a credential
// decrypt failure, a goose migration error, an FFmpeg subprocess crash
// detected by the recording engine monitor). Each call site that detects
// such an event calls SetCustomAlert, which increments this counter.
//
// Prometheus alerting rule IronsightAppAlert (deploy/monitoring/alerts.yml)
// fires when this counter increases, bridging discrete Go log events into
// the Prom → Alertmanager → ntfy alert pipeline.
//
// Label cardinality is bounded: name is one of a small enum of event types
// defined in the codebase; severity is "warning" or "critical". Do not use
// dynamic values (user IDs, camera UUIDs, error message strings) as label
// values.
var AppAlertTotal = prometheus.NewCounterVec(
	prometheus.CounterOpts{
		Name: "ironsight_app_alert_total",
		Help: "Total one-shot application alerts emitted by SetCustomAlert, by name and severity.",
	},
	[]string{"name", "severity"},
)

func init() {
	Registry.MustRegister(AppAlertTotal)
}

// SetCustomAlert increments AppAlertTotal for the given alert name and
// severity. The msg parameter is informational only (not a label) — callers
// should also log the message at the appropriate level so it appears in
// structured logs alongside the metric increment.
//
// severity must be one of "warning" or "critical". Any other value is stored
// as-is but will not match the IronsightAppAlert alerting rule.
//
// Example call sites:
//
//	metrics.SetCustomAlert("camera_credentials_decrypt_failure", "warning", "key mismatch for camera "+id)
//	metrics.SetCustomAlert("goose_migration_failure", "critical", err.Error())
//	metrics.SetCustomAlert("ffmpeg_subprocess_crash", "warning", "camera "+name+" exited non-zero")
func SetCustomAlert(name, severity, _ string) {
	AppAlertTotal.WithLabelValues(name, severity).Inc()
}
