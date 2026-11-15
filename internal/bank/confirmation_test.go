package bank

import (
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

func TestParseGermanAmountExactValues(t *testing.T) {
	cases := []struct {
		value string
		want  int64
	}{
		{"1.234,56", 123456}, {"-0,05", -5}, {"123", 12300}, {"+1,2", 120},
		{"90.071.992.547.409,99", 9007199254740999}, {"0,00", 0},
	}
	for _, test := range cases {
		got, err := parseGermanAmount(test.value)
		if err != nil || got != test.want {
			t.Errorf("parseGermanAmount(%q) = %d, %v; want %d", test.value, got, err, test.want)
		}
	}
	for _, value := range []string{"", "-", ".", ",50", "1,234", "1,x", strings.Repeat("9", 21) + ",99", "92233720368547758.08", "x,00", "one,00"} {
		if _, err := parseGermanAmount(value); err == nil {
			t.Errorf("invalid amount %q was accepted", value)
		}
	}
}

func TestConfirmationIDsAndApplyGuard(t *testing.T) {
	if _, err := ParseConfirmationIDs(""); err == nil {
		t.Fatal("empty confirmation accepted")
	}
	confirmed, err := ParseConfirmationIDs("purchase|p1, inbound_payout|x:0")
	if err != nil || !confirmed["purchase|p1"] || !confirmed["inbound_payout|x:0"] {
		t.Fatalf("confirmed = %+v, %v", confirmed, err)
	}
	for _, value := range []string{"p1", "other|p1", "purchase|"} {
		if _, err := ParseConfirmationIDs(value); err == nil {
			t.Errorf("invalid confirmation %q accepted", value)
		}
	}

	purchases := model.PurchaseFile{SchemaVersion: model.SchemaVersion, Purchases: []model.Purchase{{
		ID: "p1", Fiat: &model.Fiat{Amount: "10.00", Currency: "USD"},
	}}}
	cache := Cache{Artifact: "evidence/bank/stmt.csv", Records: []MatchRecord{{
		Kind: "purchase", ID: "p1", Status: "matched", BankAmountEUR: "12.00", BankCurrency: "EUR",
	}, {
		Kind: "inbound_payout", ID: "x:0", Outpoint: strings.Repeat("a", 64) + ":0", Status: "missing",
	}}}
	result, updated, added := Apply(purchases, model.Report{}, cache, "provider")
	if updated != 0 || added != 0 || len(result.Purchases) != 1 {
		t.Fatalf("unconfirmed matches were applied: %d/%d", updated, added)
	}
	for index := range cache.Records {
		cache.Records[index].Confirmed = confirmed[cache.Records[index].Key()]
	}
	result, updated, added = Apply(purchases, model.Report{}, cache, "provider")
	if updated != 0 || added != 0 || result.Purchases[0].Fiat.Amount != "10.00" {
		t.Fatalf("mismatched fiat was overwritten: %d/%d, %+v", updated, added, result.Purchases[0])
	}
}
