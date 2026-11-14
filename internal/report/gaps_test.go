package report

import (
	"encoding/csv"
	"errors"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

func gapTestReport() model.Report {
	target := strings.Repeat("a", 64)
	funding := strings.Repeat("c", 64)
	policy := strings.Repeat("b", 64)
	outflow := strings.Repeat("d", 64)
	change := strings.Repeat("e", 64)
	sent := strings.Repeat("f", 64)
	outputBoundary := strings.Repeat("9", 64)
	childOut := model.Outpoint{TxID: policy, Vout: 1}
	changeOut := model.Outpoint{TxID: change, Vout: 0}
	sentOut := model.Outpoint{TxID: sent, Vout: 0}
	boundaryOut := model.Outpoint{TxID: outputBoundary, Vout: 0}
	return model.Report{
		Graph: model.Graph{
			Nodes: []model.GraphNode{
				{ID: "out:" + model.Outpoint{TxID: target, Vout: 0}.String(), Kind: "output", Outpoint: model.Outpoint{TxID: target, Vout: 0}.String(), Owned: true},
				{ID: "tx:" + funding, Kind: "transaction", TxID: funding},
				{ID: "out:" + childOut.String(), Kind: "output", Outpoint: childOut.String(), Address: "bc1qchild", ValueSats: 1000, BlockTime: "2024-08-03T00:00:00Z"},
				{ID: "tx:" + policy, Kind: "transaction", TxID: policy, TerminalReason: "policy_output_count", BlockTime: "2024-08-03T00:00:00Z", Height: 855142},
				{ID: "tx:" + outflow, Kind: "transaction", TxID: outflow, Label: "exchange a", BlockTime: "2026-08-24T00:00:00Z", Height: 963857},
				{ID: "out:" + changeOut.String(), Kind: "output", Outpoint: changeOut.String(), ValueSats: 500},
				{ID: "out:" + sentOut.String(), Kind: "output", Outpoint: sentOut.String(), Address: "bc1qexchange", ValueSats: 1000},
				{ID: "out:" + boundaryOut.String(), Kind: "output", Outpoint: boundaryOut.String(), TerminalReason: "max_depth", ValueSats: 200, BlockTime: "2024-08-04T00:00:00Z"},
				{ID: "tx:" + strings.Repeat("7", 64), Kind: "transaction", TxID: strings.Repeat("7", 64), TerminalReason: "policy_output_count"},
				{ID: "tx:" + strings.Repeat("8", 64), Kind: "transaction", TxID: strings.Repeat("8", 64), TerminalReason: "policy_output_count"},
				{ID: "out:" + model.Outpoint{TxID: strings.Repeat("4", 64), Vout: 0}.String(), Kind: "output", Outpoint: model.Outpoint{TxID: strings.Repeat("4", 64), Vout: 0}.String(), TerminalReason: "unavailable"},
				{ID: "out:" + model.Outpoint{TxID: strings.Repeat("5", 64), Vout: 0}.String(), Kind: "output", Outpoint: model.Outpoint{TxID: strings.Repeat("5", 64), Vout: 0}.String()},
				{ID: "tx:" + strings.Repeat("6", 64), Kind: "transaction", TxID: strings.Repeat("6", 64), TerminalReason: "max_depth"},
				{ID: "tx:" + strings.Repeat("g", 64), Kind: "transaction", TxID: strings.Repeat("g", 64), Label: "exchange a", BlockTime: "2026-08-24T00:00:00Z"},
				{ID: "out:" + model.Outpoint{TxID: strings.Repeat("i", 64), Vout: 0}.String(), Kind: "output", Outpoint: model.Outpoint{TxID: strings.Repeat("i", 64), Vout: 0}.String(), Address: "bc1qexchange2", ValueSats: 1000},
				{ID: "out:" + model.Outpoint{TxID: strings.Repeat("j", 64), Vout: 0}.String(), Kind: "output", Outpoint: model.Outpoint{TxID: strings.Repeat("j", 64), Vout: 0}.String(), Address: "bc1qchangelabel", ValueSats: 500},
				{ID: "tx:" + strings.Repeat("k", 64), Kind: "transaction", TxID: strings.Repeat("k", 64), Label: "exchange a", BlockTime: "2026-10-01T00:00:00Z"},
				{ID: "tx:" + strings.Repeat("m", 64), Kind: "transaction", TxID: strings.Repeat("m", 64), Label: "exchange a", BlockTime: "2026-11-01T00:00:00Z"},
				{ID: "out:" + model.Outpoint{TxID: strings.Repeat("n", 64), Vout: 0}.String(), Kind: "output", Outpoint: model.Outpoint{TxID: strings.Repeat("n", 64), Vout: 0}.String(), Address: "bc1qexchange3", ValueSats: 1000},
				{ID: "out:" + model.Outpoint{TxID: strings.Repeat("l", 64), Vout: 0}.String(), Kind: "output", Outpoint: model.Outpoint{TxID: strings.Repeat("l", 64), Vout: 0}.String(), ValueSats: 1},
			},
			Edges: []model.GraphEdge{
				{From: "out:" + model.Outpoint{TxID: target, Vout: 0}.String(), To: "tx:" + funding, Kind: "funded_by"},
				{From: "tx:" + funding, To: "out:" + childOut.String(), Kind: "input"},
				{From: "out:" + childOut.String(), To: "tx:" + policy, Kind: "funded_by"},
				{From: "out:" + changeOut.String(), To: "tx:" + outflow, Kind: "funded_by"},
				{From: "out:" + sentOut.String(), To: "tx:" + outflow, Kind: "funded_by"},
				{From: "out:missing:0", To: "tx:" + policy, Kind: "funded_by"},
				{From: "out:" + model.Outpoint{TxID: strings.Repeat("5", 64), Vout: 0}.String(), To: "tx:" + strings.Repeat("6", 64), Kind: "funded_by"},
				{From: "out:" + model.Outpoint{TxID: strings.Repeat("i", 64), Vout: 0}.String(), To: "tx:" + strings.Repeat("g", 64), Kind: "funded_by"},
				{From: "out:" + model.Outpoint{TxID: strings.Repeat("j", 64), Vout: 0}.String(), To: "tx:" + strings.Repeat("g", 64), Kind: "funded_by"},
				{From: "out:" + model.Outpoint{TxID: strings.Repeat("l", 64), Vout: 0}.String(), To: "tx:" + strings.Repeat("k", 64), Kind: "funded_by"},
				{From: "out:" + model.Outpoint{TxID: strings.Repeat("n", 64), Vout: 0}.String(), To: "tx:" + strings.Repeat("m", 64), Kind: "funded_by"},
			},
		},
		Annotations: model.AnnotationFile{
			Outpoints: map[string]model.Annotation{
				changeOut.String(): {Label: "exchange a (change)"},
				model.Outpoint{TxID: strings.Repeat("l", 64), Vout: 0}.String(): {Label: "exchange a (change)"},
			},
			Addresses: map[string]model.Annotation{"bc1qchangelabel": {Label: "exchange a (change)"}},
		},
	}
}

func TestExchangeGapsDetectsInboundPayouts(t *testing.T) {
	report := gapTestReport()
	gaps := ExchangeGaps(report)
	if len(gaps) < 3 {
		t.Fatalf("gaps = %d, want at least 3: %+v", len(gaps), gaps)
	}
	byKind := map[string]ExchangeGap{}
	for _, gap := range gaps {
		byKind[gap.TxID] = gap
	}
	policyGap, ok := byKind[strings.Repeat("b", 64)]
	if !ok {
		t.Fatal("policy boundary payout missing")
	}
	if policyGap.AmountSats != 1000 || policyGap.DateUTC != "2024-08-03" || len(policyGap.Targets) != 1 {
		t.Errorf("policy gap = %+v", policyGap)
	}
	outputGap, ok := byKind[strings.Repeat("9", 64)]
	if !ok || outputGap.AmountSats != 200 || outputGap.TxID != strings.Repeat("9", 64) {
		t.Errorf("output boundary gap = %+v", outputGap)
	}
	if empty := ExchangeGaps(model.Report{}); len(empty) != 0 {
		t.Errorf("empty report gaps = %d", len(empty))
	}
	if ExchangeGapMonths(report) != 2 {
		t.Errorf("gap months = %d, want 2", ExchangeGapMonths(report))
	}
	if err := GapCheck(report); err == nil || !strings.Contains(err.Error(), "documentation incomplete") {
		t.Errorf("GapCheck = %v", err)
	}
	if err := GapCheck(model.Report{}); err != nil {
		t.Errorf("empty GapCheck = %v", err)
	}
	if err := GapCheckAll(report); err == nil || !strings.Contains(err.Error(), "outbound transfer") {
		t.Errorf("GapCheckAll = %v", err)
	}
	if err := GapCheckAll(model.Report{}); err != nil {
		t.Errorf("empty GapCheckAll = %v", err)
	}
	outboundOnly := model.Report{Graph: model.Graph{
		Nodes: []model.GraphNode{
			{ID: "tx:" + strings.Repeat("d", 64), Kind: "transaction", TxID: strings.Repeat("d", 64), Label: "exchange a"},
			{ID: "out:" + strings.Repeat("d", 64) + ":0", Kind: "output", Outpoint: strings.Repeat("d", 64) + ":0", ValueSats: 100},
		},
		Edges: []model.GraphEdge{{From: "out:" + strings.Repeat("d", 64) + ":0", To: "tx:" + strings.Repeat("d", 64), Kind: "funded_by"}},
	}}
	if err := GapCheckAll(outboundOnly); err == nil {
		t.Error("expected outbound-only failure")
	}
	_ = ExchangeOutflowMonths(report)
}

func TestExchangeOutflowsDetectsProviderTransfers(t *testing.T) {
	report := gapTestReport()
	outflows := ExchangeOutflows(report)
	if len(outflows) != 3 {
		t.Fatalf("outflows = %d, want 3: %+v", len(outflows), outflows)
	}
	if outflows[0].Direction != "outflow" {
		t.Errorf("outflow = %+v", outflows[0])
	}
	t.Setenv("SOF_EXCHANGE_LABELS", "acme")
	if filtered := ExchangeOutflows(report); len(filtered) != 0 {
		t.Errorf("custom provider list returned %d rows", len(filtered))
	}
	t.Setenv("SOF_EXCHANGE_LABELS", " , exchange a , ")
	if filtered := ExchangeOutflows(report); len(filtered) != 3 {
		t.Errorf("custom exchange a provider returned %d rows", len(filtered))
	}
}

func TestExchangeGapCSVs(t *testing.T) {
	report := gapTestReport()
	gapsCSV, err := ExchangeGapsCSVBytes(report)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(gapsCSV))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(ExchangeGaps(report))+1 || rows[0][0] != "direction" || rows[0][10] != "evidence_status" {
		t.Fatalf("gap CSV = %v", rows)
	}
	monthsCSV, err := ExchangeGapMonthsCSVBytes(report)
	if err != nil {
		t.Fatal(err)
	}
	monthRows, err := csv.NewReader(strings.NewReader(string(monthsCSV))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(monthRows) < 2 || monthRows[0][0] != "direction" {
		t.Fatalf("gap month CSV = %v", monthRows)
	}
	if _, err := ExchangeOutflowsCSVBytes(report); err != nil {
		t.Fatal(err)
	}
	if _, err := ExchangeOutflowMonthsCSVBytes(report); err != nil {
		t.Fatal(err)
	}
	undated := ExchangeGap{DateUTC: ""}
	if rows := exchangeMonths([]ExchangeGap{undated}); rows[0].key != "undated" {
		t.Errorf("undated month = %+v", rows)
	}
}

func TestExchangeGapWriterErrors(t *testing.T) {
	rows := ExchangeGaps(gapTestReport())
	for _, test := range []struct {
		name   string
		writer *purchaseCSVWriter
	}{
		{"gaps write", &purchaseCSVWriter{failOn: 0, writeErr: errors.New("write failed")}},
		{"gaps row", &purchaseCSVWriter{failOn: 1, writeErr: errors.New("row failed")}},
		{"gaps flush", &purchaseCSVWriter{failOn: 100, flushErr: errors.New("flush failed")}},
	} {
		if err := writeExchangeGapsCSV(test.writer, rows); err == nil {
			t.Errorf("%s was accepted", test.name)
		}
	}
	for _, test := range []struct {
		name   string
		writer *purchaseCSVWriter
	}{
		{"months write", &purchaseCSVWriter{failOn: 0, writeErr: errors.New("write failed")}},
		{"months row", &purchaseCSVWriter{failOn: 1, writeErr: errors.New("row failed")}},
		{"months flush", &purchaseCSVWriter{failOn: 100, flushErr: errors.New("flush failed")}},
	} {
		if err := writeExchangeMonthsCSV(test.writer, rows); err == nil {
			t.Errorf("%s was accepted", test.name)
		}
	}
}
