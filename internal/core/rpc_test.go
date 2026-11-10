package core

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tracey-sof/internal/model"
)

type rpcRequest struct {
	Method string `json:"method"`
	Params []any  `json:"params"`
}
type rpcHandler func(rpcRequest) (any, *RPCError)

func rpcTestClient(t *testing.T, handle rpcHandler) (*Client, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("bad RPC request: %s %s", r.Method, r.Header.Get("Content-Type"))
		}
		var request rpcRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
		}
		result, rpcErr := handle(request)
		w.Header().Set("Content-Type", "application/json")
		if rpcErr != nil {
			_ = json.NewEncoder(w).Encode(map[string]any{"result": nil, "error": rpcErr, "id": nil})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": result, "error": nil, "id": "tracey-sof"})
	}))
	client, err := NewClient(Config{URL: server.URL, Timeout: time.Second, ScanTimeout: time.Second}, server.Client())
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	return client, server
}

func TestRPCConfiguration(t *testing.T) {
	cookie := filepath.Join(t.TempDir(), "cookie")
	if err := os.WriteFile(cookie, []byte("unit:secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) { return "ok", nil })
	server.Close()
	valid := []Config{{}, {URL: "https://localhost:8332/"}, {URL: "https://localhost", Username: "user", Password: "pass"}, {CookieFile: cookie}, {URL: "https://localhost", Timeout: time.Second, ScanTimeout: time.Second}}
	for i, cfg := range valid {
		client, err := NewClient(cfg, &http.Client{})
		if err != nil || client == nil {
			t.Errorf("valid config %d: %v", i, err)
		}
	}
	invalid := []Config{{URL: "://"}, {URL: "file:///tmp/node"}, {URL: "http://user:pass@localhost"}, {URL: "http://localhost?x=1"}, {URL: "http://localhost#x"}, {Password: "x"}, {Username: "x"}, {CookieFile: cookie, Username: "u", Password: "p"}}
	for i, cfg := range invalid {
		if _, err := NewClient(cfg, nil); err == nil {
			t.Errorf("invalid config %d accepted", i)
		}
	}
}

func TestRPCCallAndCookie(t *testing.T) {
	cookie := filepath.Join(t.TempDir(), "cookie")
	if err := os.WriteFile(cookie, []byte("unit:secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "unit" || pass != "secret" {
			t.Error("cookie authentication missing")
		}
		_, _ = w.Write([]byte(`{"result":"abc","error":null,"id":"tracey-sof"}`))
	}))
	defer server.Close()
	client, err := NewClient(Config{CookieFile: cookie, URL: server.URL}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	var result string
	if err := client.Call(context.Background(), "getbestblockhash", []any{}, &result); err != nil || result != "abc" {
		t.Fatalf("call: %q %v", result, err)
	}
	if err := client.Call(context.Background(), "stop", []any{}, nil); err == nil || !strings.Contains(err.Error(), "read-only") {
		t.Error("unsafe method was not blocked")
	}
	if err := client.Call(context.Background(), "getbestblockhash", func() {}, nil); err == nil {
		t.Error("invalid params accepted")
	}
	badCookie := filepath.Join(t.TempDir(), "bad-cookie")
	_ = os.WriteFile(badCookie, []byte("invalid"), 0o600)
	client.cookieFile = badCookie
	if err := client.Call(context.Background(), "getbestblockhash", []any{}, nil); err == nil {
		t.Error("malformed cookie accepted")
	}
	client.cookieFile = filepath.Join(t.TempDir(), "missing")
	if err := client.Call(context.Background(), "getbestblockhash", []any{}, nil); err == nil {
		t.Error("missing cookie accepted")
	}
}

func TestRPCErrors(t *testing.T) {
	cases := []struct {
		name, body string
		status     int
	}{
		{"rpc error", `{"result":null,"error":{"code":-5,"message":"not found"},"id":null}`, 200},
		{"http", `no`, 401},
		{"json", `not-json`, 200},
		{"empty result", `{"error":null,"id":null}`, 200},
		{"bad result", `{"result":1,"error":null,"id":null}`, 200},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			client, err := NewClient(Config{URL: server.URL}, server.Client())
			if err != nil {
				t.Fatal(err)
			}
			var result string
			if err := client.Call(context.Background(), "getbestblockhash", []any{}, &result); err == nil {
				t.Fatal("expected error")
			}
		})
	}
	client, server := rpcTestClient(t, func(rpcRequest) (any, *RPCError) { return nil, &RPCError{Code: -42, Message: "unit"} })
	defer server.Close()
	err := client.Call(context.Background(), "getbestblockhash", []any{}, nil)
	if !IsRPCCode(err, -42) || IsRPCCode(err, -5) || IsRPCCode(errors.New("other"), -42) {
		t.Error("RPC error inspection failed")
	}
}

