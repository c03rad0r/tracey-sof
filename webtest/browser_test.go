package webtest

import (
	"context"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tracey-sof/internal/input"
	"tracey-sof/internal/model"
	"tracey-sof/internal/report"
)

const browserTxID = "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16"

func browserReport() model.Report {
	key := model.Outpoint{TxID: browserTxID, Vout: 0}
	return model.Report{SchemaVersion: 1, Network: "mainnet", GeneratedAt: time.Unix(0, 0).UTC().Format(time.RFC3339),
		Chain:     model.Chain{BestBlock: strings.Repeat("0", 64), BestHeight: 170},
		Purchases: []model.Purchase{{ID: "historical-atm", Provider: "Public fixture evidence"}},
		UTXOs:     []model.Target{{Outpoint: key, TxID: browserTxID, Vout: 0, Address: "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa", ValueSats: 100, Status: "documented", Trace: "complete", PurchaseIDs: []string{"historical-atm"}}},
		Graph: model.Graph{Nodes: []model.GraphNode{{ID: "out:" + key.String(), Kind: "output", Outpoint: key.String(), Address: "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa", ValueSats: 100, Owned: true}, {ID: "tx:" + browserTxID, Kind: "transaction", TxID: browserTxID, PurchaseIDs: []string{"historical-atm"}, TerminalReason: "purchase"}},
			Edges: []model.GraphEdge{{From: "out:" + key.String(), To: "tx:" + browserTxID, Kind: "funded_by", ValueSats: 100}}},
		Summary: model.Summary{TotalUTXOs: 1, TotalSats: 100, DocumentedUTXOs: 1, DocumentedSats: 100}, Annotations: input.EmptyAnnotations(), Warnings: []string{}}
}

func chromiumPath(t *testing.T) string {
	t.Helper()
	for _, candidate := range []string{"chromium", "chromium-browser", "google-chrome"} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	t.Fatal("Chromium is required for release browser verification")
	return ""
}

func projectBrowserDir(t *testing.T) string {
	t.Helper()
	if err := os.MkdirAll("../.cache", 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("../.cache", "browser-test-")
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

func runChromium(t *testing.T, ctx context.Context, browser, html string) string {
	t.Helper()
	profile := filepath.Join(filepath.Dir(html), "profile")
	args := []string{"--headless", "--disable-gpu", "--disable-background-networking", "--disable-component-update", "--no-first-run", "--no-default-browser-check", "--virtual-time-budget=2000", "--run-all-compositor-stages-before-draw", "--user-data-dir=" + profile, "--dump-dom", (&url.URL{Scheme: "file", Path: html}).String()}
	command := exec.CommandContext(ctx, browser, args...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("Chromium: %v\n%s", err, output)
	}
	return string(output)
}

func TestOfflineReportRendersWithoutExternalServices(t *testing.T) {
	dir := projectBrowserDir(t)
	data, err := report.HTMLBytes(browserReport(), false)
	if err != nil {
		t.Fatal(err)
	}
	html := filepath.Join(dir, "report.html")
	if err := os.WriteFile(html, data, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	dom := runChromium(t, ctx, chromiumPath(t), html)
	for _, expected := range []string{`class="sof-node node-output selected-root"`, `class="sof-node node-transaction"`, "1 of 1 current UTXOs selected", "linked purchase record(s)", "graph-viewport", "Redaction: on"} {
		if !strings.Contains(dom, expected) {
			t.Errorf("rendered DOM lacks %q; output follows:\n%s", expected, dom)
		}
	}
	if !strings.Contains(dom, `<div id="load-notices" class="notice-stack" aria-live="polite"></div>`) {
		t.Error("HTML has an active report-loading error")
	}
}

func TestBrowserPrivacyAndMalformedInput(t *testing.T) {
	dir := projectBrowserDir(t)
	custom := browserReport()
	custom.Purchases[0].Provider = `<img src=x onerror=alert(1)>`
	data, err := report.HTMLBytes(custom, false)
	if err != nil {
		t.Fatal(err)
	}
	html := filepath.Join(dir, "report.html")
	if err := os.WriteFile(html, data, 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	dom := runChromium(t, ctx, chromiumPath(t), html)
	if strings.Contains(dom, `<img src=x onerror=alert(1)>`) {
		t.Fatal("purchase evidence was inserted as executable markup")
	}
	if strings.Contains(dom, `>No current UTXOs match the filters.<`) {
		t.Error("selected UTXOs produced an empty graph")
	}
	if !strings.Contains(dom, "Current selected UTXO") {
		t.Fatal("report did not render with hostile text input")
	}
}
