package main

import (
	"bytes"
	"context"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTraceLateFailureBranches(t *testing.T) {
	t.Run("final chain error", func(t *testing.T) {
		fake := newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureDiscovery(t, p)
		fake.beforeRPC = func(method string, call int) error {
			if method == "getblockchaininfo" && call == 2 {
				return errors.New("final tip failed")
			}
			return nil
		}
		var stdout, stderr bytes.Buffer
		if err := run(context.Background(), cliCommandArguments("trace", p), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "final tip failed") {
			t.Fatalf("final chain error = %v", err)
		}
	})
	t.Run("chain changed", func(t *testing.T) {
		fake := newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureDiscovery(t, p)
		fake.beforeRPC = func(method string, call int) error {
			if method == "getblockchaininfo" && call == 2 {
				fake.tip.BlockHash = strings.Repeat("f", 64)
			}
			return nil
		}
		var stdout, stderr bytes.Buffer
		if err := run(context.Background(), cliCommandArguments("trace", p), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "chain tip changed during tracing") {
			t.Fatalf("chain change error = %v", err)
		}
	})
	t.Run("canceled provenance", func(t *testing.T) {
		fake := newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureDiscovery(t, p)
		ctx, cancel := context.WithCancel(context.Background())
		fake.beforeRPC = func(method string, call int) error {
			if method == "getrawtransaction" && call == 1 {
				cancel()
			}
			return nil
		}
		var stdout, stderr bytes.Buffer
		if err := run(ctx, cliCommandArguments("trace", p), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "context canceled") {
			t.Fatalf("canceled trace error = %v", err)
		}
	})
	t.Run("source cache save", func(t *testing.T) {
		fake := newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureDiscovery(t, p)
		cachePath := filepath.Join(p.cacheDir, "transactions.json")
		fake.beforeRPC = func(method string, call int) error {
			if method == "getrawtransaction" && call == 1 {
				_ = os.MkdirAll(p.cacheDir, 0o700)
				_ = os.Remove(cachePath)
				_ = os.Mkdir(cachePath, 0o700)
			}
			return nil
		}
		var stdout, stderr bytes.Buffer
		if err := run(context.Background(), cliCommandArguments("trace", p), &stdout, &stderr); err == nil {
			t.Fatal("late transaction-cache save failure was ignored")
		}
	})
}

func TestDiscoverLateCacheSaveFailure(t *testing.T) {
	fake := newCLIFakeCore(t)
	p := newCLIPaths(t, false)
	writeCLIInputs(t, p, false)
	cachePath := filepath.Join(p.cacheDir, "transactions.json")
	fake.beforeRPC = func(method string, call int) error {
		if method == "getrawtransaction" && call == 1 {
			_ = os.MkdirAll(p.cacheDir, 0o700)
			_ = os.Remove(cachePath)
			_ = os.Mkdir(cachePath, 0o700)
		}
		return nil
	}
	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), cliCommandArguments("discover", p), &stdout, &stderr); err == nil {
		t.Fatal("late transaction-cache save failure was ignored")
	}
}

func TestUIErrorBranches(t *testing.T) {
	t.Run("report prerequisite", func(t *testing.T) {
		p := newCLIPaths(t, false)
		app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		if err := app.ui(context.Background(), p); err == nil || !strings.Contains(err.Error(), "run trace first") {
			t.Fatalf("UI report error = %v", err)
		}
	})
	t.Run("non-loopback address", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		t.Setenv("SOF_LISTEN", "0.0.0.0:8765")
		app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		if err := app.ui(context.Background(), p); err == nil || !strings.Contains(err.Error(), "loopback") {
			t.Fatalf("non-loopback UI error = %v", err)
		}
	})
	t.Run("listen failure", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		t.Setenv("SOF_LISTEN", listener.Addr().String())
		app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		if err := app.ui(context.Background(), p); err == nil {
			t.Fatal("occupied UI address was accepted")
		}
	})
	t.Run("rebuild failure", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		saveCLIFixtureTrace(t, p)
		app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
		rebuild := app.reportRebuilder(p)
		if _, err := rebuild(); err != nil {
			t.Fatalf("initial rebuild: %v", err)
		}
		if err := os.WriteFile(filepath.Join(p.dataDir, "trace-policy.json"), []byte(`{"schema_version":1,"max_hops":8}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := rebuild(); err == nil || !strings.Contains(err.Error(), "trace policy changed") {
			t.Fatalf("rebuild stale policy = %v", err)
		}
	})
}

func TestBuildReportPolicyLoadFailure(t *testing.T) {
	p := newCLIPaths(t, false)
	writeCLIInputs(t, p, false)
	saveCLIFixtureTrace(t, p)
	policyPath := filepath.Join(p.dataDir, "trace-policy.json")
	if err := os.Mkdir(policyPath, 0o700); err != nil {
		t.Fatal(err)
	}
	app := application{stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	if _, err := app.buildReport(p); err == nil {
		t.Fatal("unreadable policy was accepted")
	}
}

func TestRunAuditAndExportPropagation(t *testing.T) {
	t.Run("audit discovery error", func(t *testing.T) {
		fake := newCLIFakeCore(t)
		fake.chain = "test"
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		var stdout, stderr bytes.Buffer
		if err := run(context.Background(), cliCommandArguments("audit", p), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "expected main") {
			t.Fatalf("audit discovery error = %v", err)
		}
	})
	t.Run("export report error", func(t *testing.T) {
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		var stdout, stderr bytes.Buffer
		if err := run(context.Background(), cliCommandArguments("export", p), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "run trace first") {
			t.Fatalf("export report error = %v", err)
		}
	})
	t.Run("export requires inputs", func(t *testing.T) {
		p := newCLIPaths(t, false)
		var stdout, stderr bytes.Buffer
		if err := run(context.Background(), cliCommandArguments("export", p), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "owned.json") {
			t.Fatalf("export input error = %v", err)
		}
	})
	t.Run("audit trace error", func(t *testing.T) {
		fake := newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeCLIInputs(t, p, false)
		fake.beforeRPC = func(method string, call int) error {
			if method == "getblockchaininfo" && call == 4 {
				return errors.New("audit trace failure")
			}
			return nil
		}
		var stdout, stderr bytes.Buffer
		if err := run(context.Background(), cliCommandArguments("audit", p), &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "audit trace failure") {
			t.Fatalf("audit trace error = %v", err)
		}
	})
}
