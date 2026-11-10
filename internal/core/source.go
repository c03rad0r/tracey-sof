package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"tracey-sof/internal/model"
)

type Source struct {
	client       *Client
	cachePath    string
	mu           sync.Mutex
	transactions map[string]model.Transaction
	blockHints   map[string]int64
}

type diskCache struct {
	SchemaVersion int                          `json:"schema_version"`
	Transactions  map[string]model.Transaction `json:"transactions"`
}

func NewSource(client *Client, cachePath string) *Source {
	return &Source{client: client, cachePath: cachePath, transactions: map[string]model.Transaction{}, blockHints: map[string]int64{}}
}

func (s *Source) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cachePath == "" {
		return nil
	}
	data, err := os.ReadFile(s.cachePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read transaction cache: %w", err)
	}
	var cached diskCache
	if err := json.Unmarshal(data, &cached); err != nil {
		return fmt.Errorf("decode transaction cache: %w", err)
	}
	if cached.SchemaVersion != model.SchemaVersion {
		return fmt.Errorf("unsupported transaction cache schema %d", cached.SchemaVersion)
	}
	for id, stored := range cached.Transactions {
		tx, err := DecodeTransaction(stored.Hex)
		if err != nil {
			return fmt.Errorf("cached transaction %s: %w", id, err)
		}
		if tx.TxID != id || tx.TxID != stored.TxID || len(tx.Inputs) != len(stored.Inputs) {
			return fmt.Errorf("cached transaction %s has inconsistent identity", id)
		}
		tx.BlockHash, tx.BlockHeight, tx.BlockTime = stored.BlockHash, stored.BlockHeight, stored.BlockTime
		tx.Confirmations, tx.InActiveChain = stored.Confirmations, stored.InActiveChain
		for i := range tx.Inputs {
			tx.Inputs[i].PrevHeight = stored.Inputs[i].PrevHeight
		}
		s.transactions[id] = tx
	}
	return nil
}

func (s *Source) Save() error {
	return s.save(json.MarshalIndent, osAtomicFileStore{})
}

func (s *Source) save(indent jsonIndenter, files atomicFileStore) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cachePath == "" {
		return nil
	}
	if err := files.MkdirAll(filepath.Dir(s.cachePath), 0o700); err != nil {
		return fmt.Errorf("create cache directory: %w", err)
	}
	data, err := indent(diskCache{SchemaVersion: model.SchemaVersion, Transactions: s.transactions}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode transaction cache: %w", err)
	}
	data = append(data, '\n')
	tmp, err := files.CreateTemp(filepath.Dir(s.cachePath), ".transactions-*.tmp")
	if err != nil {
		return fmt.Errorf("create transaction cache: %w", err)
	}
	path := tmp.Name()
	defer files.Remove(path)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write transaction cache: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync transaction cache: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close transaction cache: %w", err)
	}
	if err := files.Rename(path, s.cachePath); err != nil {
		return fmt.Errorf("replace transaction cache: %w", err)
	}
	return nil
}

func (s *Source) rememberHints(tx model.Transaction) {
	for _, in := range tx.Inputs {
		if !in.Coinbase && in.PrevHeight > 0 {
			s.blockHints[in.PrevOut.TxID] = in.PrevHeight
		}
	}
}

// Prefetch retrieves a transaction using a known containing block. This is how
// discovery supports arbitrary target transactions without assuming txindex.
func (s *Source) Prefetch(ctx context.Context, txid, blockHash string, height int64) error {
	s.mu.Lock()
	_, exists := s.transactions[txid]
	if height > 0 {
		s.blockHints[txid] = height
	}
	s.mu.Unlock()
	if exists {
		return nil
	}
	_, err := s.fetch(ctx, txid, blockHash, height)
	return err
}

func (s *Source) Transaction(ctx context.Context, txid string) (model.Transaction, error) {
	txid = strings.ToLower(txid)
	s.mu.Lock()
	if tx, ok := s.transactions[txid]; ok {
		s.rememberHints(tx)
		s.mu.Unlock()
		return tx, nil
	}
	height := s.blockHints[txid]
	s.mu.Unlock()
	blockHash := ""
	var err error
	if height > 0 {
		blockHash, err = s.client.BlockHash(ctx, height)
		if err != nil {
			return model.Transaction{}, fmt.Errorf("find active block for %s: %w", txid, err)
		}
	}
	return s.fetch(ctx, txid, blockHash, height)
}

