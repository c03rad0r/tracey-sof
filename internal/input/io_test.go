package input

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/btcsuite/btcd/btcec/v2"
	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg"
	"tracey-sof/internal/model"
)

const (
	address = "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"
	txid    = "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16"
)

func testOwned(t *testing.T) model.Owned {
	t.Helper()
	key, err := hdkeychain.NewKeyFromString("xpub661MyMwAqRbcFtXgS5sYJABqqG9YLmC4Q1Rdap9gSE8NqtwybGhePY2gZ29ESFjqJoCu1Rupje8YtGqsefD265TMg7usUDFdp6W1EGMcet8")
	if err != nil {
		t.Fatal(err)
	}
	return model.Owned{SchemaVersion: 1, Network: "mainnet", Addresses: []model.OwnedAddress{{Address: address, Label: "owned"}},
		UTXOs:       []model.OwnedUTXO{{TxID: txid, Vout: 2}},
		Descriptors: []model.OwnedDescriptor{{ID: "desc", Descriptor: "addr(" + address + ")", Range: &[2]uint32{0, 2}}},
		Xpubs:       []model.OwnedXpub{{ID: "xp", Xpub: key.String(), ScriptType: "wpkh", Branches: []uint32{0, 1}, Range: [2]uint32{0, 999}}}}
}

func validPurchases() model.PurchaseFile {
	amount := int64(1000)
	return model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{{ID: "buy-1", Date: time.Unix(0, 0).UTC().Format(time.RFC3339), Provider: "ATM", TxIDs: []string{txid}, Addresses: []string{address}, Outpoints: []model.Outpoint{{TxID: txid, Vout: 0}}, AmountSats: &amount, Fiat: &model.Fiat{Amount: "10.50", Currency: "EUR"}}}}
}

func TestOwnedValidationAndDescriptors(t *testing.T) {
	owned := testOwned(t)
	if err := ValidateOwned(owned); err != nil {
		t.Fatal(err)
	}
	objects, err := ScanObjects(owned)
	if err != nil || len(objects) != 4 {
		t.Fatalf("ScanObjects len=%d err=%v", len(objects), err)
	}
	if !DescriptorHasRange(objects[3].Descriptor) || DescriptorHasRange(objects[0].Descriptor) {
		t.Error("range detection failed")
	}
	for _, kind := range []string{"pkh", "sh(wpkh)", "wpkh", "tr"} {
		owned.Xpubs[0].ScriptType = kind
		if _, err := ScanObjects(owned); err != nil {
			t.Errorf("%s: %v", kind, err)
		}
	}
	owned.Xpubs[0].ScriptType = "unknown"
	if _, err := ScanObjects(owned); err == nil {
		t.Error("unsupported xpub descriptor should fail")
	}
}

