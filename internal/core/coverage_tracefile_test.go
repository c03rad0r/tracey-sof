package core

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"tracey-sof/internal/model"
)

func TestCoverageTraceRoundTripAndLoadErrors(t *testing.T) {
	fixture := readFixtureFile(t, firstTransfer)
	tx, err := DecodeTransaction(fixture.Hex)
	if err != nil {
		t.Fatal(err)
	}
	trace := model.Trace{
		SchemaVersion: model.SchemaVersion,
		Network:       "main",
		BestBlock:     fixture.BlockHash,
		BestHeight:    fixture.BlockHeight,
		Targets:       []model.TargetRef{{TxID: tx.TxID, Vout: 0, ValueSats: tx.Outputs[0].ValueSats, OwnerIDs: []string{"fixture"}}},
		Transactions:  map[string]model.Transaction{tx.TxID: tx},
	}
	path := filepath.Join(t.TempDir(), "nested", "trace.json")
	if err := SaveTrace(path, trace); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrace(path)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(loaded, trace) {
		t.Errorf("trace round trip changed data\n got: %+v\nwant: %+v", loaded, trace)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 || data[len(data)-1] != '\n' {
		t.Error("saved trace is not newline terminated")
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("trace permissions = %o, want 600", info.Mode().Perm())
	}

	if _, err := LoadTrace(path + ".missing"); err == nil || !strings.Contains(err.Error(), "read trace") {
		t.Errorf("missing trace error = %v", err)
	}
	if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTrace(path); err == nil || !strings.Contains(err.Error(), "decode trace") {
		t.Errorf("malformed trace error = %v", err)
	}
	unsupported := trace
	unsupported.SchemaVersion++
	if err := SaveTrace(path, unsupported); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadTrace(path); err == nil || !strings.Contains(err.Error(), "unsupported trace schema") {
		t.Errorf("unsupported trace error = %v", err)
	}
}

func TestCoverageSaveTraceDirectoryFailure(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocker, []byte("not a directory"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveTrace(filepath.Join(blocker, "trace.json"), model.Trace{SchemaVersion: model.SchemaVersion}); err == nil {
		t.Error("expected creating a trace below a regular file to fail")
	}
}

func TestCoverageSnapshotAndNetworkMatch(t *testing.T) {
	base := NodeInfo{Version: 300000, Chain: "main", Blocks: 100, Headers: 100, TxIndex: true}
	if got, want := StringSnapshot(base), "Core version 300000, chain=main, height=100/100, synchronized, txindex=true, pruned=false"; got != want {
		t.Errorf("StringSnapshot() = %q, want %q", got, want)
	}
	base.InitialBlockDownload = true
	base.Headers = 101
	base.Pruned = true
	if got := StringSnapshot(base); !strings.Contains(got, "height=100/101, synchronizing") || !strings.Contains(got, "pruned=true") {
		t.Errorf("synchronizing snapshot = %q", got)
	}

	hash := make([]byte, 20)
	mainnet, err := btcutil.NewAddressPubKeyHash(hash, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatal(err)
	}
	testnet, err := btcutil.NewAddressPubKeyHash(hash, &chaincfg.TestNet3Params)
	if err != nil {
		t.Fatal(err)
	}
	if !NetMatches(mainnet.EncodeAddress()) {
		t.Error("valid mainnet address was rejected")
	}
	if NetMatches(testnet.EncodeAddress()) || NetMatches("not-an-address") {
		t.Error("testnet or malformed address matched mainnet")
	}
}