func TestRPCCancellation(t *testing.T) {
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-release }))
	defer server.Close()
	defer close(release)
	client, err := NewClient(Config{URL: server.URL, Timeout: 20 * time.Millisecond}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Call(context.Background(), "getbestblockhash", []any{}, nil); err == nil {
		t.Fatal("expected timeout")
	}
}

func TestNodeAndSource(t *testing.T) {
	child, parent := readFixtureFile(t, firstTransfer), readFixtureFile(t, parentCoinbase)
	var client *Client
	client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
		switch request.Method {
		case "getblockchaininfo":
			return map[string]any{"chain": "main", "blocks": 170, "headers": 170, "bestblockhash": child.BlockHash}, nil
		case "getnetworkinfo":
			return map[string]any{"version": 310100}, nil
		case "getindexinfo":
			return map[string]any{"txindex": map[string]any{"synced": true}, "basic block filter index": map[string]any{"synced": true}}, nil
		case "getblockhash":
			if int(request.Params[0].(float64)) != 9 {
				t.Error("wrong parent height")
			}
			return parent.BlockHash, nil
		case "getblockheader":
			return map[string]any{"height": 170}, nil
		case "getrawtransaction":
			id := request.Params[0].(string)
			if id != child.TxID && id != parent.TxID {
				return nil, &RPCError{Code: -5, Message: "missing"}
			}
			if len(request.Params) < 3 && id == child.TxID {
				return nil, &RPCError{Code: -5, Message: "requires block"}
			}
			fixture := child
			if id == parent.TxID {
				fixture = parent
			}
			vin := []any{}
			if id == child.TxID {
				vin = append(vin, map[string]any{"prevout": map[string]any{"height": 9}})
			} else {
				vin = append(vin, map[string]any{})
			}
			return map[string]any{"txid": id, "hex": fixture.Hex, "blockhash": fixture.BlockHash, "blocktime": fixture.BlockTime, "confirmations": 1, "vin": vin}, nil
		default:
			return nil, &RPCError{Code: -32601, Message: "unsupported " + request.Method}
		}
	})
	defer server.Close()
	info, err := client.NodeCheck(context.Background())
	if err != nil || !info.TxIndex || !info.BasicBlockFilter || len(info.Warnings) != 0 {
		t.Fatalf("node check: %+v %v", info, err)
	}
	tip, err := client.ChainTip(context.Background())
	if err != nil || tip.BestHeight != 170 {
		t.Fatalf("chain tip: %+v %v", tip, err)
	}
	if height, err := client.BlockHeight(context.Background(), child.BlockHash); err != nil || height != 170 {
		t.Errorf("block height %d %v", height, err)
	}
	source := NewSource(client, filepath.Join(t.TempDir(), "cache", "transactions.json"))
	if err := source.Load(); err != nil {
		t.Fatal(err)
	}
	if err := source.Prefetch(context.Background(), child.TxID, child.BlockHash, 170); err != nil {
		t.Fatal(err)
	}
	loaded, err := source.Transaction(context.Background(), parent.TxID)
	if err != nil || loaded.TxID != parent.TxID {
		t.Fatalf("fallback parent: %v %v", loaded.TxID, err)
	}
	if loaded.BlockHeight != 9 {
		t.Errorf("parent block height %d", loaded.BlockHeight)
	}
	if err := source.Save(); err != nil {
		t.Fatal(err)
	}
	second := NewSource(client, source.cachePath)
	if err := second.Load(); err != nil {
		t.Fatal(err)
	}
	if got, err := second.Transaction(context.Background(), child.TxID); err != nil || got.TxID != child.TxID {
		t.Fatalf("cache read: %v %v", got.TxID, err)
	}
	empty := NewSource(nil, "")
	if err := empty.Load(); err != nil || empty.Save() != nil {
		t.Error("in-memory source setup")
	}
}

func TestDiscoveryPersistence(t *testing.T) {
	dir := t.TempDir()
	chain := model.Chain{BestBlock: strings.Repeat("0", 64), BestHeight: 2}
	targets := []model.TargetRef{{TxID: firstTransfer, Vout: 0, ValueSats: 1}}
	path := filepath.Join(dir, "nested", "discovery.json")
	if err := SaveDiscovery(path, chain, targets); err != nil {
		t.Fatal(err)
	}
	loaded, loadedChain, err := LoadDiscovery(path)
	if err != nil || len(loaded) != 1 || loadedChain != chain {
		t.Fatalf("discovery roundtrip: %+v %+v %v", loaded, loadedChain, err)
	}
	if err := SaveDiscovery(path, chain, nil); err == nil {
		t.Error("empty discovery saved")
	}
	if _, _, err := LoadDiscovery(path + ".missing"); err == nil {
		t.Error("missing discovery loaded")
	}
	if err := os.WriteFile(path, []byte("bad"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := LoadDiscovery(path); err == nil {
		t.Error("bad discovery loaded")
	}
}
