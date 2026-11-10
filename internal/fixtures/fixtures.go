package fixtures

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"tracey-sof/internal/core"
	"tracey-sof/internal/model"
)

type Fixture struct {
	SchemaVersion int    `json:"schema_version"`
	TxID          string `json:"txid"`
	Hex           string `json:"hex"`
	BlockHash     string `json:"block_hash"`
	BlockHeight   int64  `json:"block_height"`
	BlockTime     int64  `json:"block_time"`
	SourceURL     string `json:"source_url"`
	Description   string `json:"description"`
}

func LoadDir(dir string) ([]Fixture, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var fixtures []Fixture
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		var fixture Fixture
		if err := json.Unmarshal(data, &fixture); err != nil {
			return nil, fmt.Errorf("%s: %w", entry.Name(), err)
		}
		if fixture.SchemaVersion != model.SchemaVersion || fixture.TxID == "" || fixture.Hex == "" {
			return nil, fmt.Errorf("%s: incomplete fixture", entry.Name())
		}
		tx, err := core.DecodeTransaction(fixture.Hex)
		if err != nil || tx.TxID != fixture.TxID || entry.Name() != fixture.TxID+".json" {
			return nil, fmt.Errorf("%s: txid or transaction bytes are inconsistent", entry.Name())
		}
		fixtures = append(fixtures, fixture)
	}
	if len(fixtures) == 0 {
		return nil, fmt.Errorf("no transaction fixtures found in %s", dir)
	}
	sort.Slice(fixtures, func(i, j int) bool { return fixtures[i].TxID < fixtures[j].TxID })
	return fixtures, nil
}

type Source map[string]model.Transaction

func NewSource(fixtures []Fixture) (Source, error) {
	source := Source{}
	for _, fixture := range fixtures {
		tx, err := core.DecodeTransaction(fixture.Hex)
		if err != nil {
			return nil, err
		}
		if tx.TxID != fixture.TxID {
			return nil, fmt.Errorf("fixture txid mismatch for %s", fixture.TxID)
		}
		tx.BlockHash, tx.BlockHeight, tx.BlockTime = fixture.BlockHash, fixture.BlockHeight, fixture.BlockTime
		source[fixture.TxID] = tx
	}
	return source, nil
}

func (s Source) Transaction(_ context.Context, txid string) (model.Transaction, error) {
	tx, ok := s[txid]
	if !ok {
		return model.Transaction{}, fmt.Errorf("fixture transaction %s not found", txid)
	}
	return tx, nil
}
