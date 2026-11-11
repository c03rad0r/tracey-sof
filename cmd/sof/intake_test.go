package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracey-sof/internal/input"
	"tracey-sof/internal/intake"
)

func fixtureOutputAddress(t *testing.T) string {
	t.Helper()
	return "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"
}

func writeIntakeReceipt(t *testing.T, p paths, address string) {
	t.Helper()
	if err := os.MkdirAll(p.inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	contents := "ATM receipt\nreceived_at: " + address + "\ntxid: " + firstTransfer + "\noutpoint: " + firstTransfer + ":0\n"
	if err := os.WriteFile(filepath.Join(p.inbox, "atm-receipt.txt"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runIntakeCommand(t *testing.T, p paths, command string, extra ...string) (string, error) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	err := run(context.Background(), cliCommandArguments(command, p, extra...), &stdout, &stderr)
	return stdout.String(), err
}

func TestPurchaseIntakeWorkflow(t *testing.T) {
	fake := newCLIFakeCore(t)
	p := newCLIPaths(t, false)
	writeCLIInputs(t, p, false)
	saveCLIFixtureTrace(t, p)
	address := fixtureOutputAddress(t)
	writeIntakeReceipt(t, p, address)

	output, err := runIntakeCommand(t, p, "intake")
	if err != nil || !strings.Contains(output, "scanned") {
		t.Fatalf("intake = %q, %v", output, err)
	}
	staged, err := intake.Load(filepath.Join(p.dataDir, "purchase-intake.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(staged.Sources) != 1 || len(staged.Sources[0].Candidates) != 3 {
		t.Fatalf("staged = %+v", staged)
	}
	var addressCandidate, txidCandidate, outpointCandidate string
	for _, candidate := range staged.Sources[0].Candidates {
		switch candidate.Kind {
		case intake.KindAddress:
			addressCandidate = candidate.ID
		case intake.KindTxID:
			txidCandidate = candidate.ID
		case intake.KindOutpoint:
			outpointCandidate = candidate.ID
		}
	}
	if addressCandidate == "" || txidCandidate == "" || outpointCandidate == "" {
		t.Fatalf("candidate kinds = %+v", staged.Sources[0].Candidates)
	}

	output, err = runIntakeCommand(t, p, "resolve-intake")
	if err != nil || !strings.Contains(output, "resolved") {
		t.Fatalf("resolve = %q, %v", output, err)
	}
	staged, err = intake.Load(filepath.Join(p.dataDir, "purchase-intake.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, candidate := range staged.Sources[0].Candidates {
		if candidate.Resolution == nil || !candidate.Resolution.Valid {
			t.Fatalf("candidate not resolved: %+v", candidate)
		}
		if candidate.Kind == intake.KindAddress && len(candidate.Resolution.CurrentUTXOs) != 1 {
			t.Fatalf("address resolution = %+v", candidate.Resolution)
		}
		if candidate.Kind == intake.KindTxID && (!candidate.Resolution.TxIDExists || candidate.Resolution.BlockHeight == 0) {
			t.Fatalf("txid resolution = %+v", candidate.Resolution)
		}
		if candidate.Kind == intake.KindOutpoint && !candidate.Resolution.OutpointExists {
			t.Fatalf("outpoint resolution = %+v", candidate.Resolution)
		}
	}

	output, err = runIntakeCommand(t, p, "review-intake", "-list")
	if err != nil || !strings.Contains(output, addressCandidate) || !strings.Contains(output, "pending") {
		t.Fatalf("list = %q, %v", output, err)
	}
	output, err = runIntakeCommand(t, p, "review-intake", "-accept", addressCandidate, "-role", intake.RoleReceivedAt,
		"-purchase-id", "atm-cli-001", "-date", "2024-01-31T14:05:00Z", "-provider", "ATM operator")
	if err != nil || !strings.Contains(output, "1 accepted") {
		t.Fatalf("accept = %q, %v", output, err)
	}

	output, err = runIntakeCommand(t, p, "promote-intake")
	if err != nil || !strings.Contains(output, "promoted") {
		t.Fatalf("promote = %q, %v", output, err)
	}
	purchases, err := input.LoadPurchases(filepath.Join(p.dataDir, "purchases.json"), true)
	if err != nil {
		t.Fatal(err)
	}
	var promoted = false
	for _, purchase := range purchases.Purchases {
		if purchase.ID == "atm-cli-001" {
			promoted = true
			if len(purchase.Addresses) != 1 || purchase.Addresses[0] != address || len(purchase.Artifacts) != 1 {
				t.Fatalf("promoted purchase = %+v", purchase)
			}
		}
	}
	if !promoted {
		t.Fatalf("promoted purchase missing: %+v", purchases.Purchases)
	}
	if _, err := runIntakeCommand(t, p, "validate"); err != nil {
		t.Fatalf("validate after promotion: %v", err)
	}
	if fake.callsFor("scantxoutset") == 0 {
		t.Fatal("resolve did not query current UTXOs")
	}
}

func TestPurchaseIntakeRejectAndDuplicate(t *testing.T) {
	newCLIFakeCore(t)
	p := newCLIPaths(t, false)
	writeCLIInputs(t, p, false)
	writeIntakeReceipt(t, p, fixtureOutputAddress(t))
	if _, err := runIntakeCommand(t, p, "intake"); err != nil {
		t.Fatal(err)
	}
	staged, err := intake.Load(filepath.Join(p.dataDir, "purchase-intake.json"))
	if err != nil {
		t.Fatal(err)
	}
	candidate := staged.Sources[0].Candidates[0]
	if _, err := runIntakeCommand(t, p, "review-intake", "-reject", candidate.ID, "-note", "operator deposit address"); err != nil {
		t.Fatal(err)
	}
	if _, err := runIntakeCommand(t, p, "promote-intake"); err == nil {
		t.Fatal("rejected-only intake was promoted")
	}
	if _, err := runIntakeCommand(t, p, "review-intake", "-pending", candidate.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := runIntakeCommand(t, p, "review-intake", "-accept", candidate.ID, "-role", intake.RolePaidTo); err == nil {
		t.Fatal("paid-to candidate was accepted")
	}
	if _, err := runIntakeCommand(t, p, "review-intake", "-accept", candidate.ID, "-role", intake.RoleReceivedAt, "-purchase-id", "fixture-purchase"); err != nil {
		t.Fatal(err)
	}
	if _, err := runIntakeCommand(t, p, "promote-intake"); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate purchase ID = %v", err)
	}
	if _, err := runIntakeCommand(t, p, "promote-intake", "-replace"); err != nil {
		t.Fatalf("replace promotion = %v", err)
	}
}

func TestPurchaseIntakeErrorPaths(t *testing.T) {
	newCLIFakeCore(t)
	p := newCLIPaths(t, false)
	if err := os.MkdirAll(p.inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := runIntakeCommand(t, p, "intake"); err == nil || !strings.Contains(err.Error(), "no supported source files") {
		t.Fatalf("empty inbox = %v", err)
	}
	if _, err := runIntakeCommand(t, p, "resolve-intake"); err == nil || !strings.Contains(err.Error(), "run intake first") {
		t.Fatalf("missing staging resolve = %v", err)
	}
	if _, err := runIntakeCommand(t, p, "review-intake", "-list"); err == nil || !strings.Contains(err.Error(), "run intake first") {
		t.Fatalf("missing staging review = %v", err)
	}
	if _, err := runIntakeCommand(t, p, "promote-intake"); err == nil || !strings.Contains(err.Error(), "run intake first") {
		t.Fatalf("missing staging promote = %v", err)
	}
	writeIntakeReceipt(t, p, fixtureOutputAddress(t))
	if _, err := runIntakeCommand(t, p, "intake"); err != nil {
		t.Fatal(err)
	}
	if _, err := runIntakeCommand(t, p, "review-intake"); err == nil || !strings.Contains(err.Error(), "no review action") {
		t.Fatalf("no review action = %v", err)
	}
	if err := os.Mkdir(p.cacheDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p.cacheDir, "trace.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runIntakeCommand(t, p, "resolve-intake"); err == nil {
		t.Fatal("malformed trace cache accepted")
	}
	if err := os.Remove(filepath.Join(p.cacheDir, "trace.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(p.cacheDir, "trace.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := runIntakeCommand(t, p, "resolve-intake"); err == nil {
		t.Fatal("trace cache stat failure accepted")
	}
}
