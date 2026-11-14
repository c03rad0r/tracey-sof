package report

import (
	"fmt"
	"path/filepath"

	"tracey-sof/internal/model"
)

// SelectGraph returns the subgraph reachable from the given roots by following
// outgoing edges. It mirrors the browser's per-UTXO filter so exported SVGs
// contain only the ancestry of one target.
func SelectGraph(graph model.Graph, roots []string) model.Graph {
	rootSet := make(map[string]bool, len(roots))
	for _, root := range roots {
		if root != "" {
			rootSet[root] = true
		}
	}
	ids := make(map[string]bool, len(graph.Nodes))
	for _, node := range graph.Nodes {
		ids[node.ID] = true
	}
	outgoing := make(map[string][]string, len(graph.Nodes))
	for _, edge := range graph.Edges {
		outgoing[edge.From] = append(outgoing[edge.From], edge.To)
	}
	included := make(map[string]bool, len(graph.Nodes))
	queue := make([]string, 0, len(graph.Nodes))
	for _, node := range graph.Nodes {
		if rootSet[node.ID] && !included[node.ID] {
			included[node.ID] = true
			queue = append(queue, node.ID)
		}
	}
	for head := 0; head < len(queue); head++ {
		for _, next := range outgoing[queue[head]] {
			if ids[next] && !included[next] {
				included[next] = true
				queue = append(queue, next)
			}
		}
	}
	nodes := make([]model.GraphNode, 0, len(included))
	for _, node := range graph.Nodes {
		if included[node.ID] {
			nodes = append(nodes, node)
		}
	}
	edges := make([]model.GraphEdge, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		if included[edge.From] && included[edge.To] {
			edges = append(edges, edge)
		}
	}
	return model.Graph{Nodes: nodes, Edges: edges}
}

// shortOutpointFile returns a filesystem-safe identifier for a target.
func shortOutpointFile(utxo model.Target) string {
	txid := utxo.TxID
	if len(txid) > 12 {
		txid = txid[:12]
	}
	return fmt.Sprintf("graph-%s-%d.svg", txid, utxo.Vout)
}

// WriteGraphs writes graph-all.svg plus one focused SVG per current UTXO.
func WriteGraphs(report model.Report, outDir string) ([]string, error) {
	return writeGraphs(report, outDir, SVGBytes)
}

func writeGraphs(report model.Report, outDir string, render func(model.Report) ([]byte, error)) ([]string, error) {
	written := make([]string, 0, len(report.UTXOs)+1)
	full, err := render(report)
	if err != nil {
		return nil, err
	}
	allPath := filepath.Join(outDir, "graph-all.svg")
	if err := AtomicWrite(allPath, full, 0o600); err != nil {
		return nil, fmt.Errorf("write graph-all.svg: %w", err)
	}
	written = append(written, allPath)
	for _, utxo := range report.UTXOs {
		focused := report
		focused.Graph = SelectGraph(report.Graph, []string{"out:" + utxo.Key()})
		data, err := render(focused)
		if err != nil {
			return nil, err
		}
		path := filepath.Join(outDir, shortOutpointFile(utxo))
		if err := AtomicWrite(path, data, 0o600); err != nil {
			return nil, fmt.Errorf("write %s: %w", filepath.Base(path), err)
		}
		written = append(written, path)
	}
	return written, nil
}
