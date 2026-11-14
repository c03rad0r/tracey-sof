package report

import (
	"encoding/csv"
	"errors"
	"strings"
	"testing"
	"time"

	"tracey-sof/internal/model"
)

type purchaseCSVWriter struct {
	failOn   int
	writeErr error
	flushErr error
}

func (w *purchaseCSVWriter) Write([]string) error {
	if w.failOn <= 0 {
		return w.writeErr
	}
	w.failOn--
	return nil
}

func (w *purchaseCSVWriter) Flush()       {}
func (w *purchaseCSVWriter) Error() error { return w.flushErr }

func purchaseTestReport() model.Report {
	first := int64(1000)
	second := int64(500)
	third := int64(250)
	fourth := int64(125)
	return model.Report{
		Evidence: []model.EvidenceStatus{
			{Kind: "outpoint", Value: "a:1", PurchaseIDs: []string{"purchase-one"}, Matched: true},
			{Kind: "outpoint", Value: "b:0", PurchaseIDs: []string{"purchase-two"}, Matched: false},
		},
		Purchases: []model.Purchase{
			{ID: "purchase-one", Date: "2023-05-01T22:29:00Z", Provider: "Example Exchange", AmountSats: &first,
				TxIDs: []string{strings.Repeat("a", 64)}, Outpoints: []model.Outpoint{{TxID: strings.Repeat("a", 64), Vout: 1}},
				Fiat: &model.Fiat{Amount: "0.64", Currency: "USD"}},
			{ID: "purchase-two", Date: "2023-05-02T10:00:00Z", Provider: "Example Exchange", AmountSats: &second,
				TxIDs: []string{strings.Repeat("b", 64)}},
			{ID: "purchase-three", Date: "2024-02-05T22:47:00Z", StatementDate: "2024-02-06", Provider: "Example Exchange", AmountSats: &third},
			{ID: "purchase-undated", AmountSats: &fourth, Provider: "Example Exchange"},
			{ID: "statement-only", StatementDate: "2022-12-31", AmountSats: &fourth},
			{ID: "broken-date", Date: "not-a-date", AmountSats: &fourth},
		}}
}

func TestPurchasesCSVUsesStatementDates(t *testing.T) {
	location := time.FixedZone("CEST", 2*3600)
	data, err := purchasesCSVBytes(purchaseTestReport(), location)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 7 {
		t.Fatalf("rows = %d, want 7", len(rows))
	}
	if rows[0][0] != "purchase_id" || rows[0][3] != "statement_date" || rows[0][4] != "matched_current_ancestry" {
		t.Fatalf("header = %v", rows[0])
	}
	if rows[1][4] != "true" || rows[2][4] != "false" {
		t.Errorf("matched flags = %q, %q", rows[1][4], rows[2][4])
	}
	if rows[1][2] != "2023-05-02" || rows[1][3] != "2023-05-02" {
		t.Errorf("purchase one local/statement = %v", rows[1])
	}
	if rows[3][3] != "2024-02-06" || rows[3][2] != "2024-02-06" {
		t.Errorf("explicit statement date not honoured: %v", rows[3])
	}
	if rows[5][1] != "" || rows[5][3] != "2022-12-31" {
		t.Errorf("statement-only purchase = %v", rows[5])
	}
	if rows[6][2] != "" || rows[6][3] != "" {
		t.Errorf("broken date purchase = %v", rows[6])
	}
}

func TestPurchaseDatesCSVGroupsByStatementDay(t *testing.T) {
	location := time.FixedZone("CEST", 2*3600)
	data, err := purchaseDatesCSVBytes(purchaseTestReport(), location)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 5 {
		t.Fatalf("rows = %d, want 5: %v", len(rows), rows)
	}
	// undated (two records with empty statement dates) sorts first.
	if rows[1][0] != "" || rows[1][1] != "2" {
		t.Errorf("undated row = %v", rows[1])
	}
	if rows[2][0] != "2022-12-31" || rows[4][0] != "2024-02-06" {
		t.Errorf("statement order = %v", rows)
	}
	group := rows[3]
	if group[0] != "2023-05-02" || group[1] != "2" || group[2] != "1500" {
		t.Errorf("2023-05-02 row = %v", group)
	}
	if !strings.Contains(group[4], "Example Exchange") || !strings.Contains(group[5], "purchase-one") {
		t.Errorf("group identifiers = %v", group)
	}
	if group[7] != "2023-05-01|2023-05-02" {
		t.Errorf("utc days = %q", group[7])
	}
}

func TestPurchaseCSVExportsAndFallbacks(t *testing.T) {
	t.Setenv("SOF_TIMEZONE", "UTC")
	if _, err := PurchasesCSVBytes(purchaseTestReport()); err != nil {
		t.Fatal(err)
	}
	if _, err := PurchaseDatesCSVBytes(purchaseTestReport()); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SOF_TIMEZONE", "Not/AZone")
	if location := purchaseLocation(); location == nil {
		t.Fatal("fallback location is nil")
	}
	if value := optionSats(nil); value != "" {
		t.Errorf("nil sats = %q", value)
	}
	if joined := joinUnique([]string{"b", "", "a", "b"}, "|"); joined != "a|b" {
		t.Errorf("joinUnique = %q", joined)
	}
}

