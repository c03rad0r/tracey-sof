package provenance

import (
	"context"
	"reflect"
	"testing"
	"time"

	"tracey-sof/internal/model"
)

func TestEffectivePolicyDefaultsAndOverrides(t *testing.T) {
	defaults := EffectivePolicy(Limits{})
	if defaults.SchemaVersion != model.SchemaVersion || defaults.MaxHops != model.DefaultMaxHops || defaults.MaxTransactions != model.DefaultMaxTransactions {
		t.Fatalf("default policy = %+v", defaults)
	}
	configured := EffectivePolicy(Limits{MaxDepth: 8, MaxTransactions: 250, StopIfPrevoutValueSatsGTE: 1000, StopIfInputCountGTE: 5, StopIfOutputCountGTE: 6, StopBeforeBlockHeight: 700000})
	if configured.MaxHops != 8 || configured.MaxTransactions != 250 || configured.StopIfPrevoutValueSatsGTE != 1000 || configured.StopIfInputCountGTE != 5 || configured.StopIfOutputCountGTE != 6 || configured.StopBeforeBlockHeight != 700000 {
		t.Fatalf("configured policy = %+v", configured)
	}
}

func TestPolicyPrevoutValueStopsBeforeParentExpansion(t *testing.T) {
	target, parent, grandparent := testTxID("a"), testTxID("b"), testTxID("c")
	source := newMemorySource(
		testTransaction(target, []model.Input{testInput(parent, 0)}),
		testTransaction(parent, []model.Input{testInput(grandparent, 0)}),
	)
	trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(target, 0)}, nil, model.Chain{}, Limits{StopIfPrevoutValueSatsGTE: 1000})
	if err != nil {
		t.Fatal(err)
	}
	boundary := findBoundary(trace.Boundaries, outpointNodeID(testOutpoint(parent, 0)), reasonPolicyPrevoutValue)
	if boundary == nil || !boundary.ReviewRequired || boundary.PolicyRule != "prevout_value" || boundary.PolicyObservedSats != 1000 || boundary.PolicyThresholdSats != 1000 {
		t.Fatalf("value boundary = %+v", boundary)
	}
	if callCount(source, grandparent) != 0 || !trace.Incomplete || trace.Policy.StopIfPrevoutValueSatsGTE != 1000 {
		t.Fatalf("value policy did not stop correctly: calls=%v trace=%+v", source.calls, trace)
	}
	report, err := Evaluate(trace, nil, model.AnnotationFile{}, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if report.UTXOs[0].Status != undocumentedStatus || report.UTXOs[0].Trace != incompleteTraceStatus || !report.UTXOs[0].ReviewRequired || !reflect.DeepEqual(report.UTXOs[0].ReviewReasons, []string{"prevout_value"}) || report.Summary.ReviewUTXOs != 1 {
		t.Fatalf("policy target/report = %+v / %+v", report.UTXOs[0], report.Summary)
	}
}

func TestPolicyTargetValueAloneDoesNotTriggerAncestorRule(t *testing.T) {
	target, parent := testTxID("a"), testTxID("b")
	source := newMemorySource(
		testTransaction(target, []model.Input{testInput(parent, 0)}),
		testTransaction(parent, []model.Input{{Coinbase: true}}),
	)
	targetRef := testTarget(target, 0)
	targetRef.ValueSats = 2_000_000_000
	trace, err := Trace(context.Background(), source, []model.TargetRef{targetRef}, nil, model.Chain{}, Limits{StopIfPrevoutValueSatsGTE: 500_000_000})
	if err != nil {
		t.Fatal(err)
	}
	if findBoundary(trace.Boundaries, outpointNodeID(testOutpoint(parent, 0)), reasonPolicyPrevoutValue) != nil {
		t.Fatal("large target value incorrectly triggered the ancestor-output policy")
	}
	if findBoundary(trace.Boundaries, transactionNodeID(parent), reasonCoinbase) == nil {
		t.Fatal("ancestry below target value was not traced")
	}
}

