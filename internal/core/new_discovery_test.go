package core

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"tracey-sof/internal/input"
	"tracey-sof/internal/model"
)

func coverageRawResult(tx model.Transaction, fixture fixtureFile) map[string]any {
	vin := make([]any, len(tx.Inputs))
	for i := range vin {
		vin[i] = map[string]any{}
	}
	return map[string]any{
		"txid": tx.TxID, "hex": fixture.Hex, "blockhash": fixture.BlockHash,
		"blocktime": fixture.BlockTime, "confirmations": 1,
		"in_active_chain": true, "vin": vin,
	}
}

func TestCoveragePrepareScansRangedOwnership(t *testing.T) {
	firstHash := make([]byte, 20)
	firstHash[0] = 1
	secondHash := make([]byte, 20)
	secondHash[0] = 2
	first, err := btcutil.NewAddressPubKeyHash(firstHash, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatal(err)
	}
	second, err := btcutil.NewAddressPubKeyHash(secondHash, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatal(err)
	}
	addresses := []string{first.EncodeAddress(), second.EncodeAddress()}
	derivedCalls := 0
	client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
		switch request.Method {
		case "getdescriptorinfo":
			return map[string]any{"descriptor": "wpkh(xpub/0/*)#checked"}, nil
		case "deriveaddresses":
			derivedCalls++
			if !reflect.DeepEqual(request.Params, []any{"wpkh(xpub/0/*)#checked", []any{float64(2), float64(3)}}) {
				t.Errorf("unexpected deriveaddresses params: %#v", request.Params)
			}
			return addresses, nil
		default:
			return nil, &RPCError{Code: -32601, Message: "unexpected method"}
		}
	})
	defer server.Close()

	r := [2]uint32{2, 3}
	params, byScript, byDescriptor, err := client.prepareScans(context.Background(), []input.ScanObject{{
		ID: "ranged", Label: "derived wallet", Descriptor: "wpkh(xpub/0/*)", Range: &r,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if derivedCalls != 1 || len(byDescriptor) != 0 || len(params) != 1 {
		t.Fatalf("unexpected ranged preparation: calls=%d descriptors=%v params=%#v", derivedCalls, byDescriptor, params)
	}
	param := params[0].(map[string]any)
	if param["desc"] != "wpkh(xpub/0/*)#checked" || !reflect.DeepEqual(param["range"], []uint32{2, 3}) {
		t.Errorf("incorrect scan parameters: %#v", param)
	}
	for _, address := range addresses {
		script, err := addressScript(address)
		if err != nil {
			t.Fatal(err)
		}
		owners := byScript[script]
		if !owners.ids["ranged"] || !owners.labels["derived wallet"] || len(owners.ids) != 1 || len(owners.labels) != 1 {
			t.Errorf("derived address %s was not correlated to its owner: %#v", address, owners)
		}
	}
	ids, labels := ownersFor(strings.ToUpper(mustCoverageAddressScript(t, addresses[1])), "wpkh(other)#ignored", byScript, byDescriptor)
	if !reflect.DeepEqual(ids, []string{"ranged"}) || !reflect.DeepEqual(labels, []string{"derived wallet"}) {
		t.Errorf("derived output ownership mismatch: ids=%v labels=%v", ids, labels)
	}
}

func mustCoverageAddressScript(t *testing.T, address string) string {
	t.Helper()
	script, err := addressScript(address)
	if err != nil {
		t.Fatal(err)
	}
	return script
}

func TestCoverageDiscoverFixedRawRangedAndExplicitOutpoints(t *testing.T) {
	childFixture := readFixtureFile(t, firstTransfer)
	segwitFixture := readFixtureFile(t, segwitMultiInput)
	child, err := DecodeTransaction(childFixture.Hex)
	if err != nil {
		t.Fatal(err)
	}
	segwit, err := DecodeTransaction(segwitFixture.Hex)
	if err != nil {
		t.Fatal(err)
	}
	if len(child.Outputs) < 2 || len(segwit.Outputs) != 1 || segwit.Outputs[0].Address == "" {
		t.Fatalf("fixtures do not provide the expected outputs: child=%d segwit=%+v", len(child.Outputs), segwit.Outputs)
	}

	// Exercise prepareScans directly for address derivation while Discover below
	// uses only scripts and addresses taken from committed raw transactions.
	rangedHash := make([]byte, 20)
	rangedHash[0] = 3
	rangedAddress, err := btcutil.NewAddressPubKeyHash(rangedHash, &chaincfg.MainNetParams)
	if err != nil {
		t.Fatal(err)
	}
	ranged := [2]uint32{7, 7}
	tip := model.Chain{BestBlock: segwitFixture.BlockHash, BestHeight: segwitFixture.BlockHeight}
	client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
		switch request.Method {
		case "getblockchaininfo":
			return map[string]any{"chain": "main", "blocks": tip.BestHeight, "bestblockhash": tip.BestBlock}, nil
		case "getdescriptorinfo":
			return map[string]any{"descriptor": request.Params[0].(string) + "#checked"}, nil
		case "deriveaddresses":
			if !reflect.DeepEqual(request.Params[1], []any{float64(7), float64(7)}) {
				t.Errorf("unexpected derivation range: %#v", request.Params)
			}
			return []string{rangedAddress.EncodeAddress()}, nil
		case "scantxoutset":
			params := request.Params[1].([]any)
			if len(params) != 4 {
				t.Errorf("expected fixed, raw, ranged, and xpub scan objects; got %#v", params)
			}
			return map[string]any{
				"success": true, "height": tip.BestHeight, "bestblock": tip.BestBlock,
				"unspents": []any{
					map[string]any{
						"txid": child.TxID, "vout": 0,
						"scriptPubKey": child.Outputs[0].ScriptPubKey,
						"desc":         "raw(" + child.Outputs[0].ScriptPubKey + ")#core",
						"amount":       json.Number(SatString(child.Outputs[0].ValueSats)),
						"height":       childFixture.BlockHeight, "blockhash": childFixture.BlockHash,
					},
					map[string]any{
						"txid": segwit.TxID, "vout": 0,
						"scriptPubKey": segwit.Outputs[0].ScriptPubKey,
						"desc":         "addr(" + segwit.Outputs[0].Address + ")#core",
						"amount":       json.Number(SatString(segwit.Outputs[0].ValueSats)),
						"height":       segwitFixture.BlockHeight, "blockhash": segwitFixture.BlockHash,
					},
				},
			}, nil
		case "gettxout":
			if request.Params[0] != segwit.TxID || request.Params[1] != float64(0) || request.Params[2] != true {
				t.Errorf("unexpected gettxout params: %#v", request.Params)
			}
			return map[string]any{
				"bestblock": tip.BestBlock, "confirmations": 1,
				"value":        json.Number(SatString(segwit.Outputs[0].ValueSats)),
				"scriptPubKey": map[string]any{"address": segwit.Outputs[0].Address, "hex": segwit.Outputs[0].ScriptPubKey},
			}, nil
		case "getblockheader":
			if request.Params[0] != tip.BestBlock || request.Params[1] != true {
				t.Errorf("unexpected getblockheader params: %#v", request.Params)
			}
			return map[string]any{"height": tip.BestHeight}, nil
		case "getblockhash":
			if request.Params[0] != float64(tip.BestHeight) {
				t.Errorf("unexpected block height: %#v", request.Params)
			}
			return segwitFixture.BlockHash, nil
		case "getrawtransaction":
			tx, fixture := child, childFixture
			if request.Params[0] == segwit.TxID {
				tx, fixture = segwit, segwitFixture
			} else if request.Params[0] != child.TxID {
				t.Errorf("unexpected transaction request: %#v", request.Params)
			}
			if len(request.Params) != 3 || request.Params[2] != fixture.BlockHash {
				t.Errorf("discovery did not supply the containing block: %#v", request.Params)
			}
			return coverageRawResult(tx, fixture), nil
		default:
			return nil, &RPCError{Code: -32601, Message: "unexpected " + request.Method}
		}
	})
	defer server.Close()

	owned := model.Owned{Descriptors: []model.OwnedDescriptor{
		{ID: "fixed", Descriptor: "addr(" + segwit.Outputs[0].Address + ")", Label: "fixed-label"},
		{ID: "raw", Descriptor: "raw(" + child.Outputs[0].ScriptPubKey + ")", Label: "raw-label"},
		{ID: "ranged", Descriptor: "wpkh(xpub/0/*)", Range: &ranged, Label: "unused"},
		{ID: "other-raw", Descriptor: "raw(51)"},
	}, UTXOs: []model.OwnedUTXO{{TxID: segwit.TxID, Vout: 0, Label: "explicit"}}}
	targets, chain, err := client.Discover(context.Background(), owned, NewSource(client, ""))
	if err != nil {
		t.Fatal(err)
	}
	if chain != tip || len(targets) != 2 {
		t.Fatalf("unexpected discovery result: chain=%+v targets=%+v", chain, targets)
	}
	if targets[0].TxID != segwit.TxID || targets[0].Vout != 0 {
		t.Fatalf("targets are not sorted by outpoint: %+v", targets)
	}
	if !reflect.DeepEqual(targets[0].OwnerIDs, []string{"descriptor:fixed", "utxo:0"}) ||
		!reflect.DeepEqual(targets[0].Labels, []string{"explicit", "fixed-label"}) ||
		targets[0].Address != segwit.Outputs[0].Address || targets[0].ValueSats != segwit.Outputs[0].ValueSats {
		t.Errorf("fixed-address and explicit ownership did not merge correctly: %+v", targets[0])
	}
	if targets[1].TxID != child.TxID || targets[1].Vout != 0 ||
		!reflect.DeepEqual(targets[1].OwnerIDs, []string{"descriptor:raw"}) ||
		!reflect.DeepEqual(targets[1].Labels, []string{"raw-label"}) || targets[1].Address != "" {
		t.Errorf("raw descriptor ownership mismatch: %+v", targets[1])
	}
}

