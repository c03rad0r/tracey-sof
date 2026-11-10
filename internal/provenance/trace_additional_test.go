package provenance

import (
	"context"
	"errors"
	"testing"

	"tracey-sof/internal/model"
)

func TestTraceExpandedSharedTransactionIsNotExpandedTwice(t *testing.T) {
	targetOne, targetTwo, shared := testTxID("a"), testTxID("b"), testTxID("c")
	source := newMemorySource(
		testTransaction(targetOne, []model.Input{testInput(shared, 0)}),
		testTransaction(targetTwo, []model.Input{testInput(shared, 0)}),
		testTransaction(shared, []model.Input{{Coinbase: true}}),
	)
	trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(targetOne, 0), testTarget(targetTwo, 0)}, nil, model.Chain{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if len(source.calls) != 3 || callCount(source, shared) != 1 || len(trace.Edges) != 2 || len(trace.Boundaries) != 1 {
		t.Fatalf("shared transaction expanded more than once: calls=%v edges=%v boundaries=%v", source.calls, trace.Edges, trace.Boundaries)
	}
}

func TestTraceDepthJoinDoesNotDuplicateSharedTerminal(t *testing.T) {
	targetOne, targetTwo, middle, shared, unusedParent := testTxID("a"), testTxID("b"), testTxID("c"), testTxID("d"), testTxID("e")
	tests := []struct {
		name           string
		sharedInputs   []model.Input
		purchase       model.Purchase
		terminalNode   string
		terminalReason string
	}{
		{name: "address", sharedInputs: []model.Input{testInput(unusedParent, 0)}, purchase: model.Purchase{ID: "buy", Addresses: []string{"purchase-address"}}, terminalNode: outpointNodeID(testOutpoint(shared, 0)), terminalReason: reasonPurchase},
		{name: "transaction", sharedInputs: []model.Input{testInput(unusedParent, 0)}, purchase: model.Purchase{ID: "buy", TxIDs: []string{shared}}, terminalNode: transactionNodeID(shared), terminalReason: reasonPurchase},
		{name: "coinbase", sharedInputs: []model.Input{{Coinbase: true}}, terminalNode: transactionNodeID(shared), terminalReason: reasonCoinbase},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			sharedTx := testTransaction(shared, test.sharedInputs, model.Output{Index: 0, Address: "purchase-address"})
			source := newMemorySource(
				testTransaction(targetOne, []model.Input{testInput(shared, 0)}),
				testTransaction(targetTwo, []model.Input{testInput(middle, 0)}),
				testTransaction(middle, []model.Input{testInput(shared, 0)}),
				sharedTx,
			)
			purchases := model.PurchaseFile{}
			if test.purchase.ID != "" {
				purchases.Purchases = []model.Purchase{test.purchase}
			}
			evidence := mustEvidence(t, purchases, model.AnnotationFile{})
			trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(targetOne, 0), testTarget(targetTwo, 0)}, evidence, model.Chain{}, Limits{MaxDepth: 1})
			if err != nil {
				t.Fatal(err)
			}
			if len(trace.Boundaries) != 1 || trace.Boundaries[0].NodeID != test.terminalNode || trace.Boundaries[0].Reason != test.terminalReason {
				t.Fatalf("shared terminal duplicated or replaced: %+v", trace.Boundaries)
			}
			if callCount(source, unusedParent) != 0 {
				t.Fatalf("terminal shared transaction was traversed: %v", source.calls)
			}
		})
	}
}

type cancelAfterTransactionSource struct {
	transaction model.Transaction
	cancel      context.CancelFunc
	calls       int
}

func (s *cancelAfterTransactionSource) Transaction(context.Context, string) (model.Transaction, error) {
	s.calls++
	s.cancel()
	return s.transaction, nil
}

func TestTraceChecksCancellationBetweenVisitsAndAtCompletion(t *testing.T) {
	t.Run("between visits", func(t *testing.T) {
		targetTx, parentTx := testTxID("a"), testTxID("b")
		ctx, cancel := context.WithCancel(context.Background())
		source := &cancelAfterTransactionSource{transaction: testTransaction(targetTx, []model.Input{testInput(parentTx, 0)}), cancel: cancel}
		_, err := Trace(ctx, source, []model.TargetRef{testTarget(targetTx, 0)}, nil, model.Chain{}, Limits{})
		if !errors.Is(err, context.Canceled) || source.calls != 1 {
			t.Fatalf("between-visit cancellation: calls=%d error=%v", source.calls, err)
		}
	})
	t.Run("at completion", func(t *testing.T) {
		targetTx := testTxID("a")
		ctx, cancel := context.WithCancel(context.Background())
		source := &cancelAfterTransactionSource{transaction: testTransaction(targetTx, nil), cancel: cancel}
		_, err := Trace(ctx, source, []model.TargetRef{testTarget(targetTx, 0)}, nil, model.Chain{}, Limits{})
		if !errors.Is(err, context.Canceled) || source.calls != 1 {
			t.Fatalf("completion cancellation: calls=%d error=%v", source.calls, err)
		}
	})
}
