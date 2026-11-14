package bank

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"tracey-sof/internal/model"
)

// Event is an on-chain purchase or inbound exchange payout that needs bank
// evidence.
type Event struct {
	Kind         string // purchase | inbound_payout
	ID           string
	Date         string // YYYY-MM-DD
	Provider     string
	TxID         string
	Outpoint     string
	DateTime     string
	AmountSats   int64
	Height       int64
	FiatAmount   string
	FiatCurrency string
	BankEvidence bool
	BankArtifact string
	BankSHA      string
	BankDate     string
}

func (e Event) Key() string { return e.Kind + "|" + e.ID }

// MatchRecord is one event plus its bank match (or absence of one).
type MatchRecord struct {
	Kind          string `json:"kind"`
	ID            string `json:"id"`
	Date          string `json:"date"`
	Provider      string `json:"provider,omitempty"`
	TxID          string `json:"txid,omitempty"`
	Outpoint      string `json:"outpoint,omitempty"`
	DateTime      string `json:"date_time,omitempty"`
	AmountSats    int64  `json:"amount_sats,omitempty"`
	Height        int64  `json:"height,omitempty"`
	Status        string `json:"status"`
	BankDate      string `json:"bank_date,omitempty"`
	BankReference string `json:"bank_reference,omitempty"`
	BankAmountEUR string `json:"bank_amount_eur,omitempty"`
	BankCurrency  string `json:"bank_currency,omitempty"`
}

func (m MatchRecord) Key() string { return m.Kind + "|" + m.ID }

// Cache is written below the project cache directory after a bank import.
type Cache struct {
	SchemaVersion int           `json:"schema_version"`
	BankFile      string        `json:"bank_file,omitempty"`
	SHA256        string        `json:"sha256,omitempty"`
	Artifact      string        `json:"artifact,omitempty"`
	ImportedAt    string        `json:"imported_at,omitempty"`
	Records       []MatchRecord `json:"records"`
}

// LoadCache reads a bank match cache; a missing file returns an empty cache.
func LoadCache(path string) (Cache, error) {
	cache := Cache{SchemaVersion: model.SchemaVersion}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return cache, nil
		}
		return cache, fmt.Errorf("read bank cache: %w", err)
	}
	if err := json.Unmarshal(data, &cache); err != nil {
		return cache, fmt.Errorf("decode bank cache: %w", err)
	}
	if cache.SchemaVersion != model.SchemaVersion {
		return cache, fmt.Errorf("unsupported bank cache schema %d", cache.SchemaVersion)
	}
	return cache, nil
}

// SaveCache writes the cache with mode 0600.
func SaveCache(path string, cache Cache) error {
	cache.SchemaVersion = model.SchemaVersion
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(cache, "", "  ")
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o600)
}

func matchesMerchant(tx Transaction, merchants []string) bool {
	blob := tx.Blob()
	for _, merchant := range merchants {
		if merchant != "" && strings.Contains(blob, merchant) {
			return true
		}
	}
	return false
}

func dateDistance(event string, booking time.Time) (int, bool) {
	parsed, err := time.Parse("2006-01-02", event)
	if err != nil {
		return 0, false
	}
	days := int(booking.Sub(parsed).Hours() / 24)
	if days < 0 {
		days = -days
	}
	return days, true
}

// MatchEvents matches each event against debit rows from the provided export.
// A unique closest row within the window is a match; ties are ambiguous.
func MatchEvents(events []Event, transactions []Transaction, merchants []string, windowDays int) []MatchRecord {
	normalized := make([]string, 0, len(merchants))
	for _, merchant := range merchants {
		if value := strings.ToLower(strings.TrimSpace(merchant)); value != "" {
			normalized = append(normalized, value)
		}
	}
	records := make([]MatchRecord, 0, len(events))
	for _, event := range events {
		record := MatchRecord{Kind: event.Kind, ID: event.ID, Date: event.Date, DateTime: event.DateTime,
			Provider: event.Provider, TxID: event.TxID, Outpoint: event.Outpoint,
			AmountSats: event.AmountSats, Height: event.Height, Status: "missing"}
		type candidate struct {
			tx       Transaction
			distance int
		}
		best := make([]candidate, 0)
		for _, tx := range transactions {
			if tx.AmountCents >= 0 || !matchesMerchant(tx, normalized) {
				continue
			}
			distance, ok := dateDistance(event.Date, tx.BookingDate)
			if !ok || distance > windowDays {
				continue
			}
			if len(best) == 0 || distance < best[0].distance {
				best = []candidate{{tx: tx, distance: distance}}
			} else if distance == best[0].distance {
				best = append(best, candidate{tx: tx, distance: distance})
			}
		}
		switch {
		case len(best) == 1:
			record.Status = "matched"
			record.BankDate = best[0].tx.BookingDate.Format("2006-01-02")
			record.BankReference = best[0].tx.Reference()
			record.BankAmountEUR = AmountEUR(-best[0].tx.AmountCents)
			record.BankCurrency = best[0].tx.Currency
			if record.BankCurrency == "" {
				record.BankCurrency = "EUR"
			}
		case len(best) > 1:
			record.Status = "ambiguous"
		}
		records = append(records, record)
	}
	sort.Slice(records, func(i, j int) bool {
		if records[i].Date != records[j].Date {
			return records[i].Date < records[j].Date
		}
		return records[i].Key() < records[j].Key()
	})
	return records
}