func TestPolicyMixedBranchIsPartialAndReviewRequired(t *testing.T) {
	target, purchased, unbounded, older := testTxID("a"), testTxID("b"), testTxID("c"), testTxID("d")
	unboundedOutputs := []model.Output{{Index: 0, ValueSats: 5000}}
	source := newMemorySource(
		testTransaction(target, []model.Input{testInput(purchased, 0), testInput(unbounded, 0)}),
		testTransaction(purchased, []model.Input{{Coinbase: true}}),
		testTransaction(unbounded, []model.Input{testInput(older, 0)}, unboundedOutputs...),
	)
	evidence := mustEvidence(t, model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{{ID: "buy", Outpoints: []model.Outpoint{testOutpoint(purchased, 0)}}}}, model.AnnotationFile{})
	trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(target, 0)}, evidence, model.Chain{}, Limits{StopIfPrevoutValueSatsGTE: 5000})
	if err != nil {
		t.Fatal(err)
	}
	report, err := Evaluate(trace, nil, model.AnnotationFile{}, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	targetReport := report.UTXOs[0]
	if targetReport.Status != partiallyDocumentedStatus || targetReport.Trace != incompleteTraceStatus || !targetReport.ReviewRequired || len(targetReport.PurchaseIDs) != 1 {
		t.Fatalf("mixed policy target = %+v", targetReport)
	}
}

func TestPolicyPurchaseTransactionPrecedesFanInRule(t *testing.T) {
	target, parentOne, parentTwo := testTxID("a"), testTxID("b"), testTxID("c")
	source := newMemorySource(testTransaction(target, []model.Input{testInput(parentOne, 0), testInput(parentTwo, 0)}))
	evidence := mustEvidence(t, model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{{ID: "buy", TxIDs: []string{target}}}}, model.AnnotationFile{})
	trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(target, 0)}, evidence, model.Chain{}, Limits{StopIfInputCountGTE: 1})
	if err != nil {
		t.Fatal(err)
	}
	boundary := findBoundary(trace.Boundaries, transactionNodeID(target), reasonPurchase)
	if boundary == nil || boundary.ReviewRequired || findBoundary(trace.Boundaries, transactionNodeID(target), reasonPolicyInputCount) != nil || len(source.calls) != 1 {
		t.Fatalf("purchase precedence = %+v calls=%v", trace.Boundaries, source.calls)
	}
}

func TestPolicyValueRuleDoesNotOverrideExactPurchaseEvidence(t *testing.T) {
	target, parent, grandparent := testTxID("a"), testTxID("b"), testTxID("c")
	parentOutputs := []model.Output{{Index: 0, ValueSats: 5000, Address: "purchase-address", ScriptType: "wpkh"}}
	source := newMemorySource(
		testTransaction(target, []model.Input{testInput(parent, 0)}),
		testTransaction(parent, []model.Input{testInput(grandparent, 0)}, parentOutputs...),
	)
	evidence := mustEvidence(t, model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{{ID: "buy", Addresses: []string{"purchase-address"}}}}, model.AnnotationFile{})
	trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(target, 0)}, evidence, model.Chain{}, Limits{StopIfPrevoutValueSatsGTE: 1})
	if err != nil {
		t.Fatal(err)
	}
	boundary := findBoundary(trace.Boundaries, outpointNodeID(testOutpoint(parent, 0)), reasonPurchase)
	if boundary == nil || boundary.MatchedEvidenceKind != "address" || boundary.MatchedEvidenceValue != "purchase-address" || boundary.ReviewRequired {
		t.Fatalf("exact address precedence boundary = %+v", boundary)
	}
	if findBoundary(trace.Boundaries, outpointNodeID(testOutpoint(parent, 0)), reasonPolicyPrevoutValue) != nil {
		t.Fatal("policy boundary overrode exact purchase evidence")
	}
	if callCount(source, grandparent) != 0 {
		t.Fatal("purchase-address branch expanded beyond its boundary")
	}
}

