package provenance

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"tracey-sof/internal/model"
)

const (
	defaultMaxDepth        = 25
	defaultMaxTransactions = 10000

	reasonPurchase               = "purchase"
	reasonCoinbase               = "coinbase"
	reasonTransactionUnavailable = "transaction_unavailable"
	reasonInvalidParent          = "invalid_parent"
	reasonMaxDepth               = "max_depth"
	reasonMaxTransactions        = "max_transactions"
	reasonPolicyPrevoutValue     = "policy_prevout_value"
	reasonPolicyInputCount       = "policy_input_count"
	reasonPolicyOutputCount      = "policy_output_count"
	reasonPolicyBlockHeight      = "policy_stop_before_block_height"
)

// TransactionSource retrieves a transaction by its exact txid.
type TransactionSource interface {
	Transaction(context.Context, string) (model.Transaction, error)
}

// Limits bound a single shared traversal across all targets.
type Limits struct {
	MaxDepth                  int
	MaxTransactions           int
	StopIfPrevoutValueSatsGTE int64
	StopIfInputCountGTE       int
	StopIfOutputCountGTE      int
	StopBeforeBlockHeight     int64
}

// EffectivePolicy returns the fully resolved values stored in a trace/report.
func EffectivePolicy(limits Limits) model.TracePolicy {
	if limits.MaxDepth == 0 {
		limits.MaxDepth = defaultMaxDepth
	}
	if limits.MaxTransactions == 0 {
		limits.MaxTransactions = defaultMaxTransactions
	}
	return model.TracePolicy{
		SchemaVersion:             model.SchemaVersion,
		MaxHops:                   limits.MaxDepth,
		MaxTransactions:           limits.MaxTransactions,
		StopIfPrevoutValueSatsGTE: limits.StopIfPrevoutValueSatsGTE,
		StopIfInputCountGTE:       limits.StopIfInputCountGTE,
		StopIfOutputCountGTE:      limits.StopIfOutputCountGTE,
		StopBeforeBlockHeight:     limits.StopBeforeBlockHeight,
	}
}

func transactionNodeID(txid string) string { return "tx:" + txid }
func outpointNodeID(outpoint model.Outpoint) string {
	return "out:" + outpoint.String()
}

func cloneTarget(target model.TargetRef) model.TargetRef {
	target.OwnerIDs = append([]string(nil), target.OwnerIDs...)
	target.Labels = append([]string(nil), target.Labels...)
	return target
}

type traceVisit struct {
	outpoint model.Outpoint
	depth    int
	initial  bool
}

type transactionLoad struct {
	transaction model.Transaction
	err         error
	reason      string
}

type tracer struct {
	source     TransactionSource
	evidence   *Evidence
	limits     Limits
	trace      model.Trace
	queue      []traceVisit
	loads      map[string]transactionLoad
	attempts   int
	expanded   map[string]bool
	edges      map[string]model.TraceEdge
	boundaries map[string]model.Boundary
}

func edgeKey(edge model.TraceEdge) string {
	return fmt.Sprintf("%s\x00%d\x00%s", edge.SpendingTxID, edge.InputIndex, edge.PrevOut.String())
}

func (t *tracer) addEdge(edge model.TraceEdge) {
	key := edgeKey(edge)
	if _, exists := t.edges[key]; exists {
		return
	}
	t.edges[key] = edge
	t.trace.Edges = append(t.trace.Edges, edge)
}

func (t *tracer) addBoundary(boundary model.Boundary) {
	key := boundary.NodeID + "\x00" + boundary.Reason
	if _, exists := t.boundaries[key]; exists {
		return
	}
	boundary.PurchaseIDs = append([]string(nil), boundary.PurchaseIDs...)
	if boundary.Reason != reasonPurchase && boundary.Reason != reasonCoinbase {
		boundary.ReviewRequired = true
		t.trace.Incomplete = true
	}
	switch boundary.Reason {
	case reasonMaxDepth:
		boundary.PolicyRule = "max_hops"
		boundary.PolicyThresholdCount = t.limits.MaxDepth
	case reasonMaxTransactions:
		boundary.PolicyRule = "max_transactions"
		boundary.PolicyThresholdCount = t.limits.MaxTransactions
		boundary.PolicyObservedCount = t.attempts
	}

	t.boundaries[key] = boundary
	t.trace.Boundaries = append(t.trace.Boundaries, boundary)
}

