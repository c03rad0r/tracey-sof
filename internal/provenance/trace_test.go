package provenance

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

func TestTraceDirectPurchaseOutpointDoesNotLoadTransaction(t *testing.T) {
	txid := testTxID("a")
	source := newMemorySource(testTransaction(txid, nil))
	evidence := mustEvidence(t, model.PurchaseFile{Purchases: []model.Purchase{
		{ID: "z", Outpoints: []model.Outpoint{testOutpoint(txid, 3)}},
		{ID: "a", Outpoints: []model.Outpoint{testOutpoint(txid, 3)}},
	}}, model.AnnotationFile{})
	target := testTarget(txid, 3)
	target.OwnerIDs = []string{"owner"}
	target.Labels = []string{"label"}
	chain := model.Chain{BestBlock: "best", BestHeight: 42}

	trace, err := Trace(context.Background(), source, []model.TargetRef{target}, evidence, chain, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.calls) != 0 || len(trace.Transactions) != 0 || len(trace.Edges) != 0 {
		t.Fatalf("direct purchase did unnecessary work: calls=%v transactions=%d edges=%v", source.calls, len(trace.Transactions), trace.Edges)
	}
	if trace.Incomplete || trace.SchemaVersion != model.SchemaVersion || trace.BestBlock != "best" || trace.BestHeight != 42 {
		t.Fatalf("unexpected trace metadata: %+v", trace)
	}
	if len(trace.Boundaries) != 1 {
		t.Fatalf("boundaries = %v", trace.Boundaries)
	}
	boundary := trace.Boundaries[0]
	if boundary.NodeID != "out:"+testOutpoint(txid, 3).String() || boundary.Reason != reasonPurchase || boundary.Outpoint != testOutpoint(txid, 3).String() || !reflect.DeepEqual(boundary.PurchaseIDs, []string{"a", "z"}) {
		t.Fatalf("purchase boundary = %+v", boundary)
	}
	target.OwnerIDs[0] = "changed"
	target.Labels[0] = "changed"
	if trace.Targets[0].OwnerIDs[0] != "owner" || trace.Targets[0].Labels[0] != "label" {
		t.Fatalf("trace targets alias input: %+v", trace.Targets[0])
	}
}

func TestTracePurchaseTransactionIsFullBoundary(t *testing.T) {
	targetTx := testTxID("a")
	parentTx := testTxID("b")
	target := testTransaction(targetTx, []model.Input{testInput(parentTx, 0)})
	source := newMemorySource(target, testTransaction(parentTx, nil))
	evidence := mustEvidence(t, model.PurchaseFile{Purchases: []model.Purchase{{ID: "buy", TxIDs: []string{targetTx}}}}, model.AnnotationFile{})

	trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(targetTx, 0)}, evidence, model.Chain{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.calls) != 1 || source.calls[0] != targetTx {
		t.Fatalf("calls = %v, want only target transaction", source.calls)
	}
	if len(trace.Edges) != 0 || len(trace.Transactions) != 1 {
		t.Fatalf("purchase transaction was traversed: %+v", trace)
	}
	boundary := findBoundary(trace.Boundaries, transactionNodeID(targetTx), reasonPurchase)
	if boundary == nil || boundary.Transaction != targetTx || !reflect.DeepEqual(boundary.PurchaseIDs, []string{"buy"}) {
		t.Fatalf("transaction purchase boundary = %+v (%v)", boundary, trace.Boundaries)
	}
	if trace.Incomplete {
		t.Fatal("purchase boundary marked trace incomplete")
	}
}

func TestTraceMultihopAndExactPrevoutPurchase(t *testing.T) {
	targetTx := testTxID("a")
	middleTx := testTxID("b")
	purchaseTx := testTxID("c")
	target := testTransaction(targetTx, []model.Input{testInput(middleTx, 2)})
	middle := testTransaction(middleTx, []model.Input{testInput(purchaseTx, 1)},
		model.Output{Index: 2, Address: "ordinary-output", ValueSats: 25},
		model.Output{Index: 0, ValueSats: 5})
	source := newMemorySource(target, middle)
	source.errors[purchaseTx] = errors.New("must not be loaded")
	evidence := mustEvidence(t, model.PurchaseFile{Purchases: []model.Purchase{{ID: "purchase", Outpoints: []model.Outpoint{testOutpoint(purchaseTx, 1)}}}}, model.AnnotationFile{})

	trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(targetTx, 0)}, evidence, model.Chain{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.calls) != 2 || callCount(source, purchaseTx) != 0 {
		t.Fatalf("loads = %v, exact purchase parent must not be loaded", source.calls)
	}
	if len(trace.Edges) != 2 || len(trace.Transactions) != 2 {
		t.Fatalf("multihop trace lost nodes: %+v", trace)
	}
	if findBoundary(trace.Boundaries, outpointNodeID(testOutpoint(purchaseTx, 1)), reasonPurchase) == nil {
		t.Fatalf("missing exact purchase boundary: %v", trace.Boundaries)
	}
}

