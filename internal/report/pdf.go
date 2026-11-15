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

// PDFOptions controls report printing.
type PDFOptions struct {
	// Unredacted renders identifiers and amounts instead of [redacted] placeholders.
	// Callers must request this explicitly; the default output stays safe to share.
	Unredacted bool
}

// PDF renders report.html with the default redacted display.
func PDF(ctx context.Context, outDir, browser string) error {
	return PDFWithOptions(ctx, outDir, browser, PDFOptions{})
}

// PDFWithOptions renders report.html, optionally with identifiers visible.
func PDFWithOptions(ctx context.Context, outDir, browser string, options PDFOptions) error {
	return PrintHTML(ctx, outDir, "report.html", "report.pdf", browser, options)
}

// PrintHTML renders one generated HTML file into a PDF in the same directory.
func PrintHTML(ctx context.Context, outDir, htmlName, pdfName, browser string, options PDFOptions) error {
	absoluteOut, err := filepath.Abs(outDir)
	if err != nil {
		return fmt.Errorf("resolve report output directory: %w", err)
	}
	outDir = absoluteOut
	if browser == "" {
		for _, candidate := range []string{"google-chrome", "chromium", "chromium-browser"} {
			if path, err := exec.LookPath(candidate); err == nil {
				browser = path
				break
			}
		}
	}
	if browser == "" {
		return fmt.Errorf("Chromium is required for PDF output; set CHROMIUM to a local executable")
	}
	if _, err := os.Stat(filepath.Join(outDir, htmlName)); err != nil {
		return fmt.Errorf("%s is required before PDF export: %w", htmlName, err)
	}
	profile, err := os.MkdirTemp(outDir, ".chromium-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(profile)
	pdfPath := filepath.Join(outDir, pdfName)
	htmlLocation := &url.URL{Scheme: "file", Path: filepath.Join(outDir, htmlName)}
	if options.Unredacted {
		htmlLocation.RawQuery = ""
	} else {
		htmlLocation.RawQuery = "redact=1"
	}
	htmlURL := htmlLocation.String()
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
