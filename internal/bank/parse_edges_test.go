package bank

import (
	"bytes"
	"strings"
	"testing"
)

func TestCSVQuotedFieldsMalformedInputAndLimit(t *testing.T) {
	header := "Buchungstag;Valutadatum;Name Zahlungsbeteiligter;Verwendungszweck;Buchungstext;Betrag;Waehrung\n"
	quoted := header + `"02.01.2024";"02.01.2024";"Shop, Inc.";"SEPA REF-001";;"-1,25";"EUR"` + "\n"
	rows, err := ParseCSV(strings.NewReader(quoted))
	if err != nil || len(rows) != 1 || rows[0].Name != "Shop, Inc." || rows[0].AmountCents != -125 {
		t.Fatalf("quoted csv = %+v, %v", rows, err)
	}
	if _, err := ParseCSV(strings.NewReader(header + `"01.01.2024";"unterminated` + "\n")); err == nil {
		t.Fatal("malformed quoted field accepted")
	}
	if _, err := ParseCSV(bytes.NewReader(make([]byte, maxBankCSVBytes+1))); err == nil {
		t.Fatal("oversized bank CSV accepted")
	}
}