func TestTraceAssociatedAddressStopsOnlyThatBranch(t *testing.T) {
	targetTx := testTxID("a")
	parentTx := testTxID("b")
	unknownTx := testTxID("c")
	parentOutput := model.Output{Index: 0, Address: "purchase-address", ValueSats: 800, ScriptType: "p2wpkh"}
	target := testTransaction(targetTx, []model.Input{testInput(parentTx, 0)})
	parent := testTransaction(parentTx, []model.Input{testInput(unknownTx, 0)}, parentOutput)
	source := newMemorySource(target, parent)
	evidence := mustEvidence(t, model.PurchaseFile{Purchases: []model.Purchase{{ID: "address-buy", Addresses: []string{"purchase-address"}}}}, model.AnnotationFile{})

	trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(targetTx, 0)}, evidence, model.Chain{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if callCount(source, parentTx) != 1 || callCount(source, unknownTx) != 0 {
		t.Fatalf("address branch did not stop after parent validation: %v", source.calls)
	}
	boundary := findBoundary(trace.Boundaries, outpointNodeID(testOutpoint(parentTx, 0)), reasonPurchase)
	if boundary == nil || !reflect.DeepEqual(boundary.PurchaseIDs, []string{"address-buy"}) {
		t.Fatalf("address purchase boundary = %+v", boundary)
	}
	if len(trace.Transactions) != 2 || trace.Transactions[parentTx].Outputs[0] != parentOutput {
		t.Fatalf("validated parent output not retained: %+v", trace.Transactions)
	}
}

func TestTraceCoinbaseIsCompleteNonPurchaseOrigin(t *testing.T) {
	targetTx := testTxID("a")
	coinbaseTx := testTxID("b")
	target := testTransaction(targetTx, []model.Input{testInput(coinbaseTx, 0)})
	coinbase := testTransaction(coinbaseTx, []model.Input{{Coinbase: true, PrevOut: testOutpoint(strings.Repeat("0", 64), ^uint32(0))}})
	source := newMemorySource(target, coinbase)
	trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(targetTx, 0)}, nil, model.Chain{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if trace.Incomplete {
		t.Fatal("coinbase must not make trace incomplete")
	}
	if len(trace.Edges) != 1 || findBoundary(trace.Boundaries, transactionNodeID(coinbaseTx), reasonCoinbase) == nil {
		t.Fatalf("coinbase origin not recorded: %+v", trace)
	}
}

func TestTraceMixedPurchaseAndUnavailableInput(t *testing.T) {
	targetTx := testTxID("a")
	purchaseTx := testTxID("b")
	missingTx := testTxID("c")
	target := testTransaction(targetTx, []model.Input{testInput(missingTx, 2), testInput(purchaseTx, 1)})
	source := newMemorySource(target)
	evidence := mustEvidence(t, model.PurchaseFile{Purchases: []model.Purchase{{ID: "buy", Outpoints: []model.Outpoint{testOutpoint(purchaseTx, 1)}}}}, model.AnnotationFile{})
	trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(targetTx, 0)}, evidence, model.Chain{}, Limits{})
	if err != nil {
		t.Fatalf("ordinary source error must be recorded: %v", err)
	}
	if !trace.Incomplete || len(trace.Edges) != 2 || len(trace.Boundaries) != 2 {
		t.Fatalf("mixed trace = %+v", trace)
	}
	missing := findBoundary(trace.Boundaries, outpointNodeID(testOutpoint(missingTx, 2)), reasonTransactionUnavailable)
	if missing == nil || !strings.Contains(missing.Detail, "transaction not found") {
		t.Fatalf("unavailable boundary = %+v", missing)
	}
	if !sort.SliceIsSorted(trace.Edges, func(i, j int) bool {
		if trace.Edges[i].SpendingTxID != trace.Edges[j].SpendingTxID {
			return trace.Edges[i].SpendingTxID < trace.Edges[j].SpendingTxID
		}
		return trace.Edges[i].InputIndex < trace.Edges[j].InputIndex
	}) {
		t.Fatalf("edges not sorted: %v", trace.Edges)
	}
}

