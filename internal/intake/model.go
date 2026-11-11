package intake

import (
	"fmt"
	"strings"
	"time"

	"tracey-sof/internal/model"
)

const SchemaVersion = 1

const (
	KindAddress  = "address"
	KindTxID     = "txid"
	KindOutpoint = "outpoint"
)

const (
	RoleReceivedAt       = "received_at"
	RolePurchaseTxID     = "purchase_txid"
	RolePurchaseOutpoint = "purchase_outpoint"
	RolePaidTo           = "paid_to"
	RoleOperatorAddress  = "operator_address"
	RoleUnknown          = "unknown"
)

const (
	DecisionPending  = "pending"
	DecisionAccepted = "accepted"
	DecisionRejected = "rejected"
)

const (
	ConfidenceHigh   = "high"
	ConfidenceMedium = "medium"
	ConfidenceLow    = "low"
)

// Candidate is one extracted identifier. Resolution is populated only by the
// explicit resolve step; Review is populated only by human review commands.
type Candidate struct {
	ID         string      `json:"id"`
	Kind       string      `json:"kind"`
	Value      string      `json:"value"`
	Role       string      `json:"role"`
	Confidence string      `json:"confidence"`
	Line       int         `json:"line,omitempty"`
	Resolution *Resolution `json:"resolution,omitempty"`
	Review     Review      `json:"review"`
}

type Resolution struct {
	Valid          bool          `json:"valid"`
	Canonical      string        `json:"canonical,omitempty"`
	CurrentUTXOs   []CurrentUTXO `json:"current_utxos,omitempty"`
	TxIDExists     bool          `json:"txid_exists,omitempty"`
	BlockHeight    int64         `json:"block_height,omitempty"`
	OutpointExists bool          `json:"outpoint_exists,omitempty"`
	InTrace        bool          `json:"in_trace,omitempty"`
	Note           string        `json:"note,omitempty"`
}

type CurrentUTXO struct {
	Outpoint      string `json:"outpoint"`
	Address       string `json:"address,omitempty"`
	ValueSats     int64  `json:"value_sats"`
	Confirmations int64  `json:"confirmations,omitempty"`
	BlockHeight   int64  `json:"block_height,omitempty"`
}

type Review struct {
	Decision string `json:"decision"`
	Note     string `json:"note,omitempty"`
}

// PurchaseProposal holds source-level fields that a human can edit before
// promotion. It never contains automatically trusted OCR dates or amounts.
type PurchaseProposal struct {
	ID         string      `json:"id,omitempty"`
	Date       string      `json:"date,omitempty"`
	Provider   string      `json:"provider,omitempty"`
	AmountSats *int64      `json:"amount_sats,omitempty"`
	Fiat       *model.Fiat `json:"fiat,omitempty"`
	Notes      string      `json:"notes,omitempty"`
}

type Source struct {
	ID         string           `json:"id"`
	Path       string           `json:"path"`
	SHA256     string           `json:"sha256"`
	MediaType  string           `json:"media_type"`
	Extraction []string         `json:"extraction"`
	Candidates []Candidate      `json:"candidates"`
	Purchase   PurchaseProposal `json:"purchase"`
}

type File struct {
	SchemaVersion int      `json:"schema_version"`
	GeneratedAt   string   `json:"generated_at"`
	Sources       []Source `json:"sources"`
}

func NewFile(now time.Time) File {
	return File{SchemaVersion: SchemaVersion, GeneratedAt: now.UTC().Format(time.RFC3339), Sources: []Source{}}
}

func (f *File) SourceByID(id string) *Source {
	for i := range f.Sources {
		if f.Sources[i].ID == id {
			return &f.Sources[i]
		}
	}
	return nil
}

func (f *File) CandidateByID(id string) (*Source, *Candidate) {
	for i := range f.Sources {
		for j := range f.Sources[i].Candidates {
			if f.Sources[i].Candidates[j].ID == id {
				return &f.Sources[i], &f.Sources[i].Candidates[j]
			}
		}
	}
	return nil, nil
}

func ValidRole(role string) bool {
	switch role {
	case RoleReceivedAt, RolePurchaseTxID, RolePurchaseOutpoint, RolePaidTo, RoleOperatorAddress, RoleUnknown:
		return true
	default:
		return false
	}
}

func PromotableRole(role string) bool {
	switch role {
	case RoleReceivedAt, RolePurchaseTxID, RolePurchaseOutpoint:
		return true
	default:
		return false
	}
}

func KindForRole(role string) string {
	switch role {
	case RolePurchaseTxID:
		return KindTxID
	case RolePurchaseOutpoint:
		return KindOutpoint
	case RoleReceivedAt:
		return KindAddress
	default:
		return ""
	}
}

func ValidateRoleForKind(kind, role string) error {
	if !ValidRole(role) {
		return fmt.Errorf("invalid candidate role %q", role)
	}
	if expected := KindForRole(role); expected != "" && expected != kind {
		return fmt.Errorf("role %q requires kind %q, got %q", role, expected, kind)
	}
	return nil
}

func normalizeKind(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	switch value {
	case KindAddress:
		return KindAddress
	case KindTxID:
		return KindTxID
	case KindOutpoint:
		return KindOutpoint
	default:
		return ""
	}
}
