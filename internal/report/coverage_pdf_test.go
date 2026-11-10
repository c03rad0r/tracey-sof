package report

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func coverageBrowserDir(t *testing.T) string {
	t.Helper()
	cache := filepath.Join("..", "..", ".cache")
	if err := os.MkdirAll(cache, 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(cache, "coverage browser ")
	if err != nil {
		t.Fatal(err)
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(absolute) })
	return absolute
}

func coverageBrowserScript(t *testing.T, dir, name, body string) string {
	t.Helper()
	name = filepath.Join(dir, name)
	if err := os.WriteFile(name, []byte("#!/bin/sh\nset -eu\n"+body), 0o700); err != nil {
		t.Fatal(err)
	}
	return name
}

func coverageReportHTML(t *testing.T, dir string) {
	t.Helper()
	coverageWriteFile(t, filepath.Join(dir, "report.html"), []byte("<!doctype html><title>offline report</title>\n"))
}

func TestCoveragePDFSuccessfulAutoDetectedBrowser(t *testing.T) {
	dir := coverageBrowserDir(t)
	coverageReportHTML(t, dir)
	browserDir := filepath.Join(dir, "browser-bin")
	if err := os.Mkdir(browserDir, 0o700); err != nil {
		t.Fatal(err)
	}
	body := `
pdf=
for argument in "$@"; do
  case "$argument" in
    --print-to-pdf=*) pdf=${argument#--print-to-pdf=} ;;
  esac
done
printf '%s\n' "$@" > "$pdf.arguments"
printf '%%PDF-1.7\n%% synthetic offline browser output\n' > "$pdf"
`
	coverageBrowserScript(t, browserDir, "chromium", body)
	t.Setenv("PATH", browserDir)
	if err := PDF(context.Background(), dir, ""); err != nil {
		t.Fatal(err)
	}
	pdfPath := filepath.Join(dir, "report.pdf")
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(data), "%PDF-1.7\n") {
		t.Errorf("PDF has unexpected contents %q", data)
	}
	info, err := os.Stat(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Errorf("PDF mode = %04o, want 0600", got)
	}
	arguments, err := os.ReadFile(pdfPath + ".arguments")
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSuffix(string(arguments), "\n"), "\n")
	if len(args) != 10 {
		t.Fatalf("browser received %d arguments, want 10: %q", len(args), args)
	}
	fixed := []string{"--headless", "--disable-gpu", "--disable-background-networking", "--disable-component-update", "--no-first-run", "--no-default-browser-check", "--no-pdf-header-footer"}
	for i, want := range fixed {
		if args[i] != want {
			t.Errorf("argument %d = %q, want %q", i, args[i], want)
		}
	}
	profile := strings.TrimPrefix(args[7], "--user-data-dir=")
	if !strings.HasPrefix(args[7], "--user-data-dir="+dir+string(os.PathSeparator)+".chromium-") {
		t.Errorf("browser profile is outside report output directory: %q", args[7])
	}
	if args[8] != "--print-to-pdf="+pdfPath {
		t.Errorf("PDF argument = %q, want %q", args[8], "--print-to-pdf="+pdfPath)
	}
	if !strings.HasPrefix(args[9], "file://") || !strings.Contains(args[9], "report.html") || !strings.Contains(args[9], "%20") {
		t.Errorf("HTML file URL is not correctly escaped: %q", args[9])
	}
	profiles, err := filepath.Glob(filepath.Join(dir, ".chromium-*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(profiles) != 0 {
		t.Errorf("temporary Chromium profiles were not removed: %v", profiles)
	}
	if _, err := os.Stat(profile); !os.IsNotExist(err) {
		t.Errorf("recorded browser profile still exists: %v", err)
	}
}

