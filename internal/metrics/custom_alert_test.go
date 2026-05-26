package metrics_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/prometheus/client_golang/prometheus/testutil"
	"gopkg.in/yaml.v3"

	"ironsight/internal/metrics"
)

// TestSetCustomAlert_Increments verifies that a single SetCustomAlert call
// increments AppAlertTotal by exactly 1 for the given (name, severity) pair.
func TestSetCustomAlert_Increments(t *testing.T) {
	before := testutil.ToFloat64(metrics.AppAlertTotal.WithLabelValues("test_event", "warning"))

	metrics.SetCustomAlert("test_event", "warning", "hi")

	after := testutil.ToFloat64(metrics.AppAlertTotal.WithLabelValues("test_event", "warning"))
	if delta := after - before; delta != 1 {
		t.Errorf("expected counter delta 1, got %.0f", delta)
	}
}

// TestSetCustomAlert_Accumulates verifies that multiple calls to
// SetCustomAlert with the same (name, severity) accumulate — the counter
// is not reset between calls.
func TestSetCustomAlert_Accumulates(t *testing.T) {
	name := "test_accumulate_event"
	before := testutil.ToFloat64(metrics.AppAlertTotal.WithLabelValues(name, "critical"))

	metrics.SetCustomAlert(name, "critical", "first call")
	metrics.SetCustomAlert(name, "critical", "second call")
	metrics.SetCustomAlert(name, "critical", "third call")

	after := testutil.ToFloat64(metrics.AppAlertTotal.WithLabelValues(name, "critical"))
	if delta := after - before; delta != 3 {
		t.Errorf("expected counter delta 3 after three calls, got %.0f", delta)
	}
}

// TestSetCustomAlert_LabelIsolation verifies that (name, severity) label
// pairs produce independent series — a "warning" call does not affect the
// "critical" series for the same name, and vice versa.
func TestSetCustomAlert_LabelIsolation(t *testing.T) {
	name := "test_isolation_event"

	beforeWarn := testutil.ToFloat64(metrics.AppAlertTotal.WithLabelValues(name, "warning"))
	beforeCrit := testutil.ToFloat64(metrics.AppAlertTotal.WithLabelValues(name, "critical"))

	metrics.SetCustomAlert(name, "warning", "warn msg")

	afterWarn := testutil.ToFloat64(metrics.AppAlertTotal.WithLabelValues(name, "warning"))
	afterCrit := testutil.ToFloat64(metrics.AppAlertTotal.WithLabelValues(name, "critical"))

	if delta := afterWarn - beforeWarn; delta != 1 {
		t.Errorf("warning counter: expected delta 1, got %.0f", delta)
	}
	if delta := afterCrit - beforeCrit; delta != 0 {
		t.Errorf("critical counter: expected delta 0 (unaffected), got %.0f", delta)
	}
}

// TestSetCustomAlert_NameRoundtrip verifies that the name label round-trips
// through the counter correctly — two different names produce separate series.
func TestSetCustomAlert_NameRoundtrip(t *testing.T) {
	nameA := "test_name_a"
	nameB := "test_name_b"

	beforeA := testutil.ToFloat64(metrics.AppAlertTotal.WithLabelValues(nameA, "warning"))
	beforeB := testutil.ToFloat64(metrics.AppAlertTotal.WithLabelValues(nameB, "warning"))

	metrics.SetCustomAlert(nameA, "warning", "a fires")
	metrics.SetCustomAlert(nameA, "warning", "a fires again")

	afterA := testutil.ToFloat64(metrics.AppAlertTotal.WithLabelValues(nameA, "warning"))
	afterB := testutil.ToFloat64(metrics.AppAlertTotal.WithLabelValues(nameB, "warning"))

	if delta := afterA - beforeA; delta != 2 {
		t.Errorf("nameA counter: expected delta 2, got %.0f", delta)
	}
	if delta := afterB - beforeB; delta != 0 {
		t.Errorf("nameB counter: expected delta 0 (nameB not called), got %.0f", delta)
	}
}

// TestAppAlertInRegistry verifies that AppAlertTotal is present in the
// package-level Registry output after at least one label combination has
// been observed. This catches a missing MustRegister in init().
func TestAppAlertInRegistry(t *testing.T) {
	metrics.SetCustomAlert("test_registry_check", "warning", "ping")

	mfs, err := metrics.Registry.Gather()
	if err != nil {
		t.Fatalf("Registry.Gather: %v", err)
	}
	found := false
	for _, mf := range mfs {
		if mf.GetName() == "ironsight_app_alert_total" {
			found = true
			break
		}
	}
	if !found {
		t.Error("ironsight_app_alert_total not found in Registry — MustRegister may have been missed")
	}
}

