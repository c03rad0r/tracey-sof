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

// outpointFile returns a filesystem-safe identifier for a target.
func outpointFile(prefix string, utxo model.Target) string {
	txid := utxo.TxID
	if len(txid) > 12 {
		txid = txid[:12]
	}
	return fmt.Sprintf("%s-%s-%d.svg", prefix, txid, utxo.Vout)
}

// WriteGraphs writes hop-layout and chronological graph families: graph-all.svg
// and graph-time-all.svg plus one focused pair per current UTXO.
func WriteGraphs(report model.Report, outDir string) ([]string, error) {
	return writeGraphs(report, outDir, SVGBytes, SVGBytesTime)
}

func writeGraphs(report model.Report, outDir string, render, renderTime func(model.Report) ([]byte, error)) ([]string, error) {
	written := make([]string, 0, 2*(len(report.UTXOs)+1))
	write := func(name string, data []byte) error {
		path := filepath.Join(outDir, name)
		if err := AtomicWrite(path, data, 0o600); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
		written = append(written, path)
		return nil
	}
	all, err := render(report)
	if err != nil {
		return nil, err
	}
	if err := write("graph-all.svg", all); err != nil {
		return nil, err
	}
	allTime, err := renderTime(report)
	if err != nil {
		return nil, err
	}
	if err := write("graph-time-all.svg", allTime); err != nil {
		return nil, err
	}
	for _, utxo := range report.UTXOs {
		focused := report
		focused.Graph = SelectGraph(report.Graph, []string{"out:" + utxo.Key()})
		data, err := render(focused)
		if err != nil {
			return nil, err
		}
		if err := write(outpointFile("graph", utxo), data); err != nil {
			return nil, err
		}
		timeData, err := renderTime(focused)
		if err != nil {
			return nil, err
		}
		if err := write(outpointFile("graph-time", utxo), timeData); err != nil {
			return nil, err
		}
	}
	return written, nil
}
