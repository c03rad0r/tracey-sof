package provenance

import (
	"context"
	"math"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"tracey-sof/internal/model"
)

func TestEvaluateDirectPurchaseBuildsCompleteReport(t *testing.T) {
	txid := testTxID("a")
	outpoint := testOutpoint(txid, 4)
	target := model.TargetRef{
		TxID: txid, Vout: 4, Address: "owned-address", ValueSats: 12_345,
		OwnerIDs: []string{"owner-b", "owner-a"}, Labels: []string{"cold"},
	}
	evidence := mustEvidence(t, model.PurchaseFile{Purchases: []model.Purchase{
		{ID: "purchase-z", Outpoints: []model.Outpoint{outpoint}},
		{ID: "purchase-a", Outpoints: []model.Outpoint{outpoint}},
	}}, model.AnnotationFile{})
	trace, err := Trace(context.Background(), newMemorySource(), []model.TargetRef{target}, evidence, model.Chain{BestBlock: "tip", BestHeight: 900_000}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	trace.Network = "mainnet"
	amount := int64(12_345)
	purchases := []model.Purchase{{ID: "purchase-a", AmountSats: &amount, Fiat: &model.Fiat{Amount: "100.00", Currency: "USD"}}}
	annotations := model.AnnotationFile{
		SchemaVersion: model.SchemaVersion,
		Outpoints:     map[string]model.Annotation{outpoint.String(): {Label: "exact-output-label", Note: "evidence checked"}},
		Addresses:     map[string]model.Annotation{"owned-address": {Label: "address-label"}},
	}
	generated := time.Date(2026, time.November, 10, 12, 34, 56, 987_654_321, time.FixedZone("test", -7*60*60))
	report, err := Evaluate(trace, purchases, annotations, generated)
	if err != nil {
		t.Fatal(err)
	}
	if report.SchemaVersion != model.SchemaVersion || report.Network != "mainnet" || report.GeneratedAt != "2026-11-10T19:34:56Z" {
		t.Fatalf("report metadata = %+v", report)
	}
	if report.Chain != (model.Chain{BestBlock: "tip", BestHeight: 900_000}) {
		t.Fatalf("report chain = %+v", report.Chain)
	}
	if !reflect.DeepEqual(report.Purchases, purchases) || !reflect.DeepEqual(report.Annotations, annotations) {
		t.Fatal("report did not preserve purchase and annotation files")
	}
	if report.Warnings == nil || len(report.Warnings) != 0 {
		t.Fatalf("purchase-only report warnings = %#v", report.Warnings)
	}
	if len(report.UTXOs) != 1 {
		t.Fatalf("UTXOs = %+v", report.UTXOs)
	}
	utxo := report.UTXOs[0]
	if utxo.Outpoint != outpoint || utxo.Key() != outpoint.String() || utxo.Address != target.Address || utxo.ValueSats != target.ValueSats || !reflect.DeepEqual(utxo.OwnerIDs, target.OwnerIDs) || !reflect.DeepEqual(utxo.Labels, target.Labels) {
		t.Fatalf("target values not preserved: %+v", utxo)
	}
	if utxo.Status != documentedStatus || utxo.Trace != completeTraceStatus || !reflect.DeepEqual(utxo.PurchaseIDs, []string{"purchase-a", "purchase-z"}) {
		t.Fatalf("target classification = %+v", utxo)
	}
	wantSummary := model.Summary{TotalUTXOs: 1, TotalSats: 12_345, DocumentedUTXOs: 1, DocumentedSats: 12_345}
	if report.Summary != wantSummary {
		t.Fatalf("summary = %+v, want %+v", report.Summary, wantSummary)
	}
	if len(report.Graph.Nodes) != 1 || len(report.Graph.Edges) != 0 {
		t.Fatalf("direct graph = %+v", report.Graph)
	}
	node := report.Graph.Nodes[0]
	if node.ID != outpointNodeID(outpoint) || node.Kind != "output" || node.Outpoint != outpoint.String() || node.Address != target.Address || node.ValueSats != target.ValueSats || !node.Owned || node.Label != "exact-output-label" || node.TerminalReason != reasonPurchase || node.TerminalDetail == "" || !reflect.DeepEqual(node.PurchaseIDs, []string{"purchase-a", "purchase-z"}) {
		t.Fatalf("direct output node = %+v", node)
	}
}

func TestEvaluateGraphMetadataAndStableOrdering(t *testing.T) {
	targetTx := testTxID("a")
	parentTx := testTxID("b")
	targetOutput := model.Output{Index: 0, ValueSats: 2_000, ScriptType: "raw-target-type", Address: "raw-target-address"}
	siblingOutput := model.Output{Index: 1, ValueSats: 75, ScriptType: "p2sh", Address: "sibling-address"}
	parentOutput := model.Output{Index: 0, ValueSats: 2_100, ScriptType: "p2wpkh", Address: "purchase-address"}
	target := testTransaction(targetTx, []model.Input{testInput(parentTx, 0)}, siblingOutput, targetOutput)
	parent := testTransaction(parentTx, []model.Input{testInput(testTxID("c"), 0)}, parentOutput)
	source := newMemorySource(target, parent)
	evidence := mustEvidence(t, model.PurchaseFile{Purchases: []model.Purchase{{ID: "address-buy", Addresses: []string{"purchase-address"}}}}, model.AnnotationFile{})
	targetRef := testTarget(targetTx, 0)
	targetRef.Address = "target-address"
	targetRef.ValueSats = 2_000
	trace, err := Trace(context.Background(), source, []model.TargetRef{targetRef}, evidence, model.Chain{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	annotations := model.AnnotationFile{
		Transactions: map[string]model.Annotation{targetTx: {Label: "funding-transaction"}},
		Addresses: map[string]model.Annotation{
			"purchase-address": {Label: "address-label"},
			"target-address":   {Label: "target-label"},
		},
		Outpoints: map[string]model.Annotation{testOutpoint(parentTx, 0).String(): {Label: "outpoint-label"}},
	}
	report, err := Evaluate(trace, nil, annotations, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if report.UTXOs[0].Status != documentedStatus || report.UTXOs[0].Trace != completeTraceStatus {
		t.Fatalf("address-only evaluation = %+v", report.UTXOs[0])
	}
	if len(report.Graph.Nodes) != 5 || len(report.Graph.Edges) != 4 {
		t.Fatalf("graph sizes: nodes=%d edges=%d, graph=%+v", len(report.Graph.Nodes), len(report.Graph.Edges), report.Graph)
	}
	if !sort.SliceIsSorted(report.Graph.Nodes, func(i, j int) bool { return report.Graph.Nodes[i].ID < report.Graph.Nodes[j].ID }) {
		t.Fatalf("nodes are not sorted: %v", report.Graph.Nodes)
	}
	nodeByID := make(map[string]model.GraphNode)
	for _, node := range report.Graph.Nodes {
		nodeByID[node.ID] = node
	}
	targetNode := nodeByID[outpointNodeID(testOutpoint(targetTx, 0))]
	if !targetNode.Owned || targetNode.ValueSats != 2_000 || targetNode.Address != "target-address" || targetNode.ScriptType != "raw-target-type" || targetNode.Label != "target-label" {
		t.Fatalf("target graph node = %+v", targetNode)
	}
	parentNode := nodeByID[outpointNodeID(testOutpoint(parentTx, 0))]
	if parentNode.ValueSats != 2_100 || parentNode.ScriptType != "p2wpkh" || parentNode.Label != "outpoint-label" || parentNode.TerminalReason != reasonPurchase {
		t.Fatalf("parent graph node = %+v", parentNode)
	}
	if sibling := nodeByID[outpointNodeID(testOutpoint(targetTx, 1))]; sibling.ValueSats != 75 || sibling.Address != "sibling-address" || sibling.Owned {
		t.Fatalf("sibling graph node = %+v", sibling)
	}
	if txNode := nodeByID[transactionNodeID(targetTx)]; txNode.Kind != "transaction" || txNode.TxID != targetTx || txNode.Label != "funding-transaction" {
		t.Fatalf("transaction node = %+v", txNode)
	}
	for _, edge := range report.Graph.Edges {
		switch edge.Kind {
		case "funded_by":
			if edge.ValueSats != nodeByID[edge.From].ValueSats {
				t.Fatalf("funded_by edge value = %+v", edge)
			}
		case "input":
			if edge.ValueSats != nodeByID[edge.To].ValueSats {
				t.Fatalf("input edge value = %+v", edge)
			}
		default:
			t.Fatalf("unexpected edge kind: %+v", edge)
		}
	}
	second, err := Evaluate(trace, nil, annotations, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(report.Graph, second.Graph) || !reflect.DeepEqual(report.Warnings, second.Warnings) {
		t.Fatal("graph output is not deterministic")
	}
}

func TestEvaluateDocumentationAndTraceStatuses(t *testing.T) {
	t.Run("purchase and coinbase is partially documented but complete", func(t *testing.T) {
		targetTx, purchaseTx, coinbaseTx := testTxID("a"), testTxID("b"), testTxID("c")
		source := newMemorySource(
			testTransaction(targetTx, []model.Input{testInput(purchaseTx, 0), testInput(coinbaseTx, 0)}),
			testTransaction(coinbaseTx, []model.Input{{Coinbase: true}}),
		)
		evidence := mustEvidence(t, model.PurchaseFile{Purchases: []model.Purchase{{ID: "buy", Outpoints: []model.Outpoint{testOutpoint(purchaseTx, 0)}}}}, model.AnnotationFile{})
		trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(targetTx, 0)}, evidence, model.Chain{}, Limits{})
		if err != nil {
			t.Fatal(err)
		}
		report, err := Evaluate(trace, nil, model.AnnotationFile{}, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		utxo := report.UTXOs[0]
		if utxo.Status != partiallyDocumentedStatus || utxo.Trace != completeTraceStatus || !reflect.DeepEqual(utxo.PurchaseIDs, []string{"buy"}) {
			t.Fatalf("classification = %+v", utxo)
		}
		if report.Summary.PartialUTXOs != 1 || report.Summary.DocumentedSats != 0 || report.Summary.IncompleteUTXOs != 0 {
			t.Fatalf("summary = %+v", report.Summary)
		}
		if len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "non-purchase terminal") || !strings.Contains(report.Warnings[0], reasonCoinbase) {
			t.Fatalf("coinbase warning = %v", report.Warnings)
		}
	})
	t.Run("purchase and unavailable input is partial and incomplete", func(t *testing.T) {
		targetTx, purchaseTx, missingTx := testTxID("a"), testTxID("b"), testTxID("c")
		source := newMemorySource(testTransaction(targetTx, []model.Input{testInput(purchaseTx, 0), testInput(missingTx, 0)}))
		evidence := mustEvidence(t, model.PurchaseFile{Purchases: []model.Purchase{{ID: "buy", Outpoints: []model.Outpoint{testOutpoint(purchaseTx, 0)}}}}, model.AnnotationFile{})
		trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(targetTx, 0)}, evidence, model.Chain{}, Limits{})
		if err != nil {
			t.Fatal(err)
		}
		report, err := Evaluate(trace, nil, model.AnnotationFile{}, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		utxo := report.UTXOs[0]
		if utxo.Status != partiallyDocumentedStatus || utxo.Trace != incompleteTraceStatus || len(report.Warnings) != 1 {
			t.Fatalf("classification = %+v warnings=%v", utxo, report.Warnings)
		}
		if report.Summary.PartialUTXOs != 1 || report.Summary.IncompleteUTXOs != 1 || report.Summary.DocumentedUTXOs != 0 {
			t.Fatalf("summary = %+v", report.Summary)
		}
	})
	t.Run("coinbase only is undocumented and complete", func(t *testing.T) {
		targetTx, coinbaseTx := testTxID("a"), testTxID("b")
		source := newMemorySource(
			testTransaction(targetTx, []model.Input{testInput(coinbaseTx, 0)}),
			testTransaction(coinbaseTx, nil),
		)
		transactions := source.transactions[coinbaseTx]
		transactions.Inputs = []model.Input{{Coinbase: true}}
		source.transactions[coinbaseTx] = transactions
		trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(targetTx, 0)}, nil, model.Chain{}, Limits{})
		if err != nil {
			t.Fatal(err)
		}
		report, err := Evaluate(trace, nil, model.AnnotationFile{}, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if report.UTXOs[0].Status != undocumentedStatus || report.UTXOs[0].Trace != completeTraceStatus || report.UTXOs[0].PurchaseIDs != nil {
			t.Fatalf("classification = %+v", report.UTXOs[0])
		}
		if report.Summary.UndocumentedUTXOs != 1 || report.Summary.DocumentedSats != 0 {
			t.Fatalf("summary = %+v", report.Summary)
		}
	})
}

func TestEvaluateTargetsAreIndependentAndSorted(t *testing.T) {
	directTx, mixedTx, purchaseTx, missingTx := testTxID("d"), testTxID("a"), testTxID("b"), testTxID("c")
	directOutpoint := testOutpoint(directTx, 0)
	purchaseOutpoint := testOutpoint(purchaseTx, 0)
	evidence := mustEvidence(t, model.PurchaseFile{Purchases: []model.Purchase{
		{ID: "direct", Outpoints: []model.Outpoint{directOutpoint}},
		{ID: "ancestor", Outpoints: []model.Outpoint{purchaseOutpoint}},
	}}, model.AnnotationFile{})
	source := newMemorySource(testTransaction(mixedTx, []model.Input{testInput(purchaseTx, 0), testInput(missingTx, 0)}))
	targets := []model.TargetRef{testTarget(directTx, 0), testTarget(mixedTx, 0)}
	trace, err := Trace(context.Background(), source, targets, evidence, model.Chain{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if !trace.Incomplete {
		t.Fatal("test trace should be globally incomplete")
	}
	report, err := Evaluate(trace, nil, model.AnnotationFile{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.UTXOs) != 2 || report.UTXOs[0].TxID != mixedTx || report.UTXOs[1].TxID != directTx {
		t.Fatalf("UTXOs not stably sorted: %+v", report.UTXOs)
	}
	for _, utxo := range report.UTXOs {
		switch utxo.TxID {
		case directTx:
			if utxo.Status != documentedStatus || utxo.Trace != completeTraceStatus {
				t.Fatalf("global incompleteness contaminated direct target: %+v", utxo)
			}
		case mixedTx:
			if utxo.Status != partiallyDocumentedStatus || utxo.Trace != incompleteTraceStatus {
				t.Fatalf("mixed target = %+v", utxo)
			}
		}
	}
	if report.Summary.TotalUTXOs != 2 || report.Summary.TotalSats != 2_000 || report.Summary.DocumentedUTXOs != 1 || report.Summary.DocumentedSats != 1_000 || report.Summary.PartialUTXOs != 1 || report.Summary.IncompleteUTXOs != 1 {
		t.Fatalf("aggregate summary = %+v", report.Summary)
	}
}

func TestEvaluateCyclesAreIncompleteWithoutLooping(t *testing.T) {
	txA, txB, purchaseTx := testTxID("a"), testTxID("b"), testTxID("c")
	for _, withPurchase := range []bool{false, true} {
		t.Run(map[bool]string{false: "no purchase", true: "purchase on another branch"}[withPurchase], func(t *testing.T) {
			txAInputs := []model.Input{testInput(txB, 0)}
			edges := []model.TraceEdge{
				{SpendingTxID: txA, InputIndex: 0, PrevOut: testOutpoint(txB, 0)},
				{SpendingTxID: txB, InputIndex: 0, PrevOut: testOutpoint(txA, 0)},
			}
			boundaries := make([]model.Boundary, 0)
			if withPurchase {
				txAInputs = append(txAInputs, testInput(purchaseTx, 0))
				edges = append(edges, model.TraceEdge{SpendingTxID: txA, InputIndex: 1, PrevOut: testOutpoint(purchaseTx, 0)})
				boundaries = append(boundaries, model.Boundary{NodeID: outpointNodeID(testOutpoint(purchaseTx, 0)), Reason: reasonPurchase, Outpoint: testOutpoint(purchaseTx, 0).String(), PurchaseIDs: []string{"buy"}})
			}
			trace := model.Trace{
				SchemaVersion: model.SchemaVersion,
				Targets:       []model.TargetRef{testTarget(txA, 0)},
				Transactions: map[string]model.Transaction{
					txA: testTransaction(txA, txAInputs),
					txB: testTransaction(txB, []model.Input{testInput(txA, 0)}),
				},
				Edges: edges, Boundaries: boundaries,
			}
			report, err := Evaluate(trace, nil, model.AnnotationFile{}, time.Time{})
			if err != nil {
				t.Fatal(err)
			}
			utxo := report.UTXOs[0]
			wantStatus := undocumentedStatus
			if withPurchase {
				wantStatus = partiallyDocumentedStatus
			}
			if utxo.Status != wantStatus || utxo.Trace != incompleteTraceStatus {
				t.Fatalf("cycle classification = %+v", utxo)
			}
			foundCycleWarning := false
			for _, warning := range report.Warnings {
				foundCycleWarning = foundCycleWarning || strings.Contains(warning, "cycle detected")
			}
			if !foundCycleWarning {
				t.Fatalf("cycle warning missing: %v", report.Warnings)
			}
		})
	}
}

func validEvaluationTrace() model.Trace {
	txA, txB := testTxID("a"), testTxID("b")
	return model.Trace{
		SchemaVersion: model.SchemaVersion,
		Network:       "mainnet",
		Targets:       []model.TargetRef{testTarget(txA, 0)},
		Transactions: map[string]model.Transaction{
			txA: testTransaction(txA, []model.Input{testInput(txB, 0)}),
			txB: testTransaction(txB, []model.Input{{Coinbase: true}}),
		},
		Edges: []model.TraceEdge{{SpendingTxID: txA, InputIndex: 0, PrevOut: testOutpoint(txB, 0)}},
		Boundaries: []model.Boundary{{
			NodeID: transactionNodeID(txB), Reason: reasonCoinbase, Transaction: txB, Detail: "origin",
		}},
	}
}

func TestEvaluateRejectsMalformedTraces(t *testing.T) {
	txA, txB, txC := testTxID("a"), testTxID("b"), testTxID("c")
	tests := []struct {
		name   string
		mutate func(*model.Trace)
	}{
		{name: "transaction map mismatch", mutate: func(trace *model.Trace) {
			transaction := trace.Transactions[txA]
			transaction.TxID = txC
			trace.Transactions[txA] = transaction
		}},
		{name: "duplicate output", mutate: func(trace *model.Trace) {
			transaction := trace.Transactions[txA]
			transaction.Outputs = append(transaction.Outputs, model.Output{Index: 0})
			trace.Transactions[txA] = transaction
		}},
		{name: "negative output value", mutate: func(trace *model.Trace) {
			transaction := trace.Transactions[txA]
			transaction.Outputs[0].ValueSats = -1
			trace.Transactions[txA] = transaction
		}},
		{name: "missing spending transaction", mutate: func(trace *model.Trace) { trace.Edges[0].SpendingTxID = txC }},
		{name: "negative input index", mutate: func(trace *model.Trace) { trace.Edges[0].InputIndex = -1 }},
		{name: "input index out of range", mutate: func(trace *model.Trace) { trace.Edges[0].InputIndex = 10 }},
		{name: "edge mismatches input", mutate: func(trace *model.Trace) { trace.Edges[0].PrevOut = testOutpoint(txC, 0) }},
		{name: "coinbase input edge", mutate: func(trace *model.Trace) {
			trace.Edges = append(trace.Edges, model.TraceEdge{SpendingTxID: txB, InputIndex: 0, PrevOut: testOutpoint(txC, 0)})
		}},
		{name: "duplicate trace edge", mutate: func(trace *model.Trace) { trace.Edges = append(trace.Edges, trace.Edges[0]) }},
		{name: "empty target txid", mutate: func(trace *model.Trace) { trace.Targets[0].TxID = "" }},
		{name: "negative target value", mutate: func(trace *model.Trace) { trace.Targets[0].ValueSats = -1 }},
		{name: "reachable output missing funding", mutate: func(trace *model.Trace) { delete(trace.Transactions, txB) }},
		{name: "reachable transaction without origin boundary", mutate: func(trace *model.Trace) { trace.Boundaries = nil }},
		{name: "missing required transaction edge", mutate: func(trace *model.Trace) { trace.Edges = nil }},
		{name: "duplicate boundary", mutate: func(trace *model.Trace) { trace.Boundaries = append(trace.Boundaries, trace.Boundaries[0]) }},
		{name: "empty boundary node", mutate: func(trace *model.Trace) { trace.Boundaries[0].NodeID = "" }},
		{name: "empty boundary reason", mutate: func(trace *model.Trace) { trace.Boundaries[0].Reason = "" }},
		{name: "inconsistent boundary transaction", mutate: func(trace *model.Trace) { trace.Boundaries[0].Transaction = txC }},
		{name: "invalid boundary node", mutate: func(trace *model.Trace) {
			trace.Boundaries = append(trace.Boundaries, model.Boundary{NodeID: "invalid", Reason: reasonMaxDepth})
		}},
		{name: "invalid boundary outpoint", mutate: func(trace *model.Trace) {
			trace.Boundaries = append(trace.Boundaries, model.Boundary{NodeID: "out:no-index", Reason: reasonMaxDepth})
		}},
		{name: "noncanonical boundary outpoint", mutate: func(trace *model.Trace) {
			trace.Boundaries = append(trace.Boundaries, model.Boundary{NodeID: "out:" + txC + ":00", Reason: reasonMaxDepth})
		}},
		{name: "inconsistent boundary outpoint", mutate: func(trace *model.Trace) {
			trace.Boundaries = append(trace.Boundaries, model.Boundary{NodeID: "out:" + txC + ":0", Outpoint: txC + ":1", Reason: reasonMaxDepth})
		}},
		{name: "purchase without IDs", mutate: func(trace *model.Trace) {
			trace.Boundaries = append(trace.Boundaries, model.Boundary{NodeID: "out:" + txC + ":0", Reason: reasonPurchase})
		}},
		{name: "blank purchase ID", mutate: func(trace *model.Trace) {
			trace.Boundaries = append(trace.Boundaries, model.Boundary{NodeID: "out:" + txC + ":0", Reason: reasonPurchase, PurchaseIDs: []string{""}})
		}},
		{name: "nonpurchase with purchase IDs", mutate: func(trace *model.Trace) { trace.Boundaries[0].PurchaseIDs = []string{"wrong"} }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			trace := validEvaluationTrace()
			test.mutate(&trace)
			if _, err := Evaluate(trace, nil, model.AnnotationFile{}, time.Time{}); err == nil {
				t.Fatalf("Evaluate malformed trace succeeded: %+v", trace)
			}
		})
	}
}

func TestEvaluateSatoshiOverflowAndEmptyTrace(t *testing.T) {
	txA, txB := testTxID("a"), testTxID("b")
	targetA := testTarget(txA, 0)
	targetA.ValueSats = math.MaxInt64
	targetB := testTarget(txB, 0)
	targetB.ValueSats = 1
	trace := model.Trace{
		Targets: []model.TargetRef{targetA, targetB},
		Boundaries: []model.Boundary{
			{NodeID: outpointNodeID(testOutpoint(txA, 0)), Reason: reasonPurchase, PurchaseIDs: []string{"p"}},
			{NodeID: outpointNodeID(testOutpoint(txB, 0)), Reason: reasonPurchase, PurchaseIDs: []string{"p"}},
		},
	}
	if _, err := Evaluate(trace, nil, model.AnnotationFile{}, time.Time{}); err == nil {
		t.Fatal("satoshi overflow was not rejected")
	}
	report, err := Evaluate(model.Trace{}, nil, model.AnnotationFile{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if report.UTXOs == nil || report.Graph.Nodes == nil || report.Graph.Edges == nil || report.Warnings == nil || len(report.UTXOs) != 0 || len(report.Graph.Nodes) != 0 || len(report.Graph.Edges) != 0 {
		t.Fatalf("empty report should contain stable empty slices: %+v", report)
	}
}

func TestEvaluateUnknownAndIncompleteBoundaryDetails(t *testing.T) {
	txA, txB, txC := testTxID("a"), testTxID("b"), testTxID("c")
	trace := model.Trace{
		Targets: []model.TargetRef{testTarget(txA, 0), testTarget(txB, 0), testTarget(txC, 0)},
		Boundaries: []model.Boundary{
			{NodeID: outpointNodeID(testOutpoint(txC, 0)), Reason: "future-unknown-terminal", Detail: "details"},
			{NodeID: outpointNodeID(testOutpoint(txB, 0)), Reason: reasonMaxTransactions, Detail: "capped"},
			{NodeID: outpointNodeID(testOutpoint(txA, 0)), Reason: reasonCoinbase},
		},
	}
	report, err := Evaluate(trace, nil, model.AnnotationFile{}, time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Warnings) != 3 || !sort.StringsAreSorted(report.Warnings) {
		t.Fatalf("warnings are not deterministic: %v", report.Warnings)
	}
	for _, utxo := range report.UTXOs {
		if utxo.Status != undocumentedStatus {
			t.Fatalf("nonpurchase target = %+v", utxo)
		}
		switch utxo.TxID {
		case txA:
			if utxo.Trace != completeTraceStatus {
				t.Fatalf("coinbase classified incomplete: %+v", utxo)
			}
		case txB, txC:
			if utxo.Trace != incompleteTraceStatus {
				t.Fatalf("unknown boundary not conservatively incomplete: %+v", utxo)
			}
		}
	}
}
