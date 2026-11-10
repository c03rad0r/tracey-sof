package core

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"github.com/btcsuite/btcd/txscript"
	"tracey-sof/internal/input"
	"tracey-sof/internal/model"
)

type txoutResult struct {
	BestBlock     string      `json:"bestblock"`
	Confirmations int64       `json:"confirmations"`
	Value         json.Number `json:"value"`
	ScriptPubKey  struct {
		Address string `json:"address"`
		Type    string `json:"type"`
		Hex     string `json:"hex"`
	} `json:"scriptPubKey"`
}

type scanResult struct {
	Success   bool   `json:"success"`
	Height    int64  `json:"height"`
	BestBlock string `json:"bestblock"`
	Unspents  []struct {
		TxID         string      `json:"txid"`
		Vout         uint32      `json:"vout"`
		ScriptPubKey string      `json:"scriptPubKey"`
		Descriptor   string      `json:"desc"`
		Amount       json.Number `json:"amount"`
		Height       int64       `json:"height"`
		BlockHash    string      `json:"blockhash"`
	} `json:"unspents"`
}

type ownership struct{ ids, labels map[string]bool }

func addOwnership(m map[string]ownership, key, id, label string) {
	entry := m[key]
	if entry.ids == nil {
		entry.ids, entry.labels = map[string]bool{}, map[string]bool{}
	}
	entry.ids[id] = true
	if label != "" {
		entry.labels[label] = true
	}
	m[key] = entry
}