type rawTransaction struct {
	TxID          string `json:"txid"`
	Hex           string `json:"hex"`
	BlockHash     string `json:"blockhash"`
	BlockTime     int64  `json:"blocktime"`
	Confirmations int64  `json:"confirmations"`
	InActiveChain bool   `json:"in_active_chain"`
	Vin           []struct {
		Prevout *struct {
			Height int64 `json:"height"`
		} `json:"prevout"`
	} `json:"vin"`
}

func (s *Source) fetch(ctx context.Context, txid, blockHash string, height int64) (model.Transaction, error) {
	params := []any{txid, 2}
	if blockHash != "" {
		params = append(params, blockHash)
	}
	var raw rawTransaction
	if err := s.client.Call(ctx, "getrawtransaction", params, &raw); err != nil {
		if blockHash == "" && IsRPCCode(err, -5) {
			return model.Transaction{}, fmt.Errorf("transaction %s unavailable without txindex: %w", txid, err)
		}
		return model.Transaction{}, err
	}
	tx, err := DecodeTransaction(raw.Hex)
	if err != nil {
		return model.Transaction{}, err
	}
	if tx.TxID != txid || raw.TxID != txid {
		return model.Transaction{}, fmt.Errorf("Core returned mismatched txid for %s", txid)
	}
	if blockHash != "" && raw.BlockHash != blockHash {
		return model.Transaction{}, fmt.Errorf("transaction %s is not in active block %s", txid, blockHash)
	}
	if len(raw.Vin) != len(tx.Inputs) {
		return model.Transaction{}, fmt.Errorf("transaction %s has inconsistent Core input metadata", txid)
	}
	for i := range raw.Vin {
		if raw.Vin[i].Prevout != nil {
			tx.Inputs[i].PrevHeight = raw.Vin[i].Prevout.Height
		}
	}
	tx.BlockHash, tx.BlockHeight, tx.BlockTime = raw.BlockHash, height, raw.BlockTime
	tx.Confirmations, tx.InActiveChain = raw.Confirmations, raw.InActiveChain
	s.mu.Lock()
	defer s.mu.Unlock()
	if prior, ok := s.transactions[txid]; ok {
		return prior, nil
	}
	s.transactions[txid] = tx
	s.rememberHints(tx)
	return tx, nil
}

func LoadDiscovery(path string) ([]model.TargetRef, model.Chain, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, model.Chain{}, fmt.Errorf("read discovery cache: %w", err)
	}
	var saved struct {
		SchemaVersion int               `json:"schema_version"`
		Chain         model.Chain       `json:"chain"`
		Targets       []model.TargetRef `json:"targets"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		return nil, model.Chain{}, fmt.Errorf("decode discovery cache: %w", err)
	}
	if saved.SchemaVersion != model.SchemaVersion || len(saved.Targets) == 0 {
		return nil, model.Chain{}, errors.New("discovery cache is empty or has an unsupported schema")
	}
	return saved.Targets, saved.Chain, nil
}

type discoveryFileStore interface {
	MkdirAll(string, os.FileMode) error
	WriteFile(string, []byte, os.FileMode) error
}

type osDiscoveryFileStore struct{}

func (osDiscoveryFileStore) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (osDiscoveryFileStore) WriteFile(path string, data []byte, perm os.FileMode) error {
	return os.WriteFile(path, data, perm)
}

func SaveDiscovery(path string, chain model.Chain, targets []model.TargetRef) error {
	return saveDiscovery(path, chain, targets, json.MarshalIndent, osDiscoveryFileStore{})
}

func saveDiscovery(path string, chain model.Chain, targets []model.TargetRef, indent jsonIndenter, files discoveryFileStore) error {
	if len(targets) == 0 {
		return fmt.Errorf("no current UTXOs were discovered; nothing was saved")
	}
	data, err := indent(struct {
		SchemaVersion int               `json:"schema_version"`
		Chain         model.Chain       `json:"chain"`
		Targets       []model.TargetRef `json:"targets"`
	}{model.SchemaVersion, chain, targets}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := files.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return files.WriteFile(path, data, 0o600)
}
