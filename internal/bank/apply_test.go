package bank

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

func TestStoreArtifactAndApply(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "bank.csv")
	if err := os.WriteFile(source, []byte("a;b\n1;2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	artifact, digest, err := StoreArtifact(filepath.Join(root, "data"), source)
	if err != nil {
		t.Fatal(err)
	}
	if artifact != "evidence/bank/bank.csv" || len(digest) != 64 {
		t.Fatalf("artifact = %q, digest = %q", artifact, digest)
	}
	if _, _, err := StoreArtifact(filepath.Join(root, "data"), source); err != nil {
		t.Fatalf("repeat store: %v", err)
	}
	if err := os.WriteFile(source, []byte("changed"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, _, err := StoreArtifact(filepath.Join(root, "data"), source)
	if err != nil || !strings.HasPrefix(second, "evidence/bank/") || second == artifact {
		t.Fatalf("changed artifact = %q, %v", second, err)
	}
	if _, _, err := StoreArtifact(filepath.Join(root, "data"), filepath.Join(root, "missing.csv")); err == nil {
		t.Fatal("missing file accepted")
	}

	txid := strings.Repeat("a", 64)
	secondTx := strings.Repeat("b", 64)
	amount := int64(100)
	purchases := model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{{
		ID: "p1", Outpoints: []model.Outpoint{{TxID: txid, Vout: 0}}, AmountSats: &amount,
	}}}
	cache := Cache{SHA256: digest, Artifact: artifact, Records: []MatchRecord{
		{Kind: "purchase", ID: "p1", Status: "matched", Confirmed: true, BankDate: "2024-01-02", BankAmountEUR: "123.45", BankCurrency: "EUR", BankReference: "EXAMPLE-REF-001"},
		{Kind: "inbound_payout", ID: secondTx + ":1", Date: "2024-01-16", DateTime: "2024-01-16T12:00:00Z", TxID: secondTx, Outpoint: secondTx + ":1", AmountSats: 200, Height: 800000, Provider: "unknown exchange", Status: "matched", Confirmed: true, BankDate: "2024-01-15", BankAmountEUR: "67.89", BankCurrency: "EUR", BankReference: "EXAMPLE-REF-001"},
		{Kind: "purchase", ID: "missing", Status: "missing"},
	}}
	updated, added := 0, 0
	purchases, updated, added = Apply(purchases, model.Report{}, cache, "Example Exchange")
	if updated != 1 || added != 1 || len(purchases.Purchases) != 2 {
		t.Fatalf("apply = updated %d added %d purchases %d", updated, added, len(purchases.Purchases))
	}
	if purchases.Purchases[0].Fiat == nil || purchases.Purchases[0].Fiat.Amount != "123.45" || len(purchases.Purchases[0].Artifacts) != 1 {
		t.Errorf("purchase evidence = %+v", purchases.Purchases[0])
	}
	created := purchases.Purchases[1]
	if created.Provider != "Example Exchange" || created.StatementDate != "2024-01-15" || created.Fiat == nil || created.Height != 800000 {
		t.Errorf("created purchase = %+v", created)
	}
	_, updated, added = Apply(purchases, model.Report{}, cache, "Example Exchange")
	if updated != 0 || added != 0 {
		t.Fatalf("reapply = %d/%d", updated, added)
	}
	if eventDate(MatchRecord{Date: "2024-01-02"}) != "2024-01-02T00:00:00Z" || eventDate(MatchRecord{}) != "" {
		t.Error("eventDate fallbacks wrong")
	}
	emptyProvider := Cache{SHA256: digest, Artifact: artifact, Records: []MatchRecord{
		{Kind: "inbound_payout", ID: secondTx + ":2", Date: "2024-02-01", DateTime: "2024-02-01T00:00:00Z", TxID: secondTx, Outpoint: secondTx + ":2", AmountSats: 300, Status: "matched", Confirmed: true, BankAmountEUR: "1.00", BankCurrency: "EUR"},
		{Kind: "inbound_payout", ID: secondTx + ":3", Date: "2024-02-01", DateTime: "2024-02-01T00:00:00Z", TxID: secondTx, Outpoint: secondTx + ":3", AmountSats: 400, Status: "matched", Confirmed: true, BankAmountEUR: "1.00", BankCurrency: "EUR"},
	}}
	more, _, added := Apply(model.PurchaseFile{SchemaVersion: 1}, model.Report{}, emptyProvider, "Fallback Provider")
	if added != 2 {
		t.Fatalf("fallback apply = %d", added)
	}
	if more.Purchases[0].Provider != "Fallback Provider" || more.Purchases[1].ID == more.Purchases[0].ID {
		t.Errorf("fallback purchases = %+v", more.Purchases)
	}
	bad := Cache{Artifact: artifact, SHA256: digest, Records: []MatchRecord{{Kind: "inbound_payout", ID: "bad", Outpoint: "bad", TxID: strings.Repeat("c", 64), Status: "matched", Confirmed: true}}}
	if _, _, added := Apply(model.PurchaseFile{SchemaVersion: 1}, model.Report{}, bad, "x"); added != 0 {
		t.Fatal("invalid outpoint was applied")
	}
}

func TestStoreArtifactDirectoryError(t *testing.T) {
	root := t.TempDir()
	source := filepath.Join(root, "bank.csv")
	if err := os.WriteFile(source, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(root, "data")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := StoreArtifact(blocked, source); err == nil {
		t.Fatal("artifact below a file was accepted")
	}
	root2 := t.TempDir()
	dataDir := filepath.Join(root2, "data")
	if err := os.MkdirAll(filepath.Join(dataDir, "evidence", "bank", "bank.csv"), 0o700); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(root2, "bank.csv")
	if err := os.WriteFile(other, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := StoreArtifact(dataDir, other); err == nil {
		t.Fatal("write over a directory was accepted")
	}
}