func unique(values []string) []string {
	seen := map[string]bool{}
	for _, value := range values {
		if value != "" {
			seen[value] = true
		}
	}
	result := make([]string, 0, len(seen))
	for value := range seen {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func addTarget(targets map[string]*model.TargetRef, ref model.TargetRef) {
	current, ok := targets[ref.Key()]
	if !ok {
		ref.OwnerIDs, ref.Labels = unique(ref.OwnerIDs), unique(ref.Labels)
		targets[ref.Key()] = &ref
		return
	}
	if current.Address == "" {
		current.Address = ref.Address
	}
	current.OwnerIDs = unique(append(current.OwnerIDs, ref.OwnerIDs...))
	current.Labels = unique(append(current.Labels, ref.Labels...))
}

func cleanDescriptor(descriptor string) string {
	if i := strings.LastIndex(descriptor, "#"); i >= 0 {
		return descriptor[:i]
	}
	return descriptor
}

type payToAddressScriptFunc func(btcutil.Address) ([]byte, error)

func addressScript(address string) (string, error) {
	return addressScriptWith(address, txscript.PayToAddrScript)
}

func addressScriptWith(address string, payToAddress payToAddressScriptFunc) (string, error) {
	decoded, err := btcutil.DecodeAddress(address, &chaincfg.MainNetParams)
	if err != nil {
		return "", err
	}
	script, err := payToAddress(decoded)
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(script), nil
}

func fixedAddress(descriptor string) string {
	if strings.HasPrefix(descriptor, "addr(") && strings.HasSuffix(descriptor, ")") {
		return strings.TrimSuffix(strings.TrimPrefix(descriptor, "addr("), ")")
	}
	return ""
}

// prepareScans derives exact script ownership locally through the user's Core.
// No address or xpub is disclosed to another service.
func (c *Client) prepareScans(ctx context.Context, objects []input.ScanObject) ([]any, map[string]ownership, map[string]ownership, error) {
	params := make([]any, 0, len(objects))
	byScript, byDescriptor := map[string]ownership{}, map[string]ownership{}
	for _, object := range objects {
		var info struct {
			Descriptor string `json:"descriptor"`
		}
		if err := c.Call(ctx, "getdescriptorinfo", []any{object.Descriptor}, &info); err != nil {
			return nil, nil, nil, fmt.Errorf("descriptor %s: %w", object.ID, err)
		}
		param := map[string]any{"desc": info.Descriptor}
		if input.DescriptorHasRange(info.Descriptor) {
			r := [2]uint32{0, 999}
			if object.Range != nil {
				r = *object.Range
			}
			param["range"] = []uint32{r[0], r[1]}
			var addresses []string
			if err := c.Call(ctx, "deriveaddresses", []any{info.Descriptor, []uint32{r[0], r[1]}}, &addresses); err != nil {
				return nil, nil, nil, fmt.Errorf("derive owner scripts for %s: %w", object.ID, err)
			}
			if uint64(len(addresses)) != uint64(r[1])-uint64(r[0])+1 {
				return nil, nil, nil, fmt.Errorf("descriptor %s derived an unexpected address count", object.ID)
			}
			for _, address := range addresses {
				script, err := addressScript(address)
				if err != nil {
					return nil, nil, nil, fmt.Errorf("decode derived address for %s: %w", object.ID, err)
				}
				addOwnership(byScript, script, object.ID, object.Label)
			}
		} else {
			key := cleanDescriptor(info.Descriptor)
			addOwnership(byDescriptor, key, object.ID, object.Label)
			if address := fixedAddress(key); address != "" {
				script, err := addressScript(address)
				if err != nil {
					return nil, nil, nil, err
				}
				addOwnership(byScript, script, object.ID, object.Label)
			}
			if strings.HasPrefix(key, "raw(") && strings.HasSuffix(key, ")") {
				script := strings.TrimSuffix(strings.TrimPrefix(key, "raw("), ")")
				if _, err := hex.DecodeString(script); err != nil {
					return nil, nil, nil, fmt.Errorf("descriptor %s has invalid raw script", object.ID)
				}
				addOwnership(byScript, script, object.ID, object.Label)
			}
		}
		params = append(params, param)
	}
	return params, byScript, byDescriptor, nil
}

func ownersFor(outputScript, descriptor string, byScript, byDescriptor map[string]ownership) ([]string, []string) {
	var ids, labels []string
	for _, key := range []string{strings.ToLower(outputScript), cleanDescriptor(descriptor)} {
		owners := byScript[key]
		if len(owners.ids) == 0 {
			owners = byDescriptor[key]
		}
		for id := range owners.ids {
			ids = append(ids, id)
		}
		for label := range owners.labels {
			labels = append(labels, label)
		}
	}
	return unique(ids), unique(labels)
}

// Discover finds current UTXOs and seeds their raw transactions into source.
func (c *Client) Discover(ctx context.Context, owned model.Owned, source *Source) ([]model.TargetRef, model.Chain, error) {
	tip, err := c.ChainTip(ctx)
	if err != nil {
		return nil, tip, err
	}
	objects, err := input.ScanObjects(owned)
	if err != nil {
		return nil, tip, err
	}
	params, byScript, byDescriptor, err := c.prepareScans(ctx, objects)
	if err != nil {
		return nil, tip, err
	}
	targets := map[string]*model.TargetRef{}
	if len(params) > 0 {
		var result scanResult
		if err := c.Call(ctx, "scantxoutset", []any{"start", params}, &result); err != nil {
			return nil, tip, fmt.Errorf("scan owned descriptors: %w", err)
		}
		if !result.Success || result.BestBlock != tip.BestBlock || result.Height != tip.BestHeight {
			return nil, tip, fmt.Errorf("chain tip changed or UTXO scan was incomplete; rerun discovery")
		}
		for _, output := range result.Unspents {
			value, err := DecimalSats(output.Amount.String())
			if err != nil {
				return nil, tip, err
			}
			address, _, err := ScriptAddress(output.ScriptPubKey)
			if err != nil {
				return nil, tip, err
			}
			ids, labels := ownersFor(output.ScriptPubKey, output.Descriptor, byScript, byDescriptor)
			if len(ids) == 0 && len(objects) == 1 {
				ids, labels = []string{objects[0].ID}, unique([]string{objects[0].Label})
			}
			if len(ids) == 0 {
				ids = []string{"unattributed-scan-output"}
			}
			addTarget(targets, model.TargetRef{TxID: output.TxID, Vout: output.Vout, Address: address, ValueSats: value, OwnerIDs: ids, Labels: labels})
			if err := source.Prefetch(ctx, output.TxID, output.BlockHash, output.Height); err != nil {
				return nil, tip, fmt.Errorf("fetch discovered funding transaction %s: %w", output.TxID, err)
			}
		}
	}
	for i, utxo := range owned.UTXOs {
		var result *txoutResult
		if err := c.Call(ctx, "gettxout", []any{utxo.TxID, utxo.Vout, true}, &result); err != nil {
			return nil, tip, fmt.Errorf("check UTXO %s:%d: %w", utxo.TxID, utxo.Vout, err)
		}
		if result == nil {
			return nil, tip, fmt.Errorf("supplied outpoint %s:%d is not a current UTXO at this node", utxo.TxID, utxo.Vout)
		}
		value, err := DecimalSats(result.Value.String())
		if err != nil {
			return nil, tip, err
		}
		blockHash, height := "", int64(0)
		if result.Confirmations > 0 {
			tipHeight, err := c.BlockHeight(ctx, result.BestBlock)
			if err != nil {
				return nil, tip, err
			}
			height = tipHeight - result.Confirmations + 1
			blockHash, err = c.BlockHash(ctx, height)
			if err != nil {
				return nil, tip, err
			}
		}
		ref := model.TargetRef{TxID: utxo.TxID, Vout: utxo.Vout, Address: result.ScriptPubKey.Address, ValueSats: value, OwnerIDs: []string{fmt.Sprintf("utxo:%d", i)}}
		if utxo.Label != "" {
			ref.Labels = []string{utxo.Label}
		}
		addTarget(targets, ref)
		if err := source.Prefetch(ctx, utxo.TxID, blockHash, height); err != nil {
			return nil, tip, fmt.Errorf("fetch funding transaction %s: %w", utxo.TxID, err)
		}
	}
	end, err := c.ChainTip(ctx)
	if err != nil {
		return nil, tip, err
	}
	if end != tip {
		return nil, tip, fmt.Errorf("chain tip changed during discovery; rerun for a consistent snapshot")
	}
	result := make([]model.TargetRef, 0, len(targets))
	for _, target := range targets {
		result = append(result, *target)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Key() < result[j].Key() })
	return result, tip, nil
}
