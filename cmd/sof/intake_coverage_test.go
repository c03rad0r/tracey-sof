package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracey-sof/internal/intake"
)

func TestPurchaseIntakeCommandFailureBranches(t *testing.T) {
	t.Run("scan error", func(t *testing.T) {
		newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		if err := os.MkdirAll(p.inbox, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p.inbox, "bad.csv"), []byte("address\n\"unterminated\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := runIntakeCommand(t, p, "intake"); err == nil {
			t.Fatal("scan error accepted")
		}
	})
	t.Run("save error", func(t *testing.T) {
		newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeIntakeReceipt(t, p, fixtureOutputAddress(t))
		if err := os.MkdirAll(filepath.Join(p.dataDir, "purchase-intake.json"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := runIntakeCommand(t, p, "intake"); err == nil {
			t.Fatal("intake save error accepted")
		}
	})
	t.Run("rpc configuration", func(t *testing.T) {
		newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeIntakeReceipt(t, p, fixtureOutputAddress(t))
		if _, err := runIntakeCommand(t, p, "intake"); err != nil {
			t.Fatal(err)
		}
		t.Setenv("BITCOIND_RPC_URL", "://invalid")
		if _, err := runIntakeCommand(t, p, "resolve-intake"); err == nil {
			t.Fatal("invalid RPC configuration accepted")
		}
	})
	t.Run("trace stat error", func(t *testing.T) {
		newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeIntakeReceipt(t, p, fixtureOutputAddress(t))
		if _, err := runIntakeCommand(t, p, "intake"); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Dir(p.cacheDir), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p.cacheDir, []byte("not a directory"), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := runIntakeCommand(t, p, "resolve-intake"); err == nil || !strings.Contains(err.Error(), "stat trace cache") {
			t.Fatalf("trace stat error = %v", err)
		}
	})
	t.Run("resolve error", func(t *testing.T) {
		fake := newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeIntakeReceipt(t, p, fixtureOutputAddress(t))
		if _, err := runIntakeCommand(t, p, "intake"); err != nil {
			t.Fatal(err)
		}
		fake.beforeRPC = func(method string, _ int) error {
			if method == "scantxoutset" {
				return errInjected
			}
			return nil
		}
		if _, err := runIntakeCommand(t, p, "resolve-intake"); err == nil {
			t.Fatal("resolution failure accepted")
		}
	})
	t.Run("resolve save error", func(t *testing.T) {
		fake := newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeIntakeReceipt(t, p, fixtureOutputAddress(t))
		if _, err := runIntakeCommand(t, p, "intake"); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p.dataDir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(p.dataDir, 0o700) })
		if _, err := runIntakeCommand(t, p, "resolve-intake"); err == nil {
			t.Fatal("resolve save failure accepted")
		}
		_ = fake
	})
	t.Run("review save error", func(t *testing.T) {
		newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeIntakeReceipt(t, p, fixtureOutputAddress(t))
		if _, err := runIntakeCommand(t, p, "intake"); err != nil {
			t.Fatal(err)
		}
		staged, err := intake.Load(filepath.Join(p.dataDir, "purchase-intake.json"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p.dataDir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(p.dataDir, 0o700) })
		if _, err := runIntakeCommand(t, p, "review-intake", "-accept", staged.Sources[0].Candidates[0].ID, "-role", intake.RoleReceivedAt); err == nil {
			t.Fatal("review save failure accepted")
		}
	})
	t.Run("promote purchases load error", func(t *testing.T) {
		newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeIntakeReceipt(t, p, fixtureOutputAddress(t))
		if _, err := runIntakeCommand(t, p, "intake"); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(filepath.Join(p.dataDir, "purchases.json"), 0o700); err != nil {
			t.Fatal(err)
		}
		if _, err := runIntakeCommand(t, p, "promote-intake"); err == nil {
			t.Fatal("invalid purchases load accepted")
		}
	})
	t.Run("promote stat error", func(t *testing.T) {
		newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeIntakeReceipt(t, p, fixtureOutputAddress(t))
		if _, err := runIntakeCommand(t, p, "intake"); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("purchases.json", filepath.Join(p.dataDir, "purchases.json")); err != nil {
			t.Fatal(err)
		}
		if _, err := runIntakeCommand(t, p, "promote-intake"); err == nil || !strings.Contains(err.Error(), "stat purchases") {
			t.Fatalf("purchases stat error = %v", err)
		}
	})
	t.Run("promote save error", func(t *testing.T) {
		newCLIFakeCore(t)
		p := newCLIPaths(t, false)
		writeIntakeReceipt(t, p, fixtureOutputAddress(t))
		if _, err := runIntakeCommand(t, p, "intake"); err != nil {
			t.Fatal(err)
		}
		staged, err := intake.Load(filepath.Join(p.dataDir, "purchase-intake.json"))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := runIntakeCommand(t, p, "review-intake", "-accept", staged.Sources[0].Candidates[0].ID, "-role", intake.RoleReceivedAt, "-amount-sats", "1000", "-fiat-amount", "10.00", "-fiat-currency", "EUR"); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p.dataDir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(p.dataDir, 0o700) })
		if _, err := runIntakeCommand(t, p, "promote-intake"); err == nil {
			t.Fatal("promote save failure accepted")
		}
	})
}

var errInjected = injectedError("injected RPC failure")

type injectedError string

func (e injectedError) Error() string { return string(e) }

func TestPurchaseIntakeUsageAndFlagErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), []string{"review-intake", "-amount-sats", "bad"}, &stdout, &stderr)
	if err == nil {
		t.Fatal("invalid amount flag accepted")
	}
}
