package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"tracey-sof/internal/core"
	"tracey-sof/internal/fixtures"
	"tracey-sof/internal/input"
	"tracey-sof/internal/model"
)

type cliRPCRequest struct {
	Method string `json:"method"`
	Params []any  `json:"params"`
}

type cliFakeCore struct {
	t        *testing.T
	server   *httptest.Server
	fixtures map[string]fixtures.Fixture
	tip      fixtures.Fixture
	version  int
	chain    string
	ibd      bool
	pruned   bool

	mu          sync.Mutex
	calls       []string
	wrongRawID  bool
	missingTxID string
}

func cliFixtureDirectory() string {
	return filepath.Join("..", "..", "testdata", "mainnet")
}

func loadCLIFixtures(t *testing.T) []fixtures.Fixture {
	t.Helper()
	loaded, err := fixtures.LoadDir(cliFixtureDirectory())
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded) < 3 {
		t.Fatalf("loaded only %d real mainnet fixtures", len(loaded))
	}
	return loaded
}

func newCLIFakeCore(t *testing.T) *cliFakeCore {
	t.Helper()
	loaded := loadCLIFixtures(t)
	fake := &cliFakeCore{
		t:        t,
		fixtures: make(map[string]fixtures.Fixture, len(loaded)),
		version:  290000,
		chain:    "main",
	}
	for _, fixture := range loaded {
		fake.fixtures[fixture.TxID] = fixture
		if fixture.TxID == firstTransfer {
			fake.tip = fixture
		}
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.serveHTTP))
	t.Cleanup(fake.server.Close)
	t.Setenv("BITCOIND_RPC_URL", fake.server.URL)
	t.Setenv("BITCOIND_COOKIE_FILE", "")
	t.Setenv("BITCOIND_RPC_USER", "")
	t.Setenv("BITCOIND_RPC_PASSWORD", "")
	return fake
}

func (fake *cliFakeCore) rpcError(code int64, message string) map[string]any {
	return map[string]any{"result": nil, "error": map[string]any{"code": code, "message": message}, "id": nil}
}

func (fake *cliFakeCore) serveHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost || request.Header.Get("Content-Type") != "application/json" {
		fake.t.Errorf("unexpected RPC transport: %s %q", request.Method, request.Header.Get("Content-Type"))
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	var call cliRPCRequest
	if err := json.NewDecoder(request.Body).Decode(&call); err != nil {
		fake.t.Errorf("decode RPC request: %v", err)
		writer.WriteHeader(http.StatusBadRequest)
		return
	}
	fake.mu.Lock()
	fake.calls = append(fake.calls, call.Method)
	fake.mu.Unlock()

	result, err := fake.result(call)
	writer.Header().Set("Content-Type", "application/json")
	if err != nil {
		_ = json.NewEncoder(writer).Encode(fake.rpcError(-32601, err.Error()))
		return
	}
	_ = json.NewEncoder(writer).Encode(map[string]any{"result": result, "error": nil, "id": "tracey-sof"})
}