func TestPolicyFanInFanOutAndHeight(t *testing.T) {
	t.Run("input count exact threshold", func(t *testing.T) {
		target, parentOne, parentTwo := testTxID("a"), testTxID("b"), testTxID("c")
		source := newMemorySource(testTransaction(target, []model.Input{testInput(parentOne, 0), testInput(parentTwo, 0)}))
		trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(target, 0)}, nil, model.Chain{}, Limits{StopIfInputCountGTE: 2})
		if err != nil {
			t.Fatal(err)
		}
		boundary := findBoundary(trace.Boundaries, transactionNodeID(target), reasonPolicyInputCount)
		if boundary == nil || boundary.PolicyObservedCount != 2 || boundary.PolicyThresholdCount != 2 || len(source.calls) != 1 {
			t.Fatalf("fan-in boundary = %+v, calls=%v", boundary, source.calls)
		}
	})
	t.Run("output count below threshold continues", func(t *testing.T) {
		target := testTxID("a")
		source := newMemorySource(testTransaction(target, []model.Input{{Coinbase: true}}, []model.Output{{Index: 0}, {Index: 1}}...))
		trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(target, 0)}, nil, model.Chain{}, Limits{StopIfOutputCountGTE: 3})
		if err != nil {
			t.Fatal(err)
		}
		if findBoundary(trace.Boundaries, transactionNodeID(target), reasonCoinbase) == nil || findBoundary(trace.Boundaries, transactionNodeID(target), reasonPolicyOutputCount) != nil {
			t.Fatalf("below-threshold output policy = %+v", trace.Boundaries)
		}
	})
	t.Run("output count exact threshold", func(t *testing.T) {
		target, parent := testTxID("a"), testTxID("b")
		source := newMemorySource(testTransaction(target, []model.Input{testInput(parent, 0)}, []model.Output{{Index: 0}, {Index: 1}}...))
		trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(target, 0)}, nil, model.Chain{}, Limits{StopIfOutputCountGTE: 2})
		if err != nil {
			t.Fatal(err)
		}
		boundary := findBoundary(trace.Boundaries, transactionNodeID(target), reasonPolicyOutputCount)
		if boundary == nil || boundary.PolicyObservedCount != 2 || len(source.calls) != 1 {
			t.Fatalf("fan-out boundary = %+v calls=%v", boundary, source.calls)
		}
	})
	t.Run("block height", func(t *testing.T) {
		target, parent := testTxID("a"), testTxID("b")
		transaction := testTransaction(target, []model.Input{testInput(parent, 0)})
		transaction.BlockHeight = 500
		source := newMemorySource(transaction)
		trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(target, 0)}, nil, model.Chain{}, Limits{StopBeforeBlockHeight: 501})
		if err != nil {
			t.Fatal(err)
		}
		boundary := findBoundary(trace.Boundaries, transactionNodeID(target), reasonPolicyBlockHeight)
		if boundary == nil || boundary.PolicyRule != "stop_before_block_height" || len(source.calls) != 1 {
			t.Fatalf("height boundary = %+v calls=%v", boundary, source.calls)
		}
	})
	t.Run("block height equal does not stop", func(t *testing.T) {
		target := testTxID("a")
		transaction := testTransaction(target, []model.Input{{Coinbase: true}})
		transaction.BlockHeight = 500
		trace, err := Trace(context.Background(), newMemorySource(transaction), []model.TargetRef{testTarget(target, 0)}, nil, model.Chain{}, Limits{StopBeforeBlockHeight: 500})
		if err != nil {
			t.Fatal(err)
		}
		if findBoundary(trace.Boundaries, transactionNodeID(target), reasonPolicyBlockHeight) != nil {
			t.Fatal("equal height should not stop")
		}
	})
}

