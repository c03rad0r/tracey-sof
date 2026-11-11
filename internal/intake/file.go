package intake

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"tracey-sof/internal/input"
	"tracey-sof/internal/model"
)

const maxFileBytes = 64 << 20

var (
	idPattern       = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
	fiatPattern     = regexp.MustCompile(`^(0|[1-9][0-9]*)(\.[0-9]{1,18})?$`)
	currencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

func Load(path string) (File, error) {
	var file File
	f, err := os.Open(path)
	if err != nil {
		return file, fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil {
		return file, fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxFileBytes {
		return file, fmt.Errorf("%s exceeds %d bytes", path, maxFileBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&file); err != nil {
		return file, fmt.Errorf("decode %s: %w", path, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return file, fmt.Errorf("decode %s: multiple JSON values", path)
		}
		return file, fmt.Errorf("decode %s trailing data: %w", path, err)
	}
	if err := Validate(file); err != nil {
		return file, fmt.Errorf("validate %s: %w", path, err)
	}
	return file, nil
}

func Save(path string, file File) error {
	return save(path, file, json.MarshalIndent, osIntakeStore{})
}

type intakeTempFile interface {
	Name() string
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type intakeStore interface {
	MkdirAll(string, os.FileMode) error
	CreateTemp(string, string) (intakeTempFile, error)
	Remove(string) error
	Rename(string, string) error
}

type osIntakeStore struct{}

func (osIntakeStore) MkdirAll(path string, mode os.FileMode) error { return os.MkdirAll(path, mode) }
func (osIntakeStore) CreateTemp(dir, pattern string) (intakeTempFile, error) {
	return os.CreateTemp(dir, pattern)
}
func (osIntakeStore) Remove(path string) error             { return os.Remove(path) }
func (osIntakeStore) Rename(oldPath, newPath string) error { return os.Rename(oldPath, newPath) }

func save(path string, file File, marshal func(any, string, string) ([]byte, error), store intakeStore) error {
	if err := Validate(file); err != nil {
		return err
	}
	data, err := marshal(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode purchase intake: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := store.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create purchase intake directory: %w", err)
	}
	tmp, err := store.CreateTemp(dir, ".purchase-intake-*.tmp")
	if err != nil {
		return fmt.Errorf("create purchase intake temporary file: %w", err)
	}
	name := tmp.Name()
	defer store.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("set purchase intake permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write purchase intake: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync purchase intake: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close purchase intake: %w", err)
	}
	if err := store.Rename(name, path); err != nil {
		return fmt.Errorf("replace purchase intake: %w", err)
	}
	return nil
}

func Validate(file File) error {
	if file.SchemaVersion != SchemaVersion {
		return fmt.Errorf("unsupported schema_version %d", file.SchemaVersion)
	}
	if _, err := time.Parse(time.RFC3339, file.GeneratedAt); err != nil {
		return fmt.Errorf("invalid generated_at: %w", err)
	}
	if len(file.Sources) == 0 {
		return fmt.Errorf("at least one intake source is required")
	}
	sourceIDs := map[string]bool{}
	candidateIDs := map[string]bool{}
	for _, source := range file.Sources {
		if !idPattern.MatchString(source.ID) || sourceIDs[source.ID] {
			return fmt.Errorf("source ID %q is empty or duplicated", source.ID)
		}
		sourceIDs[source.ID] = true
		if err := validateRelativePath(source.Path); err != nil {
			return fmt.Errorf("source %q: %w", source.ID, err)
		}
		if len(source.SHA256) != 64 || strings.ToLower(source.SHA256) != source.SHA256 {
			return fmt.Errorf("source %q has invalid sha256", source.ID)
		}
		if _, err := hex.DecodeString(source.SHA256); err != nil {
			return fmt.Errorf("source %q has invalid sha256", source.ID)
		}
		if source.MediaType == "" {
			return fmt.Errorf("source %q has no media_type", source.ID)
		}
		if len(source.Extraction) == 0 {
			return fmt.Errorf("source %q has no extraction method", source.ID)
		}
		for _, method := range source.Extraction {
			if strings.TrimSpace(method) == "" {
				return fmt.Errorf("source %q has an empty extraction method", source.ID)
			}
		}
		if err := ValidateProposal(source.Purchase); err != nil {
			return fmt.Errorf("source %q: %w", source.ID, err)
		}
		for _, candidate := range source.Candidates {
			if !idPattern.MatchString(candidate.ID) || candidateIDs[candidate.ID] {
				return fmt.Errorf("candidate ID %q is empty or duplicated", candidate.ID)
			}
			candidateIDs[candidate.ID] = true
			if normalizeKind(candidate.Kind) != candidate.Kind {
				return fmt.Errorf("candidate %q has invalid kind %q", candidate.ID, candidate.Kind)
			}
			if err := validateCandidateValue(candidate.Kind, candidate.Value); err != nil {
				return fmt.Errorf("candidate %q: %w", candidate.ID, err)
			}
			if err := ValidateRoleForKind(candidate.Kind, candidate.Role); err != nil {
				return fmt.Errorf("candidate %q: %w", candidate.ID, err)
			}
			switch candidate.Confidence {
			case ConfidenceHigh, ConfidenceMedium, ConfidenceLow:
			default:
				return fmt.Errorf("candidate %q has invalid confidence %q", candidate.ID, candidate.Confidence)
			}
			switch candidate.Review.Decision {
			case DecisionPending, DecisionAccepted, DecisionRejected:
			default:
				return fmt.Errorf("candidate %q has invalid review decision %q", candidate.ID, candidate.Review.Decision)
			}
			if candidate.Resolution != nil {
				if err := validateResolution(*candidate.Resolution); err != nil {
					return fmt.Errorf("candidate %q: %w", candidate.ID, err)
				}
			}
		}
	}
	return nil
}

func ValidateProposal(proposal PurchaseProposal) error {
	if proposal.ID != "" && !idPattern.MatchString(proposal.ID) {
		return fmt.Errorf("invalid purchase ID %q", proposal.ID)
	}
	if proposal.Date != "" {
		if _, err := time.Parse(time.RFC3339, proposal.Date); err != nil {
			return fmt.Errorf("invalid purchase date: %w", err)
		}
	}
	if proposal.AmountSats != nil && *proposal.AmountSats <= 0 {
		return fmt.Errorf("amount_sats must be positive")
	}
	if proposal.Fiat != nil && (!fiatPattern.MatchString(proposal.Fiat.Amount) || !currencyPattern.MatchString(proposal.Fiat.Currency)) {
		return fmt.Errorf("invalid fiat amount or three-letter currency")
	}
	return nil
}

func validateCandidateValue(kind, value string) error {
	switch kind {
	case KindAddress:
		return input.ValidateMainnetAddress(value)
	case KindTxID:
		if !input.ValidTxID(value) {
			return fmt.Errorf("invalid txid %q", value)
		}
		return nil
	case KindOutpoint:
		outpoint, err := model.ParseOutpoint(value)
		if err != nil {
			return err
		}
		if !input.ValidTxID(outpoint.TxID) || outpoint.String() != value {
			return fmt.Errorf("invalid outpoint %q", value)
		}
		return nil
	default:
		return fmt.Errorf("invalid candidate kind %q", kind)
	}
}

func validateResolution(resolution Resolution) error {
	if resolution.Valid && resolution.Canonical == "" {
		return fmt.Errorf("valid resolution requires canonical value")
	}
	for _, utxo := range resolution.CurrentUTXOs {
		outpoint, err := model.ParseOutpoint(utxo.Outpoint)
		if err != nil || !input.ValidTxID(outpoint.TxID) || outpoint.String() != utxo.Outpoint {
			return fmt.Errorf("invalid resolved outpoint %q", utxo.Outpoint)
		}
		if utxo.ValueSats < 0 {
			return fmt.Errorf("resolved outpoint %q has a negative value", utxo.Outpoint)
		}
	}
	return nil
}

func validateRelativePath(value string) error {
	if value == "" || filepath.IsAbs(value) || filepath.Clean(value) != value || value == ".." || strings.HasPrefix(value, ".."+string(filepath.Separator)) {
		return fmt.Errorf("unsafe relative source path %q", value)
	}
	return nil
}
