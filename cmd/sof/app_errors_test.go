package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracey-sof/internal/core"
	"tracey-sof/internal/input"
	"tracey-sof/internal/model"
)

func TestValidateHappyPathAndInputErrors(t *testing.T) {
	t.Run("valid inputs including artifact", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, true)
		var stdout, stderr bytes.Buffer
		if err := run(context.Background(), cliCommandArguments("validate", p), &stdout, &stderr); err != nil {
			t.Fatal(err)
		}
		if stdout.String() != "owned, purchase, annotation, artifact, and trace-policy inputs are valid\n" {
			t.Fatalf("validate output = %q", stdout.String())
		}
	})

	t.Run("missing inputs", func(t *testing.T) {
		p := newCLIPaths(t, false)
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("validate", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "owned.json") || stdout.Len() != 0 {
			t.Fatalf("error = %v, stdout = %q", err, stdout.String())
		}
	})

	t.Run("invalid owned network", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIJSON(t, filepath.Join(p.dataDir, "owned.json"), model.Owned{
			SchemaVersion: model.SchemaVersion, Network: "testnet", UTXOs: []model.OwnedUTXO{{TxID: firstTransfer}},
		})
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("validate", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "network must be mainnet") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("malformed purchase JSON", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		if err := os.WriteFile(filepath.Join(p.dataDir, "purchases.json"), []byte("{not-json\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("validate", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "decode") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("changed artifact", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, true)
		if err := os.WriteFile(filepath.Join(p.dataDir, "invoice.txt"), []byte("modified after digest was recorded"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("validate", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "sha256 mismatch") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("invalid annotations", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		annotations := input.EmptyAnnotations()
		annotations.Transactions["not-a-transaction"] = model.Annotation{Label: "bad key"}
		writeCLIJSON(t, filepath.Join(p.dataDir, "annotations.json"), annotations)
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("validate", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "invalid annotated txid") {
			t.Fatalf("error = %v", err)
		}
	})
}

func TestRPCConfigurationErrorsReachCLI(t *testing.T) {
	tests := []struct {
		name     string
		url      string
		cookie   string
		username string
		password string
		want     string
	}{
		{name: "invalid URL", url: "://bad", want: "invalid Bitcoin RPC URL"},
		{name: "password without username", password: "secret", want: "both RPC username and password"},
		{name: "username without password", username: "user", want: "both RPC username and password"},
		{name: "conflicting authentication", cookie: "cookie", username: "user", password: "secret", want: "choose cookie authentication"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("BITCOIND_RPC_URL", test.url)
			if test.url == "" {
				t.Setenv("BITCOIND_RPC_URL", "http://127.0.0.1:8332")
			}
			t.Setenv("BITCOIND_COOKIE_FILE", test.cookie)
			t.Setenv("BITCOIND_RPC_USER", test.username)
			t.Setenv("BITCOIND_RPC_PASSWORD", test.password)
			p := newCLIPaths(t, false)
			var stdout, stderr bytes.Buffer
			err := run(context.Background(), cliCommandArguments("node-check", p), &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want substring %q", err, test.want)
			}
		})
	}

	t.Run("missing cookie contents", func(t *testing.T) {
		fake := newCLIFakeCore(t)
		t.Setenv("BITCOIND_COOKIE_FILE", filepath.Join(t.TempDir(), "missing.cookie"))
		p := newCLIPaths(t, false)
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("node-check", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "read Core RPC cookie") {
			t.Fatalf("error = %v", err)
		}
		if fake.callsFor("getblockchaininfo") != 0 {
			t.Fatal("RPC request was sent despite cookie read failure")
		}
	})
}

func TestDiscoverRejectsUnsynchronizedNode(t *testing.T) {
	fake := newCLIFakeCore(t)
	fake.ibd = true
	p := newCLIPaths(t, false)
	writeCLIInputs(t, p, false)
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), cliCommandArguments("discover", p), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "node is not synchronized") {
		t.Fatalf("error = %v", err)
	}
	if !strings.Contains(stdout.String(), "node is still synchronizing") {
		t.Fatalf("synchronization warning missing from %q", stdout.String())
	}
	if _, statErr := os.Stat(filepath.Join(p.cacheDir, "discovery.json")); !os.IsNotExist(statErr) {
		t.Fatalf("discovery file unexpectedly exists: %v", statErr)
	}
}

func TestTracePrerequisitesAndConfigurationErrors(t *testing.T) {
	fake := newCLIFakeCore(t)
	p := newCLIPaths(t, false)
	writeCLIInputs(t, p, false)

	invokeTrace := func() error {
		t.Helper()
		var stdout, stderr bytes.Buffer
		return run(context.Background(), cliCommandArguments("trace", p), &stdout, &stderr)
	}
	if err := invokeTrace(); err == nil || !strings.Contains(err.Error(), "run discover first") {
		t.Fatalf("missing discovery error = %v", err)
	}

	if err := core.SaveDiscovery(filepath.Join(p.cacheDir, "discovery.json"), model.Chain{BestBlock: strings.Repeat("0", 64), BestHeight: 1}, []model.TargetRef{{TxID: firstTransfer}}); err != nil {
		t.Fatal(err)
	}
	if err := invokeTrace(); err == nil || !strings.Contains(err.Error(), "chain tip changed since discovery") {
		t.Fatalf("changed tip error = %v", err)
	}

	if err := core.SaveDiscovery(filepath.Join(p.cacheDir, "discovery.json"), model.Chain{BestBlock: fake.tip.BlockHash, BestHeight: fake.tip.BlockHeight}, []model.TargetRef{{TxID: firstTransfer}}); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TRACE_MAX_DEPTH", "invalid")
	if err := invokeTrace(); err == nil || !strings.Contains(err.Error(), "TRACE_MAX_DEPTH must be a positive integer") {
		t.Fatalf("trace limit error = %v", err)
	}
	t.Setenv("TRACE_MAX_DEPTH", "")
	if err := os.MkdirAll(p.cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.cacheDir, "transactions.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := invokeTrace(); err == nil || !strings.Contains(err.Error(), "decode transaction cache") {
		t.Fatalf("transaction cache error = %v", err)
	}
}

func TestReportAuditAndExportErrorPaths(t *testing.T) {
	t.Run("report needs trace", func(t *testing.T) {
		p := newCLIPaths(t, false)
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("report", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "run trace first") {
			t.Fatalf("error = %v", err)
		}
	})

	t.Run("audit validates before contacting node", func(t *testing.T) {
		t.Setenv("BITCOIND_RPC_URL", "://invalid")
		p := newCLIPaths(t, false)
		var stdout, stderr bytes.Buffer
		err := run(context.Background(), cliCommandArguments("audit", p), &stdout, &stderr)
		if err == nil || !strings.Contains(err.Error(), "owned.json") || strings.Contains(err.Error(), "RPC") {
			t.Fatalf("audit error = %v", err)
		}
	})

	t.Run("direct export requires a report", func(t *testing.T) {
		p := newCLIPaths(t, false)
		var stdout bytes.Buffer
		app := application{stdout: &stdout, stderr: &bytes.Buffer{}}
		err := app.export(p, model.PurchaseFile{SchemaVersion: model.SchemaVersion})
		if err == nil || !strings.Contains(err.Error(), "read report") {
			t.Fatalf("export error = %v", err)
		}
	})
}

func TestNodeCheckRejectsNonMainnet(t *testing.T) {
	fake := newCLIFakeCore(t)
	fake.chain = "signet"
	p := newCLIPaths(t, false)
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), cliCommandArguments("node-check", p), &stdout, &stderr)
	if err == nil || !strings.Contains(err.Error(), "expected main") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(stdout.String(), "\"chain\"") {
		t.Fatalf("failed node check printed successful JSON: %q", stdout.String())
	}
}
