package input

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

func TestTracePolicyDefaultsAndLoading(t *testing.T) {
	path := filepath.Join(t.TempDir(), "trace-policy.json")
	policy, err := LoadTracePolicy(path)
	if err != nil || policy != DefaultTracePolicy() {
		t.Fatalf("defaults = %+v, %v", policy, err)
	}
	contents := `{"schema_version":1,"max_hops":8,"max_transactions":250,"stop_if_prevout_value_sats_gte":500000000,"stop_if_input_count_gte":50,"stop_if_output_count_gte":75,"stop_before_block_height":700000}`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	policy, err = LoadTracePolicy(path)
	if err != nil {
		t.Fatal(err)
	}
	if policy.MaxHops != 8 || policy.MaxTransactions != 250 || policy.StopIfPrevoutValueSatsGTE != 500000000 || policy.StopIfInputCountGTE != 50 || policy.StopIfOutputCountGTE != 75 || policy.StopBeforeBlockHeight != 700000 {
		t.Fatalf("loaded policy = %+v", policy)
	}
}

func TestTracePolicyValidationFailures(t *testing.T) {
	valid := DefaultTracePolicy()
	cases := []struct {
		name   string
		mutate func(*model.TracePolicy)
		want   string
	}{
		{"schema", func(p *model.TracePolicy) { p.SchemaVersion = 2 }, "schema_version"},
		{"max hops negative", func(p *model.TracePolicy) { p.MaxHops = -1 }, "max_hops"},
		{"max hops huge", func(p *model.TracePolicy) { p.MaxHops = 10001 }, "max_hops"},
		{"max tx negative", func(p *model.TracePolicy) { p.MaxTransactions = -1 }, "max_transactions"},
		{"max tx huge", func(p *model.TracePolicy) { p.MaxTransactions = 1000001 }, "max_transactions"},
		{"value negative", func(p *model.TracePolicy) { p.StopIfPrevoutValueSatsGTE = -1 }, "stop_if_prevout"},
		{"value huge", func(p *model.TracePolicy) { p.StopIfPrevoutValueSatsGTE = 2100000000000001 }, "stop_if_prevout"},
		{"inputs negative", func(p *model.TracePolicy) { p.StopIfInputCountGTE = -1 }, "stop_if_input"},
		{"inputs huge", func(p *model.TracePolicy) { p.StopIfInputCountGTE = 1000001 }, "stop_if_input"},
		{"outputs negative", func(p *model.TracePolicy) { p.StopIfOutputCountGTE = -1 }, "stop_if_output"},
		{"outputs huge", func(p *model.TracePolicy) { p.StopIfOutputCountGTE = 1000001 }, "stop_if_output"},
		{"height negative", func(p *model.TracePolicy) { p.StopBeforeBlockHeight = -1 }, "stop_before"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			policy := valid
			tc.mutate(&policy)
			if err := ValidateTracePolicy(policy); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("ValidateTracePolicy(%+v) = %v", policy, err)
			}
		})
	}
}

func TestTracePolicyLoadErrors(t *testing.T) {
	dir := t.TempDir()
	parent := filepath.Join(dir, "file")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTracePolicy(filepath.Join(parent, "trace-policy.json")); err == nil {
		t.Error("stat failure accepted")
	}
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte(`{"schema_version":1,"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTracePolicy(bad); err == nil {
		t.Error("unknown field accepted")
	}
	if err := os.WriteFile(bad, []byte(`{"schema_version":1,"max_hops":-1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTracePolicy(bad); err == nil {
		t.Error("invalid policy accepted")
	}
}
