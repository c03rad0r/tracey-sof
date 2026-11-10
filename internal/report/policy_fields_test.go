package report

import (
	"bytes"
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

func TestPolicyAndEvidenceReportOutputs(t *testing.T) {
	report := testReport()
	report.Policy = model.TracePolicy{SchemaVersion: 1, MaxHops: 8, MaxTransactions: 250, StopIfPrevoutValueSatsGTE: 500000000, StopIfInputCountGTE: 50, StopIfOutputCountGTE: 60, StopBeforeBlockHeight: 700000}
	report.UTXOs[0].ReviewRequired = true
	report.UTXOs[0].ReviewReasons = []string{"prevout_value", "input_count"}
	report.Summary.ReviewUTXOs = 1
	report.Summary.UnmatchedEvidence = 1
	report.Evidence = []model.EvidenceStatus{
		{Kind: "address", Value: "bc1qnotobserved", PurchaseIDs: []string{"buy"}, Matched: false},
		{Kind: "transaction", Value: report.UTXOs[0].TxID, PurchaseIDs: []string{"buy"}, Matched: true, MatchedNodeID: "tx:" + report.UTXOs[0].TxID},
	}
	data, err := EvidenceCSVBytes(report)
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 3 || records[0][0] != "kind" || records[1][0] != "address" || records[1][3] != "false" || records[2][3] != "true" {
		t.Fatalf("evidence CSV = %q", records)
	}
	dir := t.TempDir()
	if err := WriteStructured(report, dir, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"evidence.csv", "report.csv"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	utxoCSV, err := os.ReadFile(filepath.Join(dir, "report.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(utxoCSV), "prevout_value|input_count") {
		t.Fatalf("review reasons absent from report CSV: %s", utxoCSV)
	}
	html, err := os.ReadFile(filepath.Join(dir, "report.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(html), `"stop_if_prevout_value_sats_gte":500000000`) {
		t.Fatal("effective policy absent from HTML report data")
	}
}
