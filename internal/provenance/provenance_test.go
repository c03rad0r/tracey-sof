package provenance

import (
	"context"
	"errors"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

func testTxID(char string) string {
	return strings.Repeat(char, 64)
}

func testOutpoint(txid string, vout uint32) model.Outpoint {
	return model.Outpoint{TxID: txid, Vout: vout}
}

func testInput(txid string, vout uint32) model.Input {
	return model.Input{PrevOut: testOutpoint(txid, vout)}
}

func testTransaction(txid string, inputs []model.Input, outputs ...model.Output) model.Transaction {
	if len(outputs) == 0 {
		outputs = []model.Output{{Index: 0, ValueSats: 1_000, ScriptType: "wpkh"}}
	}
	return model.Transaction{
		TxID:          txid,
		Version:       2,
		LockTime:      7,
		Inputs:        inputs,
		Outputs:       outputs,
		BlockHash:     "block-" + txid[:4],
		BlockHeight:   123,
		BlockTime:     456,
		Confirmations: 9,
		InActiveChain: true,
	}
}

type memorySource struct {
	transactions map[string]model.Transaction
	errors       map[string]error
	calls        []string
	hook         func(context.Context, string)
}

func newMemorySource(transactions ...model.Transaction) *memorySource {
	source := &memorySource{
		transactions: make(map[string]model.Transaction),
		errors:       make(map[string]error),
	}
	for _, transaction := range transactions {
		source.transactions[transaction.TxID] = transaction
	}
	return source
}

func (s *memorySource) Transaction(ctx context.Context, txid string) (model.Transaction, error) {
	if err := ctx.Err(); err != nil {
		return model.Transaction{}, err
	}
	s.calls = append(s.calls, txid)
	if s.hook != nil {
		s.hook(ctx, txid)
	}
	if err := ctx.Err(); err != nil {
		return model.Transaction{}, err
	}
	if err := s.errors[txid]; err != nil {
		return model.Transaction{}, err
	}
	transaction, exists := s.transactions[txid]
	if !exists {
		return model.Transaction{}, errors.New("transaction not found")
	}
	return transaction, nil
}

func callCount(source *memorySource, txid string) int {
	count := 0
	for _, call := range source.calls {
		if call == txid {
			count++
		}
	}
	return count
}

func mustEvidence(t testing.TB, purchases model.PurchaseFile, annotations model.AnnotationFile) *Evidence {
	t.Helper()
	evidence, err := NewEvidence(purchases, annotations)
	if err != nil {
		t.Fatalf("NewEvidence: %v", err)
	}
	return evidence
}

func testTarget(txid string, vout uint32) model.TargetRef {
	return model.TargetRef{TxID: txid, Vout: vout, Address: "target-address", ValueSats: 1_000}
}

func findBoundary(boundaries []model.Boundary, nodeID, reason string) *model.Boundary {
	for index := range boundaries {
		if boundaries[index].NodeID == nodeID && boundaries[index].Reason == reason {
			return &boundaries[index]
		}
	}
	return nil
}