func TestPurchaseCSVWriterErrors(t *testing.T) {
	location := time.FixedZone("CEST", 2*3600)
	report := purchaseTestReport()
	cases := []struct {
		name   string
		writer *purchaseCSVWriter
		dates  bool
	}{
		{"purchases write", &purchaseCSVWriter{failOn: 0, writeErr: errors.New("write failed")}, false},
		{"purchases row", &purchaseCSVWriter{failOn: 1, writeErr: errors.New("row failed")}, false},
		{"purchases flush", &purchaseCSVWriter{failOn: 100, flushErr: errors.New("flush failed")}, false},
		{"dates write", &purchaseCSVWriter{failOn: 0, writeErr: errors.New("write failed")}, true},
		{"dates row", &purchaseCSVWriter{failOn: 1, writeErr: errors.New("row failed")}, true},
		{"dates flush", &purchaseCSVWriter{failOn: 100, flushErr: errors.New("flush failed")}, true},
	}
	for _, test := range cases {
		var err error
		if test.dates {
			err = writePurchaseDatesCSV(test.writer, report, location)
		} else {
			err = writePurchasesCSV(test.writer, report, location)
		}
		if err == nil {
			t.Errorf("%s was accepted", test.name)
		}
	}
}

func TestPurchaseUnmatchedMonthsAndYears(t *testing.T) {
	location := time.FixedZone("CEST", 2*3600)
	unmatched, err := purchasesUnmatchedCSVBytes(purchaseTestReport(), location)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(unmatched))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 6 {
		t.Fatalf("unmatched rows = %d, want 6", len(rows))
	}
	if rows[0][len(rows[0])-1] != "match_status" || rows[1][len(rows[1])-1] != "not_in_current_ancestry" {
		t.Errorf("unmatched header/status = %v / %v", rows[0], rows[1])
	}
	for _, row := range rows[1:] {
		if row[0] == "purchase-one" {
			t.Error("matched purchase was included in the unmatched list")
		}
	}
	months, err := purchaseMonthsCSVBytes(purchaseTestReport(), location)
	if err != nil {
		t.Fatal(err)
	}
	monthRows, err := csv.NewReader(strings.NewReader(string(months))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(monthRows) != 5 {
		t.Fatalf("month rows = %d: %v", len(monthRows), monthRows)
	}
	if monthRows[1][0] != "" || monthRows[1][2] != "undated" {
		t.Errorf("undated month row = %v", monthRows[1])
	}
	if monthRows[2][0] != "2022" || monthRows[2][1] != "12" {
		t.Errorf("2022-12 row = %v", monthRows[2])
	}
	if monthRows[3][2] != "2023-04" || monthRows[3][3] != "2" || monthRows[3][4] != "1500" {
		t.Errorf("2023-04 row = %v", monthRows[3])
	}
	years, err := purchaseYearsCSVBytes(purchaseTestReport(), location)
	if err != nil {
		t.Fatal(err)
	}
	yearRows, err := csv.NewReader(strings.NewReader(string(years))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(yearRows) != 5 {
		t.Fatalf("year rows = %d: %v", len(yearRows), yearRows)
	}
	if yearRows[3][0] != "2023" || yearRows[3][1] != "2" || yearRows[3][2] != "1500" {
		t.Errorf("2023 year row = %v", yearRows[2])
	}
}

func TestPurchaseUnmatchedMonthsYearsWrappers(t *testing.T) {
	t.Setenv("SOF_TIMEZONE", "UTC")
	for name, encode := range map[string]func(model.Report) ([]byte, error){
		"unmatched": PurchasesUnmatchedCSVBytes,
		"months":    PurchaseMonthsCSVBytes,
		"years":     PurchaseYearsCSVBytes,
	} {
		if _, err := encode(purchaseTestReport()); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestPurchaseUnmatchedMonthsYearsWriterErrors(t *testing.T) {
	location := time.FixedZone("CEST", 2*3600)
	report := purchaseTestReport()
	writers := map[string]func(reportCSVWriter, model.Report, *time.Location) error{
		"unmatched": writePurchasesUnmatchedCSV,
		"months":    writePurchaseMonthsCSV,
		"years":     writePurchaseYearsCSV,
	}
	for name, write := range writers {
		for _, test := range []struct {
			label  string
			writer *purchaseCSVWriter
		}{
			{"write", &purchaseCSVWriter{failOn: 0, writeErr: errors.New("write failed")}},
			{"row", &purchaseCSVWriter{failOn: 1, writeErr: errors.New("row failed")}},
			{"flush", &purchaseCSVWriter{failOn: 100, flushErr: errors.New("flush failed")}},
		} {
			if err := write(test.writer, report, location); err == nil {
				t.Errorf("%s %s was accepted", name, test.label)
			}
		}
	}
}
