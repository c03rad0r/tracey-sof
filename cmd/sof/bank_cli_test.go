package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracey-sof/internal/bank"
	"tracey-sof/internal/model"
)

func bankFileFixture(t *testing.T, rows string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "bank.csv")
	header := "Buchungstag;Valutadatum;Name Zahlungsbeteiligter;Verwendungszweck;Buchungstext;Betrag;Waehrung\n"
	if err := os.WriteFile(path, []byte(header+rows), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func bankCLIApplication() application {
	var stdout, stderr bytes.Buffer
	return application{stdout: &stdout, stderr: &stderr}
}

func TestBankImportBranches(t *testing.T) {
	ctx := context.Background()
	t.Run("missing file", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		app := bankCLIApplication()
		if err := app.bankImport(p, filepath.Join(t.TempDir(), "missing.csv"), "Example Merchant", "Example Exchange", 3, false); err == nil {
			t.Fatal("missing bank file accepted")
		}
	})

	for _, test := range []struct {
		name string
		rows string
		want string
	}{
		{"missing status", "01.01.2020;01.01.2020;Example Merchant;ref;;-100,00;EUR\n", "without match: 1"},
		{"ambiguous status", "12.01.2009;12.01.2009;Example Merchant;ref;;-100,00;EUR\n12.01.2009;12.01.2009;Example Merchant;ref;;-100,00;EUR\n", "ambiguous: 1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			p := newCLIPaths(t, false)
			writeCLIInputs(t, p, false)
			saveCLIFixtureTrace(t, p)
			var stdout, stderr bytes.Buffer
			app := application{stdout: &stdout, stderr: &stderr}
			if err := app.bankImport(p, bankFileFixture(t, test.rows), "Example Merchant", "Example Exchange", 3, false); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(stdout.String(), test.want) || !strings.Contains(stdout.String(), "review the lists") {
				t.Fatalf("output = %q", stdout.String())
			}
		})
	}

	t.Run("artifact error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		if err := os.MkdirAll(filepath.Join(p.dataDir, "evidence"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p.dataDir, "evidence", "bank"), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		app := bankCLIApplication()
		if err := app.bankImport(p, bankFileFixture(t, "12.01.2009;12.01.2009;Example Merchant;ref;;-100,00;EUR\n"), "Example Merchant", "Example Exchange", 3, false); err == nil {
			t.Fatal("artifact error accepted")
		}
	})

	t.Run("cache write error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		if err := os.Mkdir(filepath.Join(p.cacheDir, "bank-matches.json"), 0o700); err != nil {
			t.Fatal(err)
		}
		app := bankCLIApplication()
		if err := app.bankImport(p, bankFileFixture(t, "12.01.2009;12.01.2009;Example Merchant;ref;;-100,00;EUR\n"), "Example Merchant", "Example Exchange", 3, false); err == nil {
			t.Fatal("cache write error accepted")
		}
	})

	t.Run("output write error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		blocked := filepath.Join(t.TempDir(), "out-file")
		if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		p.outDir = blocked
		app := bankCLIApplication()
		if err := app.bankImport(p, bankFileFixture(t, "12.01.2009;12.01.2009;Example Merchant;ref;;-100,00;EUR\n"), "Example Merchant", "Example Exchange", 3, false); err == nil {
			t.Fatal("output write error accepted")
		}
	})

	t.Run("statement write error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		if err := os.MkdirAll(p.outDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(p.outDir, "statement-requests.csv"), 0o700); err != nil {
			t.Fatal(err)
		}
		app := bankCLIApplication()
		if err := app.bankImport(p, bankFileFixture(t, "12.01.2009;12.01.2009;Example Merchant;ref;;-100,00;EUR\n"), "Example Merchant", "Example Exchange", 3, false); err == nil {
			t.Fatal("statement write error accepted")
		}
	})

	t.Run("apply load error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		if err := os.WriteFile(filepath.Join(p.dataDir, "purchases.json"), []byte("{bad"), 0o600); err != nil {
			t.Fatal(err)
		}
		app := bankCLIApplication()
		if err := app.bankImport(p, bankFileFixture(t, "12.01.2009;12.01.2009;Example Merchant;ref;;-100,00;EUR\n"), "Example Merchant", "Example Exchange", 3, true); err == nil {
			t.Fatal("apply load error accepted")
		}
	})

	t.Run("backup error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		if err := os.Mkdir(filepath.Join(p.dataDir, "purchases.json.bak"), 0o700); err != nil {
			t.Fatal(err)
		}
		app := bankCLIApplication()
		if err := app.bankImport(p, bankFileFixture(t, "12.01.2009;12.01.2009;Example Merchant;ref;;-100,00;EUR\n"), "Example Merchant", "Example Exchange", 3, true); err == nil {
			t.Fatal("backup error accepted")
		}
	})

	t.Run("save error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		bank := bankFileFixture(t, "12.01.2009;12.01.2009;Example Merchant;ref;;-100,00;EUR\n")
		app := bankCLIApplication()
		if err := os.Chmod(p.dataDir, 0o500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(p.dataDir, 0o700)
		if err := app.bankImport(p, bank, "Example Merchant", "Example Exchange", 3, true); err == nil {
			t.Fatal("save error accepted")
		}
	})

	t.Run("flags", func(t *testing.T) {
		var stdout, stderr bytes.Buffer
		if err := run(ctx, []string{"bank-import"}, &stdout, &stderr); err == nil {
			t.Fatal("missing -bank-file accepted")
		}
		if err := run(ctx, []string{"bank-import", "-not-a-flag"}, &stdout, &stderr); err == nil {
			t.Fatal("bad bank-import flag accepted")
		}
		if err := run(ctx, []string{"statement-requests", "-not-a-flag"}, &stdout, &stderr); err == nil {
			t.Fatal("bad flag accepted")
		}
	})
}

