package model

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const SchemaVersion = 1
const SoftwareVersion = "tracey-sof/0.1.0"

// Outpoint uniquely identifies a transaction output. Vout is never negative.
type Outpoint struct {
	TxID string `json:"txid"`
	Vout uint32 `json:"vout"`
}

func (o Outpoint) String() string { return o.TxID + ":" + strconv.FormatUint(uint64(o.Vout), 10) }

func ParseOutpoint(s string) (Outpoint, error) {
	txid, index, ok := strings.Cut(s, ":")
	if !ok || len(txid) != 64 || strings.Contains(index, ":") {
		return Outpoint{}, fmt.Errorf("invalid outpoint %q: expected txid:vout", s)
	}
	v, err := strconv.ParseUint(index, 10, 32)
	if err != nil {
		return Outpoint{}, fmt.Errorf("invalid outpoint %q: %w", s, err)
	}
	return Outpoint{TxID: strings.ToLower(txid), Vout: uint32(v)}, nil
}

type Target struct {
	Outpoint    Outpoint `json:"-"`
	TxID        string   `json:"txid"`
	Vout        uint32   `json:"vout"`
	Address     string   `json:"address,omitempty"`
	ValueSats   int64    `json:"value_sats"`
	OwnerIDs    []string `json:"owner_ids,omitempty"`
	Labels      []string `json:"labels,omitempty"`
	Status      string   `json:"documentation_status"`
	Trace       string   `json:"trace_status"`
	PurchaseIDs []string `json:"purchase_ids,omitempty"`
	Notes       []string `json:"notes,omitempty"`
}

func (t Target) Key() string { return Outpoint{TxID: t.TxID, Vout: t.Vout}.String() }

type TargetRef struct {
	TxID      string   `json:"txid"`
	Vout      uint32   `json:"vout"`
	Address   string   `json:"address,omitempty"`
	ValueSats int64    `json:"value_sats"`
	OwnerIDs  []string `json:"owner_ids,omitempty"`
	Labels    []string `json:"labels,omitempty"`
}

func (t TargetRef) Key() string { return Outpoint{TxID: t.TxID, Vout: t.Vout}.String() }

type Input struct {
	PrevOut    Outpoint `json:"prevout"`
	Coinbase   bool     `json:"coinbase,omitempty"`
	PrevHeight int64    `json:"prev_height,omitempty"`
}

type Output struct {
	Index        uint32 `json:"index"`
	ValueSats    int64  `json:"value_sats"`
	ScriptPubKey string `json:"script_pubkey"`
	ScriptType   string `json:"script_type"`
	Address      string `json:"address,omitempty"`
}

type Transaction struct {
	TxID          string   `json:"txid"`
	Hex           string   `json:"hex,omitempty"`
	Version       int32    `json:"version"`
	LockTime      uint32   `json:"locktime"`
	Inputs        []Input  `json:"inputs"`
	Outputs       []Output `json:"outputs"`
	BlockHash     string   `json:"block_hash,omitempty"`
	BlockHeight   int64    `json:"block_height,omitempty"`
	BlockTime     int64    `json:"block_time,omitempty"`
	Confirmations int64    `json:"confirmations,omitempty"`
	InActiveChain bool     `json:"in_active_chain,omitempty"`
}

type Boundary struct {
	NodeID      string   `json:"node_id"`
	Reason      string   `json:"reason"`
	Detail      string   `json:"detail,omitempty"`
	PurchaseIDs []string `json:"purchase_ids,omitempty"`
	Outpoint    string   `json:"outpoint,omitempty"`
	Transaction string   `json:"transaction,omitempty"`
}

type TraceEdge struct {
	SpendingTxID string   `json:"spending_txid"`
	InputIndex   int      `json:"input_index"`
	PrevOut      Outpoint `json:"prevout"`
}

type Trace struct {
	SchemaVersion int                    `json:"schema_version"`
	Network       string                 `json:"network"`
	BestBlock     string                 `json:"best_block"`
	BestHeight    int64                  `json:"best_height"`
	Targets       []TargetRef            `json:"targets"`
	Transactions  map[string]Transaction `json:"transactions"`
	Edges         []TraceEdge            `json:"edges"`
	Boundaries    []Boundary             `json:"boundaries"`
	Incomplete    bool                   `json:"incomplete"`
}

type Artifact struct {
	Path        string `json:"path"`
	SHA256      string `json:"sha256"`
	Description string `json:"description,omitempty"`
}

type Fiat struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

type Purchase struct {
	ID         string     `json:"id"`
	Date       string     `json:"date,omitempty"`
	Provider   string     `json:"provider,omitempty"`
	TxIDs      []string   `json:"txids,omitempty"`
	Addresses  []string   `json:"addresses,omitempty"`
	Outpoints  []Outpoint `json:"outpoints,omitempty"`
	AmountSats *int64     `json:"amount_sats,omitempty"`
	Fiat       *Fiat      `json:"fiat,omitempty"`
	Artifacts  []Artifact `json:"artifacts,omitempty"`
	Notes      string     `json:"notes,omitempty"`
}

type Annotation struct {
	Label string `json:"label,omitempty"`
	Note  string `json:"note,omitempty"`
}

