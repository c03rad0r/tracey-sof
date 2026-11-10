package core

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"tracey-sof/internal/input"
	"tracey-sof/internal/model"
)

func TestCoveragePrepareScansFailures(t *testing.T) {
	validAddress := readFixtureFile(t, segwitMultiInput)
	tx, err := DecodeTransaction(validAddress.Hex)
	if err != nil {
		t.Fatal(err)
	}
	derived := tx.Outputs[0].Address
	base := func(request rpcRequest) (any, *RPCError) {
		switch request.Method {
		case "getdescriptorinfo":
			return map[string]any{"descriptor": request.Params[0].(string) + "#checksum"}, nil
		case "deriveaddresses":
			return []string{derived}, nil
		default:
			return nil, &RPCError{Code: -32601, Message: "unexpected"}
		}
	}
	cases := []struct {
		name   string
		object input.ScanObject
		edit   func(func(rpcRequest) (any, *RPCError)) func(rpcRequest) (any, *RPCError)
	}{
		{"descriptor info", input.ScanObject{ID: "x", Descriptor: "addr(1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa)"}, func(base func(rpcRequest) (any, *RPCError)) func(rpcRequest) (any, *RPCError) {
			return func(r rpcRequest) (any, *RPCError) {
				if r.Method == "getdescriptorinfo" {
					return nil, &RPCError{Code: -1, Message: "descriptor failure"}
				}
				return base(r)
			}
		}},
		{"derive", input.ScanObject{ID: "x", Descriptor: "wpkh(xpub/*)", Range: &[2]uint32{0, 0}}, func(base func(rpcRequest) (any, *RPCError)) func(rpcRequest) (any, *RPCError) {
			return func(r rpcRequest) (any, *RPCError) {
				if r.Method == "deriveaddresses" {
					return nil, &RPCError{Code: -1, Message: "derive failure"}
				}
				return base(r)
			}
		}},
		{"derived count", input.ScanObject{ID: "x", Descriptor: "wpkh(xpub/*)", Range: &[2]uint32{0, 1}}, nil},
		{"derived invalid address", input.ScanObject{ID: "x", Descriptor: "wpkh(xpub/*)", Range: &[2]uint32{0, 0}}, func(base func(rpcRequest) (any, *RPCError)) func(rpcRequest) (any, *RPCError) {
			return func(r rpcRequest) (any, *RPCError) {
				if r.Method == "deriveaddresses" {
					return []string{"not-an-address"}, nil
				}
				return base(r)
			}
		}},
		{"fixed invalid address", input.ScanObject{ID: "x", Descriptor: "addr(not-an-address)"}, nil},
		{"invalid raw script", input.ScanObject{ID: "x", Descriptor: "raw(not-hex)"}, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			handler := base
			if tc.edit != nil {
				handler = tc.edit(base)
			}
			client, server := rpcTestClient(t, handler)
			defer server.Close()
			if _, _, _, err := client.prepareScans(context.Background(), []input.ScanObject{tc.object}); err == nil {
				t.Fatal("invalid scan preparation was accepted")
			}
		})
	}
}

type coverageDiscoveryOverride func(method string, params []any, tipCall int) (any, *RPCError, bool)

func runCoverageDiscovery(t *testing.T, owned model.Owned, override coverageDiscoveryOverride) ([]model.TargetRef, model.Chain, error) {
	t.Helper()
	fixture := readFixtureFile(t, segwitMultiInput)
	transaction, err := DecodeTransaction(fixture.Hex)
	if err != nil {
		t.Fatal(err)
	}
	tip := model.Chain{BestBlock: fixture.BlockHash, BestHeight: fixture.BlockHeight}
	tipCalls := 0
	client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
		if request.Method == "getblockchaininfo" {
			tipCalls++
		}
		if override != nil {
			if result, rpcErr, handled := override(request.Method, request.Params, tipCalls); handled {
				return result, rpcErr
			}
		}
		switch request.Method {
		case "getblockchaininfo":
			return map[string]any{"chain": "main", "blocks": tip.BestHeight, "bestblockhash": tip.BestBlock}, nil
		case "getdescriptorinfo":
			return map[string]any{"descriptor": request.Params[0].(string) + "#checksum"}, nil
		case "scantxoutset":
			return map[string]any{"success": true, "height": tip.BestHeight, "bestblock": tip.BestBlock, "unspents": []any{
				map[string]any{"txid": transaction.TxID, "vout": 0, "scriptPubKey": transaction.Outputs[0].ScriptPubKey,
					"desc": "raw(" + transaction.Outputs[0].ScriptPubKey + ")#checksum", "amount": json.Number(SatString(transaction.Outputs[0].ValueSats)),
					"height": fixture.BlockHeight, "blockhash": fixture.BlockHash},
			}}, nil
		case "getrawtransaction":
			return coverageRawResult(transaction, fixture), nil
		case "gettxout":
			return map[string]any{"bestblock": tip.BestBlock, "confirmations": 1, "value": json.Number(SatString(transaction.Outputs[0].ValueSats)),
				"scriptPubKey": map[string]any{"address": transaction.Outputs[0].Address}}, nil
		case "getblockheader":
			return map[string]any{"height": tip.BestHeight}, nil
		case "getblockhash":
			return fixture.BlockHash, nil
		default:
			return nil, &RPCError{Code: -32601, Message: "unsupported " + request.Method}
		}
	})
	defer server.Close()
	source := NewSource(client, "")
	return client.Discover(context.Background(), owned, source)
}