func TestPolicyExistingLimitsRecordRules(t *testing.T) {
	target, parent, grandparent := testTxID("a"), testTxID("b"), testTxID("c")
	source := newMemorySource(
		testTransaction(target, []model.Input{testInput(parent, 0)}),
		testTransaction(parent, []model.Input{testInput(grandparent, 0)}),
	)
	trace, err := Trace(context.Background(), source, []model.TargetRef{testTarget(target, 0)}, nil, model.Chain{}, Limits{MaxDepth: 1})
	if err != nil {
		t.Fatal(err)
	}
	boundary := findBoundary(trace.Boundaries, outpointNodeID(testOutpoint(grandparent, 0)), reasonMaxDepth)
	if boundary == nil || boundary.PolicyRule != "max_hops" || boundary.PolicyThresholdCount != 1 || !boundary.ReviewRequired {
		t.Fatalf("max depth boundary = %+v", boundary)
	}

	source = newMemorySource(testTransaction(target, []model.Input{testInput(parent, 0)}))
	trace, err = Trace(context.Background(), source, []model.TargetRef{testTarget(target, 0)}, nil, model.Chain{}, Limits{MaxTransactions: 1})
	if err != nil {
		t.Fatal(err)
	}
	boundary = findBoundary(trace.Boundaries, outpointNodeID(testOutpoint(parent, 0)), reasonMaxTransactions)
	if boundary == nil || boundary.PolicyRule != "max_transactions" || boundary.PolicyThresholdCount != 1 || boundary.PolicyObservedCount != 1 || !boundary.ReviewRequired {
		t.Fatalf("max transactions boundary = %+v", boundary)
	}
}

func TestPolicyRejectsNegativeThresholds(t *testing.T) {
	for _, limits := range []Limits{{StopIfPrevoutValueSatsGTE: -1}, {StopIfInputCountGTE: -1}, {StopIfOutputCountGTE: -1}, {StopBeforeBlockHeight: -1}} {
		if _, err := Trace(context.Background(), newMemorySource(), nil, nil, model.Chain{}, limits); err == nil {
			t.Errorf("negative policy accepted: %+v", limits)
		}
	}
}

func TestPolicySharedTransactionExpandedOnlyOnce(t *testing.T) {
	target, parent := testTxID("a"), testTxID("b")
	source := newMemorySource(
		testTransaction(target, []model.Input{testInput(parent, 0)}, model.Output{Index: 0}, model.Output{Index: 1}),
		testTransaction(parent, []model.Input{{Coinbase: true}}),
	)
	targets := []model.TargetRef{testTarget(target, 0), testTarget(target, 1)}
	trace, err := Trace(context.Background(), source, targets, nil, model.Chain{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	if callCount(source, target) != 1 || trace.Incomplete {
		t.Fatalf("shared transaction expansion: calls=%v trace=%+v", source.calls, trace)
	}
}

func TestEvidenceStatusDiagnostics(t *testing.T) {
	target, parent := testTxID("a"), testTxID("b")
	purchases := []model.Purchase{{ID: "matched", TxIDs: []string{parent}, Addresses: []string{"not-seen"}, Outpoints: []model.Outpoint{testOutpoint(parent, 0)}}}
	ev := mustEvidence(t, model.PurchaseFile{SchemaVersion: 1, Purchases: purchases}, model.AnnotationFile{})
	trace, err := Trace(context.Background(), newMemorySource(testTransaction(target, []model.Input{testInput(parent, 0)}), testTransaction(parent, []model.Input{{Coinbase: true}})), []model.TargetRef{testTarget(target, 0)}, ev, model.Chain{}, Limits{})
	if err != nil {
		t.Fatal(err)
	}
	statuses, unmatched := evidenceStatuses(purchases, trace.Boundaries)
	if unmatched != 2 || len(statuses) != 3 {
		t.Fatalf("evidence statuses = %+v unmatched=%d", statuses, unmatched)
	}
	var matchedOutpoint bool
	for _, status := range statuses {
		if status.Kind == "outpoint" && status.Matched && status.MatchedNodeID != "" {
			matchedOutpoint = true
		}
	}
	if !matchedOutpoint {
		t.Fatalf("outpoint was not marked matched: %+v", statuses)
	}
	report, err := Evaluate(trace, purchases, model.AnnotationFile{}, time.Unix(0, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Evidence) != len(statuses) || report.Summary.UnmatchedEvidence != 2 {
		t.Fatalf("evidence report = %+v summary=%+v", report.Evidence, report.Summary)
	}
}
