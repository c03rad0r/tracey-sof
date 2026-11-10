package report

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tracey-sof/internal/input"
	"tracey-sof/internal/model"
)

func testReport() model.Report {
	txid := "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16"
	return model.Report{SchemaVersion: 1, Network: "mainnet", GeneratedAt: time.Unix(0, 0).UTC().Format(time.RFC3339),
		Chain:     model.Chain{BestBlock: "00000000d1145790a8694403d4063f323d499e655c83426834d4ce2f8dd4a2ee", BestHeight: 170},
		Purchases: []model.Purchase{{ID: "buy", Provider: "ATM"}}, Warnings: []string{"none"},
		UTXOs: []model.Target{{Outpoint: model.Outpoint{TxID: txid, Vout: 0}, TxID: txid, Vout: 0, Address: "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa", ValueSats: 100, Status: "documented", Trace: "complete", PurchaseIDs: []string{"buy"}}},
		Graph: model.Graph{Nodes: []model.GraphNode{{ID: "out:" + txid + ":0", Kind: "output", Outpoint: txid + ":0", Owned: true, ValueSats: 100}, {ID: "tx:" + txid, Kind: "transaction", TxID: txid, PurchaseIDs: []string{"buy"}, TerminalReason: "purchase"}},
			Edges: []model.GraphEdge{{From: "out:" + txid + ":0", To: "tx:" + txid, Kind: "funded_by", ValueSats: 100}}}, Summary: model.Summary{TotalUTXOs: 1, TotalSats: 100, DocumentedUTXOs: 1, DocumentedSats: 100}, Annotations: input.EmptyAnnotations()}
}

func TestStructuredExportsAndHTMLPrivacy(t *testing.T) {
	dir := t.TempDir()
	report := testReport()
	if err := WriteStructured(report, dir, false); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"report.json", "report.html", "report.csv", "report.svg"} {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(data) == 0 {
			t.Fatalf("%s: %v", name, err)
		}
	}
	html, err := os.ReadFile(filepath.Join(dir, "report.html"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(html)
	for _, good := range []string{"<!doctype html", "SOFGraph", "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16:0", `"server":false`} {
		if !strings.Contains(text, good) {
			t.Errorf("HTML missing %q", good)
		}
	}
	for _, bad := range []string{"mempool.space/api", "arkm.com", "https://mempool.space", "/*__APP_JS__*/"} {
		if strings.Contains(text, bad) {
			t.Errorf("HTML contains external or unresolved content %q", bad)
		}
	}
	loaded, err := Load(filepath.Join(dir, "report.json"))
	if err != nil || loaded.UTXOs[0].Key() != report.UTXOs[0].Key() {
		t.Fatalf("report roundtrip: %v %+v", err, loaded.UTXOs)
	}
	csvData, _ := CSVBytes(report)
	if !strings.Contains(string(csvData), "documentation_status") || !strings.Contains(string(csvData), "100") {
		t.Error("CSV lacks required fields")
	}
}

func TestSVGValidationAndEscaping(t *testing.T) {
	report := testReport()
	report.Graph.Nodes[0].Label = `<script>alert("x")</script>`
	data, err := SVGBytes(report)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "<script>") || !strings.Contains(string(data), "&lt;script&gt;") {
		t.Error("SVG escaping failed")
	}
	cases := []func(*model.Report){
		func(r *model.Report) { r.Graph.Nodes[0].ID = "" },
		func(r *model.Report) { r.Graph.Nodes[1].ID = r.Graph.Nodes[0].ID },
		func(r *model.Report) { r.Graph.Edges[0].From = "missing" },
		func(r *model.Report) { r.Graph.Edges[0].To = "missing" },
		func(r *model.Report) { r.Graph.Edges[0].Kind = "guess" },
	}
	for i, mutate := range cases {
		current := testReport()
		mutate(&current)
		if _, err := SVGBytes(current); err == nil {
			t.Errorf("malformed graph %d accepted", i)
		}
	}
}

func TestBundleAndPDF(t *testing.T) {
	if err := os.MkdirAll("../../.cache", 0o700); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("../../.cache", "report-pdf-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	report := testReport()
	if err := WriteStructured(report, dir, false); err != nil {
		t.Fatal(err)
	}
	purchases := model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{{ID: "buy"}}}
	if err := os.WriteFile(filepath.Join(dir, "owned.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "purchases.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(dir, "report.pdf")); err == nil && len(data) > 0 {
		_ = data
	}
	ctx, cancel := context.WithTimeout(context.Background(), PDFTimeout())
	defer cancel()
	if err := PDF(ctx, dir, ""); err != nil {
		t.Skipf("Chromium unavailable: %v", err)
	}
	if err := Bundle(report, purchases, dir, dir); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(filepath.Join(dir, "accountant.zip"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	info, _ := file.Stat()
	reader, err := zip.NewReader(file, info.Size())
	if err != nil {
		t.Fatal(err)
	}
	seenManifest := false
	for _, entry := range reader.File {
		if entry.Name == "manifest.sha256" {
			seenManifest = true
		}
		content, err := entry.Open()
		if err != nil {
			t.Fatal(err)
		}
		var data bytes.Buffer
		_, _ = data.ReadFrom(content)
		_ = content.Close()
		if len(data.Bytes()) == 0 {
			t.Errorf("empty bundle entry %s", entry.Name)
		}
	}
	if !seenManifest {
		t.Error("bundle lacks manifest")
	}
}

func TestPDFRequiresInputs(t *testing.T) {
	if err := PDF(context.Background(), t.TempDir(), "/definitely/not/a/browser"); err == nil {
		t.Fatal("expected browser validation error")
	}
	if err := PDF(context.Background(), t.TempDir(), ""); err == nil {
		t.Fatal("expected missing report/browser error")
	}
}

func TestFileErrors(t *testing.T) {
	if _, err := Load("/nonexistent/report.json"); err == nil {
		t.Error("missing report accepted")
	}
	if _, err := JSONBytes(make(chan int)); err == nil {
		t.Error("invalid JSON value accepted")
	}
	if err := WriteStructured(model.Report{Graph: model.Graph{Nodes: []model.GraphNode{{}}}}, t.TempDir(), false); err == nil {
		t.Error("malformed report accepted")
	}
}