// TestAlertRulesYAML validates that deploy/monitoring/alerts.yml is
// well-formed YAML and that every rule contains the required fields:
// expr, labels.severity, annotations.summary, annotations.runbook_url.
// This is a structural round-trip test — it does not require promtool
// to be installed on the test host.
func TestAlertRulesYAML(t *testing.T) {
	// Locate the repo root relative to this source file so the test works
	// regardless of which directory `go test` is invoked from.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	// thisFile: .../internal/metrics/custom_alert_test.go
	// repo root: three levels up
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	alertsPath := filepath.Join(repoRoot, "deploy", "monitoring", "alerts.yml")

	data, err := os.ReadFile(alertsPath)
	if err != nil {
		t.Skipf("alerts.yml not readable (path: %s): %v", alertsPath, err)
	}

	type annotationFields struct {
		Summary    string `yaml:"summary"`
		RunbookURL string `yaml:"runbook_url"`
	}
	type labelFields struct {
		Severity string `yaml:"severity"`
	}
	type ruleFields struct {
		Alert       string           `yaml:"alert"`
		Expr        string           `yaml:"expr"`
		Labels      labelFields      `yaml:"labels"`
		Annotations annotationFields `yaml:"annotations"`
	}
	type groupFields struct {
		Name  string       `yaml:"name"`
		Rules []ruleFields `yaml:"rules"`
	}
	type alertFile struct {
		Groups []groupFields `yaml:"groups"`
	}

	var af alertFile
	if err := yaml.Unmarshal(data, &af); err != nil {
		t.Fatalf("alerts.yml YAML parse error: %v", err)
	}

	if len(af.Groups) == 0 {
		t.Fatal("alerts.yml contains no groups")
	}

	totalRules := 0
	for _, g := range af.Groups {
		for _, r := range g.Rules {
			totalRules++
			if r.Alert == "" {
				t.Errorf("group %q: a rule is missing the 'alert' field", g.Name)
			}
			if r.Expr == "" {
				t.Errorf("alert %q: missing 'expr'", r.Alert)
			}
			if r.Labels.Severity == "" {
				t.Errorf("alert %q: missing 'labels.severity'", r.Alert)
			}
			if r.Annotations.Summary == "" {
				t.Errorf("alert %q: missing 'annotations.summary'", r.Alert)
			}
			if r.Annotations.RunbookURL == "" {
				t.Errorf("alert %q: missing 'annotations.runbook_url'", r.Alert)
			}
		}
	}

	if totalRules == 0 {
		t.Error("alerts.yml groups exist but contain no rules")
	}
	t.Logf("alerts.yml validated: %d rules across %d groups", totalRules, len(af.Groups))
}

// TestAlertmanagerYAML validates that deploy/monitoring/alertmanager.yml is
// well-formed YAML and that each route has a receiver and each named
// receiver exists in the receivers list.
func TestAlertmanagerYAML(t *testing.T) {
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Join(filepath.Dir(thisFile), "..", "..")
	amPath := filepath.Join(repoRoot, "deploy", "monitoring", "alertmanager.yml")

	data, err := os.ReadFile(amPath)
	if err != nil {
		t.Skipf("alertmanager.yml not readable (path: %s): %v", amPath, err)
	}

	type routeFields struct {
		Receiver string        `yaml:"receiver"`
		Routes   []routeFields `yaml:"routes"`
	}
	type receiverFields struct {
		Name string `yaml:"name"`
	}
	type amFile struct {
		Route     routeFields      `yaml:"route"`
		Receivers []receiverFields `yaml:"receivers"`
	}

	var am amFile
	if err := yaml.Unmarshal(data, &am); err != nil {
		t.Fatalf("alertmanager.yml YAML parse error: %v", err)
	}

	// Build set of declared receiver names.
	receiverNames := make(map[string]bool)
	for _, r := range am.Receivers {
		if r.Name == "" {
			t.Error("a receiver entry is missing the 'name' field")
			continue
		}
		receiverNames[r.Name] = true
	}

	if len(receiverNames) == 0 {
		t.Fatal("alertmanager.yml has no receivers")
	}

	// Verify the default route has a receiver.
	if am.Route.Receiver == "" {
		t.Error("alertmanager.yml route is missing 'receiver'")
	} else if !receiverNames[am.Route.Receiver] {
		t.Errorf("default route receiver %q is not declared in receivers list", am.Route.Receiver)
	}

	// Verify all sub-routes have receivers that exist.
	for i, sub := range am.Route.Routes {
		if sub.Receiver == "" {
			t.Errorf("sub-route[%d] is missing 'receiver'", i)
		} else if !receiverNames[sub.Receiver] {
			t.Errorf("sub-route[%d] receiver %q is not declared in receivers list", i, sub.Receiver)
		}
	}

	t.Logf("alertmanager.yml validated: %d receivers, %d sub-routes", len(am.Receivers), len(am.Route.Routes))
}
