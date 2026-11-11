package core

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

const resolveAddress = "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"

func resolveHandler(t *testing.T, unspents []any, mutate func(method string, call int)) func(rpcRequest) (any, *RPCError) {
	t.Helper()
	script, err := addressScript(resolveAddress)
	if err != nil {
		t.Fatal(err)
	}
	tipCalls := 0
	return func(request rpcRequest) (any, *RPCError) {
		if request.Method == "getblockchaininfo" {
			tipCalls++
			if mutate != nil {
				mutate(request.Method, tipCalls)
			}
			return map[string]any{"chain": "main", "blocks": 100, "bestblockhash": strings.Repeat("a", 64)}, nil
		}
		if mutate != nil {
			mutate(request.Method, 0)
		}
		switch request.Method {
		case "getdescriptorinfo":
			return map[string]any{"descriptor": request.Params[0].(string) + "#checksum"}, nil
		case "scantxoutset":
			if len(unspents) > 0 {
				return map[string]any{"success": true, "height": 100, "bestblock": strings.Repeat("a", 64), "unspents": unspents}, nil
			}
			return map[string]any{"success": true, "height": 100, "bestblock": strings.Repeat("a", 64), "unspents": []any{}}, nil
		default:
			return nil, &RPCError{Code: -32601, Message: "unexpected " + request.Method + " " + script}
		}
	}
}

func TestResolveAddressesCurrentUTXOs(t *testing.T) {
	script, err := addressScript(resolveAddress)
	if err != nil {
		t.Fatal(err)
	}
	otherScript, err := addressScript("3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy")
	if err != nil {
		t.Fatal(err)
	}
	client, server := rpcTestClient(t, resolveHandler(t, []any{
		map[string]any{
			"txid": strings.Repeat("b", 64), "vout": 1, "scriptPubKey": script,
			"amount": json.Number("0.00000001"), "height": 90, "blockhash": strings.Repeat("c", 64),
		},
		map[string]any{
			"txid": strings.Repeat("b", 64), "vout": 0, "scriptPubKey": script,
			"amount": json.Number("0.00000002"), "height": 91, "blockhash": strings.Repeat("c", 64),
		},
		map[string]any{
			"txid": strings.Repeat("d", 64), "vout": 0, "scriptPubKey": otherScript,
			"amount": json.Number("0.00000003"), "height": 92, "blockhash": strings.Repeat("c", 64),
		},
	}, nil))
	defer server.Close()
	result, chain, err := client.ResolveAddresses(context.Background(), []string{resolveAddress, resolveAddress})
	if err != nil {
		t.Fatal(err)
	}
	if chain.BestHeight != 100 || len(result) != 1 || len(result[0].CurrentUTXOs) != 2 {
		t.Fatalf("resolution = %+v chain=%+v", result, chain)
	}
	utxo := result[0].CurrentUTXOs[0]
	if utxo.Outpoint != strings.Repeat("b", 64)+":0" || utxo.ValueSats != 2 || utxo.Confirmations != 10 || utxo.BlockHeight != 91 || utxo.Address != resolveAddress {
		t.Fatalf("resolved UTXO = %+v", utxo)
	}
	if result[0].CurrentUTXOs[1].Outpoint != strings.Repeat("b", 64)+":1" {
		t.Fatalf("UTXOs not sorted: %+v", result[0].CurrentUTXOs)
	}
}

func TestResolveAddressesRejectsSubSatoshiAmount(t *testing.T) {
	script, err := addressScript(resolveAddress)
	if err != nil {
		t.Fatal(err)
	}
	client, server := rpcTestClient(t, resolveHandler(t, []any{map[string]any{
		"txid": strings.Repeat("b", 64), "vout": 0, "scriptPubKey": script,
		"amount": json.Number("0.000000001"), "height": 90,
	}}, nil))
	defer server.Close()
	if _, _, err := client.ResolveAddresses(context.Background(), []string{resolveAddress}); err == nil {
		t.Fatal("sub-satoshi amount accepted")
	}
}

