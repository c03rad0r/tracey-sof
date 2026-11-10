package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

func writeSourceCache(t *testing.T, path string, cache diskCache) {
	t.Helper()
	data, err := json.Marshal(cache)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestCoverageSourceCacheRoundTripPreservesMetadata(t *testing.T) {
	fixture := readFixtureFile(t, firstTransfer)
	tx, err := DecodeTransaction(fixture.Hex)
	if err != nil {
		t.Fatal(err)
	}
	tx.BlockHash = fixture.BlockHash
	tx.BlockHeight = fixture.BlockHeight
	tx.BlockTime = fixture.BlockTime
	tx.Confirmations = 42
	tx.InActiveChain = true
	tx.Inputs[0].PrevHeight = 9

	path := filepath.Join(t.TempDir(), "cache", "transactions.json")
	saved := NewSource(nil, path)
	saved.transactions[tx.TxID] = tx
	if err := saved.Save(); err != nil {
		t.Fatal(err)
	}
	loaded := NewSource(nil, path) // A nil client proves the cached transaction is used.
	if err := loaded.Load(); err != nil {
		t.Fatal(err)
	}
	got, err := loaded.Transaction(context.Background(), tx.TxID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, tx) {
		t.Errorf("cached transaction changed\n got: %+v\nwant: %+v", got, tx)
	}
	if loaded.blockHints[parentCoinbase] != 9 {
		t.Errorf("previous-output block hint was not restored: %v", loaded.blockHints)
	}
}

func TestCoverageSourceLoadRejectsBadCaches(t *testing.T) {
	fixture := readFixtureFile(t, firstTransfer)
	tx, err := DecodeTransaction(fixture.Hex)
	if err != nil {
		t.Fatal(err)
	}
	valid := func() diskCache {
		return diskCache{SchemaVersion: model.SchemaVersion, Transactions: map[string]model.Transaction{
			tx.TxID: {TxID: tx.TxID, Hex: tx.Hex, Inputs: tx.Inputs},
		}}
	}
	tests := []struct {
		name  string
		edit  func(*diskCache)
		match string
	}{
		{"unsupported schema", func(c *diskCache) { c.SchemaVersion++ }, "unsupported transaction cache schema"},
		{"invalid raw transaction", func(c *diskCache) {
			stored := c.Transactions[tx.TxID]
			stored.Hex = "not-hex"
			c.Transactions[tx.TxID] = stored
		}, "cached transaction " + tx.TxID},
		{"map key identity mismatch", func(c *diskCache) {
			c.Transactions[strings.Repeat("0", 64)] = c.Transactions[tx.TxID]
			delete(c.Transactions, tx.TxID)
		}, "inconsistent identity"},
		{"stored txid mismatch", func(c *diskCache) {
			stored := c.Transactions[tx.TxID]
			stored.TxID = strings.Repeat("0", 64)
			c.Transactions[tx.TxID] = stored
		}, "inconsistent identity"},
		{"stored input count mismatch", func(c *diskCache) {
			stored := c.Transactions[tx.TxID]
			stored.Inputs = nil
			c.Transactions[tx.TxID] = stored
		}, "inconsistent identity"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cache := valid()
			test.edit(&cache)
			path := filepath.Join(t.TempDir(), "transactions.json")
			writeSourceCache(t, path, cache)
			if err := NewSource(nil, path).Load(); err == nil || !strings.Contains(err.Error(), test.match) {
				t.Errorf("Load() error = %v, want substring %q", err, test.match)
			}
		})
	}

	t.Run("malformed JSON", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "transactions.json")
		if err := os.WriteFile(path, []byte("not-json"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := NewSource(nil, path).Load(); err == nil || !strings.Contains(err.Error(), "decode transaction cache") {
			t.Errorf("Load() error = %v", err)
		}
	})
	t.Run("read failure", func(t *testing.T) {
		if err := NewSource(nil, t.TempDir()).Load(); err == nil || !strings.Contains(err.Error(), "read transaction cache") {
			t.Errorf("Load() error = %v", err)
		}
	})
	t.Run("missing cache", func(t *testing.T) {
		if err := NewSource(nil, filepath.Join(t.TempDir(), "missing.json")).Load(); err != nil {
			t.Errorf("missing cache must be accepted: %v", err)
		}
	})
}