func TestOwnedValidationFailures(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*model.Owned)
	}{
		{"version", func(o *model.Owned) { o.SchemaVersion = 2 }},
		{"network", func(o *model.Owned) { o.Network = "testnet" }},
		{"empty", func(o *model.Owned) { o.Addresses = nil; o.UTXOs = nil; o.Descriptors = nil; o.Xpubs = nil }},
		{"bad address", func(o *model.Owned) { o.Addresses[0].Address = "bad" }},
		{"duplicate address", func(o *model.Owned) { o.Addresses = append(o.Addresses, o.Addresses[0]) }},
		{"bad txid", func(o *model.Owned) { o.UTXOs[0].TxID = "bad" }},
		{"duplicate outpoint", func(o *model.Owned) { o.UTXOs = append(o.UTXOs, o.UTXOs[0]) }},
		{"empty descriptor ID", func(o *model.Owned) { o.Descriptors[0].ID = "" }},
		{"unsafe descriptor ID", func(o *model.Owned) { o.Descriptors[0].ID = "../bad" }},
		{"duplicate descriptor ID", func(o *model.Owned) { o.Xpubs[0].ID = "desc" }},
		{"private descriptor", func(o *model.Owned) { o.Descriptors[0].Descriptor = "wpkh(xprv123)" }},
		{"space descriptor", func(o *model.Owned) { o.Descriptors[0].Descriptor = " addr(" + address + ")" }},
		{"bad range", func(o *model.Owned) { o.Descriptors[0].Range = &[2]uint32{2, 1} }},
		{"large range", func(o *model.Owned) { o.Descriptors[0].Range = &[2]uint32{0, 10000} }},
		{"invalid xpub", func(o *model.Owned) { o.Xpubs[0].Xpub = "not-an-xpub" }},
		{"xpub branches", func(o *model.Owned) { o.Xpubs[0].Branches = nil }},
		{"duplicate branch", func(o *model.Owned) { o.Xpubs[0].Branches = []uint32{0, 0} }},
		{"hardened branch", func(o *model.Owned) { o.Xpubs[0].Branches = []uint32{2} }},
		{"bad xpub range", func(o *model.Owned) { o.Xpubs[0].Range = [2]uint32{2, 1} }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			owned := testOwned(t)
			tc.mutate(&owned)
			if err := ValidateOwned(owned); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
	private, err := btcec.NewPrivateKey()
	if err != nil {
		t.Fatal(err)
	}
	wif, err := btcutil.NewWIF(private, &chaincfg.MainNetParams, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := validatePublicDescriptor("wpkh(" + wif.String() + ")"); err == nil {
		t.Error("WIF was not rejected")
	}
	owned2 := testOwned(t)
	owned2.Xpubs[0].ScriptType = "bad"
	if err := ValidateOwned(owned2); err == nil {
		t.Error("script type should fail")
	}
}

func TestPurchaseAndAnnotationValidation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "receipt.txt"), []byte("receipt"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("receipt"))
	purchases := validPurchases()
	purchases.Purchases[0].Artifacts = []model.Artifact{{Path: "receipt.txt", SHA256: hex.EncodeToString(sum[:])}}
	if err := ValidatePurchases(purchases, dir, true); err != nil {
		t.Fatal(err)
	}
	annotations := model.AnnotationFile{SchemaVersion: 1, Transactions: map[string]model.Annotation{txid: {Label: "purchase"}}, Addresses: map[string]model.Annotation{address: {Note: "mine"}}, Outpoints: map[string]model.Annotation{txid + ":0": {Label: "coin"}}, PurchaseLinks: []model.PurchaseLink{{Target: txid, PurchaseID: "buy-1", Decision: "confirmed"}, {Target: txid + ":0", PurchaseID: "buy-1", Decision: "rejected"}, {Target: address, PurchaseID: "buy-1", Decision: "confirmed"}}}
	if err := ValidateAnnotations(annotations); err != nil {
		t.Fatal(err)
	}
	bad := purchases
	bad.Purchases[0].Artifacts[0].SHA256 = strings.Repeat("0", 64)
	if err := ValidatePurchases(bad, dir, true); err == nil {
		t.Fatal("expected invalid hash")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*model.PurchaseFile)
	}{
		{"version", func(p *model.PurchaseFile) { p.SchemaVersion = 2 }},
		{"empty purchases", func(p *model.PurchaseFile) { p.Purchases = nil }},
		{"duplicate ID", func(p *model.PurchaseFile) { p.Purchases = append(p.Purchases, p.Purchases[0]) }},
		{"unsafe ID", func(p *model.PurchaseFile) { p.Purchases[0].ID = "x/y" }},
		{"date", func(p *model.PurchaseFile) { p.Purchases[0].Date = "yesterday" }},
		{"txid", func(p *model.PurchaseFile) { p.Purchases[0].TxIDs[0] = "bad" }},
		{"address", func(p *model.PurchaseFile) { p.Purchases[0].Addresses[0] = "bad" }},
		{"outpoint", func(p *model.PurchaseFile) { p.Purchases[0].Outpoints[0].TxID = "bad" }},
		{"amount", func(p *model.PurchaseFile) { zero := int64(0); p.Purchases[0].AmountSats = &zero }},
		{"fiat", func(p *model.PurchaseFile) { p.Purchases[0].Fiat.Currency = "eur" }},
		{"fiat amount", func(p *model.PurchaseFile) { p.Purchases[0].Fiat.Amount = "01" }},
		{"path", func(p *model.PurchaseFile) { p.Purchases[0].Artifacts[0].Path = "../secret" }},
		{"absolute path", func(p *model.PurchaseFile) { p.Purchases[0].Artifacts[0].Path = "/etc/passwd" }},
		{"artifact hash", func(p *model.PurchaseFile) { p.Purchases[0].Artifacts[0].SHA256 = "bad" }},
		{"duplicate artifact", func(p *model.PurchaseFile) {
			p.Purchases[0].Artifacts = append(p.Purchases[0].Artifacts, p.Purchases[0].Artifacts[0])
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			current := validPurchases()
			current.Purchases[0].Artifacts = append([]model.Artifact(nil), purchases.Purchases[0].Artifacts...)
			tc.mutate(&current)
			if err := ValidatePurchases(current, dir, true); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	purchases.Purchases[0].Artifacts[0].SHA256 = strings.Repeat("0", 64)
	if err := ValidatePurchases(purchases, dir, true); err == nil {
		t.Error("expected hash validation error")
	}
	badAnnotations := []model.AnnotationFile{{SchemaVersion: 2}, {SchemaVersion: 1, Transactions: map[string]model.Annotation{"bad": {}}}, {SchemaVersion: 1, Addresses: map[string]model.Annotation{"bad": {}}}, {SchemaVersion: 1, Outpoints: map[string]model.Annotation{"bad": {}}}, {SchemaVersion: 1, PurchaseLinks: []model.PurchaseLink{{Target: "bad", PurchaseID: "buy", Decision: "maybe"}}}}
	for i, item := range badAnnotations {
		if err := ValidateAnnotations(item); err == nil {
			t.Errorf("bad annotation %d accepted", i)
		}
	}
}

func TestLoadAndRoundTrip(t *testing.T) {
	dir := t.TempDir()
	write := func(name string, value any) {
		data, _ := json.Marshal(value)
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("owned.json", testOwned(t))
	write("purchases.json", validPurchases())
	write("annotations.json", EmptyAnnotations())
	if _, err := LoadOwned(filepath.Join(dir, "owned.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPurchases(filepath.Join(dir, "purchases.json"), true); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadAnnotations(filepath.Join(dir, "annotations.json"))
	if err != nil {
		t.Fatal(err)
	}
	loaded.Transactions = map[string]model.Annotation{txid: {Label: "origin"}}
	if err := SaveAnnotations(filepath.Join(dir, "nested", "annotations.json"), loaded); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAnnotations(filepath.Join(dir, "nested", "annotations.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAnnotations(filepath.Join(dir, "absent.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bad.json"), []byte(`{"schema_version":1} {"x":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAnnotations(filepath.Join(dir, "bad.json")); err == nil {
		t.Error("multiple JSON values accepted")
	}
	if err := os.WriteFile(filepath.Join(dir, "unknown.json"), []byte(`{"schema_version":1,"unknown":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAnnotations(filepath.Join(dir, "unknown.json")); err == nil {
		t.Error("unknown fields accepted")
	}
	if _, err := LoadOwned(filepath.Join(dir, "missing.json")); err == nil {
		t.Error("missing owned accepted")
	}
}

func TestEmptyAnnotations(t *testing.T) {
	file := EmptyAnnotations()
	if file.SchemaVersion != 1 || file.Transactions == nil || file.Addresses == nil || file.Outpoints == nil {
		t.Fatal("empty file not initialized")
	}
}
