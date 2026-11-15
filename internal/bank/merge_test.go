package bank

import "testing"

func TestMergeStatementsPreservesConfirmedAndAddsSources(t *testing.T) {
	existing := Cache{
		Artifact: "evidence/bank/csv.csv",
		SHA256:   "csv-sha",
		Records: []MatchRecord{
			{Kind: "purchase", ID: "p1", Date: "2024-01-01", Status: "matched", Confirmed: true, BankDate: "2024-01-02"},
			{Kind: "purchase", ID: "p2", Date: "2024-02-01", Status: "ambiguous"},
			{Kind: "purchase", ID: "p4", Date: "2024-04-01", Status: "matched"},
		},
		Statements: []StatementSource{{Period: "2024-01", File: "old.pdf", SHA256: "old-sha"}},
	}
	incoming := []MatchRecord{
		{Kind: "purchase", ID: "p1", Date: "2024-01-01", Status: "matched", Confirmed: true, StatementFile: "evidence/bank/2024-01.pdf",
			StatementPage: 2, StatementSHA256: "pdf-sha", StatementMonth: "2024-01", StatementRecipient: "Example Merchant",
			StatementPurpose: "purchase", StatementReference: "REF", StatementText: "row",
			Artifact: "evidence/bank/2024-01.pdf", ArtifactSHA256: "pdf-sha"},
		{Kind: "purchase", ID: "p2", Date: "2024-02-01", Status: "matched", StatementFile: "evidence/bank/2024-02.pdf",
			Artifact: "evidence/bank/2024-02.pdf", ArtifactSHA256: "pdf-sha-2"},
		{Kind: "purchase", ID: "p3", Date: "2024-03-01", Status: "missing"},
		{Kind: "purchase", ID: "p4", Date: "2024-04-01", Status: "missing"},
	}
	sources := []StatementSource{
		{Period: "2024-01", File: "2024-01.pdf", SHA256: "pdf-sha"},
		{Period: "2024-01", File: "duplicate.pdf", SHA256: "pdf-sha"},
		{Period: "2024-02", File: "2024-02.pdf", SHA256: "pdf-sha-2"},
	}
	merged := MergeStatements(existing, incoming, sources)
	if len(merged.Records) != 4 {
		t.Fatalf("records = %+v", merged.Records)
	}
	byID := map[string]MatchRecord{}
	for _, record := range merged.Records {
		byID[record.ID] = record
	}
	if !byID["p1"].Confirmed || byID["p1"].StatementFile != "evidence/bank/2024-01.pdf" ||
		byID["p1"].Artifact != "evidence/bank/2024-01.pdf" || byID["p1"].StatementRecipient != "Example Merchant" {
		t.Errorf("confirmed merge = %+v", byID["p1"])
	}
	if byID["p2"].Status != "matched" || byID["p2"].Artifact != "evidence/bank/2024-02.pdf" {
		t.Errorf("ambiguous replacement = %+v", byID["p2"])
	}
	if byID["p3"].Status != "missing" {
		t.Errorf("new record = %+v", byID["p3"])
	}
	if byID["p4"].Status != "matched" {
		t.Errorf("matched record was downgraded = %+v", byID["p4"])
	}
	if len(merged.Statements) != 3 {
		t.Errorf("sources = %+v", merged.Statements)
	}
	if merged.Statements[0].Period != "2024-01" || merged.Statements[2].Period != "2024-02" {
		t.Errorf("source sort = %+v", merged.Statements)
	}
}

func TestMergeStatementsConfirmedWithoutStatement(t *testing.T) {
	existing := Cache{Records: []MatchRecord{{Kind: "purchase", ID: "p1", Status: "matched", Confirmed: true}}}
	merged := MergeStatements(existing, []MatchRecord{{Kind: "purchase", ID: "p1", Status: "matched"}}, nil)
	if len(merged.Records) != 1 || merged.Records[0].StatementFile != "" {
		t.Fatalf("merged = %+v", merged.Records)
	}
}

func TestMergeStatementsSortsSameDateByKey(t *testing.T) {
	merged := MergeStatements(Cache{}, []MatchRecord{
		{Kind: "purchase", ID: "b", Date: "2024-01-01"},
		{Kind: "purchase", ID: "a", Date: "2024-01-01"},
	}, nil)
	if len(merged.Records) != 2 || merged.Records[0].ID != "a" || merged.Records[1].ID != "b" {
		t.Fatalf("order = %+v", merged.Records)
	}
}