func TestResolveAddressesEmptyAndInvalid(t *testing.T) {
	client, server := rpcTestClient(t, resolveHandler(t, nil, nil))
	defer server.Close()
	result, chain, err := client.ResolveAddresses(context.Background(), nil)
	if err != nil || len(result) != 0 || chain.BestHeight != 100 {
		t.Fatalf("empty resolution = %+v %+v %v", result, chain, err)
	}
	if _, _, err := client.ResolveAddresses(context.Background(), []string{"not-an-address"}); err == nil {
		t.Fatal("invalid address accepted")
	}
}

func TestResolveAddressesErrors(t *testing.T) {
	valid := []string{resolveAddress}
	t.Run("chain tip", func(t *testing.T) {
		client, server := rpcTestClient(t, func(rpcRequest) (any, *RPCError) {
			return nil, &RPCError{Code: -1, Message: "tip failed"}
		})
		defer server.Close()
		if _, _, err := client.ResolveAddresses(context.Background(), valid); err == nil {
			t.Fatal("chain tip failure accepted")
		}
	})
	t.Run("descriptor", func(t *testing.T) {
		client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
			if request.Method == "getblockchaininfo" {
				return map[string]any{"chain": "main", "blocks": 100, "bestblockhash": strings.Repeat("a", 64)}, nil
			}
			return nil, &RPCError{Code: -1, Message: "descriptor failure"}
		})
		defer server.Close()
		if _, _, err := client.ResolveAddresses(context.Background(), valid); err == nil {
			t.Fatal("descriptor failure accepted")
		}
	})
	t.Run("scan rpc", func(t *testing.T) {
		client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
			switch request.Method {
			case "getblockchaininfo":
				return map[string]any{"chain": "main", "blocks": 100, "bestblockhash": strings.Repeat("a", 64)}, nil
			case "getdescriptorinfo":
				return map[string]any{"descriptor": request.Params[0].(string) + "#checksum"}, nil
			default:
				return nil, &RPCError{Code: -1, Message: "scan failure"}
			}
		})
		defer server.Close()
		if _, _, err := client.ResolveAddresses(context.Background(), valid); err == nil {
			t.Fatal("scan failure accepted")
		}
	})
	t.Run("incomplete scan", func(t *testing.T) {
		client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
			switch request.Method {
			case "getblockchaininfo":
				return map[string]any{"chain": "main", "blocks": 100, "bestblockhash": strings.Repeat("a", 64)}, nil
			case "getdescriptorinfo":
				return map[string]any{"descriptor": request.Params[0].(string) + "#checksum"}, nil
			default:
				return map[string]any{"success": false, "height": 100, "bestblock": strings.Repeat("a", 64)}, nil
			}
		})
		defer server.Close()
		if _, _, err := client.ResolveAddresses(context.Background(), valid); err == nil {
			t.Fatal("incomplete scan accepted")
		}
	})
	t.Run("amount", func(t *testing.T) {
		client, server := rpcTestClient(t, resolveHandler(t, []any{map[string]any{"txid": strings.Repeat("b", 64), "vout": 0, "scriptPubKey": "00", "amount": json.Number("bad"), "height": 90}}, nil))
		defer server.Close()
		if _, _, err := client.ResolveAddresses(context.Background(), valid); err == nil {
			t.Fatal("invalid amount accepted")
		}
	})
}

func TestResolveAddressesChainChanges(t *testing.T) {
	calls := 0
	client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
		switch request.Method {
		case "getblockchaininfo":
			calls++
			hash := strings.Repeat("a", 64)
			if calls == 2 {
				hash = strings.Repeat("d", 64)
			}
			return map[string]any{"chain": "main", "blocks": 100, "bestblockhash": hash}, nil
		case "getdescriptorinfo":
			return map[string]any{"descriptor": request.Params[0].(string) + "#checksum"}, nil
		case "scantxoutset":
			return map[string]any{"success": true, "height": 100, "bestblock": strings.Repeat("a", 64), "unspents": []any{}}, nil
		default:
			return nil, &RPCError{Code: -1, Message: "unexpected"}
		}
	})
	defer server.Close()
	if _, _, err := client.ResolveAddresses(context.Background(), []string{resolveAddress}); err == nil || !strings.Contains(err.Error(), "chain tip changed") {
		t.Fatalf("chain change = %v", err)
	}
}