func coverageOwnedDescriptor() model.Owned {
	return model.Owned{SchemaVersion: 1, Network: "mainnet", Descriptors: []model.OwnedDescriptor{{ID: "raw", Descriptor: "raw(0014aabbccddeeff00112233445566778899aabbccdd)"}}}
}

func TestCoverageDiscoveryFailureStages(t *testing.T) {
	t.Run("chain tip initial", func(t *testing.T) {
		_, _, err := runCoverageDiscovery(t, coverageOwnedDescriptor(), func(method string, _ []any, _ int) (any, *RPCError, bool) {
			if method == "getblockchaininfo" {
				return nil, &RPCError{Code: -1, Message: "chain failure"}, true
			}
			return nil, nil, false
		})
		if err == nil {
			t.Fatal("initial chain failure accepted")
		}
	})
	t.Run("invalid scan object", func(t *testing.T) {
		owned := coverageOwnedDescriptor()
		owned.Descriptors[0].Descriptor = "wpkh(xpub)"
		owned.Xpubs = []model.OwnedXpub{{ID: "bad", Xpub: "x", ScriptType: "bogus", Branches: []uint32{0}}}
		if _, _, err := runCoverageDiscovery(t, owned, nil); err == nil {
			t.Fatal("invalid scan object accepted")
		}
	})
	t.Run("prepare scan", func(t *testing.T) {
		_, _, err := runCoverageDiscovery(t, coverageOwnedDescriptor(), func(method string, _ []any, _ int) (any, *RPCError, bool) {
			if method == "getdescriptorinfo" {
				return nil, &RPCError{Code: -1, Message: "descriptor failure"}, true
			}
			return nil, nil, false
		})
		if err == nil {
			t.Fatal("descriptor failure accepted")
		}
	})
	t.Run("scan rpc", func(t *testing.T) {
		_, _, err := runCoverageDiscovery(t, coverageOwnedDescriptor(), func(method string, _ []any, _ int) (any, *RPCError, bool) {
			if method == "scantxoutset" {
				return nil, &RPCError{Code: -1, Message: "scan failure"}, true
			}
			return nil, nil, false
		})
		if err == nil {
			t.Fatal("scan failure accepted")
		}
	})
	t.Run("scan amount", func(t *testing.T) {
		_, _, err := runCoverageDiscovery(t, coverageOwnedDescriptor(), func(method string, _ []any, _ int) (any, *RPCError, bool) {
			if method == "scantxoutset" {
				return map[string]any{"success": true, "height": readFixtureFile(t, segwitMultiInput).BlockHeight, "bestblock": readFixtureFile(t, segwitMultiInput).BlockHash, "unspents": []any{map[string]any{"txid": strings.Repeat("a", 64), "vout": 0, "amount": json.Number("0.000000001")}}}, nil, true
			}
			return nil, nil, false
		})
		if err == nil {
			t.Fatal("invalid scan amount accepted")
		}
	})
	t.Run("scan script", func(t *testing.T) {
		_, _, err := runCoverageDiscovery(t, coverageOwnedDescriptor(), func(method string, _ []any, _ int) (any, *RPCError, bool) {
			if method == "scantxoutset" {
				fixture := readFixtureFile(t, segwitMultiInput)
				return map[string]any{"success": true, "height": fixture.BlockHeight, "bestblock": fixture.BlockHash, "unspents": []any{map[string]any{"txid": strings.Repeat("a", 64), "vout": 0, "scriptPubKey": "not-hex", "amount": json.Number("1")}}}, nil, true
			}
			return nil, nil, false
		})
		if err == nil {
			t.Fatal("invalid scan script accepted")
		}
	})
	t.Run("scan prefetch", func(t *testing.T) {
		_, _, err := runCoverageDiscovery(t, coverageOwnedDescriptor(), func(method string, _ []any, _ int) (any, *RPCError, bool) {
			if method == "getrawtransaction" {
				return nil, &RPCError{Code: -5, Message: "missing"}, true
			}
			return nil, nil, false
		})
		if err == nil {
			t.Fatal("scan prefetch failure accepted")
		}
	})
	t.Run("explicit gettxout rpc", func(t *testing.T) {
		owned := model.Owned{SchemaVersion: 1, Network: "mainnet", UTXOs: []model.OwnedUTXO{{TxID: segwitMultiInput, Vout: 0}}}
		_, _, err := runCoverageDiscovery(t, owned, func(method string, _ []any, _ int) (any, *RPCError, bool) {
			if method == "gettxout" {
				return nil, &RPCError{Code: -1, Message: "txout failure"}, true
			}
			return nil, nil, false
		})
		if err == nil {
			t.Fatal("gettxout failure accepted")
		}
	})
	t.Run("explicit spent", func(t *testing.T) {
		owned := model.Owned{SchemaVersion: 1, Network: "mainnet", UTXOs: []model.OwnedUTXO{{TxID: segwitMultiInput, Vout: 0}}}
		_, _, err := runCoverageDiscovery(t, owned, func(method string, _ []any, _ int) (any, *RPCError, bool) {
			if method == "gettxout" {
				return nil, nil, true
			}
			return nil, nil, false
		})
		if err == nil {
			t.Fatal("spent explicit outpoint accepted")
		}
	})
	t.Run("explicit amount", func(t *testing.T) {
		owned := model.Owned{SchemaVersion: 1, Network: "mainnet", UTXOs: []model.OwnedUTXO{{TxID: segwitMultiInput, Vout: 0}}}
		_, _, err := runCoverageDiscovery(t, owned, func(method string, _ []any, _ int) (any, *RPCError, bool) {
			if method == "gettxout" {
				return map[string]any{"bestblock": readFixtureFile(t, segwitMultiInput).BlockHash, "confirmations": 1, "value": json.Number("0.000000001")}, nil, true
			}
			return nil, nil, false
		})
		if err == nil {
			t.Fatal("invalid explicit amount accepted")
		}
	})
	t.Run("explicit blockheight", func(t *testing.T) {
		owned := model.Owned{SchemaVersion: 1, Network: "mainnet", UTXOs: []model.OwnedUTXO{{TxID: segwitMultiInput, Vout: 0}}}
		_, _, err := runCoverageDiscovery(t, owned, func(method string, _ []any, _ int) (any, *RPCError, bool) {
			if method == "getblockheader" {
				return nil, &RPCError{Code: -1, Message: "header failure"}, true
			}
			return nil, nil, false
		})
		if err == nil {
			t.Fatal("block height failure accepted")
		}
	})
	t.Run("explicit blockhash", func(t *testing.T) {
		owned := model.Owned{SchemaVersion: 1, Network: "mainnet", UTXOs: []model.OwnedUTXO{{TxID: segwitMultiInput, Vout: 0}}}
		_, _, err := runCoverageDiscovery(t, owned, func(method string, _ []any, _ int) (any, *RPCError, bool) {
			if method == "getblockhash" {
				return nil, &RPCError{Code: -1, Message: "hash failure"}, true
			}
			return nil, nil, false
		})
		if err == nil {
			t.Fatal("block hash failure accepted")
		}
	})
	t.Run("explicit prefetch", func(t *testing.T) {
		owned := model.Owned{SchemaVersion: 1, Network: "mainnet", UTXOs: []model.OwnedUTXO{{TxID: segwitMultiInput, Vout: 0}}}
		_, _, err := runCoverageDiscovery(t, owned, func(method string, _ []any, _ int) (any, *RPCError, bool) {
			if method == "getrawtransaction" {
				return nil, &RPCError{Code: -5, Message: "missing"}, true
			}
			return nil, nil, false
		})
		if err == nil {
			t.Fatal("explicit prefetch failure accepted")
		}
	})
	t.Run("final chain tip", func(t *testing.T) {
		_, _, err := runCoverageDiscovery(t, coverageOwnedDescriptor(), func(method string, _ []any, tipCall int) (any, *RPCError, bool) {
			if method == "getblockchaininfo" && tipCall == 2 {
				return nil, &RPCError{Code: -1, Message: "final chain failure"}, true
			}
			return nil, nil, false
		})
		if err == nil {
			t.Fatal("final chain failure accepted")
		}
	})
}