func TestTraceSharedAncestryUsesGlobalCacheAndMinimumDepth(t *testing.T) {
	targetOne := testTxID("a")
	targetTwo := testTxID("b")
	middle := testTxID("c")
	shared := testTxID("d")
	root := testTxID("e")
	source := newMemorySource(
		testTransaction(targetOne, []model.Input{testInput(shared, 0)}),
		testTransaction(targetTwo, []model.Input{testInput(middle, 0)}),
		testTransaction(middle, []model.Input{testInput(shared, 0)}),
		testTransaction(shared, []model.Input{testInput(root, 0)}),
	)
	targets := []model.TargetRef{testTarget(targetOne, 0), testTarget(targetTwo, 0)}
	trace, err := Trace(context.Background(), source, targets, nil, model.Chain{}, Limits{MaxDepth: 1, MaxTransactions: 10})
	if err != nil {
		t.Fatal(err)
	}
	if callCount(source, shared) != 1 || callCount(source, middle) != 1 || callCount(source, root) != 0 {
		t.Fatalf("shared BFS loads = %v", source.calls)
	}
	if len(trace.Edges) != 4 || len(trace.Boundaries) != 1 {
		t.Fatalf("shared BFS trace = %+v", trace)
	}
	if findBoundary(trace.Boundaries, outpointNodeID(testOutpoint(root, 0)), reasonMaxDepth) == nil {
		t.Fatalf("minimum-depth limit boundary missing: %v", trace.Boundaries)
	}
}

func TestTraceLimits(t *testing.T) {
	t.Run("depth", func(t *testing.T) {
		targetTx, parentTx, grandparentTx := testTxID("a"), testTxID("b"), testTxID("c")
		source := newMemorySource(
			testTransaction(targetTx, []model.Input{testInput(parentTx, 0)}),
			testTransaction(parentTx, []model.Input{testInput(grandparentTx, 0)}),
		)
		trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(targetTx, 0)}, nil, model.Chain{}, Limits{MaxDepth: 1})
		if err != nil {
			t.Fatal(err)
		}
		if callCount(source, grandparentTx) != 0 || !trace.Incomplete || findBoundary(trace.Boundaries, outpointNodeID(testOutpoint(grandparentTx, 0)), reasonMaxDepth) == nil {
			t.Fatalf("max-depth result: calls=%v trace=%+v", source.calls, trace)
		}
	})
	t.Run("transactions", func(t *testing.T) {
		targetTx, parentTx := testTxID("a"), testTxID("b")
		source := newMemorySource(
			testTransaction(targetTx, []model.Input{testInput(parentTx, 0)}),
			testTransaction(parentTx, nil),
		)
		trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(targetTx, 0)}, nil, model.Chain{}, Limits{MaxTransactions: 1})
		if err != nil {
			t.Fatal(err)
		}
		if len(source.calls) != 1 || findBoundary(trace.Boundaries, outpointNodeID(testOutpoint(parentTx, 0)), reasonMaxTransactions) == nil || !trace.Incomplete {
			t.Fatalf("max-transactions result: calls=%v trace=%+v", source.calls, trace)
		}
	})
	t.Run("known prevout purchase beats depth cap", func(t *testing.T) {
		targetTx, purchaseTx := testTxID("a"), testTxID("b")
		source := newMemorySource(testTransaction(targetTx, []model.Input{testInput(purchaseTx, 0)}))
		evidence := mustEvidence(t, model.PurchaseFile{Purchases: []model.Purchase{{ID: "buy", Outpoints: []model.Outpoint{testOutpoint(purchaseTx, 0)}}}}, model.AnnotationFile{})
		trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(targetTx, 0)}, evidence, model.Chain{}, Limits{MaxDepth: 1})
		if err != nil {
			t.Fatal(err)
		}
		if trace.Incomplete || len(trace.Boundaries) != 1 || trace.Boundaries[0].Reason != reasonPurchase {
			t.Fatalf("known purchase was truncated: %+v", trace)
		}
	})
}

func TestTraceRejectsInvalidConfiguration(t *testing.T) {
	source := newMemorySource()
	if _, err := Trace(context.Background(), source, nil, nil, model.Chain{}, Limits{MaxDepth: -1}); err == nil {
		t.Fatal("negative max depth accepted")
	}
	if _, err := Trace(context.Background(), source, nil, nil, model.Chain{}, Limits{MaxTransactions: -1}); err == nil {
		t.Fatal("negative max transactions accepted")
	}
	if _, err := Trace(context.Background(), nil, nil, nil, model.Chain{}, Limits{}); err == nil {
		t.Fatal("nil source accepted")
	}
	var nilContext context.Context
	if _, err := Trace(nilContext, source, nil, nil, model.Chain{}, Limits{}); err == nil {
		t.Fatal("nil context accepted")
	}
}