func TestResolveAddressesFinalTipError(t *testing.T) {
	calls := 0
	client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
		if request.Method == "getblockchaininfo" {
			calls++
			if calls == 2 {
				return nil, &RPCError{Code: -1, Message: "final tip failed"}
			}
			return map[string]any{"chain": "main", "blocks": 100, "bestblockhash": strings.Repeat("a", 64)}, nil
		}
		if request.Method == "getdescriptorinfo" {
			return map[string]any{"descriptor": request.Params[0].(string) + "#checksum"}, nil
		}
		return map[string]any{"success": true, "height": 100, "bestblock": strings.Repeat("a", 64), "unspents": []any{}}, nil
	})
	defer server.Close()
	if _, _, err := client.ResolveAddresses(context.Background(), []string{resolveAddress}); err == nil {
		t.Fatal("final tip failure accepted")
	}
}

func TestConfirmTransactionAndOutpoint(t *testing.T) {
	txid := strings.Repeat("b", 64)
	client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
		switch request.Method {
		case "getrawtransaction":
			return map[string]any{"txid": txid, "blockhash": strings.Repeat("c", 64)}, nil
		case "getblockheader":
			return map[string]any{"height": 42}, nil
		case "gettxout":
			return map[string]any{"bestblock": strings.Repeat("c", 64), "confirmations": 1, "value": json.Number("0.1")}, nil
		default:
			return nil, &RPCError{Code: -1, Message: "unexpected"}
		}
	})
	defer server.Close()
	exists, height, note, err := client.ConfirmTransaction(context.Background(), txid)
	if err != nil || !exists || height != 42 || note != "" {
		t.Fatalf("transaction confirmation = %v %d %q %v", exists, height, note, err)
	}
	current, note, err := client.ConfirmOutpoint(context.Background(), model.Outpoint{TxID: txid, Vout: 0})
	if err != nil || !current || note != "" {
		t.Fatalf("outpoint confirmation = %v %q %v", current, note, err)
	}
}

func TestConfirmTransactionVariants(t *testing.T) {
	txid := strings.Repeat("b", 64)
	tests := []struct {
		name    string
		handler func(rpcRequest) (any, *RPCError)
		want    bool
		note    string
		err     bool
	}{
		{"missing", func(rpcRequest) (any, *RPCError) { return nil, &RPCError{Code: -5, Message: "missing"} }, false, "not found", false},
		{"rpc error", func(rpcRequest) (any, *RPCError) { return nil, &RPCError{Code: -1, Message: "failure"} }, false, "", true},
		{"mismatch", func(rpcRequest) (any, *RPCError) { return map[string]any{"txid": strings.Repeat("c", 64)}, nil }, false, "mismatched", false},
		{"no block", func(rpcRequest) (any, *RPCError) { return map[string]any{"txid": txid}, nil }, true, "", false},
		{"header error", func(request rpcRequest) (any, *RPCError) {
			if request.Method == "getblockheader" {
				return nil, &RPCError{Code: -1, Message: "header failure"}
			}
			return map[string]any{"txid": txid, "blockhash": strings.Repeat("c", 64)}, nil
		}, true, "block height unavailable", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, server := rpcTestClient(t, test.handler)
			defer server.Close()
			exists, _, note, err := client.ConfirmTransaction(context.Background(), txid)
			if exists != test.want || (test.err && err == nil) || (!test.err && err != nil) || !strings.Contains(note, test.note) {
				t.Fatalf("result = %v %q %v", exists, note, err)
			}
		})
	}
}

func TestConfirmOutpointVariants(t *testing.T) {
	txid := strings.Repeat("b", 64)
	client, server := rpcTestClient(t, func(rpcRequest) (any, *RPCError) { return nil, nil })
	defer server.Close()
	current, note, err := client.ConfirmOutpoint(context.Background(), model.Outpoint{TxID: txid, Vout: 0})
	if err != nil || current || !strings.Contains(note, "spent") {
		t.Fatalf("spent outpoint = %v %q %v", current, note, err)
	}
	client, server = rpcTestClient(t, func(rpcRequest) (any, *RPCError) { return nil, &RPCError{Code: -1, Message: "failure"} })
	defer server.Close()
	if _, _, err := client.ConfirmOutpoint(context.Background(), model.Outpoint{TxID: txid, Vout: 0}); err == nil {
		t.Fatal("outpoint RPC failure accepted")
	}
}
