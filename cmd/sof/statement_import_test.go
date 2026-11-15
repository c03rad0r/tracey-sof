package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tracey-sof/internal/statement"
)

func fakeStatementImport(t *testing.T, text string) func() {
	t.Helper()
	originalParse, originalStore := parseStatementPDF, storeStatementArtifact
	parseStatementPDF = func(context.Context, string, string) (statement.Statement, error) {
		return statement.Statement{
			Institution: "de-bank", AccountType: "checking", Period: "2009-01", Pages: 1, Rows: []statement.Row{{
				BookingDate: time.Date(2009, 1, 12, 0, 0, 0, 0, time.UTC),
				AmountCents: -10000, Currency: "EUR", Recipient: "Example Merchant",
				Purpose: text, Page: 1,
			}},
		}, nil
	}
	storeStatementArtifact = func(dataDir, _ string) (string, string, error) {
		relative := "evidence/bank/fake.pdf"
		if err := os.MkdirAll(filepath.Join(dataDir, "evidence", "bank"), 0o700); err != nil {
			return "", "", err
		}
		data := []byte("pdf")
		if err := os.WriteFile(filepath.Join(dataDir, relative), data, 0o600); err != nil {
			return "", "", err
		}
		sum := sha256.Sum256(data)
		return relative, hex.EncodeToString(sum[:]), nil
	}
	return func() {
		parseStatementPDF, storeStatementArtifact = originalParse, originalStore
	}
}

func statementImportInputs(t *testing.T) (paths, string) {
	t.Helper()
	p := newCLIPaths(t, false)
	writeCLIInputs(t, p, false)
	saveCLIFixtureTrace(t, p)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "statement.pdf"), []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p, dir
}

func TestStatementImportCandidateAndConfirmed(t *testing.T) {
	p, dir := statementImportInputs(t)
	restore := fakeStatementImport(t, "Example Merchant")
	defer restore()
	var output bytes.Buffer
	app := application{stdout: &output, stderr: &output}
	if err := app.statementImport(context.Background(), p, dir, "de-bank", "Example Merchant", "Example Exchange", 3, false, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "matched events: 1") || !strings.Contains(output.String(), "review the lists") {
		t.Fatalf("candidate output = %q", output.String())
	}
	if _, err := os.Stat(filepath.Join(p.cacheDir, "bank-matches.json")); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := app.statementImport(context.Background(), p, dir, "de-bank", "Example Merchant", "Example Exchange", 3, true, "purchase|fixture-purchase"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "attached bank evidence") {
		t.Fatalf("confirmed output = %q", output.String())
	}
}

func TestStatementImportErrors(t *testing.T) {
	p, dir := statementImportInputs(t)
	restore := fakeStatementImport(t, "Example Merchant")
	defer restore()
	app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	if err := app.statementImport(context.Background(), p, dir, "de-bank", "Example Merchant", "Example Exchange", 3, true, ""); err == nil {
		t.Fatal("apply without confirmation accepted")
	}
	parseStatementPDF = func(context.Context, string, string) (statement.Statement, error) {
		return statement.Statement{}, errors.New("parse failed")
	}
	if err := app.statementImport(context.Background(), p, dir, "de-bank", "Example Merchant", "Example Exchange", 3, false, ""); err == nil {
		t.Fatal("parser error accepted")
	}
	parseStatementPDF = func(context.Context, string, string) (statement.Statement, error) {
		return statement.Statement{Institution: "de-bank", AccountType: "checking", Rows: []statement.Row{}}, nil
	}
	storeStatementArtifact = func(string, string) (string, string, error) {
		return "", "", errors.New("store failed")
	}
	if err := app.statementImport(context.Background(), p, dir, "de-bank", "Example Merchant", "Example Exchange", 3, false, ""); err == nil {
		t.Fatal("store error accepted")
	}
}

func TestStatementFiles(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "b.pdf"), []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "nested", "a.PDF"), []byte("pdf"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ignored.txt"), []byte("txt"), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := statementFiles(dir)
	if err != nil || len(files) != 2 || filepath.Base(files[0]) != "b.pdf" || filepath.Base(files[1]) != "a.PDF" {
		t.Fatalf("files = %v, %v", files, err)
	}
	if _, err := statementFiles(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing directory accepted")
	}
	empty := t.TempDir()
	if _, err := statementFiles(empty); err == nil {
		t.Fatal("empty directory accepted")
	}
}

func TestStatementImportFlagErrors(t *testing.T) {
	for _, args := range [][]string{
		{"statement-import"},
		{"statement-import", "-statement-dir", "x", "-merchant", "m"},
		{"statement-import", "-statement-dir", "x", "-institution", "de-bank", "-merchant", "m"},
	} {
		var stdout, stderr bytes.Buffer
		if err := run(context.Background(), args, &stdout, &stderr); err == nil {
			t.Fatalf("arguments accepted: %v", args)
		}
	}
}

