package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	reportfiles "tracey-sof/internal/report"
)

func requireCLIChromium(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"chromium", "chromium-browser", "google-chrome"} {
		if path, err := exec.LookPath(candidate); err == nil {
			t.Setenv("CHROMIUM", path)
			return path
		}
	}
	t.Fatal("Chromium is required for the actual PDF-generation test")
	return ""
}

func TestExampleGeneratesStructuredReportAndActualPDF(t *testing.T) {
	browser := requireCLIChromium(t)
	p := newCLIPaths(t, true)
	var stdout, stderr bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), reportfiles.PDFTimeout())
	defer cancel()
	if err := run(ctx, cliCommandArguments("example", p), &stdout, &stderr); err != nil {
		t.Fatalf("example with %s failed: %v\nstderr: %s", browser, err, stderr.String())
	}
	for _, name := range []string{"report.json", "report.csv", "report.html", "report.svg", "report.pdf"} {
		info, err := os.Stat(filepath.Join(p.outDir, name))
		if err != nil || info.Size() == 0 {
			t.Fatalf("generated %s: info=%v error=%v", name, info, err)
		}
	}
	pdf, err := os.ReadFile(filepath.Join(p.outDir, "report.pdf"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || len(pdf) < 1000 {
		t.Fatalf("Chromium output is not a substantial PDF: %d bytes, prefix %q", len(pdf), pdf[:min(len(pdf), 8)])
	}
	report, err := reportfiles.Load(filepath.Join(p.outDir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if report.Network != "mainnet" || len(report.UTXOs) != 1 || report.UTXOs[0].TxID != firstTransfer {
		t.Fatalf("unexpected example report: network=%q UTXOs=%+v", report.Network, report.UTXOs)
	}
	if report.UTXOs[0].Status != "documented" || len(report.UTXOs[0].PurchaseIDs) != 1 || report.UTXOs[0].PurchaseIDs[0] != "example-first-transfer" {
		t.Fatalf("example provenance was not evaluated: %+v", report.UTXOs[0])
	}
	if !strings.Contains(strings.Join(report.Warnings, "\n"), "Offline historical mainnet demonstration") {
		t.Fatalf("offline warning missing: %+v", report.Warnings)
	}
	if !strings.Contains(stdout.String(), filepath.Join(p.outDir, "report.pdf")) {
		t.Fatalf("example stdout = %q", stdout.String())
	}
}

func TestTestNodeVerifiesEveryRealFixtureThroughRPC(t *testing.T) {
	fake := newCLIFakeCore(t)
	p := newCLIPaths(t, false)
	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), cliCommandArguments("test-node", p), &stdout, &stderr); err != nil {
		t.Fatalf("test-node: %v\nstderr: %s", err, stderr.String())
	}
	for _, fixture := range loadCLIFixtures(t) {
		want := fmt.Sprintf("verified %s at block %d\n", fixture.TxID, fixture.BlockHeight)
		if !strings.Contains(stdout.String(), want) {
			t.Errorf("stdout %q does not contain %q", stdout.String(), want)
		}
	}
	if got := fake.callsFor("getrawtransaction"); got != 3 {
		t.Fatalf("getrawtransaction calls = %d, want one for each real fixture", got)
	}
	if got := fake.callsFor("getblockchaininfo"); got != 1 {
		t.Fatalf("initial chain checks = %d, want 1", got)
	}
	if !strings.Contains(stdout.String(), "verified first-transfer backward ancestry through its block-9 coinbase parent") {
		t.Fatalf("test-node did not verify real ancestry: %q", stdout.String())
	}
}

func TestTestNodeRejectsMismatchedRPCData(t *testing.T) {
	fake := newCLIFakeCore(t)
	fake.wrongRawID = true
	p := newCLIPaths(t, false)
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), cliCommandArguments("test-node", p), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "mismatched txid") {
		t.Fatalf("mismatched fixture RPC data error = %v", err)
	}
	if strings.Contains(stdout.String(), "verified") {
		t.Fatalf("command reported success for mismatched data: %q", stdout.String())
	}
}

func TestPDFCommandRequiresGeneratedHTML(t *testing.T) {
	p := newCLIPaths(t, false)
	t.Setenv("CHROMIUM", "/definitely/not/a/browser")
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), cliCommandArguments("pdf", p), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "report.html is required") {
		t.Fatalf("PDF prerequisite error = %v", err)
	}
}

func TestExampleRejectsDirectoryWithoutFirstTransfer(t *testing.T) {
	p := newCLIPaths(t, false)
	p.fixtures = t.TempDir()
	fixture := filepath.Join(cliFixtureDirectory(), "0437cd7f8525ceed2324359c2d0ba26006d92d856a9c20fa0241106ee5a597c9.json")
	data, err := os.ReadFile(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.fixtures, filepath.Base(fixture)), data, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	err = run(context.Background(), cliCommandArguments("example", p), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "first-transfer fixture is missing") {
		t.Fatalf("missing first-transfer fixture error = %v", err)
	}
}
