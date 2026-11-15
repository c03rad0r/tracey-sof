package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverviewErrors(t *testing.T) {
	t.Run("missing trace", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		var stdout, stderr bytes.Buffer
		app := application{stdout: &stdout, stderr: &stderr}
		if err := app.overview(context.Background(), p, true); err == nil {
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
		var stdout, stderr bytes.Buffer
		app := application{stdout: &stdout, stderr: &stderr}
		if err := app.overview(context.Background(), p, true); err == nil {
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
		var stdout, stderr bytes.Buffer
		app := application{stdout: &stdout, stderr: &stderr}
		if err := app.overview(context.Background(), p, true); err == nil {
			t.Fatal("write error accepted")
		}
	})
	t.Run("pdf error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		t.Setenv("CHROMIUM", filepath.Join(t.TempDir(), "missing-browser"))
		var stdout, stderr bytes.Buffer
		app := application{stdout: &stdout, stderr: &stderr}
		if err := app.overview(context.Background(), p, false); err == nil {
			t.Fatal("missing browser accepted")
		}
	})
}

func TestOverviewWritesPDF(t *testing.T) {
	p := newCLIPaths(t, false)
	writeCLIInputs(t, p, false)
	saveCLIFixtureTrace(t, p)
	browser := filepath.Join(t.TempDir(), "fake-browser")
	script := `#!/bin/sh
pdf=
for argument in "$@"; do
  case "$argument" in
    --print-to-pdf=*) pdf=${argument#--print-to-pdf=} ;;
  esac
done
printf '%%PDF-1.7\n' > "$pdf"
`
	if err := os.WriteFile(browser, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHROMIUM", browser)
	var stdout, stderr bytes.Buffer
	app := application{stdout: &stdout, stderr: &stderr}
	if err := app.overview(context.Background(), p, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout.String(), "overview.pdf") {
		t.Fatalf("output = %q", stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(p.outDir, "overview.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "%PDF-") {
		t.Fatalf("not a PDF: %q", data)
	}
	if _, err := os.Stat(filepath.Join(p.outDir, "overview.html")); err != nil {
		t.Fatal(err)
	}
}

func TestOverviewFlags(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), []string{"overview", "-not-a-flag"}, &stdout, &stderr); err == nil {
		t.Fatal("bad overview flag accepted")
	}
}