func (fake *cliFakeCore) result(call cliRPCRequest) (any, error) {
	switch call.Method {
	case "getblockchaininfo":
		return map[string]any{
			"chain": fake.chain, "blocks": fake.tip.BlockHeight, "headers": fake.tip.BlockHeight,
			"bestblockhash": fake.tip.BlockHash, "initialblockdownload": fake.ibd, "pruned": fake.pruned,
		}, nil
	case "getnetworkinfo":
		return map[string]any{"version": fake.version}, nil
	case "getindexinfo":
		return map[string]any{
			"txindex":                  map[string]any{"synced": true},
			"basic block filter index": map[string]any{"synced": true},
		}, nil
	case "getblockheader":
		if len(call.Params) != 2 || call.Params[0] != fake.tip.BlockHash {
			return nil, fmt.Errorf("unexpected getblockheader params %#v", call.Params)
		}
		return map[string]any{"height": fake.tip.BlockHeight}, nil
	case "getblockhash":
		if len(call.Params) != 1 {
			return nil, fmt.Errorf("unexpected getblockhash params %#v", call.Params)
		}
		height, ok := call.Params[0].(float64)
		if !ok {
			return nil, fmt.Errorf("non-numeric block height %#v", call.Params[0])
		}
		for _, fixture := range fake.fixtures {
			if fixture.BlockHeight == int64(height) {
				return fixture.BlockHash, nil
			}
		}
		return nil, fmt.Errorf("unknown block height %.0f", height)
	case "gettxout":
		if len(call.Params) != 3 || call.Params[0] != firstTransfer || call.Params[1] != float64(0) {
			return nil, fmt.Errorf("unexpected gettxout params %#v", call.Params)
		}
		transaction, err := core.DecodeTransaction(fake.fixtures[firstTransfer].Hex)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"bestblock": fake.tip.BlockHash, "confirmations": 1, "value": json.Number(core.SatString(transaction.Outputs[0].ValueSats)),
			"scriptPubKey": map[string]any{"address": transaction.Outputs[0].Address, "type": transaction.Outputs[0].ScriptType, "hex": transaction.Outputs[0].ScriptPubKey},
		}, nil
	case "getrawtransaction":
		if len(call.Params) < 2 || call.Params[1] != float64(2) {
			return nil, fmt.Errorf("unexpected getrawtransaction params %#v", call.Params)
		}
		txid, ok := call.Params[0].(string)
		if !ok {
			return nil, fmt.Errorf("transaction ID is not a string")
		}
		fixture, exists := fake.fixtures[txid]
		if !exists || txid == fake.missingTxID {
			return nil, fmt.Errorf("transaction %s is unavailable", txid)
		}
		if len(call.Params) == 3 && call.Params[2] != fixture.BlockHash {
			return nil, fmt.Errorf("wrong containing block for %s: %#v", txid, call.Params[2])
		}
		transaction, err := core.DecodeTransaction(fixture.Hex)
		if err != nil {
			return nil, err
		}
		vin := make([]any, len(transaction.Inputs))
		for i, input := range transaction.Inputs {
			if input.Coinbase {
				vin[i] = map[string]any{}
			} else {
				vin[i] = map[string]any{"prevout": map[string]any{"height": fixture.BlockHeight - 1}}
			}
		}
		returnedID := txid
		if fake.wrongRawID {
			returnedID = fake.tip.TxID
		}
		return map[string]any{
			"txid": returnedID, "hex": fixture.Hex, "blockhash": fixture.BlockHash, "blocktime": fixture.BlockTime,
			"confirmations": 1, "in_active_chain": true, "vin": vin,
		}, nil
	default:
		return nil, fmt.Errorf("unsupported RPC method %s", call.Method)
	}
}

func (fake *cliFakeCore) callsFor(method string) int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	count := 0
	for _, call := range fake.calls {
		if call == method {
			count++
		}
	}
	return count
}

func newCLIPaths(t *testing.T, chromiumAccessible bool) paths {
	t.Helper()
	root := t.TempDir()
	if chromiumAccessible {
		cache := filepath.Join("..", "..", ".cache")
		if err := os.MkdirAll(cache, 0o700); err != nil {
			t.Fatal(err)
		}
		var err error
		root, err = os.MkdirTemp(cache, "sof-cli-test-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := os.RemoveAll(root); err != nil {
				t.Errorf("clean Chromium-accessible test directory: %v", err)
			}
		})
	}
	return paths{
		dataDir:  filepath.Join(root, "data"),
		cacheDir: filepath.Join(root, "cache"),
		outDir:   filepath.Join(root, "out"),
		fixtures: cliFixtureDirectory(),
	}
}

func writeCLIJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeCLIInputs(t *testing.T, p paths, includeArtifact bool) model.PurchaseFile {
	t.Helper()
	amount := int64(1000000000)
	purchases := model.PurchaseFile{SchemaVersion: model.SchemaVersion, Purchases: []model.Purchase{{
		ID: "fixture-purchase", Date: "2009-01-12T03:03:45Z", Provider: "Local fixture provider",
		TxIDs: []string{firstTransfer}, AmountSats: &amount, Notes: "Committed fixture evidence",
	}}}
	if includeArtifact {
		contents := []byte("Local test invoice for the committed mainnet fixture.\n")
		if err := os.MkdirAll(p.dataDir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(p.dataDir, "invoice.txt"), contents, 0o600); err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(contents)
		purchases.Purchases[0].Artifacts = []model.Artifact{{Path: "invoice.txt", SHA256: hex.EncodeToString(digest[:]), Description: "test invoice"}}
	}
	owned := model.Owned{
		SchemaVersion: model.SchemaVersion,
		Network:       "mainnet",
		UTXOs:         []model.OwnedUTXO{{TxID: firstTransfer, Vout: 0, Label: "fixture current output"}},
	}
	annotations := input.EmptyAnnotations()
	annotations.Transactions[firstTransfer] = model.Annotation{Label: "Historical fixture"}
	writeCLIJSON(t, filepath.Join(p.dataDir, "owned.json"), owned)
	writeCLIJSON(t, filepath.Join(p.dataDir, "purchases.json"), purchases)
	writeCLIJSON(t, filepath.Join(p.dataDir, "annotations.json"), annotations)
	return purchases
}

func saveCLIFixtureDiscovery(t *testing.T, p paths) {
	t.Helper()
	for _, fixture := range loadCLIFixtures(t) {
		if fixture.TxID == firstTransfer {
			chain := model.Chain{BestBlock: fixture.BlockHash, BestHeight: fixture.BlockHeight}
			targets := []model.TargetRef{{TxID: firstTransfer, Vout: 0, ValueSats: 1000000000}}
			if err := core.SaveDiscovery(filepath.Join(p.cacheDir, "discovery.json"), chain, targets); err != nil {
				t.Fatal(err)
			}
			return
		}
	}
	t.Fatal("first-transfer fixture is missing")
}

func saveCLIFixtureTrace(t *testing.T, p paths) {
	t.Helper()
	for _, fixture := range loadCLIFixtures(t) {
		if fixture.TxID != firstTransfer {
			continue
		}
		transaction, err := core.DecodeTransaction(fixture.Hex)
		if err != nil {
			t.Fatal(err)
		}
		transaction.BlockHash = fixture.BlockHash
		transaction.BlockHeight = fixture.BlockHeight
		transaction.BlockTime = fixture.BlockTime
		target := model.TargetRef{TxID: firstTransfer, Vout: 0, ValueSats: 1000000000, OwnerIDs: []string{"utxo:0"}}
		trace := model.Trace{
			SchemaVersion: model.SchemaVersion,
			Network:       "mainnet",
			BestBlock:     fixture.BlockHash,
			BestHeight:    fixture.BlockHeight,
			Targets:       []model.TargetRef{target},
			Transactions:  map[string]model.Transaction{firstTransfer: transaction},
			Boundaries: []model.Boundary{{
				NodeID: "tx:" + firstTransfer, Reason: "purchase", PurchaseIDs: []string{"fixture-purchase"}, Transaction: firstTransfer,
			}},
		}
		if err := core.SaveTrace(filepath.Join(p.cacheDir, "trace.json"), trace); err != nil {
			t.Fatal(err)
		}
		return
	}
	t.Fatal("first-transfer fixture is missing")
}

func cliCommandArguments(command string, p paths, extra ...string) []string {
	arguments := []string{
		command,
		"-data-dir", p.dataDir,
		"-cache-dir", p.cacheDir,
		"-out-dir", p.outDir,
		"-fixtures", p.fixtures,
	}
	return append(arguments, extra...)
}
