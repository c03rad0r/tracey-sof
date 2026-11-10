package core

import (
	"context"
	"fmt"

	"tracey-sof/internal/model"
)

type NodeInfo struct {
	Version              int      `json:"version"`
	Chain                string   `json:"chain"`
	Blocks               int64    `json:"blocks"`
	Headers              int64    `json:"headers"`
	BestBlock            string   `json:"best_block"`
	Pruned               bool     `json:"pruned"`
	InitialBlockDownload bool     `json:"initial_block_download"`
	TxIndex              bool     `json:"txindex"`
	BasicBlockFilter     bool     `json:"basic_block_filter_index"`
	Warnings             []string `json:"warnings"`
}

func (c *Client) ChainTip(ctx context.Context) (model.Chain, error) {
	var info struct {
		Chain     string `json:"chain"`
		Blocks    int64  `json:"blocks"`
		BestBlock string `json:"bestblockhash"`
	}
	if err := c.Call(ctx, "getblockchaininfo", []any{}, &info); err != nil {
		return model.Chain{}, err
	}
	if info.Chain != "main" {
		return model.Chain{}, fmt.Errorf("node reports chain %q; this mainnet tool requires chain main", info.Chain)
	}
	return ParseHashHeight(info.BestBlock, info.Blocks)
}

func (c *Client) NodeCheck(ctx context.Context) (NodeInfo, error) {
	var chain struct {
		Chain     string `json:"chain"`
		Blocks    int64  `json:"blocks"`
		Headers   int64  `json:"headers"`
		BestBlock string `json:"bestblockhash"`
		Pruned    bool   `json:"pruned"`
		IBD       bool   `json:"initialblockdownload"`
	}
	if err := c.Call(ctx, "getblockchaininfo", []any{}, &chain); err != nil {
		return NodeInfo{}, err
	}
	var network struct {
		Version int `json:"version"`
	}
	if err := c.Call(ctx, "getnetworkinfo", []any{}, &network); err != nil {
		return NodeInfo{}, err
	}
	indices := map[string]struct {
		Synced bool `json:"synced"`
	}{}
	if err := c.Call(ctx, "getindexinfo", []any{}, &indices); err != nil {
		return NodeInfo{}, err
	}
	info := NodeInfo{Version: network.Version, Chain: chain.Chain, Blocks: chain.Blocks,
		Headers: chain.Headers, BestBlock: chain.BestBlock, Pruned: chain.Pruned,
		InitialBlockDownload: chain.IBD, TxIndex: indices["txindex"].Synced,
		BasicBlockFilter: indices["basic block filter index"].Synced}
	if _, ok := indices["txindex"]; !ok {
		info.TxIndex = false
	}
	if _, ok := indices["basic block filter index"]; !ok {
		info.BasicBlockFilter = false
	}
	if info.Chain != "main" {
		return info, fmt.Errorf("node reports chain %q; expected main", info.Chain)
	}
	if _, err := ParseHashHeight(info.BestBlock, info.Blocks); err != nil {
		return info, err
	}
	if info.Version < 290000 {
		info.Warnings = append(info.Warnings, "Bitcoin Core 29+ is the primary supported target; this older node will use only compatible RPC behavior")
	}
	if info.InitialBlockDownload || info.Blocks < info.Headers {
		info.Warnings = append(info.Warnings, "node is still synchronizing; audit is refused until synchronization completes")
	}
	if !info.TxIndex {
		info.Warnings = append(info.Warnings, "txindex is unavailable or unsynchronized; discovery seeds target transactions but some ancestry may need explicit block data")
	}
	if info.Pruned {
		info.Warnings = append(info.Warnings, "pruned node: any missing historical block or undo data will be reported as incomplete, never as undocumented")
	}
	return info, nil
}

func (c *Client) BlockHash(ctx context.Context, height int64) (string, error) {
	var hash string
	if err := c.Call(ctx, "getblockhash", []any{height}, &hash); err != nil {
		return "", err
	}
	return hash, nil
}

func (c *Client) BlockHeight(ctx context.Context, blockHash string) (int64, error) {
	var header struct {
		Height int64 `json:"height"`
	}
	if err := c.Call(ctx, "getblockheader", []any{blockHash, true}, &header); err != nil {
		return 0, err
	}
	return header.Height, nil
}
