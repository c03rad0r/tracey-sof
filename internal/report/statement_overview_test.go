package report

import (
	"strings"
	"testing"

	"tracey-sof/internal/bank"
	"tracey-sof/internal/model"
)

func statementOverviewCache() bank.Cache {
	return bank.Cache{
		Statements: []bank.StatementSource{
			{Institution: "sparda", Period: "2023-01", File: "evidence/bank/2023-01.pdf", SHA256: strings.Repeat("a", 64), Rows: 3},
			{File: "evidence/bank/fallback.pdf", SHA256: strings.Repeat("b", 64)},
		},
		Records: []bank.MatchRecord{
			{Kind: "purchase", ID: "p1", Date: "2023-01-01", Status: "matched", Confirmed: true,
				BankDate: "2023-01-02", BankAmountEUR: "10.00", BankCurrency: "EUR",
				StatementFile: "evidence/bank/2023-01.pdf", StatementPage: 2, StatementSHA256: strings.Repeat("a", 64),
				StatementMonth: "2023-01", StatementRecipient: "Example Merchant", StatementPurpose: "purchase", StatementReference: "REF-1"},
			{Kind: "purchase", ID: "p2", Date: "2023-02-01", Status: "matched",
				StatementFile: "evidence/bank/2023-02.pdf", StatementMonth: "2023-02"},
			{Kind: "purchase", ID: "p3", Date: "2023-03-01", Status: "ambiguous",
				StatementFile: "evidence/bank/2023-03.pdf", StatementMonth: "2023-03"},
			{Kind: "purchase", ID: "p4", Date: "2023-04-01", Status: "missing",
				StatementFile: "evidence/bank/2023-04.pdf", StatementMonth: "2023-04"},
			{Kind: "purchase", ID: "p5", Date: "2023-05-01", Status: "matched", Confirmed: true},
		},
	}
}

func TestStatementStatusLabelAndPurchaseFlags(t *testing.T) {
	for _, test := range []struct {
		record bank.MatchRecord
		want   string
	}{
		{bank.MatchRecord{Status: "matched", Confirmed: true}, "confirmed"},
		{bank.MatchRecord{Status: "matched"}, "candidate"},
		{bank.MatchRecord{Status: "ambiguous"}, "ambiguous"},
		{bank.MatchRecord{Status: "missing"}, "missing"},
	} {
		if got := statementStatusLabel(test.record); got != test.want {
			t.Errorf("label = %q, want %q", got, test.want)
		}
	}
	flags := statementPurchaseFlags(statementOverviewCache())
	if flags["p1"] != "confirmed" || flags["p2"] != "candidate" || flags["p3"] != "ambiguous" || flags["p4"] != "missing" {
		t.Errorf("flags = %+v", flags)
	}
	if _, exists := flags["p5"]; exists {
		t.Errorf("CSV-only record got a PDF flag: %+v", flags)
	}
}

func TestStatementCoverageRows(t *testing.T) {
	report := overviewTestReport()
	report.Purchases[0].Artifacts = []model.Artifact{{Path: "evidence/bank/2023-01.pdf"}}
	report.Purchases = append(report.Purchases,
		model.Purchase{ID: "p2", Date: "2023-02-01T00:00:00Z"},
		model.Purchase{ID: "p3", Date: "2023-03-01T00:00:00Z"},
		model.Purchase{ID: "p4", Date: "2023-04-01T00:00:00Z"},
		model.Purchase{ID: "p5", Date: "2023-05-01T00:00:00Z"},
		model.Purchase{ID: "p6", Date: "2023-06-01T00:00:00Z", Artifacts: []model.Artifact{{Path: "evidence/bank/csv.csv"}}},
		model.Purchase{ID: "undated"},
	)
	rows := statementCoverageRows(report, statementOverviewCache())
	byMonth := map[string]statementCoverageRow{}
	for _, row := range rows {
		byMonth[row.month] = row
	}
	if byMonth["2023-01"].confirmed != 1 || byMonth["2023-01"].sources[0].File == "" {
		t.Errorf("January = %+v", byMonth["2023-01"])
	}
	if byMonth["2023-02"].candidates != 1 || byMonth["2023-03"].candidates != 1 {
		t.Errorf("candidates = %+v / %+v", byMonth["2023-02"], byMonth["2023-03"])
	}
	if byMonth["2023-04"].missing != 1 || byMonth["2023-05"].confirmed != 1 || byMonth["2023-06"].confirmed != 1 {
		t.Errorf("missing/CSV = %+v / %+v / %+v", byMonth["2023-04"], byMonth["2023-05"], byMonth["2023-06"])
	}
	if byMonth["undated"].missing != 1 {
		t.Errorf("undated = %+v", byMonth["undated"])
	}
}

func TestStatementEvidenceRows(t *testing.T) {
	report := overviewTestReport()
	report.Purchases[0].Artifacts = []model.Artifact{{Path: "evidence/bank/2023-01.pdf"}}
	report.Purchases = append(report.Purchases, model.Purchase{
		ID: "p2", Date: "2023-02-01T00:00:00Z", AmountSats: ptrInt64(200),
	})
	rows := statementEvidenceRows(report, statementOverviewCache())
	if len(rows) != 4 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].id != "p1" || rows[0].bank == "" || rows[0].recipient != "Example Merchant" ||
		rows[0].purpose != "purchase" || rows[0].reference != "REF-1" || rows[0].page != 2 {
		t.Errorf("first row = %+v", rows[0])
	}
	if rows[1].date != "2023-02-01" || rows[1].sats != 200 {
		t.Errorf("fallback date/sats = %+v", rows[1])
	}
	if rows := statementEvidenceRows(model.Report{}, bank.Cache{}); len(rows) != 0 {
		t.Errorf("empty evidence = %+v", rows)
	}
}

func TestOverviewRendersStatementSections(t *testing.T) {
	text := string(overviewHTML(overviewTestReport(), statementOverviewCache()))
	for _, want := range []string{"Bank statement coverage", "Statement evidence", "2023-01", "statement confirmed", "Example Merchant"} {
		if !strings.Contains(text, want) {
			t.Errorf("overview missing %q", want)
		}
	}
}

func ptrInt64(value int64) *int64 { return &value }

func TestStatementEvidenceDateFallbackAndTie(t *testing.T) {
	report := model.Report{Purchases: []model.Purchase{
		{ID: "b", Date: "2023-01-01T00:00:00Z"},
		{ID: "a", Date: "2023-01-01T00:00:00Z"},
	}}
	cache := bank.Cache{Records: []bank.MatchRecord{
		{Kind: "purchase", ID: "b", Status: "matched", Confirmed: true, StatementFile: "b.pdf"},
		{Kind: "purchase", ID: "a", Date: "2023-01-01", Status: "matched", Confirmed: true, StatementFile: "a.pdf"},
	}}
	rows := statementEvidenceRows(report, cache)
	if len(rows) != 2 || rows[0].id != "a" || rows[1].id != "b" || rows[1].date != "2023-01-01" {
		t.Fatalf("rows = %+v", rows)
	}
}