func TestStatementRequestsErrors(t *testing.T) {
	t.Run("missing trace", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		app := bankCLIApplication()
		if err := app.statementRequests(p); err == nil {
			t.Fatal("missing trace accepted")
		}
	})

	t.Run("cache error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		if err := os.WriteFile(filepath.Join(p.cacheDir, "bank-matches.json"), []byte("{bad"), 0o600); err != nil {
			t.Fatal(err)
		}
		app := bankCLIApplication()
		if err := app.statementRequests(p); err == nil {
			t.Fatal("cache error accepted")
		}
	})

	t.Run("write error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		blocked := filepath.Join(t.TempDir(), "out-file")
		if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		p.outDir = blocked
		app := bankCLIApplication()
		if err := app.statementRequests(p); err == nil {
			t.Fatal("write error accepted")
		}
	})

	t.Run("report cache error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		if err := os.WriteFile(filepath.Join(p.cacheDir, "bank-matches.json"), []byte("{bad"), 0o600); err != nil {
			t.Fatal(err)
		}
		app := bankCLIApplication()
		if err := app.report(context.Background(), p, false, false, true); err == nil {
			t.Fatal("report cache error accepted")
		}
	})
}

func TestApplyBankEvidenceErrors(t *testing.T) {
	t.Run("load error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		if err := os.WriteFile(filepath.Join(p.dataDir, "purchases.json"), []byte("{bad"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := bankCLIApplication().applyBankEvidence(p, model.Report{}, bank.Cache{}, "x"); err == nil {
			t.Fatal("load error accepted")
		}
	})
	t.Run("backup error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		if err := os.Mkdir(filepath.Join(p.dataDir, "purchases.json.bak"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := bankCLIApplication().applyBankEvidence(p, model.Report{}, bank.Cache{}, "x"); err == nil {
			t.Fatal("backup error accepted")
		}
	})
	t.Run("save error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		if err := os.WriteFile(filepath.Join(p.dataDir, "purchases.json.bak"), []byte("old"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p.dataDir, 0o500); err != nil {
			t.Fatal(err)
		}
		defer os.Chmod(p.dataDir, 0o700)
		if err := bankCLIApplication().applyBankEvidence(p, model.Report{}, bank.Cache{}, "x"); err == nil {
			t.Fatal("save error accepted")
		}
	})
}