func TestStatementImportBuildAndFilesystemErrors(t *testing.T) {
	t.Run("build report", func(t *testing.T) {
		p := newCLIPaths(t, false)
		app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		if err := app.statementImport(context.Background(), p, t.TempDir(), "de-bank", "m", "p", 3, false, ""); err == nil {
			t.Fatal("missing trace accepted")
		}
	})
	t.Run("missing directory", func(t *testing.T) {
		p, _ := statementImportInputs(t)
		app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		if err := app.statementImport(context.Background(), p, filepath.Join(t.TempDir(), "missing"), "de-bank", "m", "p", 3, false, ""); err == nil {
			t.Fatal("missing statement directory accepted")
		}
	})
	t.Run("cache load", func(t *testing.T) {
		p, dir := statementImportInputs(t)
		if err := os.WriteFile(filepath.Join(p.cacheDir, "bank-matches.json"), []byte("{bad"), 0o600); err != nil {
			t.Fatal(err)
		}
		restore := fakeStatementImport(t, "Example Merchant")
		defer restore()
		app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		if err := app.statementImport(context.Background(), p, dir, "de-bank", "m", "p", 3, false, ""); err == nil {
			t.Fatal("invalid cache accepted")
		}
	})
	t.Run("cache save", func(t *testing.T) {
		p, dir := statementImportInputs(t)
		restore := fakeStatementImport(t, "Example Merchant")
		defer restore()
		if err := os.Chmod(p.cacheDir, 0o500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(p.cacheDir, 0o700)
		app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		if err := app.statementImport(context.Background(), p, dir, "de-bank", "m", "p", 3, false, ""); err == nil {
			t.Fatal("unwritable cache accepted")
		}
	})
	t.Run("output write", func(t *testing.T) {
		p, dir := statementImportInputs(t)
		restore := fakeStatementImport(t, "Example Merchant")
		defer restore()
		blocked := filepath.Join(t.TempDir(), "out-file")
		if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		p.outDir = blocked
		app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		if err := app.statementImport(context.Background(), p, dir, "de-bank", "m", "p", 3, false, ""); err == nil {
			t.Fatal("blocked output accepted")
		}
	})
	t.Run("statement request write", func(t *testing.T) {
		p, dir := statementImportInputs(t)
		restore := fakeStatementImport(t, "Example Merchant")
		defer restore()
		if err := os.MkdirAll(filepath.Join(p.outDir, "statement-requests.csv"), 0o700); err != nil {
			t.Fatal(err)
		}
		app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		if err := app.statementImport(context.Background(), p, dir, "de-bank", "m", "p", 3, false, ""); err == nil {
			t.Fatal("blocked statement-requests path accepted")
		}
	})
}

func TestStatementImportAmbiguousAndMissingCounts(t *testing.T) {
	p, dir := statementImportInputs(t)
	originalParse, originalStore := parseStatementPDF, storeStatementArtifact
	defer func() { parseStatementPDF, storeStatementArtifact = originalParse, originalStore }()
	storeStatementArtifact = func(dataDir, _ string) (string, string, error) {
		return "evidence/bank/fake.pdf", strings.Repeat("a", 64), nil
	}
	row := statement.Row{BookingDate: time.Date(2009, 1, 12, 0, 0, 0, 0, time.UTC), AmountCents: -10000, Currency: "EUR", Recipient: "Example Merchant", Page: 1}
	parseStatementPDF = func(context.Context, string, string) (statement.Statement, error) {
		return statement.Statement{Institution: "de-bank", AccountType: "checking", Period: "2009-01", Pages: 1, Rows: []statement.Row{row, row}}, nil
	}
	var output bytes.Buffer
	app := application{stdout: &output, stderr: &output}
	if err := app.statementImport(context.Background(), p, dir, "de-bank", "Example Merchant", "Example Exchange", 3, false, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "ambiguous: 1") {
		t.Fatalf("ambiguous output = %q", output.String())
	}
	row.Recipient = "Other"
	parseStatementPDF = func(context.Context, string, string) (statement.Statement, error) {
		return statement.Statement{Institution: "de-bank", AccountType: "checking", Period: "2009-01", Pages: 1, Rows: []statement.Row{row}}, nil
	}
	output.Reset()
	if err := app.statementImport(context.Background(), p, dir, "de-bank", "Example Merchant", "Example Exchange", 3, false, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "without match: 1") {
		t.Fatalf("missing output = %q", output.String())
	}
}

func TestStatementImportFlagAndCallErrors(t *testing.T) {
	for _, args := range [][]string{
		{"statement-import", "-bad"},
		{"statement-import", "-statement-dir", "x", "-institution", "", "-merchant", "m", "-provider", "p"},
		{"statement-import", "-statement-dir", "x", "-merchant", "", "-provider", "p"},
	} {
		var stdout, stderr bytes.Buffer
		if err := run(context.Background(), args, &stdout, &stderr); err == nil {
			t.Fatalf("arguments accepted: %v", args)
		}
	}
	p, _ := statementImportInputs(t)
	var stdout, stderr bytes.Buffer
	args := cliCommandArguments("statement-import", p, "-statement-dir", filepath.Join(t.TempDir(), "missing"), "-merchant", "m", "-provider", "p")
	if err := run(context.Background(), args, &stdout, &stderr); err == nil {
		t.Fatal("missing statement directory accepted through CLI")
	}
}

func TestStatementImportSkipsNonGirokonto(t *testing.T) {
	p, dir := statementImportInputs(t)
	originalParse, originalStore := parseStatementPDF, storeStatementArtifact
	defer func() { parseStatementPDF, storeStatementArtifact = originalParse, originalStore }()
	parseStatementPDF = func(context.Context, string, string) (statement.Statement, error) {
		return statement.Statement{Institution: "de-bank", AccountType: "savings", Period: "2023-03"}, nil
	}
	storeStatementArtifact = func(string, string) (string, string, error) {
		t.Fatal("non-Girokonto statement was stored")
		return "", "", nil
	}
	var output bytes.Buffer
	app := application{stdout: &output, stderr: &output}
	if err := app.statementImport(context.Background(), p, dir, "de-bank", "Example Merchant", "Example Exchange", 3, false, ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "PDF statements imported: 0") {
		t.Fatalf("output = %q", output.String())
	}
}
