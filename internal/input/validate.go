package input

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/btcutil/base58"
	"github.com/btcsuite/btcd/btcutil/hdkeychain"
	"github.com/btcsuite/btcd/chaincfg"
	"tracey-sof/internal/model"
)

var (
	sha256Pattern   = regexp.MustCompile(`^[0-9a-f]{64}$`)
	fiatPattern     = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,18})?$`)
	idPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

func validHash(s string) bool {
	if len(s) != 64 || strings.ToLower(s) != s {
		return false
	}
	_, err := hex.DecodeString(s)
	return err == nil
}

func validateAddress(address string) error {
	addr, err := btcutil.DecodeAddress(address, &chaincfg.MainNetParams)
	if err != nil {
		return fmt.Errorf("invalid mainnet address %q: %w", address, err)
	}
	if !addr.IsForNet(&chaincfg.MainNetParams) || addr.EncodeAddress() != address {
		return fmt.Errorf("address %q is not canonical mainnet form", address)
	}
	return nil
}

func validateRange(start, end uint32) error {
	if end < start {
		return fmt.Errorf("range end %d precedes start %d", end, start)
	}
	if uint64(end)-uint64(start)+1 > 10000 {
		return fmt.Errorf("range exceeds 10000 indexes")
	}
	return nil
}

func validatePublicDescriptor(desc string) error {
	if strings.TrimSpace(desc) != desc || desc == "" {
		return fmt.Errorf("descriptor must be nonempty and have no surrounding whitespace")
	}
	lower := strings.ToLower(desc)
	for _, marker := range []string{"xprv", "yprv", "zprv", "tprv", "priv(", "wif("} {
		if strings.Contains(lower, marker) {
			return errors.New("private key material is forbidden")
		}
	}
	for _, token := range strings.FieldsFunc(desc, func(r rune) bool {
		return strings.ContainsRune("(),#", r)
	}) {
		decoded, version, err := base58.CheckDecode(token)
		if err == nil && version == 0x80 && (len(decoded) == 32 || len(decoded) == 33) {
			return errors.New("WIF private key material is forbidden")
		}
	}
	return nil
}

func validateXpub(xpub string) error {
	if !strings.HasPrefix(xpub, "xpub") {
		return fmt.Errorf("xpub must use mainnet xpub encoding")
	}
	key, err := hdkeychain.NewKeyFromString(xpub)
	if err != nil || key.IsPrivate() || !key.IsForNet(&chaincfg.MainNetParams) {
		return fmt.Errorf("invalid mainnet extended public key")
	}
	return nil
}

func ValidateOwned(owned model.Owned) error {
	if owned.SchemaVersion != model.SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", owned.SchemaVersion)
	}
	if owned.Network != "mainnet" {
		return fmt.Errorf("network must be mainnet")
	}
	if len(owned.Addresses)+len(owned.UTXOs)+len(owned.Descriptors)+len(owned.Xpubs) == 0 {
		return fmt.Errorf("at least one address, UTXO, descriptor, or xpub is required")
	}
	seenAddresses := map[string]bool{}
	for _, item := range owned.Addresses {
		if err := validateAddress(item.Address); err != nil {
			return err
		}
		if seenAddresses[item.Address] {
			return fmt.Errorf("duplicate owned address %q", item.Address)
		}
		seenAddresses[item.Address] = true
	}
	seenOutpoints := map[string]bool{}
	for _, item := range owned.UTXOs {
		if !validHash(item.TxID) {
			return fmt.Errorf("invalid UTXO txid %q", item.TxID)
		}
		key := model.Outpoint{TxID: item.TxID, Vout: item.Vout}.String()
		if seenOutpoints[key] {
			return fmt.Errorf("duplicate owned UTXO %s", key)
		}
		seenOutpoints[key] = true
	}
	ids := map[string]bool{}
	for _, item := range owned.Descriptors {
		if item.ID == "" || !idPattern.MatchString(item.ID) || ids[item.ID] {
			return fmt.Errorf("descriptor ID %q is empty or duplicated", item.ID)
		}
		ids[item.ID] = true
		if err := validatePublicDescriptor(item.Descriptor); err != nil {
			return fmt.Errorf("descriptor %q: %w", item.ID, err)
		}
		if item.Range != nil {
			if err := validateRange(item.Range[0], item.Range[1]); err != nil {
				return fmt.Errorf("descriptor %q: %w", item.ID, err)
			}
		}
	}
	for _, item := range owned.Xpubs {
		if item.ID == "" || !idPattern.MatchString(item.ID) || ids[item.ID] {
			return fmt.Errorf("xpub ID %q is empty or duplicated", item.ID)
		}
		ids[item.ID] = true
		if err := validateXpub(item.Xpub); err != nil {
			return fmt.Errorf("xpub %q: %w", item.ID, err)
		}
		switch item.ScriptType {
		case "pkh", "sh(wpkh)", "wpkh", "tr":
		default:
			return fmt.Errorf("xpub %q has unsupported script_type %q", item.ID, item.ScriptType)
		}
		if len(item.Branches) == 0 {
			return fmt.Errorf("xpub %q needs at least one branch", item.ID)
		}
		branches := map[uint32]bool{}
		for _, branch := range item.Branches {
			if branch > 1 || branches[branch] {
				return fmt.Errorf("xpub %q branches must be unique 0 (external) and/or 1 (internal)", item.ID)
			}
			branches[branch] = true
		}
		if err := validateRange(item.Range[0], item.Range[1]); err != nil {
			return fmt.Errorf("xpub %q: %w", item.ID, err)
		}
	}
	return nil
}

func ValidatePurchases(file model.PurchaseFile, baseDir string, verifyArtifacts bool) error {
	if file.SchemaVersion != model.SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", file.SchemaVersion)
	}
	if len(file.Purchases) == 0 {
		return fmt.Errorf("at least one purchase is required; use an empty file only with an explicit no-purchase workflow")
	}
	ids := map[string]bool{}
	for _, purchase := range file.Purchases {
		if purchase.ID == "" || !idPattern.MatchString(purchase.ID) || ids[purchase.ID] {
			return fmt.Errorf("purchase ID %q is empty or duplicated", purchase.ID)
		}
		ids[purchase.ID] = true
		if purchase.Date != "" {
			if _, err := time.Parse(time.RFC3339, purchase.Date); err != nil {
				return fmt.Errorf("purchase %q has invalid RFC3339 date: %w", purchase.ID, err)
			}
		}
		for _, txid := range purchase.TxIDs {
			if !validHash(txid) {
				return fmt.Errorf("purchase %q has invalid txid %q", purchase.ID, txid)
			}
		}
		for _, address := range purchase.Addresses {
			if err := validateAddress(address); err != nil {
				return fmt.Errorf("purchase %q: %w", purchase.ID, err)
			}
		}
		for _, outpoint := range purchase.Outpoints {
			if !validHash(outpoint.TxID) {
				return fmt.Errorf("purchase %q has invalid outpoint txid %q", purchase.ID, outpoint.TxID)
			}
		}
		if purchase.AmountSats != nil && *purchase.AmountSats <= 0 {
			return fmt.Errorf("purchase %q amount_sats must be positive", purchase.ID)
		}
		if purchase.Fiat != nil && (!fiatPattern.MatchString(purchase.Fiat.Amount) || !currencyPattern.MatchString(purchase.Fiat.Currency)) {
			return fmt.Errorf("purchase %q has invalid fiat amount or three-letter currency", purchase.ID)
		}
		seenArtifacts := map[string]bool{}
		for _, artifact := range purchase.Artifacts {
			if artifact.Path == "" || filepath.IsAbs(artifact.Path) || filepath.Clean(artifact.Path) != artifact.Path || strings.HasPrefix(artifact.Path, ".."+string(filepath.Separator)) || artifact.Path == ".." || !sha256Pattern.MatchString(artifact.SHA256) {
				return fmt.Errorf("purchase %q has unsafe artifact path or invalid sha256", purchase.ID)
			}
			if seenArtifacts[artifact.Path] {
				return fmt.Errorf("purchase %q duplicates artifact %q", purchase.ID, artifact.Path)
			}
			seenArtifacts[artifact.Path] = true
			if verifyArtifacts {
				if err := verifyArtifact(baseDir, artifact); err != nil {
					return fmt.Errorf("purchase %q: %w", purchase.ID, err)
				}
			}
		}
	}
	return nil
}

type artifactCloseFunc func(*os.File) error

func verifyArtifact(baseDir string, artifact model.Artifact) error {
	return verifyArtifactWithClose(baseDir, artifact, func(file *os.File) error { return file.Close() })
}

func verifyArtifactWithClose(baseDir string, artifact model.Artifact, closeFile artifactCloseFunc) (returnErr error) {
	root, err := os.OpenRoot(baseDir)
	if err != nil {
		return fmt.Errorf("open artifact root: %w", err)
	}
	defer root.Close()
	f, err := root.Open(artifact.Path)
	if err != nil {
		return fmt.Errorf("open artifact %s: %w", artifact.Path, err)
	}
	defer func() {
		if err := closeFile(f); returnErr == nil && err != nil {
			returnErr = fmt.Errorf("close artifact: %w", err)
		}
	}()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("hash artifact %s: %w", artifact.Path, err)
	}
	if actual := hex.EncodeToString(h.Sum(nil)); actual != artifact.SHA256 {
		return fmt.Errorf("artifact %s sha256 mismatch: got %s", artifact.Path, actual)
	}
	return nil
}

func ValidateAnnotations(file model.AnnotationFile) error {
	if file.SchemaVersion != model.SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", file.SchemaVersion)
	}
	for txid := range file.Transactions {
		if !validHash(txid) {
			return fmt.Errorf("invalid annotated txid %q", txid)
		}
	}
	for address := range file.Addresses {
		if err := validateAddress(address); err != nil {
			return err
		}
	}
	for outpoint := range file.Outpoints {
		parsed, err := model.ParseOutpoint(outpoint)
		if err != nil || !validHash(parsed.TxID) {
			return fmt.Errorf("invalid annotated outpoint %q", outpoint)
		}
	}
	for _, link := range file.PurchaseLinks {
		if link.PurchaseID == "" || (link.Decision != "confirmed" && link.Decision != "rejected") {
			return fmt.Errorf("purchase links need a purchase_id and confirmed/rejected decision")
		}
		if validHash(link.Target) {
			continue
		}
		if outpoint, err := model.ParseOutpoint(link.Target); err == nil && validHash(outpoint.TxID) {
			continue
		}
		if err := validateAddress(link.Target); err != nil {
			return fmt.Errorf("invalid purchase-link target %q", link.Target)
		}
	}
	return nil
}

func EmptyAnnotations() model.AnnotationFile {
	return model.AnnotationFile{SchemaVersion: model.SchemaVersion, Transactions: map[string]model.Annotation{}, Addresses: map[string]model.Annotation{}, Outpoints: map[string]model.Annotation{}}
}
