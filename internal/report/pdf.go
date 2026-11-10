package report

import (
	"bytes"
	"context"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func PDF(ctx context.Context, outDir, browser string) error {
	absoluteOut, err := filepath.Abs(outDir)
	if err != nil {
		return fmt.Errorf("resolve report output directory: %w", err)
	}
	outDir = absoluteOut
	if browser == "" {
		for _, candidate := range []string{"chromium", "chromium-browser", "google-chrome"} {
			if path, err := exec.LookPath(candidate); err == nil {
				browser = path
				break
			}
		}
	}
	if browser == "" {
		return fmt.Errorf("Chromium is required for PDF output; set CHROMIUM to a local executable")
	}
	if _, err := os.Stat(filepath.Join(outDir, "report.html")); err != nil {
		return fmt.Errorf("report.html is required before PDF export: %w", err)
	}
	profile, err := os.MkdirTemp(outDir, ".chromium-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(profile)
	pdfPath := filepath.Join(outDir, "report.pdf")
	htmlURL := (&url.URL{Scheme: "file", Path: filepath.Join(outDir, "report.html")}).String()
	args := []string{"--headless", "--disable-gpu", "--disable-background-networking", "--disable-component-update", "--no-first-run", "--no-default-browser-check", "--no-pdf-header-footer", "--user-data-dir=" + profile, "--print-to-pdf=" + pdfPath, htmlURL}
	command := exec.CommandContext(ctx, browser, args...)
	var output bytes.Buffer
	command.Stdout, command.Stderr = &output, &output
	if err := command.Run(); err != nil {
		return fmt.Errorf("Chromium PDF export: %w: %s", err, strings.TrimSpace(output.String()))
	}
	if info, err := os.Stat(pdfPath); err != nil || info.Size() < 5 {
		return fmt.Errorf("Chromium did not produce a PDF: %s", strings.TrimSpace(output.String()))
	}
	data, err := os.ReadFile(pdfPath)
	if err != nil {
		return err
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return fmt.Errorf("Chromium output is not a PDF")
	}
	return os.Chmod(pdfPath, 0o600)
}

// PDFTimeout prevents a failed browser from hanging the whole report pipeline.
func PDFTimeout() time.Duration { return 2 * time.Minute }