func TestCoverageDiscoveryHelperBranches(t *testing.T) {
	targets := map[string]*model.TargetRef{}
	first := model.TargetRef{TxID: segwitMultiInput, Vout: 0}
	addTarget(targets, first)
	second := first
	second.Address = "filled"
	addTarget(targets, second)
	if targets[first.Key()].Address != "filled" {
		t.Fatal("empty target address was not merged")
	}
	if cleanDescriptor("no-checksum") != "no-checksum" || cleanDescriptor("x#sum") != "x" {
		t.Fatal("descriptor cleanup failed")
	}
	if _, err := addressScriptWith("not-an-address", nil); err == nil {
		t.Fatal("invalid address accepted")
	}
	if address, _ := addressScript("1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"); !strings.HasPrefix(address, "76a914") {
		t.Fatalf("unexpected address script %q", address)
	}
	_, err := addressScriptWith("1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa", func(btcutil.Address) ([]byte, error) { return nil, errors.New("script failure") })
	if err == nil {
		t.Fatal("script builder failure accepted")
	}

	owned := coverageOwnedDescriptor()
	owned.Descriptors = append(owned.Descriptors, model.OwnedDescriptor{ID: "second", Descriptor: "raw(0014ffffffffffffffffffffffffffffffffffffffff)"})
	targetsFound, _, err := runCoverageDiscovery(t, owned, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(targetsFound) != 1 || len(targetsFound[0].OwnerIDs) != 1 || targetsFound[0].OwnerIDs[0] != "unattributed-scan-output" {
		t.Fatalf("unmatched scan output ownership = %+v", targetsFound)
	}
}
