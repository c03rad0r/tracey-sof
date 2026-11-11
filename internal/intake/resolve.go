package intake

import (
	"context"
	"fmt"

	"tracey-sof/internal/core"
	"tracey-sof/internal/model"
)

// ChainResolver is the read-only subset needed by intake resolution.
type ChainResolver interface {
	ResolveAddresses(context.Context, []string) ([]core.AddressResolution, model.Chain, error)
	ConfirmTransaction(context.Context, string) (bool, int64, string, error)
	ConfirmOutpoint(context.Context, model.Outpoint) (bool, string, error)
}

type TraceIndex struct {
	addresses map[string]bool
	txids     map[string]bool
	outpoints map[string]bool
}

func NewTraceIndex(trace model.Trace) TraceIndex {
	index := TraceIndex{addresses: map[string]bool{}, txids: map[string]bool{}, outpoints: map[string]bool{}}
	for txid, transaction := range trace.Transactions {
		index.txids[txid] = true
		for _, output := range transaction.Outputs {
			if output.Address != "" {
				index.addresses[output.Address] = true
			}
			index.outpoints[model.Outpoint{TxID: txid, Vout: output.Index}.String()] = true
		}
	}
	for _, target := range trace.Targets {
		index.txids[target.TxID] = true
		index.outpoints[target.Key()] = true
		if target.Address != "" {
			index.addresses[target.Address] = true
		}
	}
	for _, edge := range trace.Edges {
		index.txids[edge.SpendingTxID] = true
		index.outpoints[edge.PrevOut.String()] = true
	}
	for _, boundary := range trace.Boundaries {
		if boundary.Transaction != "" {
			index.txids[boundary.Transaction] = true
		}
		if boundary.Outpoint != "" {
			index.outpoints[boundary.Outpoint] = true
		}
	}
	return index
}

func (i TraceIndex) Contains(candidate Candidate) bool {
	switch candidate.Kind {
	case KindAddress:
		return i.addresses[candidate.Value]
	case KindTxID:
		return i.txids[candidate.Value]
	case KindOutpoint:
		return i.outpoints[candidate.Value]
	default:
		return false
	}
}

func Resolve(ctx context.Context, resolver ChainResolver, file *File, index TraceIndex) (model.Chain, error) {
	var addresses []string
	seen := map[string]bool{}
	for _, source := range file.Sources {
		for _, candidate := range source.Candidates {
			if candidate.Kind == KindAddress && !seen[candidate.Value] {
				seen[candidate.Value] = true
				addresses = append(addresses, candidate.Value)
			}
		}
	}
	resolved, chain, err := resolver.ResolveAddresses(ctx, addresses)
	if err != nil {
		return chain, err
	}
	byAddress := map[string]core.AddressResolution{}
	for _, item := range resolved {
		byAddress[item.Address] = item
	}
	for sourceIndex := range file.Sources {
		for candidateIndex := range file.Sources[sourceIndex].Candidates {
			candidate := &file.Sources[sourceIndex].Candidates[candidateIndex]
			candidate.Resolution = &Resolution{InTrace: index.Contains(*candidate)}
			switch candidate.Kind {
			case KindAddress:
				item, ok := byAddress[candidate.Value]
				if !ok {
					return chain, fmt.Errorf("address resolution omitted %s", candidate.Value)
				}
				candidate.Resolution.Valid = true
				candidate.Resolution.Canonical = candidate.Value
				for _, utxo := range item.CurrentUTXOs {
					candidate.Resolution.CurrentUTXOs = append(candidate.Resolution.CurrentUTXOs, CurrentUTXO{
						Outpoint: utxo.Outpoint, Address: utxo.Address, ValueSats: utxo.ValueSats,
						Confirmations: utxo.Confirmations, BlockHeight: utxo.BlockHeight,
					})
				}
			case KindTxID:
				exists, height, note, err := resolver.ConfirmTransaction(ctx, candidate.Value)
				if err != nil {
					return chain, fmt.Errorf("confirm transaction %s: %w", candidate.Value, err)
				}
				candidate.Resolution.Valid = true
				candidate.Resolution.Canonical = candidate.Value
				candidate.Resolution.TxIDExists = exists
				candidate.Resolution.BlockHeight = height
				candidate.Resolution.Note = note
			case KindOutpoint:
				outpoint, _ := model.ParseOutpoint(candidate.Value)
				exists, note, err := resolver.ConfirmOutpoint(ctx, outpoint)
				if err != nil {
					return chain, fmt.Errorf("confirm outpoint %s: %w", candidate.Value, err)
				}
				candidate.Resolution.Valid = true
				candidate.Resolution.Canonical = candidate.Value
				candidate.Resolution.OutpointExists = exists
				candidate.Resolution.Note = note
			}
		}
	}
	return chain, nil
}