func TestCoverageSourceSaveFilesystemErrors(t *testing.T) {
	t.Run("create parent directory", func(t *testing.T) {
		blocker := filepath.Join(t.TempDir(), "file")
		if err := os.WriteFile(blocker, []byte("file"), 0o600); err != nil {
			t.Fatal(err)
		}
		source := NewSource(nil, filepath.Join(blocker, "transactions.json"))
		if err := source.Save(); err == nil || !strings.Contains(err.Error(), "create cache directory") {
			t.Errorf("Save() error = %v", err)
		}
	})
	t.Run("replace cache", func(t *testing.T) {
		destination := filepath.Join(t.TempDir(), "transactions.json")
		if err := os.Mkdir(destination, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := NewSource(nil, destination).Save(); err == nil || !strings.Contains(err.Error(), "replace transaction cache") {
			t.Errorf("Save() error = %v", err)
		}
	})
}

func TestCoverageSourceFetchFailures(t *testing.T) {
	fixture := readFixtureFile(t, firstTransfer)
	parentFixture := readFixtureFile(t, parentCoinbase)
	tx, err := DecodeTransaction(fixture.Hex)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		blockHash string
		height    int64
		rpcErr    *RPCError
		edit      func(map[string]any)
		match     string
		code      int64
	}{
		{name: "unknown parent without txindex", rpcErr: &RPCError{Code: -5, Message: "not found"}, match: "unavailable without txindex", code: -5},
		{name: "fetch RPC failure", blockHash: fixture.BlockHash, height: fixture.BlockHeight, rpcErr: &RPCError{Code: -8, Message: "block unavailable"}, match: "block unavailable", code: -8},
		{name: "invalid raw hex", blockHash: fixture.BlockHash, height: fixture.BlockHeight, edit: func(raw map[string]any) {
			raw["hex"] = "bad-hex"
		}, match: "invalid transaction hex"},
		{name: "decoded txid mismatch", blockHash: fixture.BlockHash, height: fixture.BlockHeight, edit: func(raw map[string]any) {
			raw["hex"] = parentFixture.Hex
		}, match: "Core returned mismatched txid"},
		{name: "Core txid mismatch", blockHash: fixture.BlockHash, height: fixture.BlockHeight, edit: func(raw map[string]any) {
			raw["txid"] = strings.Repeat("0", 64)
		}, match: "Core returned mismatched txid"},
		{name: "wrong active block", blockHash: strings.Repeat("1", 64), height: fixture.BlockHeight, match: "not in active block"},
		{name: "missing vin metadata", blockHash: fixture.BlockHash, height: fixture.BlockHeight, edit: func(raw map[string]any) {
			raw["vin"] = []any{}
		}, match: "inconsistent Core input metadata"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
				if request.Method != "getrawtransaction" {
					return nil, &RPCError{Code: -32601, Message: "unexpected " + request.Method}
				}
				if test.rpcErr != nil {
					return nil, test.rpcErr
				}
				raw := coverageRawResult(tx, fixture)
				if test.edit != nil {
					test.edit(raw)
				}
				return raw, nil
			})
			defer server.Close()
			source := NewSource(client, "")
			err := source.Prefetch(context.Background(), tx.TxID, test.blockHash, test.height)
			if err == nil || !strings.Contains(err.Error(), test.match) {
				t.Fatalf("Prefetch() error = %v, want substring %q", err, test.match)
			}
			if test.code != 0 && !IsRPCCode(err, test.code) {
				t.Errorf("RPC code was not retained: %v", err)
			}
		})
	}
}

func TestCoverageSourceBlockHintFailure(t *testing.T) {
	fixture := readFixtureFile(t, firstTransfer)
	client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
		if request.Method != "getblockhash" || !reflect.DeepEqual(request.Params, []any{float64(170)}) {
			t.Errorf("unexpected request: %+v", request)
		}
		return nil, &RPCError{Code: -8, Message: "height unavailable"}
	})
	defer server.Close()
	source := NewSource(client, "")
	source.blockHints[fixture.TxID] = 170
	_, err := source.Transaction(context.Background(), fixture.TxID)
	if err == nil || !strings.Contains(err.Error(), "find active block for "+fixture.TxID) || !IsRPCCode(err, -8) {
		t.Errorf("Transaction() error = %v", err)
	}
}
