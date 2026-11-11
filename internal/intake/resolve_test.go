package intake

import (
	"context"
	"errors"
	"strings"
	"testing"

	"tracey-sof/internal/core"
	"tracey-sof/internal/model"
)

type fakeResolver struct {
	addresses  []core.AddressResolution
	chain      model.Chain
	addressErr error
	txExists   bool
	txHeight   int64
	txNote     string
	txErr      error
	outExists  bool
	outNote    string
	outErr     error
}

func (f *fakeResolver) ResolveAddresses(context.Context, []string) ([]core.AddressResolution, model.Chain, error) {
	return f.addresses, f.chain, f.addressErr
}
func (f *fakeResolver) ConfirmTransaction(context.Context, string) (bool, int64, string, error) {
	return f.txExists, f.txHeight, f.txNote, f.txErr
}
func (f *fakeResolver) ConfirmOutpoint(context.Context, model.Outpoint) (bool, string, error) {
	return f.outExists, f.outNote, f.outErr
}

func resolverFile() File {
	return File{SchemaVersion: 1, GeneratedAt: "2026-11-11T00:00:00Z", Sources: []Source{{
		ID: "src-test", Path: "inbox/test.txt", SHA256: shaOf("data"), MediaType: "text/plain", Extraction: []string{"text"},
		Purchase: PurchaseProposal{ID: "atm-test"},
		Candidates: []Candidate{
			{ID: "cand-address", Kind: KindAddress, Value: testAddress, Role: RoleUnknown, Confidence: ConfidenceHigh, Review: Review{Decision: DecisionPending}},
			{ID: "cand-txid", Kind: KindTxID, Value: testTxID, Role: RoleUnknown, Confidence: ConfidenceMedium, Review: Review{Decision: DecisionPending}},
			{ID: "cand-outpoint", Kind: KindOutpoint, Value: testParent + ":0", Role: RoleUnknown, Confidence: ConfidenceHigh, Review: Review{Decision: DecisionPending}},
		},
	}}}
}

func TestResolveCandidatesAndTraceIndex(t *testing.T) {
	file := resolverFile()
	resolver := &fakeResolver{
		addresses: []core.AddressResolution{{Address: testAddress, CurrentUTXOs: []core.ResolvedUTXO{{Outpoint: testParent + ":1", Address: testAddress, ValueSats: 1000, Confirmations: 3, BlockHeight: 10}}}},
		chain:     model.Chain{BestBlock: strings.Repeat("a", 64), BestHeight: 100},
		txExists:  true, txHeight: 50, outExists: true,
	}
	index := NewTraceIndex(model.Trace{
		Transactions: map[string]model.Transaction{testTxID: {TxID: testTxID, Outputs: []model.Output{{Index: 0, Address: testAddress}}}},
		Targets:      []model.TargetRef{{TxID: testParent, Vout: 0, Address: testAddress}},
		Edges:        []model.TraceEdge{{SpendingTxID: testTxID, PrevOut: model.Outpoint{TxID: testParent, Vout: 0}}},
		Boundaries:   []model.Boundary{{Transaction: testTxID, Outpoint: testParent + ":0"}},
	})
	chain, err := Resolve(context.Background(), resolver, &file, index)
	if err != nil {
		t.Fatal(err)
	}
	if chain.BestHeight != 100 {
		t.Fatalf("chain = %+v", chain)
	}
	address := file.Sources[0].Candidates[0]
	if address.Resolution == nil || !address.Resolution.Valid || len(address.Resolution.CurrentUTXOs) != 1 || !address.Resolution.InTrace {
		t.Fatalf("address resolution = %+v", address.Resolution)
	}
	tx := file.Sources[0].Candidates[1]
	if tx.Resolution == nil || !tx.Resolution.TxIDExists || tx.Resolution.BlockHeight != 50 || !tx.Resolution.InTrace {
		t.Fatalf("txid resolution = %+v", tx.Resolution)
	}
	outpoint := file.Sources[0].Candidates[2]
	if outpoint.Resolution == nil || !outpoint.Resolution.OutpointExists || !outpoint.Resolution.InTrace {
		t.Fatalf("outpoint resolution = %+v", outpoint.Resolution)
	}
	if !index.Contains(Candidate{Kind: KindAddress, Value: testAddress}) ||
		!index.Contains(Candidate{Kind: KindTxID, Value: testTxID}) ||
		!index.Contains(Candidate{Kind: KindOutpoint, Value: testParent + ":0"}) ||
		index.Contains(Candidate{Kind: "bad", Value: testAddress}) {
		t.Fatal("trace index membership failed")
	}
}

func TestResolveCandidateErrors(t *testing.T) {
	file := resolverFile()
	tests := []struct {
		name     string
		resolver *fakeResolver
		want     string
	}{
		{"address rpc", &fakeResolver{addressErr: errors.New("address failure")}, "address failure"},
		{"missing address", &fakeResolver{}, "omitted"},
		{"txid rpc", &fakeResolver{addresses: []core.AddressResolution{{Address: testAddress}}, txErr: errors.New("tx failure")}, "tx failure"},
		{"outpoint rpc", &fakeResolver{addresses: []core.AddressResolution{{Address: testAddress}}, outErr: errors.New("outpoint failure")}, "outpoint failure"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			copyFile := file
			if _, err := Resolve(context.Background(), test.resolver, &copyFile, NewTraceIndex(model.Trace{})); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Resolve = %v", err)
			}
		})
	}
}

func TestResolveNotes(t *testing.T) {
	file := resolverFile()
	resolver := &fakeResolver{addresses: []core.AddressResolution{{Address: testAddress}}, txNote: "tx note", outNote: "outpoint note"}
	if _, err := Resolve(context.Background(), resolver, &file, NewTraceIndex(model.Trace{})); err != nil {
		t.Fatal(err)
	}
	if file.Sources[0].Candidates[1].Resolution.Note != "tx note" || file.Sources[0].Candidates[2].Resolution.Note != "outpoint note" {
		t.Fatalf("notes = %+v", file.Sources[0].Candidates)
	}
}
