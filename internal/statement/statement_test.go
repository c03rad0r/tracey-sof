package statement

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const checkingText = `Example Holder
Buchungen vom 01.05.2017 00.00 Uhr bis 31.05.2017 24.00 Uhr
Girokonto                                                                                        Kontoauszug Nr. 5/2017
IBAN: DE00 0000 0000 0000 0000 00

Buchungstag         Buchungstext                                                 Wertstellung                 Betrag in EUR
                     Kontostand alt am 30.04.2017                                                                     100,00
 03.05.2017          PayPal Europe SEPA-BASISLASTSCHRIFT                          03.05.2017                        -220,00
                     SVWZ+ Example purpose EREF+ REF-1 MREF+ M-1 CRED+ DE00ZZZ
 05.05.2017          Example Merchant SEPA-ÜBERWEISUNG IBAN+                           05.05.2017                         -50,00
 31.02.2017          Invalid Date SEPA-ÜBERWEISUNG                               31.02.2017                         -10,00
 07.05.2017          Too Large SEPA-ÜBERWEISUNG                                  07.05.2017 -999999999999999999999,00
` + "\f" + `Seite: 2 von 2
 06.05.2017          Example Merchant SEPA-ÜBERWEISUNG IBAN+                           06.05.2017                         -25,00
`

func TestParseChecking(t *testing.T) {
	var statement Statement
	parseChecking(checkingText, &statement)
	if statement.AccountType != "checking" || statement.Period != "2017-05" {
		t.Fatalf("period = %q", statement.Period)
	}
	if len(statement.Rows) != 3 {
		t.Fatalf("rows = %d: %+v", len(statement.Rows), statement.Rows)
	}
	first := statement.Rows[0]
	if first.BookingDate.Format("2006-01-02") != "2017-05-03" || first.AmountCents != -22000 {
		t.Errorf("first row = %+v", first)
	}
	if first.Recipient != "PayPal Europe" || first.Purpose != "Example purpose" || first.Reference != "REF-1" {
		t.Errorf("first fields = %+v", first)
	}
	if first.Page != 1 || statement.Rows[2].Page != 2 {
		t.Errorf("pages = %d/%d", first.Page, statement.Rows[2].Page)
	}
	transactions := statement.Transactions()
	if len(transactions) != 3 || transactions[0].SourcePage != 1 || transactions[0].SourceMonth != "2017-05" {
		t.Fatalf("transactions = %+v", transactions)
	}
	if transactions[0].Reference() != "REF-1" || !strings.Contains(transactions[0].Blob(), "paypal") {
		t.Errorf("transaction reference/blob = %+v", transactions[0])
	}
}

func TestParseCheckingHelpers(t *testing.T) {
	if accountType("Tagesgeld") != "savings" || accountType("Kontoauszug Buchungstag") != "checking" || accountType("other") != "" {
		t.Error("account type detection failed")
	}
	if !(&Statement{AccountType: "checking"}).IsCheckingAccount() || (&Statement{AccountType: "savings"}).IsCheckingAccount() {
		t.Error("checking-account detection failed")
	}
	if got := statementPeriodOf("no period"); got != "" {
		t.Errorf("missing period = %q", got)
	}
	if got := statementPeriodOf("Buchungen vom 31.02.2017"); got != "" {
		t.Errorf("invalid period = %q", got)
	}
	if got := recipientOf("Shop"); got != "Shop" {
		t.Errorf("recipient fallback = %q", got)
	}
	if got := fieldOf("no marker", "SVWZ+"); got != "" {
		t.Errorf("missing field = %q", got)
	}
	if got := firstFieldOf("EREF+ first MREF+ second", "MREF+", "EREF+"); got != "second" {
		t.Errorf("first field = %q", got)
	}
	if got := firstFieldOf("none", "EREF+"); got != "" {
		t.Errorf("empty first field = %q", got)
	}
	if detailLine("Seite: 1") {
		t.Error("page header treated as detail")
	}
	if !detailLine("SVWZ+ purpose") {
		t.Error("SVWZ line not treated as detail")
	}
}

func TestExtract(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "statement.pdf")
	if err := os.WriteFile(path, []byte("fake pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := extractPDFText
	t.Cleanup(func() { extractPDFText = original })
	extractPDFText = func(context.Context, string) (string, []string, error) {
		return checkingText, []string{"pdftotext"}, nil
	}
	parsed, err := Extract(context.Background(), path, "de-bank")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.File != "statement.pdf" || parsed.Pages != 2 || len(parsed.Rows) != 3 || len(parsed.Extraction) != 1 {
		t.Fatalf("parsed = %+v", parsed)
	}
	if len(parsed.SHA256) != 64 {
		t.Errorf("sha = %q", parsed.SHA256)
	}
	if _, err := Extract(context.Background(), path, "other"); err == nil {
		t.Fatal("unsupported institution accepted")
	}
	extractPDFText = func(context.Context, string) (string, []string, error) {
		return "", nil, errors.New("extract failed")
	}
	if _, err := Extract(context.Background(), path, "de-bank"); err == nil {
		t.Fatal("extract error accepted")
	}
	if _, err := Extract(context.Background(), filepath.Join(dir, "missing.pdf"), "de-bank"); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestParseCheckingBookingDate(t *testing.T) {
	var statement Statement
	parseChecking("Buchungen vom 01.05.2017\n 03.05.2017          Shop SEPA-X                          03.05.2017                         -1,00\n", &statement)
	if len(statement.Rows) != 1 || statement.Rows[0].BookingDate.Format(time.DateOnly) != "2017-05-03" {
		t.Fatalf("rows = %+v", statement.Rows)
	}
}
