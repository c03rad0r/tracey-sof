package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracey-sof/internal/core"
	reportfiles "tracey-sof/internal/report"
)

func TestCLINodeDiscoverTraceReportAndExport(t *testing.T) {
	requireCLIChromium(t)
	fake := newCLIFakeCore(t)
	fake.version = 280000
	fake.pruned = true
	p := newCLIPaths(t, true)
	writeCLIInputs(t, p, true)
	t.Setenv("TRACE_MAX_DEPTH", "2")
	t.Setenv("TRACE_MAX_TRANSACTIONS", "10")

	ctx, cancel := context.WithTimeout(context.Background(), 4*reportfiles.PDFTimeout())
	defer cancel()
	var stdout, stderr bytes.Buffer
	invoke := func(arguments []string) string {
		t.Helper()
		stdout.Reset()
		stderr.Reset()
		if err := run(ctx, arguments, &stdout, &stderr); err != nil {
			t.Fatalf("%s failed: %v\nstdout: %s\nstderr: %s", arguments[0], err, stdout.String(), stderr.String())
		}
		return stdout.String()
	}

	if output := invoke(cliCommandArguments("validate", p)); !strings.Contains(output, "owned, purchase, annotation, artifact, and trace-policy inputs are valid") {
		t.Fatalf("validate output = %q", output)
	}

	nodeOutput := invoke(cliCommandArguments("node-check", p))
	for _, expected := range []string{"Core version 280000", "chain=main", "txindex=true", "pruned=true", "warning: Bitcoin Core 29+", "warning: pruned node"} {
		if !strings.Contains(nodeOutput, expected) {
			t.Errorf("node-check output %q lacks %q", nodeOutput, expected)
		}
	}
	jsonStart := strings.Index(nodeOutput, "{\n")
	if jsonStart < 0 {
		t.Fatalf("node-check has no JSON object: %q", nodeOutput)
	}
	var info core.NodeInfo
	if err := json.Unmarshal([]byte(nodeOutput[jsonStart:]), &info); err != nil {
		t.Fatalf("decode node-check JSON: %v\n%s", err, nodeOutput)
	}
	if info.Version != 280000 || info.Blocks != 170 || !info.TxIndex || !info.BasicBlockFilter || !info.Pruned || len(info.Warnings) != 2 {
		t.Fatalf("node-check JSON = %+v", info)
	}

	discoveryOutput := invoke(cliCommandArguments("discover", p))
	if !strings.Contains(discoveryOutput, "discovered 1 current UTXOs at block 170 "+fake.tip.BlockHash) {
		t.Fatalf("discover output = %q", discoveryOutput)
	}
	targets, chain, err := core.LoadDiscovery(filepath.Join(p.cacheDir, "discovery.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].TxID != firstTransfer || targets[0].Vout != 0 || targets[0].ValueSats != 1000000000 || chain.BestHeight != 170 {
		t.Fatalf("discovery = targets %+v, chain %+v", targets, chain)
	}
	if len(targets[0].OwnerIDs) != 1 || targets[0].OwnerIDs[0] != "utxo:0" || len(targets[0].Labels) != 1 || targets[0].Labels[0] != "fixture current output" {
		t.Fatalf("discovery ownership = %+v", targets[0])
	}

	traceOutput := invoke(cliCommandArguments("trace", p))
	if !strings.Contains(traceOutput, "traced 1 transactions with 1 boundaries across 1 targets") {
		t.Fatalf("trace output = %q", traceOutput)
	}
	trace, err := core.LoadTrace(filepath.Join(p.cacheDir, "trace.json"))
	if err != nil {
		t.Fatal(err)
	}
	if trace.Network != "mainnet" || trace.Incomplete || len(trace.Transactions) != 1 || len(trace.Boundaries) != 1 || trace.Boundaries[0].Reason != "purchase" {
		t.Fatalf("trace = %+v", trace)
	}
	if _, err := os.Stat(filepath.Join(p.cacheDir, "transactions.json")); err != nil {
		t.Fatalf("transaction cache was not saved: %v", err)
	}

	reportOutput := invoke(cliCommandArguments("report", p))
	if !strings.Contains(reportOutput, "wrote report for 1 UTXOs to "+p.outDir) || !strings.Contains(reportOutput, "wrote "+filepath.Join(p.outDir, "report.pdf")) {
		t.Fatalf("report output = %q", reportOutput)
	}
	assertCLIPDF(t, filepath.Join(p.outDir, "report.pdf"))
	graphOutput := invoke(cliCommandArguments("graphs", p))
	for _, expected := range []string{"graph-all.svg", filepath.Join(p.outDir, "graph-")} {
		if !strings.Contains(graphOutput, expected) {
			t.Fatalf("graphs output = %q", graphOutput)
		}
	}
	if matches, err := filepath.Glob(filepath.Join(p.outDir, "graph-*.svg")); err != nil || len(matches) < 2 {
		t.Fatalf("graph-*.svg = %v, %v", matches, err)
	}
	gapsOutput := invoke(cliCommandArguments("gaps", p))
	if !strings.Contains(gapsOutput, "== INFLOW:") || !strings.Contains(gapsOutput, "== OUTFLOW:") {
		t.Fatalf("gaps output = %q", gapsOutput)
	}
	if inflowOutput := invoke(cliCommandArguments("gaps", p, "-inflow")); !strings.Contains(inflowOutput, "== INFLOW:") || strings.Contains(inflowOutput, "== OUTFLOW:") {
		t.Fatalf("gaps -inflow output = %q", inflowOutput)
	}
	if outflowOutput := invoke(cliCommandArguments("gaps", p, "-outflow")); !strings.Contains(outflowOutput, "== OUTFLOW:") || strings.Contains(outflowOutput, "== INFLOW:") {
		t.Fatalf("gaps -outflow output = %q", outflowOutput)
	}
	if output := invoke(cliCommandArguments("gaps", p, "-strict")); !strings.Contains(output, "== INFLOW:") {
		t.Fatalf("gaps-check output = %q", output)
	}
	if output := invoke(cliCommandArguments("gaps", p, "-strict", "-include-outflows")); !strings.Contains(output, "== OUTFLOW:") {
		t.Fatalf("gaps-check-all output = %q", output)
	}
	bankCSV := filepath.Join(t.TempDir(), "bank.csv")
	bankData := "Buchungstag;Valutadatum;Name Zahlungsbeteiligter;Verwendungszweck;Buchungstext;Betrag;Waehrung\n" +
		"12.01.2009;12.01.2009;Example Merchant;SEPA-UMSATZ EXAMPLE-REF-001;;-123,45;EUR\n"
	if err := os.WriteFile(bankCSV, []byte(bankData), 0o600); err != nil {
		t.Fatal(err)
	}
	bankOutput := invoke(cliCommandArguments("bank-import", p, "-bank-file", bankCSV, "-merchant", "Example Merchant", "-provider", "Example Exchange", "-apply", "-confirm", "purchase|fixture-purchase"))
	for _, want := range []string{"matched events: 1", "attached bank evidence", "statement-requests.csv"} {
		if !strings.Contains(bankOutput, want) {
			t.Errorf("bank-import output %q lacks %q", bankOutput, want)
		}
	}
	if output := invoke(cliCommandArguments("statement-requests", p)); !strings.Contains(output, "bank evidence matched:") {
		t.Fatalf("statement-requests output = %q", output)
	}
	overviewOutput := invoke(cliCommandArguments("overview", p, "-no-pdf"))
	if !strings.Contains(overviewOutput, "overview.html") {
		t.Fatalf("overview output = %q", overviewOutput)
	}
	if _, err := os.Stat(filepath.Join(p.outDir, "overview.html")); err != nil {
		t.Fatal(err)
	}
	structuredOnly := application{stdout: &stdout, stderr: &stderr}
	stdout.Reset()
	if err := structuredOnly.report(ctx, p, false, false, false); err != nil {
		t.Fatalf("structured-only report generation: %v", err)
	}
	if !strings.Contains(stdout.String(), "wrote report for 1 UTXOs") {
		t.Fatalf("structured-only report output = %q", stdout.String())
	}
	assertCLIPDF(t, filepath.Join(p.outDir, "report.pdf"))
	report, err := reportfiles.Load(filepath.Join(p.outDir, "report.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.UTXOs) != 1 || report.UTXOs[0].Status != "documented" || report.UTXOs[0].Trace != "complete" || report.Summary.DocumentedSats != 1000000000 {
		t.Fatalf("report evaluation = UTXOs %+v, summary %+v", report.UTXOs, report.Summary)
	}

	exportOutput := invoke(cliCommandArguments("export", p))
	if !strings.Contains(exportOutput, "wrote "+filepath.Join(p.outDir, "accountant.zip")) {
		t.Fatalf("export output = %q", exportOutput)
	}
	assertCLIAccountantBundle(t, filepath.Join(p.outDir, "accountant.zip"))

	auditOutput := invoke(cliCommandArguments("audit", p))
	for _, expected := range []string{"inputs are valid", "discovered 1 current UTXOs", "traced 1 transactions", "wrote report for 1 UTXOs", "report.pdf"} {
		if !strings.Contains(auditOutput, expected) {
			t.Errorf("audit output %q lacks %q", auditOutput, expected)
		}
	}

	t.Setenv("SOF_LISTEN", "localhost:8765")
	stdout.Reset()
	stderr.Reset()
	err = run(ctx, cliCommandArguments("ui", p), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "numeric loopback") {
		t.Fatalf("UI with non-numeric listen address error = %v", err)
	}

	t.Setenv("SOF_LISTEN", "127.0.0.1:-1")
	stdout.Reset()
	stderr.Reset()
	err = run(ctx, cliCommandArguments("ui", p), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "invalid port") || !strings.Contains(stdout.String(), "Serving local report") {
		t.Fatalf("UI listen error = %v, stdout = %q", err, stdout.String())
	}
}

func assertCLIPDF(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF-")) || len(data) < 1000 {
		t.Fatalf("%s is not an actual substantial Chromium PDF (%d bytes)", path, len(data))
	}
}

func assertCLIAccountantBundle(t *testing.T, path string) {
	t.Helper()
	reader, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reader.Close()
	required := map[string]bool{
		"README-accountant.txt":        false,
		"manifest.sha256":              false,
		"report.json":                  false,
		"report.csv":                   false,
		"report.html":                  false,
		"report.svg":                   false,
		"report.pdf":                   false,
		"inputs/owned.json":            false,
		"inputs/purchases.json":        false,
		"inputs/annotations.json":      false,
		"evidence/001-001-invoice.txt": false,
	}
	for _, entry := range reader.File {
		if _, wanted := required[entry.Name]; wanted {
			required[entry.Name] = true
		}
		if entry.Name == "evidence/001-001-invoice.txt" {
			contents, err := entry.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, readErr := io.ReadAll(contents)
			closeErr := contents.Close()
			if readErr != nil || closeErr != nil || !strings.Contains(string(data), "Local test invoice") {
				t.Fatalf("exported evidence = %q, read error %v, close error %v", data, readErr, closeErr)
			}
		}
	}
	for name, found := range required {
		if !found {
			t.Errorf("accountant bundle lacks %s", name)
		}
	}
}
