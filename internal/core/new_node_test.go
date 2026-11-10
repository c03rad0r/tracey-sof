package core

import (
	"context"
	"reflect"
	"strings"
	"testing"
)

func coverageNodeClient(t *testing.T, failMethod string, chain map[string]any, network, indices any) (*Client, func()) {
	t.Helper()
	if chain == nil {
		chain = map[string]any{
			"chain": "main", "blocks": 100, "headers": 100,
			"bestblockhash": strings.Repeat("0", 64),
		}
	}
	if network == nil {
		network = map[string]any{"version": 300000}
	}
	if indices == nil {
		indices = map[string]any{
			"txindex":                  map[string]any{"synced": true},
			"basic block filter index": map[string]any{"synced": true},
		}
	}
	client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
		if request.Method == failMethod {
			return nil, &RPCError{Code: -23, Message: failMethod + " failed"}
		}
		switch request.Method {
		case "getblockchaininfo":
			return chain, nil
		case "getnetworkinfo":
			return network, nil
		case "getindexinfo":
			return indices, nil
		default:
			return nil, &RPCError{Code: -32601, Message: "unexpected " + request.Method}
		}
	})
	return client, server.Close
}

func TestCoverageNodeCheckWarnings(t *testing.T) {
	chain := map[string]any{
		"chain": "main", "blocks": 100, "headers": 101,
		"bestblockhash": strings.Repeat("0", 64),
		"pruned":        true, "initialblockdownload": false,
	}
	client, closeServer := coverageNodeClient(t, "", chain, map[string]any{"version": 280000}, map[string]any{})
	defer closeServer()
	info, err := client.NodeCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if info.TxIndex || info.BasicBlockFilter {
		t.Errorf("missing indices were reported as available: %+v", info)
	}
	if len(info.Warnings) != 4 {
		t.Fatalf("warnings = %v, want old-Core, synchronization, txindex, and pruned warnings", info.Warnings)
	}
	joined := strings.Join(info.Warnings, "\n")
	for _, expected := range []string{"Bitcoin Core 29+", "node is still synchronizing", "txindex is unavailable", "pruned node"} {
		if !strings.Contains(joined, expected) {
			t.Errorf("warnings %v do not contain %q", info.Warnings, expected)
		}
	}
}

func TestCoverageNodeCheckSynchronizingIBD(t *testing.T) {
	chain := map[string]any{
		"chain": "main", "blocks": 100, "headers": 100,
		"bestblockhash": strings.Repeat("0", 64), "initialblockdownload": true,
	}
	client, closeServer := coverageNodeClient(t, "", chain, nil, nil)
	defer closeServer()
	info, err := client.NodeCheck(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(info.Warnings) != 1 || !strings.Contains(info.Warnings[0], "still synchronizing") {
		t.Errorf("IBD warnings = %v", info.Warnings)
	}
}

func TestCoverageNodeCheckResponseFailures(t *testing.T) {
	for _, method := range []string{"getblockchaininfo", "getnetworkinfo", "getindexinfo"} {
		t.Run(method, func(t *testing.T) {
			client, closeServer := coverageNodeClient(t, method, nil, nil, nil)
			defer closeServer()
			_, err := client.NodeCheck(context.Background())
			if err == nil || !IsRPCCode(err, -23) || !strings.Contains(err.Error(), method+" failed") {
				t.Errorf("NodeCheck() error = %v", err)
			}
		})
	}
	t.Run("malformed indices", func(t *testing.T) {
		client, closeServer := coverageNodeClient(t, "", nil, nil, "not-an-index-map")
		defer closeServer()
		_, err := client.NodeCheck(context.Background())
		if err == nil || !strings.Contains(err.Error(), "decode getindexinfo result") {
			t.Errorf("NodeCheck() error = %v", err)
		}
	})
}

func TestCoverageNodeCheckRejectsWrongChainAndInvalidTip(t *testing.T) {
	t.Run("wrong chain", func(t *testing.T) {
		chain := map[string]any{"chain": "test", "blocks": 7, "headers": 7, "bestblockhash": strings.Repeat("0", 64)}
		client, closeServer := coverageNodeClient(t, "", chain, nil, nil)
		defer closeServer()
		info, err := client.NodeCheck(context.Background())
		if err == nil || !strings.Contains(err.Error(), `node reports chain "test"; expected main`) || info.Chain != "test" || info.Blocks != 7 {
			t.Errorf("NodeCheck() = %+v, %v", info, err)
		}
	})
	t.Run("invalid tip", func(t *testing.T) {
		chain := map[string]any{"chain": "main", "blocks": 7, "headers": 7, "bestblockhash": "invalid"}
		client, closeServer := coverageNodeClient(t, "", chain, nil, nil)
		defer closeServer()
		if _, err := client.NodeCheck(context.Background()); err == nil || !strings.Contains(err.Error(), "invalid chain tip") {
			t.Errorf("NodeCheck() error = %v", err)
		}
	})
}

func TestCoverageChainTipAndBlockRPCErrors(t *testing.T) {
	t.Run("chain RPC error", func(t *testing.T) {
		client, closeServer := coverageNodeClient(t, "getblockchaininfo", nil, nil, nil)
		defer closeServer()
		if _, err := client.ChainTip(context.Background()); err == nil || !IsRPCCode(err, -23) {
			t.Errorf("ChainTip() error = %v", err)
		}
	})
	t.Run("wrong chain", func(t *testing.T) {
		chain := map[string]any{"chain": "signet", "blocks": 7, "headers": 7, "bestblockhash": strings.Repeat("0", 64)}
		client, closeServer := coverageNodeClient(t, "", chain, nil, nil)
		defer closeServer()
		if _, err := client.ChainTip(context.Background()); err == nil || !strings.Contains(err.Error(), "this mainnet tool requires chain main") {
			t.Errorf("ChainTip() error = %v", err)
		}
	})
	t.Run("block calls", func(t *testing.T) {
		client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
			switch request.Method {
			case "getblockhash":
				if !reflect.DeepEqual(request.Params, []any{float64(9)}) {
					t.Errorf("getblockhash params = %#v", request.Params)
				}
			case "getblockheader":
				if !reflect.DeepEqual(request.Params, []any{strings.Repeat("0", 64), true}) {
					t.Errorf("getblockheader params = %#v", request.Params)
				}
			default:
				t.Errorf("unexpected method %s", request.Method)
			}
			return nil, &RPCError{Code: -8, Message: "block unavailable"}
		})
		defer server.Close()
		if _, err := client.BlockHash(context.Background(), 9); err == nil || !IsRPCCode(err, -8) {
			t.Errorf("BlockHash() error = %v", err)
		}
		if _, err := client.BlockHeight(context.Background(), strings.Repeat("0", 64)); err == nil || !IsRPCCode(err, -8) {
			t.Errorf("BlockHeight() error = %v", err)
		}
	})
}
