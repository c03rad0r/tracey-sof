package report

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracey-sof/internal/bank"
	"tracey-sof/internal/model"
)

func overviewTestReport() model.Report {
	policy := strings.Repeat("b", 64)
	funding := strings.Repeat("c", 64)
	purchaseTx := strings.Repeat("a", 64)
	amount := int64(1000)
	return model.Report{
		GeneratedAt: "2026-11-15T00:00:00Z",
		Chain:       model.Chain{BestBlock: strings.Repeat("0", 64), BestHeight: 900},
		Summary: model.Summary{TotalUTXOs: 1, TotalSats: 1000, DocumentedUTXOs: 1,
			IncompleteUTXOs: 0, ReviewUTXOs: 0},
		UTXOs: []model.Target{{TxID: funding, Vout: 0, Address: "bc1qtarget", ValueSats: 1000,
			Status: "documented", Trace: "complete"}},
		Purchases: []model.Purchase{{ID: "p1", Date: "2023-01-01T00:00:00Z", Height: 50,
			Provider: "Example Exchange", TxIDs: []string{purchaseTx},
			Outpoints:  []model.Outpoint{{TxID: purchaseTx, Vout: 1}},
			AmountSats: &amount, Fiat: &model.Fiat{Amount: "500.00", Currency: "EUR"}}},
		Graph: model.Graph{
			Nodes: []model.GraphNode{
				{ID: "tx:" + policy, Kind: "transaction", TxID: policy, TerminalReason: "policy_output_count",
					BlockTime: "2024-01-01T00:00:00Z", Height: 100},
				{ID: "out:" + policy + ":1", Kind: "output", Outpoint: policy + ":1", ValueSats: 400,
					BlockTime: "2024-01-01T00:00:00Z", Height: 100},
				{ID: "tx:" + funding, Kind: "transaction", TxID: funding, Label: "recent",
					BlockTime: "2025-01-01T00:00:00Z", Height: 900},
				{ID: "out:" + funding + ":0", Kind: "output", Outpoint: funding + ":0", Address: "bc1qtarget",
					ValueSats: 1000, BlockTime: "2025-01-01T00:00:00Z", Height: 900, Owned: true},
				// A purchase output is shown as the purchase row below, not twice.
				{ID: "out:" + purchaseTx + ":1", Kind: "output", Outpoint: purchaseTx + ":1",
					ValueSats: 1000, Height: 50, PurchaseIDs: []string{"p1"}},
				{ID: "tx:" + strings.Repeat("d", 64), Kind: "transaction", TxID: strings.Repeat("d", 64),
					TerminalReason: "max_depth"},
			},
			Edges: []model.GraphEdge{{From: "out:" + policy + ":1", To: "tx:" + policy, Kind: "funded_by"}},
		},
	}
}

