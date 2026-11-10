package core

import (
	"encoding/json"
	"os"
	"testing"
)

const firstTransfer = "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16"
const segwitMultiInput = "a38d861a0584254429a8e84747c0d203ddada49d6355bbf9de92e4fffc230c47"
const parentCoinbase = "0437cd7f8525ceed2324359c2d0ba26006d92d856a9c20fa0241106ee5a597c9"

type fixtureFile struct {
	TxID        string `json:"txid"`
	Hex         string `json:"hex"`
	BlockHash   string `json:"block_hash"`
	BlockHeight int64  `json:"block_height"`
	BlockTime   int64  `json:"block_time"`
}

func readFixtureFile(t *testing.T, txid string) fixtureFile {
	t.Helper()
	data, err := os.ReadFile("../../testdata/mainnet/" + txid + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture fixtureFile
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}