func (t *tracer) outpointBoundary(outpoint model.Outpoint, reason, detail string, purchaseIDs []string) {
	t.addBoundary(model.Boundary{
		NodeID:      outpointNodeID(outpoint),
		Reason:      reason,
		Detail:      detail,
		PurchaseIDs: purchaseIDs,
		Outpoint:    outpoint.String(),
	})
}

func (t *tracer) transactionBoundary(txid, reason, detail string, purchaseIDs []string) {
	t.addBoundary(model.Boundary{
		NodeID:      transactionNodeID(txid),
		Reason:      reason,
		Detail:      detail,
		PurchaseIDs: purchaseIDs,
		Transaction: txid,
	})
}

func (t *tracer) matchedOutpointBoundary(outpoint model.Outpoint, detail string, purchaseIDs []string, kind, value string) {
	t.addBoundary(model.Boundary{
		NodeID:               outpointNodeID(outpoint),
		Reason:               reasonPurchase,
		Detail:               detail,
		PurchaseIDs:          purchaseIDs,
		Outpoint:             outpoint.String(),
		MatchedEvidenceKind:  kind,
		MatchedEvidenceValue: value,
	})
}

func (t *tracer) matchedTransactionBoundary(txid, detail string, purchaseIDs []string) {
	t.addBoundary(model.Boundary{
		NodeID:               transactionNodeID(txid),
		Reason:               reasonPurchase,
		Detail:               detail,
		PurchaseIDs:          purchaseIDs,
		Transaction:          txid,
		MatchedEvidenceKind:  "transaction",
		MatchedEvidenceValue: txid,
	})
}

func (t *tracer) policyOutpointBoundary(outpoint model.Outpoint, reason, rule, detail string, observedSats, thresholdSats int64) {
	t.addBoundary(model.Boundary{
		NodeID:              outpointNodeID(outpoint),
		Reason:              reason,
		Detail:              detail,
		Outpoint:            outpoint.String(),
		PolicyRule:          rule,
		PolicyObservedSats:  observedSats,
		PolicyThresholdSats: thresholdSats,
	})
}

func (t *tracer) policyTransactionBoundary(txid, reason, rule, detail string, observedCount, thresholdCount int) {
	t.addBoundary(model.Boundary{
		NodeID:               transactionNodeID(txid),
		Reason:               reason,
		Detail:               detail,
		Transaction:          txid,
		PolicyRule:           rule,
		PolicyObservedCount:  observedCount,
		PolicyThresholdCount: thresholdCount,
	})
}

