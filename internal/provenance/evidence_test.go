package provenance

import (
	"reflect"
	"testing"

	"tracey-sof/internal/model"
)

func TestEvidenceIndexesScopesAndManualOverrides(t *testing.T) {
	txA := testTxID("a")
	txB := testTxID("b")
	purchases := model.PurchaseFile{Purchases: []model.Purchase{
		{ID: "z-purchase", TxIDs: []string{txA, txA}, Addresses: []string{"addr-auto"}, Outpoints: []model.Outpoint{testOutpoint(txA, 1)}},
		{ID: "a-purchase", TxIDs: []string{txA}, Addresses: []string{"addr-auto"}},
		{ID: "other", TxIDs: []string{txB}},
	}}
	annotations := model.AnnotationFile{PurchaseLinks: []model.PurchaseLink{
		{Target: txB, PurchaseID: "a-purchase", Decision: "confirmed"},
		{Target: txA, PurchaseID: "other", Decision: "confirmed"},
		{Target: txA, PurchaseID: "z-purchase", Decision: "rejected"},
		{Target: txA, PurchaseID: "z-purchase", Decision: "confirmed"},
		{Target: txB + ":4", PurchaseID: "other", Decision: "confirmed"},
		{Target: txA + ":1", PurchaseID: "z-purchase", Decision: "rejected"},
		{Target: "addr-auto", PurchaseID: "z-purchase", Decision: "rejected"},
		{Target: "addr-new", PurchaseID: "other", Decision: "confirmed"},
	}}
	evidence := mustEvidence(t, purchases, annotations)

	if got, want := evidence.TransactionIDs(txA), []string{"a-purchase", "other"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("TransactionIDs(%s) = %v, want %v", txA[:8], got, want)
	}
	if got, want := evidence.TransactionIDs(txB), []string{"a-purchase", "other"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("confirmed transaction IDs = %v, want %v", got, want)
	}
	if got, want := evidence.OutpointIDs(testOutpoint(txA, 1)), []string(nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("rejected outpoint IDs = %v, want nil", got)
	}
	if got, want := evidence.OutpointIDs(testOutpoint(txB, 4)), []string{"other"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("confirmed outpoint IDs = %v, want %v", got, want)
	}
	if got, want := evidence.AddressIDs("addr-auto"), []string{"a-purchase"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("address IDs = %v, want %v", got, want)
	}
	if got, want := evidence.AddressIDs("addr-new"), []string{"other"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("confirmed address IDs = %v, want %v", got, want)
	}
	if got := evidence.TransactionIDs(testTxID("c")); got != nil {
		t.Fatalf("unknown transaction IDs = %v, want nil", got)
	}

	got := evidence.TransactionIDs(txB)
	got[0] = "mutation"
	if next := evidence.TransactionIDs(txB)[0]; next != "a-purchase" {
		t.Fatalf("caller mutated immutable evidence: %q", next)
	}

	var nilEvidence *Evidence
	if nilEvidence.TransactionIDs(txA) != nil || nilEvidence.OutpointIDs(testOutpoint(txA, 0)) != nil || nilEvidence.AddressIDs("x") != nil {
		t.Fatal("nil evidence should return no matches")
	}
}

func TestNewEvidenceRejectsMalformedInput(t *testing.T) {
	validPurchases := model.PurchaseFile{Purchases: []model.Purchase{{ID: "known"}}}
	tests := []struct {
		name        string
		purchases   model.PurchaseFile
		annotations model.AnnotationFile
	}{
		{name: "empty purchase ID", purchases: model.PurchaseFile{Purchases: []model.Purchase{{ID: ""}}}},
		{name: "duplicate purchase ID", purchases: model.PurchaseFile{Purchases: []model.Purchase{{ID: "same"}, {ID: "same"}}}},
		{name: "unknown linked purchase", purchases: validPurchases, annotations: model.AnnotationFile{PurchaseLinks: []model.PurchaseLink{{Target: "address", PurchaseID: "missing", Decision: "confirmed"}}}},
		{name: "empty target", purchases: validPurchases, annotations: model.AnnotationFile{PurchaseLinks: []model.PurchaseLink{{PurchaseID: "known", Decision: "confirmed"}}}},
		{name: "bad decision", purchases: validPurchases, annotations: model.AnnotationFile{PurchaseLinks: []model.PurchaseLink{{Target: "address", PurchaseID: "known", Decision: "maybe"}}}},
		{name: "malformed outpoint", purchases: validPurchases, annotations: model.AnnotationFile{PurchaseLinks: []model.PurchaseLink{{Target: "not-an-outpoint:0", PurchaseID: "known", Decision: "rejected"}}}},
		{name: "surrounding whitespace", purchases: validPurchases, annotations: model.AnnotationFile{PurchaseLinks: []model.PurchaseLink{{Target: " address ", PurchaseID: "known", Decision: "confirmed"}}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewEvidence(test.purchases, test.annotations); err == nil {
				t.Fatal("NewEvidence succeeded, want error")
			}
		})
	}
}

func TestEvidenceIndexesEveryPurchaseScope(t *testing.T) {
	txid := testTxID("d")
	evidence := mustEvidence(t, model.PurchaseFile{Purchases: []model.Purchase{{
		ID: "purchase", TxIDs: []string{txid}, Addresses: []string{"address"},
		Outpoints: []model.Outpoint{{TxID: txid, Vout: 7}},
	}}}, model.AnnotationFile{})
	if got := evidence.TransactionIDs(txid); !reflect.DeepEqual(got, []string{"purchase"}) {
		t.Fatalf("transaction index = %v", got)
	}
	if got := evidence.OutpointIDs(model.Outpoint{TxID: txid, Vout: 7}); !reflect.DeepEqual(got, []string{"purchase"}) {
		t.Fatalf("outpoint index = %v", got)
	}
	if got := evidence.AddressIDs("address"); !reflect.DeepEqual(got, []string{"purchase"}) {
		t.Fatalf("address index = %v", got)
	}
}
