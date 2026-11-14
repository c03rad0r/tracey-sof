package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

func TestPrintGapsAndCLIFlagErrors(t *testing.T) {
	policy := strings.Repeat("b", 64)
	outflow := strings.Repeat("d", 64)
	change := strings.Repeat("e", 64)
	sent := strings.Repeat("f", 64)
	report := model.Report{
		Graph: model.Graph{
			Nodes: []model.GraphNode{
				{ID: "tx:" + policy, Kind: "transaction", TxID: policy, TerminalReason: "policy_output_count", BlockTime: "2024-08-03T00:00:00Z"},
				{ID: "out:" + policy + ":1", Kind: "output", Outpoint: policy + ":1", Address: "bc1qchild", ValueSats: 100},
				{ID: "tx:" + outflow, Kind: "transaction", TxID: outflow, Label: "exchange a", BlockTime: "2026-08-24T00:00:00Z"},
				{ID: "out:" + change + ":0", Kind: "output", Outpoint: change + ":0", ValueSats: 50},
				{ID: "out:" + sent + ":0", Kind: "output", Outpoint: sent + ":0", Address: "bc1qexchange", ValueSats: 100},
			},
			Edges: []model.GraphEdge{
				{From: "out:" + policy + ":1", To: "tx:" + policy, Kind: "funded_by"},
				{From: "out:" + change + ":0", To: "tx:" + outflow, Kind: "funded_by"},
				{From: "out:" + sent + ":0", To: "tx:" + outflow, Kind: "funded_by"},
			},
		},
		Annotations: model.AnnotationFile{Outpoints: map[string]model.Annotation{change + ":0": {Label: "exchange a (change)"}}},
	}
	var output bytes.Buffer
	printGaps(&output, report, true, true)
	for _, want := range []string{"== INFLOW: unaccounted exchange payouts (1, 1 months) ==", "== OUTFLOW: transfers without sale evidence (1, 1 months) =="} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("printGaps output %q lacks %q", output.String(), want)
		}
	}
	var inflowOnly bytes.Buffer
	printGaps(&inflowOnly, report, true, false)
	if !strings.Contains(inflowOnly.String(), "INFLOW") || strings.Contains(inflowOnly.String(), "OUTFLOW") {
		t.Errorf("inflow-only output = %q", inflowOnly.String())
	}
	var outflowOnly bytes.Buffer
	printGaps(&outflowOnly, report, false, true)
	if !strings.Contains(outflowOnly.String(), "OUTFLOW") || strings.Contains(outflowOnly.String(), "INFLOW") {
		t.Errorf("outflow-only output = %q", outflowOnly.String())
	}

	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), []string{"gaps", "-not-a-flag"}, &stdout, &stderr); err == nil {
		t.Fatal("expected a gaps flag parse error")
	}
}
