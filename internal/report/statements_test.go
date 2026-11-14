package report

import (
	"encoding/csv"
	"errors"
	"strings"
	"testing"

	"tracey-sof/internal/bank"
	"tracey-sof/internal/model"
)

func statementTestReport() model.Report {
	report := gapTestReport()
	txid := strings.Repeat("q", 64)
	amount := int64(1000)
	report.Purchases = []model.Purchase{{
		ID: "p1", Date: "2024-01-02T22:38:23Z", Provider: "Example Exchange",
		TxIDs: []string{txid}, Outpoints: []model.Outpoint{{TxID: txid, Vout: 0}},
		AmountSats: &amount, Fiat: &model.Fiat{Amount: "123.45", Currency: "EUR"},
	}}
	return report
}

func TestStatementRequestsAndMatchesCSV(t *testing.T) {
	report := statementTestReport()
	cache := bank.Cache{SHA256: strings.Repeat("d", 64), Artifact: "evidence/bank/bank.csv", Records: []bank.MatchRecord{
		{Kind: "purchase", ID: "p1", Status: "matched", BankDate: "2024-01-02", BankAmountEUR: "123.45", BankCurrency: "EUR", BankReference: "EXAMPLE-REF-001"},
		{Kind: "inbound_payout", ID: "amb", Status: "ambiguous"},
	}}
	events := StatementEvents(report)
	if len(events) < 4 {
		t.Fatalf("events = %d", len(events))
	}
	data, err := StatementRequestsCSVBytes(report, cache)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(events)+1 || rows[0][0] != "event_type" || rows[0][11] != "status" {
		t.Fatalf("statement rows = %v", rows)
	}
	byEvent := map[string][]string{}
	for _, row := range rows[1:] {
		byEvent[row[1]] = row
	}
	purchaseRow := byEvent["p1"]
	if purchaseRow[11] != "matched_bank_evidence" || purchaseRow[12] != "2024-01-02" || purchaseRow[14] != cache.SHA256 || purchaseRow[15] != "none" {
		t.Errorf("purchase row = %v", purchaseRow)
	}
	requestRows := 0
	for _, row := range rows[1:] {
		if strings.HasPrefix(row[11], "request_physical_statement") {
			requestRows++
			if !strings.Contains(row[15], "request physical statement") {
				t.Errorf("request action = %q", row[15])
			}
		}
	}
	if requestRows == 0 {
		t.Error("no placeholder rows generated")
	}
	matched, requests := StatementRequestSummary(report, cache)
	if matched != 1 || requests != len(events)-1 {
		t.Errorf("summary = %d/%d of %d", matched, requests, len(events))
	}
	matches, err := BankMatchesCSVBytes(cache)
	if err != nil {
		t.Fatal(err)
	}
	matchRows, err := csv.NewReader(strings.NewReader(string(matches))).ReadAll()
	if err != nil || len(matchRows) != 3 || matchRows[1][0] != "purchase" {
		t.Fatalf("match rows = %v, %v", matchRows, err)
	}
	if matchRows[1][11] == "" || matchRows[2][11] != "" {
		t.Errorf("evidence sha should only be set for matched rows: %v", matchRows)
	}
}

func TestStatementStatusAndNilAmounts(t *testing.T) {
	value := int64(7)
	if optionSatsValue(nil) != 0 || optionSatsValue(&value) != 7 {
		t.Error("optionSatsValue wrong")
	}
	for _, test := range []struct {
		record bank.MatchRecord
		exists bool
		status string
	}{
		{bank.MatchRecord{Status: "matched"}, true, "matched_bank_evidence"},
		{bank.MatchRecord{Status: "ambiguous"}, true, "ambiguous_bank_match"},
		{bank.MatchRecord{}, false, "request_physical_statement"},
	} {
		status, _ := statementStatus(test.record, test.exists)
		if status != test.status {
			t.Errorf("status = %q, want %q", status, test.status)
		}
	}
}

func TestStatementRequestsUsesAttachedBankArtifact(t *testing.T) {
	report := statementTestReport()
	hash := strings.Repeat("e", 64)
	report.Purchases[0].Artifacts = []model.Artifact{{Path: "evidence/bank/bank.csv", SHA256: hash}}
	report.Purchases[0].StatementDate = "2024-01-03"
	data, err := StatementRequestsCSVBytes(report, bank.Cache{})
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows[1:] {
		if row[1] == "p1" {
			found = true
			if row[11] != "matched_bank_evidence" || row[12] != "2024-01-03" || row[14] != hash || row[15] != "none" {
				t.Errorf("attached bank row = %v", row)
			}
		}
	}
	if !found {
		t.Fatal("purchase row missing")
	}
	matched, _ := StatementRequestSummary(report, bank.Cache{})
	if matched != 1 {
		t.Errorf("matched = %d", matched)
	}
}

func TestStatementWriterErrors(t *testing.T) {
	report := statementTestReport()
	cache := bank.Cache{Records: []bank.MatchRecord{{Kind: "purchase", ID: "p1", Status: "matched"}}}
	for _, test := range []struct {
		name   string
		writer *purchaseCSVWriter
	}{
		{"statement write", &purchaseCSVWriter{failOn: 0, writeErr: errors.New("write failed")}},
		{"statement row", &purchaseCSVWriter{failOn: 1, writeErr: errors.New("row failed")}},
		{"statement flush", &purchaseCSVWriter{failOn: 100, flushErr: errors.New("flush failed")}},
	} {
		if err := writeStatementRequestsCSV(test.writer, report, cache); err == nil {
			t.Errorf("%s accepted", test.name)
		}
	}
	for _, test := range []struct {
		name   string
		writer *purchaseCSVWriter
	}{
		{"matches write", &purchaseCSVWriter{failOn: 0, writeErr: errors.New("write failed")}},
		{"matches row", &purchaseCSVWriter{failOn: 1, writeErr: errors.New("row failed")}},
		{"matches flush", &purchaseCSVWriter{failOn: 100, flushErr: errors.New("flush failed")}},
	} {
		if err := writeBankMatchesCSV(test.writer, cache); err == nil {
			t.Errorf("%s accepted", test.name)
		}
	}
}