func TestOverviewHTMLGapsFirstAndNewestBlocksFirst(t *testing.T) {
	report := overviewTestReport()
	cache := bank.Cache{Records: []bank.MatchRecord{{Kind: "inbound_payout", ID: strings.Repeat("b", 64) + ":" + strings.Repeat("b", 64) + ":1", Status: "missing"}}}
	text := string(overviewHTML(report, cache))
	gapsAt := strings.Index(text, "Evidence gaps to resolve first")
	chronologyAt := strings.Index(text, "Chronology")
	recentAt := strings.Index(text, "block 900")
	olderAt := strings.Index(text, "block 100")
	sourceAt := strings.Index(text, "block 50")
	undatedAt := strings.Index(text, "undated")
	if gapsAt < 0 || chronologyAt < 0 || !(gapsAt < chronologyAt) {
		t.Fatalf("gaps section not first: %d/%d", gapsAt, chronologyAt)
	}
	if recentAt < 0 || olderAt < 0 || sourceAt < 0 || undatedAt < 0 || !(recentAt < olderAt && olderAt < sourceAt && sourceAt < undatedAt) {
		t.Fatalf("chronology order wrong: %d %d %d %d", recentAt, olderAt, sourceAt, undatedAt)
	}
	for _, want := range []string{"Inbound exchange payouts", strings.Repeat("b", 64), "Example Exchange", "500.00 EUR", "Current UTXOs", "bc1qtarget"} {
		if !strings.Contains(text, want) {
			t.Errorf("overview missing %q", want)
		}
	}
	purchaseRow := shortID(strings.Repeat("a", 64) + ":1")
	if count := strings.Count(text, purchaseRow); count != 1 {
		t.Errorf("purchase output rendered %d times, want once", count)
	}
	if err := WriteOverview(report, bank.Cache{}, t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

func TestOverviewEventsHeightFallbackAndTieBreak(t *testing.T) {
	purchaseTx := strings.Repeat("a", 64)
	report := overviewTestReport()
	report.Graph.Nodes = append(report.Graph.Nodes, model.GraphNode{
		ID: "out:" + purchaseTx + ":1", Kind: "output", Outpoint: purchaseTx + ":1", ValueSats: 1000,
	})
	events := overviewEvents(report)
	found := false
	for _, event := range events {
		if event.outpoint == purchaseTx+":1" {
			found = true
			if event.height != 50 {
				t.Errorf("output height = %d, want the purchase height 50", event.height)
			}
		}
	}
	if !found {
		t.Fatal("output row without purchase IDs is missing")
	}

	first, second := strings.Repeat("e", 64), strings.Repeat("f", 64)
	tie := model.Report{Graph: model.Graph{Nodes: []model.GraphNode{
		{ID: "tx:" + second, Kind: "transaction", TxID: second, Height: 700},
		{ID: "tx:" + first, Kind: "transaction", TxID: first, Height: 700},
	}}}
	ordered := overviewEvents(tie)
	if len(ordered) != 2 || ordered[0].id != first || ordered[1].id != second {
		t.Fatalf("same-height ordering = %+v", ordered)
	}
}

func TestOverviewNoGapsAndUndatedFallback(t *testing.T) {
	empty := model.Report{GeneratedAt: "2026-11-15T00:00:00Z",
		Chain: model.Chain{BestBlock: strings.Repeat("0", 64), BestHeight: 900}}
	text := string(overviewHTML(empty, bank.Cache{}))
	if !strings.Contains(text, "No exchange gaps remain.") {
		t.Error("empty report should not request exchange evidence")
	}
	if strings.Contains(text, "Inbound exchange payouts") || strings.Contains(text, "Outbound exchange transfers") {
		t.Error("empty report rendered a gap table")
	}

	report := overviewTestReport()
	report.Purchases = append(report.Purchases, model.Purchase{
		ID: "p2", Provider: "Example Exchange", Notes: "moved to cold storage",
	})
	text = string(overviewHTML(report, bank.Cache{}))
	// No txid and no outpoint: the row falls back to the purchase ID.
	if !strings.Contains(text, ">p2<") {
		t.Error("undated purchase row is missing or has no identifier")
	}
	if !strings.Contains(text, "Example Exchange · moved to cold storage") {
		t.Error("provider and label were not combined")
	}
}

func TestOverviewOutboundOnlyTable(t *testing.T) {
	txid, funding := strings.Repeat("e", 64), strings.Repeat("f", 64)
	report := model.Report{GeneratedAt: "2026-11-15T00:00:00Z",
		Chain: model.Chain{BestBlock: strings.Repeat("0", 64), BestHeight: 900}}
	report.Graph.Nodes = []model.GraphNode{
		{ID: "tx:" + txid, Kind: "transaction", TxID: txid, Label: "exchange a sale",
			BlockTime: "2026-08-15T12:00:00Z", Height: 958000},
		{ID: "out:" + funding + ":0", Kind: "output", Outpoint: funding + ":0", Address: "bc1qsale",
			ValueSats: 5000, BlockTime: "2026-08-15T12:00:00Z", Height: 958000},
	}
	report.Graph.Edges = []model.GraphEdge{{From: "out:" + funding + ":0", To: "tx:" + txid, Kind: "funded_by"}}
	text := string(overviewHTML(report, bank.Cache{}))
	if !strings.Contains(text, "Outbound exchange transfers") {
		t.Fatal("outbound table missing")
	}
	if strings.Contains(text, "Inbound exchange payouts") {
		t.Error("inbound table rendered without inbound gaps")
	}
	for _, want := range []string{"bc1qsale", "exchange a sale", "958,000"} {
		if !strings.Contains(text, want) {
			t.Errorf("outbound overview missing %q", want)
		}
	}
}

func TestWriteOverviewWriteError(t *testing.T) {
	blocked := filepath.Join(t.TempDir(), "out-file")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := WriteOverview(overviewTestReport(), bank.Cache{}, blocked); err == nil {
		t.Fatal("overview below a file was accepted")
	}
}