func TestCoverageDiscoverFailures(t *testing.T) {
	fixture := readFixtureFile(t, firstTransfer)
	tx, err := DecodeTransaction(fixture.Hex)
	if err != nil {
		t.Fatal(err)
	}
	tip := model.Chain{BestBlock: fixture.BlockHash, BestHeight: fixture.BlockHeight}
	newClient := func(t *testing.T, failScan, spent, scanMismatch, finalMismatch bool) (*Client, func()) {
		chainCalls := 0
		client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
			switch request.Method {
			case "getblockchaininfo":
				chainCalls++
				best := tip.BestBlock
				if finalMismatch && chainCalls == 2 {
					best = strings.Repeat("1", 64)
				}
				return map[string]any{"chain": "main", "blocks": tip.BestHeight, "bestblockhash": best}, nil
			case "getdescriptorinfo":
				return map[string]any{"descriptor": request.Params[0].(string) + "#checked"}, nil
			case "scantxoutset":
				if failScan {
					return nil, &RPCError{Code: -1, Message: "scan failed"}
				}
				best, height := tip.BestBlock, tip.BestHeight
				if scanMismatch {
					height++
				}
				return map[string]any{"success": true, "height": height, "bestblock": best}, nil
			case "gettxout":
				if spent {
					return nil, nil
				}
				return map[string]any{"bestblock": tip.BestBlock, "value": json.Number("1"), "scriptPubKey": map[string]any{"address": ""}}, nil
			case "getrawtransaction":
				return coverageRawResult(tx, fixture), nil
			default:
				return nil, &RPCError{Code: -32601, Message: "unexpected " + request.Method}
			}
		})
		return client, server.Close
	}
	descriptorOwned := model.Owned{Descriptors: []model.OwnedDescriptor{{ID: "raw", Descriptor: "raw(" + tx.Outputs[0].ScriptPubKey + ")"}}}
	utxoOwned := model.Owned{UTXOs: []model.OwnedUTXO{{TxID: tx.TxID, Vout: 99}}}

	t.Run("scan RPC failure", func(t *testing.T) {
		client, closeServer := newClient(t, true, false, false, false)
		defer closeServer()
		_, _, err := client.Discover(context.Background(), descriptorOwned, NewSource(client, ""))
		if err == nil || !strings.Contains(err.Error(), "scan owned descriptors") || !IsRPCCode(err, -1) {
			t.Fatalf("unexpected scan error: %v", err)
		}
	})
	t.Run("spent or wrong vout", func(t *testing.T) {
		client, closeServer := newClient(t, false, true, false, false)
		defer closeServer()
		_, _, err := client.Discover(context.Background(), utxoOwned, NewSource(client, ""))
		if err == nil || !strings.Contains(err.Error(), tx.TxID+":99 is not a current UTXO") {
			t.Fatalf("unexpected spent-outpoint error: %v", err)
		}
	})
	t.Run("scan chain snapshot mismatch", func(t *testing.T) {
		client, closeServer := newClient(t, false, false, true, false)
		defer closeServer()
		_, _, err := client.Discover(context.Background(), descriptorOwned, NewSource(client, ""))
		if err == nil || !strings.Contains(err.Error(), "chain tip changed or UTXO scan was incomplete") {
			t.Fatalf("unexpected scan mismatch error: %v", err)
		}
	})
	t.Run("tip changes during discovery", func(t *testing.T) {
		client, closeServer := newClient(t, false, false, false, true)
		defer closeServer()
		_, _, err := client.Discover(context.Background(), model.Owned{}, NewSource(client, ""))
		if err == nil || !strings.Contains(err.Error(), "chain tip changed during discovery") {
			t.Fatalf("unexpected final-tip error: %v", err)
		}
	})
}
