package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracey-sof/internal/input"
	"tracey-sof/internal/model"
)

func TestDiscoverCommandErrorPropagation(t *testing.T) {
	t.Run("input error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("discover", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "owned.json") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("RPC configuration error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		t.Setenv("BITCOIND_RPC_URL", "://invalid")
		t.Setenv("BITCOIND_COOKIE_FILE", "")
		t.Setenv("BITCOIND_RPC_USER", "")
		t.Setenv("BITCOIND_RPC_PASSWORD", "")
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("discover", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "invalid Bitcoin RPC URL") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("node check error", func(t *testing.T) {
		fake := newCLIFakeCore(t)
		fake.chain = "test"
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("discover", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "expected main") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("transaction cache error", func(t *testing.T) {
		newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		if err := os.MkdirAll(p.cacheDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p.cacheDir, "transactions.json"), []byte("broken"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("discover", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "decode transaction cache") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("discovery RPC error", func(t *testing.T) {
		newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		writeCLIJSON(t, filepath.Join(p.dataDir, "owned.json"), model.Owned{
			SchemaVersion: model.SchemaVersion, Network: "mainnet",
			UTXOs: []model.OwnedUTXO{{TxID: "a38d861a0584254429a8e84747c0d203ddada49d6355bbf9de92e4fffc230c47"}},
		})
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("discover", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "unexpected gettxout params") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("discovery save error", func(t *testing.T) {
		newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		if err := os.MkdirAll(filepath.Join(p.cacheDir, "discovery.json"), 0o700); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("discover", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "is a directory") || strings.Contains(stdout.String(), "discovered 1 current") {
			t.Fatalf("error = %v, stdout = %q", err, stdout.String())
		}
	})
}

func TestTraceCommandAdditionalErrorPropagation(t *testing.T) {
	t.Run("input error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("trace", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "owned.json") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("RPC configuration error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureDiscovery(t, p)
		t.Setenv("BITCOIND_RPC_URL", "://invalid")
		t.Setenv("BITCOIND_COOKIE_FILE", "")
		t.Setenv("BITCOIND_RPC_USER", "")
		t.Setenv("BITCOIND_RPC_PASSWORD", "")
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("trace", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "invalid Bitcoin RPC URL") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("initial chain RPC error", func(t *testing.T) {
		fake := newCLIFakeCore(t)
		fake.chain = "signet"
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureDiscovery(t, p)
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("trace", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "requires chain main") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("evidence reference error", func(t *testing.T) {
		newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureDiscovery(t, p)
		annotations := input.EmptyAnnotations()
		annotations.PurchaseLinks = []model.PurchaseLink{{Target: firstTransfer, PurchaseID: "unknown", Decision: "confirmed"}}
		writeCLIJSON(t, filepath.Join(p.dataDir, "annotations.json"), annotations)
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("trace", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "unknown purchase ID") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("trace persistence error", func(t *testing.T) {
		newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureDiscovery(t, p)
		if err := os.MkdirAll(filepath.Join(p.cacheDir, "trace.json"), 0o700); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("trace", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "trace.json") || strings.Contains(stdout.String(), "traced") {
			t.Fatalf("error = %v, stdout = %q", err, stdout.String())
		}
	})
}

func TestReportAndExportWriteFailures(t *testing.T) {
	t.Run("report input error after trace load", func(t *testing.T) {
		p := newCLIPaths(t, false)
		saveCLIFixtureTrace(t, p)
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("report", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "owned.json") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("structured output error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		if err := os.MkdirAll(filepath.Join(p.outDir, "report.json"), 0o700); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("report", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "write report.json") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("Chromium execution error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		t.Setenv("CHROMIUM", filepath.Join(t.TempDir(), "missing-chromium"))
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("report", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "Chromium PDF export") {
			t.Fatalf("error = %v", err)
		}
		if !strings.Contains(stdout.String(), "wrote report for 1 UTXOs") {
			t.Fatalf("structured report was not written before PDF failure: %q", stdout.String())
		}
	})

	t.Run("bundle rejects missing PDF", func(t *testing.T) {
		p := newCLIPaths(t, false)
		purchases := writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		var stdout, stderr bytes.Buffer
		app := application{stdout: &stdout, stderr: &stderr}
		if err := app.report(context.Background(), p, false, false); err != nil {
			t.Fatal(err)
		}
		err := app.export(p, purchases)
		if err == nil || !strings.Contains(err.Error(), "bundle requires report.pdf") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestExampleAndTestNodeFailurePropagation(t *testing.T) {
	t.Run("example fixture directory is empty", func(t *testing.T) {
		p := newCLIPaths(t, false)
		p.fixtures = t.TempDir()
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("example", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "no transaction fixtures") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("example canceled trace", func(t *testing.T) {
		p := newCLIPaths(t, false)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		var stdout, stderr bytes.Buffer
		err := run(ctx, cliCommandArguments("example", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("example output error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		if err := os.MkdirAll(filepath.Join(p.outDir, "report.json"), 0o700); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("example", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "write report.json") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("test-node fixture error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		p.fixtures = t.TempDir()
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("test-node", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "no transaction fixtures") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("test-node RPC configuration error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		t.Setenv("BITCOIND_RPC_URL", "://invalid")
		t.Setenv("BITCOIND_COOKIE_FILE", "")
		t.Setenv("BITCOIND_RPC_USER", "")
		t.Setenv("BITCOIND_RPC_PASSWORD", "")
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("test-node", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "invalid Bitcoin RPC URL") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("test-node chain error", func(t *testing.T) {
		fake := newCLIFakeCore(t)
		fake.chain = "test"
		p := newCLIPaths(t, false)
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("test-node", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "requires chain main") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("test-node missing transaction", func(t *testing.T) {
		fake := newCLIFakeCore(t)
		fake.missingTxID = "0437cd7f8525ceed2324359c2d0ba26006d92d856a9c20fa0241106ee5a597c9"
		p := newCLIPaths(t, false)
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("test-node", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "verify 0437cd7f") || strings.Contains(stdout.String(), "verified 0437cd7f") {
			t.Fatalf("error = %v, stdout = %q", err, stdout.String())
		}
	})
}
