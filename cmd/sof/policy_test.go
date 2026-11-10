package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTracePolicyFileAndEnvironmentPrecedence(t *testing.T) {
	t.Setenv("TRACE_MAX_DEPTH", "9")
	t.Setenv("TRACE_MAX_TRANSACTIONS", "99")
	dir := t.TempDir()
	limits, err := traceLimits(dir)
	if err != nil || limits.MaxDepth != 9 || limits.MaxTransactions != 99 {
		t.Fatalf("legacy defaults = %+v, %v", limits, err)
	}

	policyPath := filepath.Join(dir, "trace-policy.json")
	if err := os.WriteFile(policyPath, []byte(`{"schema_version":1,"max_hops":8,"max_transactions":250,"stop_if_prevout_value_sats_gte":5000,"stop_if_input_count_gte":50,"stop_if_output_count_gte":60,"stop_before_block_height":700000}`), 0o600); err != nil {
		t.Fatal(err)
	}
	limits, err = traceLimits(dir)
	if err != nil {
		t.Fatal(err)
	}
	if limits.MaxDepth != 8 || limits.MaxTransactions != 250 || limits.StopIfPrevoutValueSatsGTE != 5000 || limits.StopIfInputCountGTE != 50 || limits.StopIfOutputCountGTE != 60 || limits.StopBeforeBlockHeight != 700000 {
		t.Fatalf("policy file did not take precedence: %+v", limits)
	}

	if err := os.WriteFile(policyPath, []byte(`{"schema_version":1,"max_hops":-1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := traceLimits(dir); err == nil {
		t.Fatal("invalid policy accepted")
	}
}

func TestBuildReportRejectsStaleTracePolicy(t *testing.T) {
	p := newCLIPaths(t, false)
	writeCLIInputs(t, p, false)
	saveCLIFixtureTrace(t, p)
	app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	if _, err := app.buildReport(p); err != nil {
		t.Fatalf("matching default policy failed: %v", err)
	}

	policyPath := filepath.Join(p.dataDir, "trace-policy.json")
	if err := os.WriteFile(policyPath, []byte(`{"schema_version":1,"max_hops":8,"max_transactions":250}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.buildReport(p); err == nil || !strings.Contains(err.Error(), "trace policy changed") {
		t.Fatalf("stale policy was not rejected: %v", err)
	}
}

func TestValidateReportsPolicyErrors(t *testing.T) {
	p := newCLIPaths(t, false)
	writeCLIInputs(t, p, false)
	app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	if err := app.validate(p); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.dataDir, "trace-policy.json"), []byte(`{"schema_version":1,"max_hops":-1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := app.validate(p); err == nil {
		t.Fatal("invalid policy passed validate")
	}
}
