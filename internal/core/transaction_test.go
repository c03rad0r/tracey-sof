package core_test

import (
	"math"
	"testing"

	"tracey-sof/internal/core"
	"tracey-sof/internal/fixtures"
)

const firstTransfer = "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16"
const segwitMultiInput = "a38d861a0584254429a8e84747c0d203ddada49d6355bbf9de92e4fffc230c47"

func TestMainnetFixtureDecoding(t *testing.T) {
	loaded, err := fixtures.LoadDir("../../testdata/mainnet")
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, fixture := range loaded {
		seen[fixture.TxID] = true
		tx, err := core.DecodeTransaction(fixture.Hex)
		if err != nil {
			t.Fatalf("%s: %v", fixture.TxID, err)
		}
		if tx.TxID != fixture.TxID || len(tx.Outputs) == 0 || len(tx.Inputs) == 0 {
			t.Fatalf("invalid normalized fixture %+v", tx)
		}
		if tx.TxID == firstTransfer {
			if len(tx.Inputs) != 1 || tx.Inputs[0].Coinbase || tx.Inputs[0].PrevOut.TxID != "0437cd7f8525ceed2324359c2d0ba26006d92d856a9c20fa0241106ee5a597c9" {
				t.Fatalf("unexpected first transfer input: %+v", tx.Inputs)
			}
			if len(tx.Outputs) != 2 || tx.Outputs[0].ValueSats != 1000000000 || tx.Outputs[1].ValueSats != 4000000000 {
				t.Fatalf("integer sat decoding failed: %+v", tx.Outputs)
			}
			for _, output := range tx.Outputs {
				if output.Address != "" {
					t.Errorf("P2PK must not be represented as a P2PKH-owned address: %q", output.Address)
				}
			}
		}
		if fixture.TxID == "0437cd7f8525ceed2324359c2d0ba26006d92d856a9c20fa0241106ee5a597c9" && (len(tx.Inputs) != 1 || !tx.Inputs[0].Coinbase) {
			t.Errorf("expected coinbase input")
		}
		if fixture.TxID == segwitMultiInput {
			if len(tx.Inputs) != 2 {
				t.Errorf("expected 2 native SegWit inputs, got %d", len(tx.Inputs))
			}
			foundAddress := false
			for _, output := range tx.Outputs {
				if len(output.Address) > 4 && output.Address[:4] == "bc1q" {
					foundAddress = true
				}
			}
			if !foundAddress {
				t.Errorf("expected a native SegWit output address")
			}
		}
	}
	for _, id := range []string{firstTransfer, segwitMultiInput, "0437cd7f8525ceed2324359c2d0ba26006d92d856a9c20fa0241106ee5a597c9"} {
		if !seen[id] {
			t.Errorf("fixture %s missing", id)
		}
	}
}

func TestAmountsAndChain(t *testing.T) {
	cases := map[string]int64{"0": 0, "1": 100000000, "0.00000001": 1, "1e-8": 1, "21000000.00000000": 2100000000000000}
	for input, expected := range cases {
		actual, err := core.DecimalSats(input)
		if err != nil || actual != expected {
			t.Errorf("DecimalSats(%q)=(%d,%v), want %d", input, actual, err, expected)
		}
	}
	for _, input := range []string{"", "-1", "0.000000001", "not-a-number", "100000000000000000000000000000000000"} {
		if _, err := core.DecimalSats(input); err == nil {
			t.Errorf("expected %q to fail", input)
		}
	}
	for _, test := range []struct {
		sats     int64
		expected string
	}{{0, "0.0"}, {1, "0.00000001"}, {100000000, "1.0"}, {-1, "-0.00000001"}, {math.MinInt64, "-92233720368.54775808"}} {
		if actual := core.SatString(test.sats); actual != test.expected {
			t.Errorf("SatString(%d)=%q want %q", test.sats, actual, test.expected)
		}
	}
	if _, err := core.ParseHashHeight("00000000d1145790a8694403d4063f323d499e655c83426834d4ce2f8dd4a2ee", 170); err != nil {
		t.Fatal(err)
	}
	for _, input := range []struct {
		hash   string
		height int64
	}{{"bad", 1}, {"zz00000000000000000000000000000000000000000000000000000000000000", 1}, {"00000000d1145790a8694403d4063f323d499e655c83426834d4ce2f8dd4a2ee", -1}} {
		if _, err := core.ParseHashHeight(input.hash, input.height); err == nil {
			t.Errorf("expected invalid chain tip")
		}
	}
}

func TestDecodeErrorsAndScripts(t *testing.T) {
	for _, raw := range []string{"xyz", "00", ""} {
		if _, err := core.DecodeTransaction(raw); err == nil {
			t.Errorf("expected decoding %q to fail", raw)
		}
	}
	if address, class, err := core.ScriptAddress("6a0474657374"); err != nil || address != "" || class != "nulldata" {
		t.Errorf("OP_RETURN normalization: %q %q %v", address, class, err)
	}
	if _, _, err := core.ScriptAddress("zz"); err == nil {
		t.Error("expected invalid script hex")
	}
}
