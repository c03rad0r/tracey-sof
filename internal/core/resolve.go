package core

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"tracey-sof/internal/input"
	"tracey-sof/internal/model"
)

type ResolvedUTXO struct {
	Outpoint      string `json:"outpoint"`
	TxID          string `json:"txid"`
	Vout          uint32 `json:"vout"`
	Address       string `json:"address,omitempty"`
	ValueSats     int64  `json:"value_sats"`
	Confirmations int64  `json:"confirmations,omitempty"`
	BlockHeight   int64  `json:"block_height,omitempty"`
}

type AddressResolution struct {
	Address      string         `json:"address"`
	CurrentUTXOs []ResolvedUTXO `json:"current_utxos,omitempty"`
}

// ResolveAddresses asks the node's current-UTXO set about exact mainnet
// addresses. It never scans historical spends and never imports a wallet.
func (c *Client) ResolveAddresses(ctx context.Context, addresses []string) ([]AddressResolution, model.Chain, error) {
	tip, err := c.ChainTip(ctx)
	if err != nil {
		return nil, tip, err
	}
	unique := make([]string, 0, len(addresses))
	seen := map[string]bool{}
	for _, address := range addresses {
		if !seen[address] {
			seen[address] = true
			unique = append(unique, address)
		}
	}
	sort.Strings(unique)
	result := make([]AddressResolution, 0, len(unique))
	if len(unique) == 0 {
		return result, tip, nil
	}
	params := make([]input.ScanObject, 0, len(unique))
	byScript := map[string]string{}
	for _, address := range unique {
		script, err := addressScript(address)
		if err != nil {
			return nil, tip, fmt.Errorf("resolve address %s: %w", address, err)
		}
		byScript[strings.ToLower(script)] = address
		params = append(params, input.ScanObject{ID: address, Descriptor: "addr(" + address + ")"})
	}
	// Core requires descriptor checksums for arbitrary scan objects.
	infoParams, _, _, err := c.prepareScans(ctx, params)
	if err != nil {
		return nil, tip, err
	}
	var scan scanResult
	if err := c.Call(ctx, "scantxoutset", []any{"start", infoParams}, &scan); err != nil {
		return nil, tip, fmt.Errorf("scan candidate addresses: %w", err)
	}
	if !scan.Success || scan.BestBlock != tip.BestBlock || scan.Height != tip.BestHeight {
		return nil, tip, fmt.Errorf("chain tip changed or address scan was incomplete; rerun resolution")
	}
	byAddress := map[string][]ResolvedUTXO{}
	for _, output := range scan.Unspents {
		value, err := DecimalSats(output.Amount.String())
		if err != nil {
			return nil, tip, err
		}
		address := byScript[strings.ToLower(output.ScriptPubKey)]
		if address == "" {
			address, _, _ = ScriptAddress(output.ScriptPubKey)
		}
		byAddress[address] = append(byAddress[address], ResolvedUTXO{
			Outpoint: model.Outpoint{TxID: output.TxID, Vout: output.Vout}.String(),
			TxID:     output.TxID, Vout: output.Vout, Address: address, ValueSats: value,
			Confirmations: tip.BestHeight - output.Height + 1, BlockHeight: output.Height,
		})
	}
	for _, address := range unique {
		utxos := byAddress[address]
		sort.Slice(utxos, func(i, j int) bool { return utxos[i].Outpoint < utxos[j].Outpoint })
		result = append(result, AddressResolution{Address: address, CurrentUTXOs: utxos})
	}
	end, err := c.ChainTip(ctx)
	if err != nil {
		return nil, tip, err
	}
	if end != tip {
		return nil, tip, fmt.Errorf("chain tip changed during address resolution; rerun for a consistent snapshot")
	}
	return result, tip, nil
}

// ConfirmTransaction checks whether a txid is retrievable through txindex or a
// supplied block context. A missing txindex is reported, not treated as proof
// that the transaction does not exist.
func (c *Client) ConfirmTransaction(ctx context.Context, txid string) (bool, int64, string, error) {
	var raw rawTransaction
	if err := c.Call(ctx, "getrawtransaction", []any{txid, 2}, &raw); err != nil {
		if IsRPCCode(err, -5) {
			return false, 0, "transaction not found or unavailable without txindex", nil
		}
		return false, 0, "", err
	}
	if raw.TxID == "" || raw.TxID != txid {
		return false, 0, "Core returned a mismatched txid", nil
	}
	height := int64(0)
	if raw.BlockHash != "" {
		var err error
		height, err = c.BlockHeight(ctx, raw.BlockHash)
		if err != nil {
			return true, 0, "transaction confirmed; block height unavailable: " + err.Error(), nil
		}
	}
	return true, height, "", nil
}

// ConfirmOutpoint reports whether an exact outpoint is a current UTXO.
func (c *Client) ConfirmOutpoint(ctx context.Context, outpoint model.Outpoint) (bool, string, error) {
	var result *txoutResult
	if err := c.Call(ctx, "gettxout", []any{outpoint.TxID, outpoint.Vout, true}, &result); err != nil {
		return false, "", err
	}
	if result == nil {
		return false, "outpoint is spent or not present in the current UTXO set", nil
	}
	return true, "", nil
}