func TestCoveragePDFBrowserFailures(t *testing.T) {
	writePDF := `
pdf=
for argument in "$@"; do
  case "$argument" in
    --print-to-pdf=*) pdf=${argument#--print-to-pdf=} ;;
  esac
done
`
	tests := []struct {
		name string
		body string
		want string
	}{
		{name: "browser exits unsuccessfully", body: `printf 'synthetic browser diagnostic' >&2; exit 23`, want: "Chromium PDF export"},
		{name: "browser creates no output", body: `exit 0`, want: "Chromium did not produce a PDF"},
		{name: "browser creates tiny output", body: writePDF + `printf 'PDF' > "$pdf"`, want: "Chromium did not produce a PDF"},
		{name: "browser creates non PDF", body: writePDF + `printf 'NOT-A-PDF' > "$pdf"`, want: "Chromium output is not a PDF"},
		{name: "PDF path is unreadable directory", body: writePDF + `
mkdir "$pdf"
i=0
while [ "$i" -lt 40 ]; do
  : > "$pdf/file-$i"
  i=$((i+1))
done`, want: ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := coverageBrowserDir(t)
			coverageReportHTML(t, dir)
			browser := coverageBrowserScript(t, dir, "fake-browser", test.body)
			err := PDF(context.Background(), dir, browser)
			if err == nil {
				t.Fatal("broken browser output was accepted")
			}
			if test.want != "" && !strings.Contains(err.Error(), test.want) {
				t.Fatalf("PDF error = %v, want text %q", err, test.want)
			}
		})
	}
}

func TestCoveragePDFSetupAndContextErrors(t *testing.T) {
	t.Run("no browser in path", func(t *testing.T) {
		dir := coverageBrowserDir(t)
		coverageReportHTML(t, dir)
		t.Setenv("PATH", filepath.Join(dir, "empty-path"))
		err := PDF(context.Background(), dir, "")
		if err == nil || !strings.Contains(err.Error(), "Chromium is required") {
			t.Fatalf("PDF error = %v", err)
		}
	})

	t.Run("missing HTML precedes browser launch", func(t *testing.T) {
		dir := coverageBrowserDir(t)
		browser := coverageBrowserScript(t, dir, "must-not-run", `printf 'ran' > "`+filepath.Join(dir, "ran")+`"`)
		err := PDF(context.Background(), dir, browser)
		if err == nil || !strings.Contains(err.Error(), "report.html is required") {
			t.Fatalf("PDF error = %v", err)
		}
		if _, statErr := os.Stat(filepath.Join(dir, "ran")); !os.IsNotExist(statErr) {
			t.Errorf("browser ran without report.html: %v", statErr)
		}
	})

	t.Run("profile directory cannot be created", func(t *testing.T) {
		dir := coverageBrowserDir(t)
		coverageReportHTML(t, dir)
		browser := coverageBrowserScript(t, dir, "fake-browser", `exit 0`)
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		if err := PDF(context.Background(), dir, browser); err == nil {
			t.Fatal("PDF export created a browser profile in a read-only output directory")
		}
	})

	t.Run("already canceled context", func(t *testing.T) {
		dir := coverageBrowserDir(t)
		coverageReportHTML(t, dir)
		browser := coverageBrowserScript(t, dir, "fake-browser", `sleep 30`)
		ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
		cancel()
		err := PDF(ctx, dir, browser)
		if err == nil || !strings.Contains(err.Error(), "Chromium PDF export") {
			t.Fatalf("PDF error = %v", err)
		}
	})

	t.Run("output directory cannot be resolved after cwd removal", func(t *testing.T) {
		original, err := os.Open(".")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = original.Chdir()
			_ = original.Close()
		})
		working := coverageBrowserDir(t)
		if err := os.Chdir(working); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(working); err != nil {
			_ = original.Chdir()
			t.Fatal(err)
		}
		err = PDF(context.Background(), "relative-output", "unused-browser")
		restoreErr := original.Chdir()
		if restoreErr != nil {
			t.Fatal(restoreErr)
		}
		if err == nil || !strings.Contains(err.Error(), "resolve report output directory") {
			t.Fatalf("PDF error = %v", err)
		}
	})
}