func contextError(ctx context.Context, err error) bool {
	return ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (t *tracer) load(ctx context.Context, txid string) (transactionLoad, error) {
	if cached, exists := t.loads[txid]; exists {
		return cached, nil
	}
	if t.attempts >= t.limits.MaxTransactions {
		return transactionLoad{
			reason: reasonMaxTransactions,
			err:    fmt.Errorf("transaction limit of %d reached while requesting %s", t.limits.MaxTransactions, txid),
		}, nil
	}
	t.attempts++
	transaction, err := t.source.Transaction(ctx, txid)
	if err != nil {
		if contextError(ctx, err) {
			if ctx.Err() != nil {
				return transactionLoad{}, ctx.Err()
			}
			return transactionLoad{}, err
		}
		result := transactionLoad{
			reason: reasonTransactionUnavailable,
			err:    fmt.Errorf("load transaction %s: %w", txid, err),
		}
		t.loads[txid] = result
		return result, nil
	}
	if transaction.TxID != txid {
		result := transactionLoad{
			reason: reasonInvalidParent,
			err:    fmt.Errorf("transaction %s returned mismatched txid %q", txid, transaction.TxID),
		}
		t.loads[txid] = result
		return result, nil
	}
	result := transactionLoad{transaction: transaction}
	t.loads[txid] = result
	t.trace.Transactions[txid] = transaction
	return result, nil
}

func exactOutput(transaction model.Transaction, vout uint32) (model.Output, bool) {
	for _, output := range transaction.Outputs {
		if output.Index == vout {
			return output, true
		}
	}
	return model.Output{}, false
}

func (t *tracer) sortResults() {
	sort.Slice(t.trace.Edges, func(i, j int) bool {
		left, right := t.trace.Edges[i], t.trace.Edges[j]
		if left.SpendingTxID != right.SpendingTxID {
			return left.SpendingTxID < right.SpendingTxID
		}
		if left.InputIndex != right.InputIndex {
			return left.InputIndex < right.InputIndex
		}
		if left.PrevOut.TxID != right.PrevOut.TxID {
			return left.PrevOut.TxID < right.PrevOut.TxID
		}
		return left.PrevOut.Vout < right.PrevOut.Vout
	})
	sort.Slice(t.trace.Boundaries, func(i, j int) bool {
		left, right := t.trace.Boundaries[i], t.trace.Boundaries[j]
		if left.NodeID != right.NodeID {
			return left.NodeID < right.NodeID
		}
		if left.Reason != right.Reason {
			return left.Reason < right.Reason
		}
		return left.Detail < right.Detail
	})
}

// Trace walks exact previous-output ancestry in breadth-first order. Transaction
// retrieval and expansion are shared across every target.
func Trace(ctx context.Context, source TransactionSource, targets []model.TargetRef, evidence *Evidence, chain model.Chain, limits Limits) (model.Trace, error) {
	if limits.MaxDepth < 0 {
		return model.Trace{}, fmt.Errorf("max depth cannot be negative")
	}
	if limits.MaxTransactions < 0 {
		return model.Trace{}, fmt.Errorf("max transactions cannot be negative")
	}
	if limits.StopIfPrevoutValueSatsGTE < 0 {
		return model.Trace{}, fmt.Errorf("prevout value stop threshold cannot be negative")
	}
	if limits.StopIfInputCountGTE < 0 {
		return model.Trace{}, fmt.Errorf("input count stop threshold cannot be negative")
	}
	if limits.StopIfOutputCountGTE < 0 {
		return model.Trace{}, fmt.Errorf("output count stop threshold cannot be negative")
	}
	if limits.StopBeforeBlockHeight < 0 {
		return model.Trace{}, fmt.Errorf("block height cutoff cannot be negative")
	}
	if limits.MaxDepth == 0 {
		limits.MaxDepth = defaultMaxDepth
	}
	if limits.MaxTransactions == 0 {
		limits.MaxTransactions = defaultMaxTransactions
	}
	if ctx == nil {
		return model.Trace{}, fmt.Errorf("context is nil")
	}
	if source == nil {
		return model.Trace{}, fmt.Errorf("transaction source is nil")
	}
	if err := ctx.Err(); err != nil {
		return model.Trace{}, err
	}

	trace := model.Trace{
		SchemaVersion: model.SchemaVersion,
		BestBlock:     chain.BestBlock,
		BestHeight:    chain.BestHeight,
		Policy:        EffectivePolicy(limits),
		Targets:       make([]model.TargetRef, 0, len(targets)),
		Transactions:  make(map[string]model.Transaction),
		Edges:         make([]model.TraceEdge, 0),
		Boundaries:    make([]model.Boundary, 0),
	}
	t := &tracer{
		source:     source,
		evidence:   evidence,
		limits:     limits,
		trace:      trace,
		loads:      make(map[string]transactionLoad),
		expanded:   make(map[string]bool),
		edges:      make(map[string]model.TraceEdge),
		boundaries: make(map[string]model.Boundary),
	}
	for _, target := range targets {
		t.trace.Targets = append(t.trace.Targets, cloneTarget(target))
		outpoint := model.Outpoint{TxID: target.TxID, Vout: target.Vout}
		if purchaseIDs := evidence.OutpointIDs(outpoint); len(purchaseIDs) > 0 {
			t.matchedOutpointBoundary(outpoint, "confirmed purchase outpoint", purchaseIDs, "outpoint", outpoint.String())
			continue
		}
		t.queue = append(t.queue, traceVisit{outpoint: outpoint, initial: true})
	}

	for cursor := 0; cursor < len(t.queue); cursor++ {
		if err := ctx.Err(); err != nil {
			return model.Trace{}, err
		}
		visit := t.queue[cursor]
		loaded, err := t.load(ctx, visit.outpoint.TxID)
		if err != nil {
			return model.Trace{}, err
		}
		if loaded.err != nil {
			t.outpointBoundary(visit.outpoint, loaded.reason, loaded.err.Error(), nil)
			continue
		}
		transaction := loaded.transaction
		output, exists := exactOutput(transaction, visit.outpoint.Vout)
		if !exists {
			t.outpointBoundary(visit.outpoint, reasonInvalidParent,
				fmt.Sprintf("transaction %s has no output with index %d", visit.outpoint.TxID, visit.outpoint.Vout), nil)
			continue
		}
		if !visit.initial {
			if purchaseIDs := evidence.AddressIDs(output.Address); len(purchaseIDs) > 0 {
				t.matchedOutpointBoundary(visit.outpoint, "confirmed purchase address on exact previous output", purchaseIDs, "address", output.Address)
				continue
			}
		}
		if purchaseIDs := evidence.TransactionIDs(visit.outpoint.TxID); len(purchaseIDs) > 0 {
			t.matchedTransactionBoundary(visit.outpoint.TxID, "confirmed purchase transaction", purchaseIDs)
			continue
		}
		hasCoinbase := false
		for _, input := range transaction.Inputs {
			if input.Coinbase {
				hasCoinbase = true
				break
			}
		}
		if hasCoinbase {
			t.transactionBoundary(visit.outpoint.TxID, reasonCoinbase, "coinbase transaction is a known non-purchase origin", nil)
			continue
		}
		if !visit.initial && t.limits.StopIfPrevoutValueSatsGTE > 0 && output.ValueSats >= t.limits.StopIfPrevoutValueSatsGTE {
			t.policyOutpointBoundary(visit.outpoint, reasonPolicyPrevoutValue, "prevout_value",
				fmt.Sprintf("ancestor output value %d sats is at least the configured %d sats threshold", output.ValueSats, t.limits.StopIfPrevoutValueSatsGTE),
				output.ValueSats, t.limits.StopIfPrevoutValueSatsGTE)
			continue
		}
		if t.limits.StopIfInputCountGTE > 0 && len(transaction.Inputs) >= t.limits.StopIfInputCountGTE {
			t.policyTransactionBoundary(visit.outpoint.TxID, reasonPolicyInputCount, "input_count",
				fmt.Sprintf("transaction input count %d is at least the configured %d threshold", len(transaction.Inputs), t.limits.StopIfInputCountGTE),
				len(transaction.Inputs), t.limits.StopIfInputCountGTE)
			continue
		}
		if t.limits.StopIfOutputCountGTE > 0 && len(transaction.Outputs) >= t.limits.StopIfOutputCountGTE {
			t.policyTransactionBoundary(visit.outpoint.TxID, reasonPolicyOutputCount, "output_count",
				fmt.Sprintf("transaction output count %d is at least the configured %d threshold", len(transaction.Outputs), t.limits.StopIfOutputCountGTE),
				len(transaction.Outputs), t.limits.StopIfOutputCountGTE)
			continue
		}
		if t.limits.StopBeforeBlockHeight > 0 && transaction.BlockHeight > 0 && transaction.BlockHeight < t.limits.StopBeforeBlockHeight {
			t.policyTransactionBoundary(visit.outpoint.TxID, reasonPolicyBlockHeight, "stop_before_block_height",
				fmt.Sprintf("transaction block height %d is before the configured cutoff %d", transaction.BlockHeight, t.limits.StopBeforeBlockHeight),
				0, 0)
			continue
		}
		if t.expanded[visit.outpoint.TxID] {
			continue
		}
		t.expanded[visit.outpoint.TxID] = true

		for inputIndex, input := range transaction.Inputs {
			t.addEdge(model.TraceEdge{
				SpendingTxID: visit.outpoint.TxID,
				InputIndex:   inputIndex,
				PrevOut:      input.PrevOut,
			})
			if purchaseIDs := evidence.OutpointIDs(input.PrevOut); len(purchaseIDs) > 0 {
				t.matchedOutpointBoundary(input.PrevOut, "confirmed purchase outpoint", purchaseIDs, "outpoint", input.PrevOut.String())
				continue
			}
			if visit.depth >= t.limits.MaxDepth {
				_, outputTerminal := t.boundaries[outpointNodeID(input.PrevOut)+"\x00"+reasonPurchase]
				_, transactionPurchase := t.boundaries[transactionNodeID(input.PrevOut.TxID)+"\x00"+reasonPurchase]
				_, coinbase := t.boundaries[transactionNodeID(input.PrevOut.TxID)+"\x00"+reasonCoinbase]
				if t.expanded[input.PrevOut.TxID] || outputTerminal || transactionPurchase || coinbase {
					continue
				}
				t.outpointBoundary(input.PrevOut, reasonMaxDepth,
					fmt.Sprintf("maximum depth %d reached", t.limits.MaxDepth), nil)
				continue
			}
			t.queue = append(t.queue, traceVisit{outpoint: input.PrevOut, depth: visit.depth + 1})
		}
	}
	if err := ctx.Err(); err != nil {
		return model.Trace{}, err
	}
	t.sortResults()
	return t.trace, nil
}
