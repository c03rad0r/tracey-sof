package bank

import (
	"errors"
	"strings"
	"testing"
)

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("read failed") }

func TestParseCSV(t *testing.T) {
	input := "\ufeffBuchungstag;Valutadatum;Name Zahlungsbeteiligter;Verwendungszweck;Buchungstext;Betrag;Waehrung\r\n" +
		"02.01.2024;02.01.2024;Example Merchant;SEPA-UMSATZ EXAMPLE-REF-001;;-123,45;EUR\r\n" +
		";;;;ignore me;-1,00;EUR\r\n" +
		"07.11.2024;07.11.2024;Shop;;Card;1.234,56;EUR\r\n"
	rows, err := ParseCSV(strings.NewReader(input))
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows = %d, want 2", len(rows))
	}
	if rows[0].AmountCents != -12345 || rows[0].Name != "Example Merchant" || rows[0].BookingDate.Format("2006-01-02") != "2024-01-02" {
		t.Errorf("first row = %+v", rows[0])
	}
	if rows[1].AmountCents != 123456 || rows[1].Currency != "EUR" {
		t.Errorf("second row = %+v", rows[1])
	}
	if rows[0].Reference() != "EXAMPLE-REF-001" {
		t.Errorf("reference = %q", rows[0].Reference())
	}
	if !strings.Contains(rows[0].Blob(), "example merchant") {
		t.Errorf("blob = %q", rows[0].Blob())
	}
	if AmountEUR(-12345) != "-123.45" || AmountEUR(5) != "0.05" {
		t.Errorf("AmountEUR formatting wrong")
	}
	if (Transaction{}).Reference() != "" {
		t.Errorf("empty reference = %q", (Transaction{}).Reference())
	}
	partial := "Buchungstag;Valutadatum;Name Zahlungsbeteiligter;Verwendungszweck;Buchungstext;Betrag\n" +
		"01.01.2024;bad;Shop;;;-1,00\n" +
		"02.01.2024;02.01.2024;Shop;;;\n"
	rows, err = ParseCSV(strings.NewReader(partial))
	if err != nil || len(rows) != 1 || rows[0].Currency != "" || rows[0].ValueDate.Format("2006-01-02") != "2024-01-01" {
		t.Fatalf("partial csv = %+v, %v", rows, err)
	}
}

func TestParseCSVErrors(t *testing.T) {
	if _, err := ParseCSV(strings.NewReader("")); err == nil {
		t.Fatal("empty input accepted")
	}
	if _, err := ParseCSV(strings.NewReader("a;b\n")); err == nil {
		t.Fatal("header-only input accepted")
	}
	if _, err := ParseCSV(errorReader{}); err == nil {
		t.Fatal("reader error accepted")
	}
	rows, err := ParseCSV(strings.NewReader("Buchungstag;Betrag\nnot-a-date;1,00\n01.01.2024;not-a-number\n"))
	if err != nil || len(rows) != 0 {
		t.Fatalf("invalid rows = %v, %v", rows, err)
	}
}
