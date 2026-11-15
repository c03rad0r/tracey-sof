package bank

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func date(value string) time.Time {
	parsed, _ := time.Parse("02.01.2006", value)
	return parsed
}

func TestMatchEvents(t *testing.T) {
	transactions := []Transaction{
		{BookingDate: date("02.01.2024"), Name: "Example Merchant", Purpose: "SEPA-UMSATZ EXAMPLE-REF-001", AmountCents: -12345, Currency: "EUR"},
		{BookingDate: date("16.01.2024"), Name: "Example Merchant", Purpose: "SEPA-UMSATZ EXAMPLE-REF-001", AmountCents: -6789, Currency: "EUR"},
		{BookingDate: date("10.02.2024"), Name: "Example Merchant", Purpose: "late", AmountCents: -6789},
		{BookingDate: date("02.01.2024"), Name: "Example Merchant", Purpose: "credit", AmountCents: 12345},
		{BookingDate: date("02.01.2024"), Name: "Other Shop", Purpose: "x", AmountCents: -12345},
	}
	events := []Event{
		{Kind: "inbound_payout", ID: "one", Date: "2024-01-02", TxID: "a", Outpoint: "a:0", AmountSats: 100},
		{Kind: "purchase", ID: "two", Date: "2024-01-15", TxID: "b", Outpoint: "b:0", AmountSats: 200},
		{Kind: "purchase", ID: "three", Date: "2024-02-28", TxID: "c", Outpoint: "c:0", AmountSats: 300},
	}
	records := MatchEvents(events, transactions, []string{"example merchant", " "}, 3)
	if len(records) != 3 {
		t.Fatalf("records = %d", len(records))
	}
	byID := map[string]MatchRecord{}
	for _, record := range records {
		byID[record.ID] = record
	}
	if byID["one"].Status != "matched" || byID["one"].BankAmountEUR != "123.45" || byID["one"].BankReference != "EXAMPLE-REF-001" {
		t.Errorf("one = %+v", byID["one"])
	}
	if byID["two"].Status != "matched" || byID["two"].BankDate != "2024-01-16" {
		t.Errorf("two = %+v", byID["two"])
	}
	if byID["three"].Status != "missing" {
		t.Errorf("three = %+v", byID["three"])
	}

	ambiguous := MatchEvents(events[:1], append(transactions, Transaction{BookingDate: date("02.01.2024"), Name: "Example Merchant", AmountCents: -10000}), []string{"example"}, 3)
	if ambiguous[0].Status != "ambiguous" {
		t.Errorf("ambiguous = %+v", ambiguous[0])
	}
	empty := MatchEvents(nil, transactions, nil, 3)
	if len(empty) != 0 {
		t.Errorf("empty events = %+v", empty)
	}
	invalid := MatchEvents([]Event{{Kind: "purchase", ID: "bad", Date: "not-a-date"}}, transactions, []string{"example"}, 3)
	if invalid[0].Status != "missing" {
		t.Errorf("invalid date = %+v", invalid[0])
	}
	noCurrency := MatchEvents([]Event{{Kind: "purchase", ID: "nc", Date: "2024-01-02"}},
		[]Transaction{{BookingDate: date("02.01.2024"), Name: "Example Merchant", AmountCents: -100}}, []string{"example"}, 3)
	if noCurrency[0].Status != "matched" || noCurrency[0].BankCurrency != "EUR" {
		t.Errorf("currency default = %+v", noCurrency[0])
	}
	if (Event{Kind: "purchase", ID: "x"}).Key() != "purchase|x" || (MatchRecord{Kind: "purchase", ID: "x"}).Key() != "purchase|x" {
		t.Error("keys mismatch")
	}
	sameDay := MatchEvents([]Event{
		{Kind: "purchase", ID: "z", Date: "2024-01-02"},
		{Kind: "purchase", ID: "a", Date: "2024-01-02"},
	}, transactions, []string{"example"}, 3)
	if len(sameDay) != 2 || sameDay[0].ID != "a" {
		t.Errorf("same-day ordering = %+v", sameDay)
	}
}

func TestCacheRoundTripAndErrors(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bank-matches.json")
	cache, err := LoadCache(path)
	if err != nil || len(cache.Records) != 0 {
		t.Fatalf("missing cache = %+v, %v", cache, err)
	}
	cache = Cache{BankFile: "x.csv", SHA256: "abc", Records: []MatchRecord{{Kind: "purchase", ID: "one", Status: "matched"}}}
	if err := SaveCache(path, cache); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadCache(path)
	if err != nil || len(loaded.Records) != 1 || loaded.Records[0].ID != "one" {
		t.Fatalf("loaded = %+v, %v", loaded, err)
	}
	if err := os.WriteFile(path, []byte("{bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCache(path); err == nil {
		t.Fatal("invalid json accepted")
	}
	if err := os.WriteFile(path, []byte(`{"schema_version":99}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadCache(path); err == nil {
		t.Fatal("unsupported schema accepted")
	}
	blocked := filepath.Join(dir, "file")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveCache(filepath.Join(blocked, "cache.json"), cache); err == nil {
		t.Fatal("save below a file was accepted")
	}
	if _, err := LoadCache(dir); err == nil {
		t.Fatal("directory cache accepted")
	}
	targetDir := filepath.Join(dir, "target.json")
	if err := os.Mkdir(targetDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := SaveCache(targetDir, cache); err == nil {
		t.Fatal("save over a directory was accepted")
	}
}
