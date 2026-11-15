package bank

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"tracey-sof/internal/model"
)

// StoreArtifact copies the bank export below dataDir/evidence/bank and returns
// the path relative to dataDir plus its SHA-256.
func StoreArtifact(dataDir, source string) (string, string, error) {
	data, err := os.ReadFile(source)
	if err != nil {
		return "", "", fmt.Errorf("read bank file: %w", err)
	}
	sum := sha256.Sum256(data)
	digest := hex.EncodeToString(sum[:])
	directory := filepath.Join(dataDir, "evidence", "bank")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return "", "", err
	}
	name := filepath.Base(source)
	target := filepath.Join(directory, name)
	if existing, err := os.ReadFile(target); err == nil {
		existingSum := sha256.Sum256(existing)
		if hex.EncodeToString(existingSum[:]) != digest {
			target = filepath.Join(directory, digest[:12]+"-"+name)
		}
	}
	if err := os.WriteFile(target, data, 0o600); err != nil {
		return "", "", fmt.Errorf("store bank artifact: %w", err)
	}
	return "evidence/bank/" + filepath.Base(target), digest, nil
}

func eventDate(record MatchRecord) string {
	if record.DateTime != "" {
		return record.DateTime
	}
	if record.Date != "" {
		return record.Date + "T00:00:00Z"
	}
	return ""
}

func hasArtifact(purchase model.Purchase, path string) bool {
	for _, artifact := range purchase.Artifacts {
		if artifact.Path == path {
			return true
		}
	}
	return false
}

func bankNote(record MatchRecord) string {
	return fmt.Sprintf("Matched local bank debit %s %s %s, reference %s.",
		record.BankDate, record.BankAmountEUR, record.BankCurrency, record.BankReference)
}

// Apply attaches corroborated bank evidence to purchase records and creates
// purchase records for confirmed inbound payouts. Matched candidates must have
// an explicit Confirmed decision; mismatched existing fiat is left unchanged.
func Apply(purchases model.PurchaseFile, report model.Report, cache Cache, provider string) (model.PurchaseFile, int, int) {
	updated, added := 0, 0
	existingOutpoints := map[string]bool{}
	for _, purchase := range purchases.Purchases {
		for _, outpoint := range purchase.Outpoints {
			existingOutpoints[outpoint.String()] = true
		}
	}
	for _, record := range cache.Records {
		artifactPath := record.Artifact
		artifactSHA := record.ArtifactSHA256
		if artifactPath == "" {
			artifactPath = cache.Artifact
			artifactSHA = cache.SHA256
		}
		if record.Status != "matched" || !record.Confirmed || artifactPath == "" {
			continue
		}
		description := "Local bank statement export"
		if record.StatementFile != "" {
			description = "Local bank statement PDF"
		}
		artifact := model.Artifact{Path: artifactPath, SHA256: artifactSHA, Description: description}
		if record.Kind == "purchase" {
			for index := range purchases.Purchases {
				purchase := &purchases.Purchases[index]
				if purchase.ID != record.ID {
					continue
				}
				if purchase.Fiat != nil && record.BankAmountEUR != "" && (purchase.Fiat.Amount != record.BankAmountEUR || purchase.Fiat.Currency != record.BankCurrency) {
					// A different expected fiat amount cannot be silently replaced by a
					// merchant/date heuristic. Keep the original evidence and skip.
					continue
				}
				if !hasArtifact(*purchase, artifact.Path) {
					purchase.Artifacts = append(purchase.Artifacts, artifact)
					updated++
				}
				if purchase.Fiat == nil && record.BankAmountEUR != "" {
					purchase.Fiat = &model.Fiat{Amount: record.BankAmountEUR, Currency: record.BankCurrency}
				}
				purchase.StatementDate = record.BankDate
				if purchase.Notes == "" {
					purchase.Notes = bankNote(record)
				}
			}
			continue
		}
		if record.Kind != "inbound_payout" || record.Outpoint == "" || existingOutpoints[record.Outpoint] {
			continue
		}
		outpoint, err := model.ParseOutpoint(record.Outpoint)
		if err != nil {
			continue
		}
		amount := record.AmountSats
		providerName := strings.TrimSpace(record.Provider)
		if providerName == "" || providerName == "unknown exchange" {
			providerName = provider
		}
		short := record.TxID
		if len(short) > 8 {
			short = short[:8]
		}
		base := "bank-" + strings.ReplaceAll(record.Date, "-", "") + "-" + short
		id := base
		for suffix := 2; ; suffix++ {
			duplicate := false
			for _, purchase := range purchases.Purchases {
				if purchase.ID == id {
					duplicate = true
					break
				}
			}
			if !duplicate {
				break
			}
			id = fmt.Sprintf("%s-%d", base, suffix)
		}
		purchases.Purchases = append(purchases.Purchases, model.Purchase{
			ID: id, Date: eventDate(record), StatementDate: record.BankDate, Height: record.Height,
			Provider: providerName, TxIDs: []string{record.TxID}, Outpoints: []model.Outpoint{outpoint},
			AmountSats: &amount,
			Fiat:       &model.Fiat{Amount: record.BankAmountEUR, Currency: record.BankCurrency},
			Artifacts:  []model.Artifact{artifact}, Notes: bankNote(record),
		})
		existingOutpoints[record.Outpoint] = true
		added++
	}
	return purchases, updated, added
}