type PurchaseLink struct {
	Target     string `json:"target"`
	PurchaseID string `json:"purchase_id"`
	Decision   string `json:"decision"`
	Note       string `json:"note,omitempty"`
}

type OwnedAddress struct {
	Address string `json:"address"`
	Label   string `json:"label,omitempty"`
}

type OwnedUTXO struct {
	TxID  string `json:"txid"`
	Vout  uint32 `json:"vout"`
	Label string `json:"label,omitempty"`
}

type OwnedDescriptor struct {
	ID         string     `json:"id"`
	Descriptor string     `json:"descriptor"`
	Range      *[2]uint32 `json:"range,omitempty"`
	Label      string     `json:"label,omitempty"`
}

type OwnedXpub struct {
	ID         string    `json:"id"`
	Xpub       string    `json:"xpub"`
	ScriptType string    `json:"script_type"`
	Branches   []uint32  `json:"branches"`
	Range      [2]uint32 `json:"range"`
	Label      string    `json:"label,omitempty"`
}

type Owned struct {
	SchemaVersion int               `json:"schema_version"`
	Network       string            `json:"network"`
	Addresses     []OwnedAddress    `json:"addresses,omitempty"`
	UTXOs         []OwnedUTXO       `json:"utxos,omitempty"`
	Descriptors   []OwnedDescriptor `json:"descriptors,omitempty"`
	Xpubs         []OwnedXpub       `json:"xpubs,omitempty"`
}

type PurchaseFile struct {
	SchemaVersion int        `json:"schema_version"`
	Purchases     []Purchase `json:"purchases"`
}

type AnnotationFile struct {
	SchemaVersion int                   `json:"schema_version"`
	Transactions  map[string]Annotation `json:"transactions,omitempty"`
	Addresses     map[string]Annotation `json:"addresses,omitempty"`
	Outpoints     map[string]Annotation `json:"outpoints,omitempty"`
	PurchaseLinks []PurchaseLink        `json:"purchase_links,omitempty"`
}

type GraphNode struct {
	ID             string   `json:"id"`
	Kind           string   `json:"kind"`
	TxID           string   `json:"txid,omitempty"`
	Outpoint       string   `json:"outpoint,omitempty"`
	Address        string   `json:"address,omitempty"`
	ValueSats      int64    `json:"value_sats,omitempty"`
	ScriptType     string   `json:"script_type,omitempty"`
	Label          string   `json:"label,omitempty"`
	Owned          bool     `json:"owned,omitempty"`
	PurchaseIDs    []string `json:"purchase_ids,omitempty"`
	TerminalReason string   `json:"terminal_reason,omitempty"`
	TerminalDetail string   `json:"terminal_detail,omitempty"`
}

type GraphEdge struct {
	From      string `json:"from"`
	To        string `json:"to"`
	Kind      string `json:"kind"`
	ValueSats int64  `json:"value_sats,omitempty"`
}

type Graph struct {
	Nodes []GraphNode `json:"nodes"`
	Edges []GraphEdge `json:"edges"`
}

type Summary struct {
	TotalUTXOs        int   `json:"total_utxos"`
	TotalSats         int64 `json:"total_sats"`
	DocumentedUTXOs   int   `json:"documented_utxos"`
	DocumentedSats    int64 `json:"documented_sats"`
	PartialUTXOs      int   `json:"partial_utxos"`
	UndocumentedUTXOs int   `json:"undocumented_utxos"`
	IncompleteUTXOs   int   `json:"incomplete_utxos"`
}

type Chain struct {
	BestBlock  string `json:"best_block"`
	BestHeight int64  `json:"best_height"`
}

type Report struct {
	SchemaVersion   int            `json:"schema_version"`
	SoftwareVersion string         `json:"software_version,omitempty"`
	Network         string         `json:"network"`
	GeneratedAt     string         `json:"generated_at"`
	Chain           Chain          `json:"chain"`
	Warnings        []string       `json:"warnings"`
	Purchases       []Purchase     `json:"purchases"`
	UTXOs           []Target       `json:"utxos"`
	Graph           Graph          `json:"graph"`
	Summary         Summary        `json:"summary"`
	Annotations     AnnotationFile `json:"annotations"`
}

func (t Target) MarshalJSON() ([]byte, error) {
	type plain Target
	return json.Marshal(struct {
		plain
		Outpoint string `json:"outpoint"`
	}{plain: plain(t), Outpoint: t.Key()})
}

func (t *Target) UnmarshalJSON(data []byte) error {
	type plain Target
	var decoded struct {
		plain
		Outpoint string `json:"outpoint"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	*t = Target(decoded.plain)
	if decoded.Outpoint == "" {
		return fmt.Errorf("missing target outpoint")
	}
	parsed, err := ParseOutpoint(decoded.Outpoint)
	if err != nil || t.TxID != parsed.TxID || t.Vout != parsed.Vout {
		return fmt.Errorf("target outpoint is inconsistent with txid/vout")
	}
	t.Outpoint = parsed
	return nil
}

func ValidDocumentationStatus(s string) bool {
	return s == "documented" || s == "partially_documented" || s == "undocumented"
}

func ValidTraceStatus(s string) bool { return s == "complete" || s == "incomplete" }