func TestTraceInvalidTransactionsBecomeBoundaries(t *testing.T) {
	targetTx := testTxID("a")
	parentTx := testTxID("b")
	tests := []struct {
		name       string
		configure  func(*memorySource)
		wantReason string
	}{
		{name: "missing funding transaction", configure: func(source *memorySource) {}, wantReason: reasonTransactionUnavailable},
		{name: "missing parent", configure: func(source *memorySource) {
			source.transactions[targetTx] = testTransaction(targetTx, []model.Input{testInput(parentTx, 0)})
		}, wantReason: reasonTransactionUnavailable},
		{name: "funding txid mismatch", configure: func(source *memorySource) {
			source.transactions[targetTx] = testTransaction(testTxID("f"), nil)
		}, wantReason: reasonInvalidParent},
		{name: "parent txid mismatch", configure: func(source *memorySource) {
			source.transactions[targetTx] = testTransaction(targetTx, []model.Input{testInput(parentTx, 0)})
			source.transactions[parentTx] = testTransaction(testTxID("f"), nil)
		}, wantReason: reasonInvalidParent},
		{name: "missing requested output", configure: func(source *memorySource) {
			source.transactions[targetTx] = testTransaction(targetTx, []model.Input{testInput(parentTx, 9)})
			source.transactions[parentTx] = testTransaction(parentTx, nil)
		}, wantReason: reasonInvalidParent},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			source := newMemorySource()
			test.configure(source)
			vout := uint32(0)
			trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(targetTx, vout)}, nil, model.Chain{}, Limits{})
			if err != nil {
				t.Fatal(err)
			}
			expectedNode := outpointNodeID(testOutpoint(targetTx, 0))
			if test.name == "missing parent" || test.name == "parent txid mismatch" || test.name == "missing requested output" {
				expectedVout := uint32(0)
				if test.name == "missing requested output" {
					expectedVout = 9
				}
				expectedNode = outpointNodeID(testOutpoint(parentTx, expectedVout))
			}
			if !trace.Incomplete || findBoundary(trace.Boundaries, expectedNode, test.wantReason) == nil {
				t.Fatalf("invalid transaction boundaries = %+v, want %s at %s", trace.Boundaries, test.wantReason, expectedNode)
			}
		})
	}
}

func TestTraceCachesFailuresAndDeduplicatesResults(t *testing.T) {
	txid := testTxID("a")
	source := newMemorySource()
	target := testTarget(txid, 0)
	trace, err := Trace(context.Background(), source, []model.TargetRef{target, target}, nil, model.Chain{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.calls) != 1 || len(trace.Boundaries) != 1 || len(trace.Edges) != 0 {
		t.Fatalf("duplicate failed target caused duplicate work: calls=%v trace=%+v", source.calls, trace)
	}
}

func TestTraceContextCancellation(t *testing.T) {
	t.Run("already canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := Trace(ctx, newMemorySource(), nil, nil, model.Chain{}, Limits{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	})
	t.Run("source cancellation error", func(t *testing.T) {
		txid := testTxID("a")
		source := newMemorySource()
		source.errors[txid] = fmt.Errorf("rpc: %w", context.Canceled)
		_, err := Trace(context.Background(), source, []model.TargetRef{testTarget(txid, 0)}, nil, model.Chain{}, Limits{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want wrapped context.Canceled", err)
		}
	})
	t.Run("canceled during source call", func(t *testing.T) {
		txid := testTxID("a")
		source := newMemorySource(testTransaction(txid, nil))
		ctx, cancel := context.WithCancel(context.Background())
		source.hook = func(context.Context, string) { cancel() }
		_, err := Trace(ctx, source, []model.TargetRef{testTarget(txid, 0)}, nil, model.Chain{}, Limits{})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context.Canceled", err)
		}
	})
	t.Run("deadline exceeded", func(t *testing.T) {
		txid := testTxID("a")
		source := newMemorySource()
		source.errors[txid] = context.DeadlineExceeded
		_, err := Trace(context.Background(), source, []model.TargetRef{testTarget(txid, 0)}, nil, model.Chain{}, Limits{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("error = %v, want deadline exceeded", err)
		}
	})
}
