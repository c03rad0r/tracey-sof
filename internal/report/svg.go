package report

import (
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"

	"tracey-sof/internal/model"
)

type svgNode struct {
	node  model.GraphNode
	depth int
	row   int
	x, y  int
}

func shortID(value string) string {
	if len(value) <= 24 {
		return value
	}
	return value[:14] + "…" + value[len(value)-7:]
}

func svgTitle(node model.GraphNode) string {
	parts := []string{node.Kind, node.Label, node.TxID, node.Outpoint, node.Address, node.TerminalReason, node.TerminalDetail, strings.Join(node.PurchaseIDs, ", ")}
	return strings.Join(nonempty(parts), " · ")
}

func nonempty(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

func layoutGraph(graph model.Graph) ([]svgNode, int, int, error) {
	byID := map[string]model.GraphNode{}
	targets := map[string]bool{}
	for _, node := range graph.Nodes {
		if node.ID == "" || node.Kind == "" {
			return nil, 0, 0, fmt.Errorf("graph contains a node without ID or kind")
		}
		if _, exists := byID[node.ID]; exists {
			return nil, 0, 0, fmt.Errorf("duplicate graph node %s", node.ID)
		}
		byID[node.ID] = node
		if node.Owned {
			targets[node.ID] = true
		}
	}
	outgoing := map[string][]string{}
	for _, edge := range graph.Edges {
		if _, ok := byID[edge.From]; !ok {
			return nil, 0, 0, fmt.Errorf("edge starts at missing node %s", edge.From)
		}
		if _, ok := byID[edge.To]; !ok {
			return nil, 0, 0, fmt.Errorf("edge ends at missing node %s", edge.To)
		}
		if edge.Kind != "funded_by" && edge.Kind != "input" {
			return nil, 0, 0, fmt.Errorf("unknown edge kind %q", edge.Kind)
		}
		outgoing[edge.From] = append(outgoing[edge.From], edge.To)
	}
	for id := range outgoing {
		sort.Strings(outgoing[id])
	}
	depth := map[string]int{}
	queue := make([]string, 0, len(targets))
	for id := range targets {
		depth[id] = 0
		queue = append(queue, id)
	}
	sort.Strings(queue)
	for head := 0; head < len(queue); head++ {
		from := queue[head]
		for _, to := range outgoing[from] {
			candidate := depth[from] + 1
			old, seen := depth[to]
			if !seen || candidate > old {
				if candidate > len(byID) {
					return nil, 0, 0, fmt.Errorf("cycle detected in graph ancestry")
				}
				depth[to] = candidate
				queue = append(queue, to)
			}
		}
	}
	columns := map[int][]string{}
	maxDepth, maxRows := 0, 0
	for id := range byID {
		d, reached := depth[id]
		if !reached {
			continue
		}
		if d > 10000 {
			return nil, 0, 0, fmt.Errorf("graph depth limit exceeded")
		}
		columns[d] = append(columns[d], id)
		if d > maxDepth {
			maxDepth = d
		}
	}
	var disconnected []string
	for id := range byID {
		if _, reached := depth[id]; !reached {
			disconnected = append(disconnected, id)
		}
	}
	sort.Strings(disconnected)
	if len(disconnected) > 0 {
		maxDepth++
		if maxDepth > 10000 {
			return nil, 0, 0, fmt.Errorf("graph depth limit exceeded")
		}
		for _, id := range disconnected {
			columns[maxDepth] = append(columns[maxDepth], id)
		}
	}
	for d := range columns {
		sort.Strings(columns[d])
		if len(columns[d]) > maxRows {
			maxRows = len(columns[d])
		}
	}
	width, height := (maxDepth+1)*260+260, maxRows*110+110
	if maxRows == 0 {
		width, height = 600, 180
	}
	result := make([]svgNode, 0, len(byID))
	for d, ids := range columns {
		for row, id := range ids {
			result = append(result, svgNode{node: byID[id], depth: d, row: row, x: width - 170 - d*260, y: 55 + row*110})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].node.ID < result[j].node.ID })
	return result, width, height, nil
}

// SVGBytes produces a static, JavaScript-free transaction graph.
func SVGBytes(report model.Report) ([]byte, error) {
	nodes, width, height, err := layoutGraph(report.Graph)
	if err != nil {
		return nil, err
	}
	position := map[string]svgNode{}
	for _, node := range nodes {
		position[node.node.ID] = node
	}
	var out strings.Builder
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-labelledby="title desc">`, width, height, width, height)
	out.WriteString(`<title id="title">Source-of-funds transaction graph</title><desc id="desc">Exact transaction ancestry from current owned UTXOs on the right to older transactions on the left. Purchase boundaries are evidence supplied by the user.</desc>`)
	out.WriteString(`<defs><marker id="arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8 z" fill="#777"/></marker><style>text{font-family:monospace;fill:#202124}.node{fill:#fff;stroke:#68707a;stroke-width:2}.owned{stroke:#16803b;stroke-width:4}.purchase{stroke:#ba1676;stroke-width:4}.terminal{fill:#fff2f2;stroke:#b42318}.edge{stroke:#777;stroke-width:1.5;fill:none;marker-end:url(#arrow)}.small{font-size:11px;fill:#4f5358}.title{font-size:12px;font-weight:bold}</style></defs>`)
	out.WriteString(`<rect width="100%" height="100%" fill="#fafafa"/>`)
	for _, edge := range report.Graph.Edges {
		from, to := position[edge.From], position[edge.To]
		x1, x2 := from.x-105, to.x+105
		if x1 < x2 {
			x1, x2 = from.x+105, to.x-105
		}
		fmt.Fprintf(&out, `<path class="edge" d="M%d %d L%d %d"><title>%s</title></path>`, x1, from.y, x2, to.y, html.EscapeString(edge.Kind+" · "+strconv.FormatInt(edge.ValueSats, 10)+" sats"))
	}
	for _, placed := range nodes {
		n, class := placed.node, "node"
		if n.Owned {
			class += " owned"
		}
		if len(n.PurchaseIDs) > 0 {
			class += " purchase"
		}
		if n.TerminalReason != "" && n.TerminalReason != "purchase" {
			class += " terminal"
		}
		fmt.Fprintf(&out, `<g><title>%s</title><rect class="%s" x="%d" y="%d" width="210" height="76" rx="10"/>`, html.EscapeString(svgTitle(n)), class, placed.x-105, placed.y-38)
		label := n.Label
		if label == "" {
			label = n.Outpoint
		}
		if label == "" {
			label = n.TxID
		}
		if label == "" {
			label = n.ID
		}
		fmt.Fprintf(&out, `<text class="title" x="%d" y="%d">%s</text>`, placed.x-95, placed.y-14, html.EscapeString(shortID(label)))
		detail := n.Address
		if detail == "" {
			detail = n.TerminalReason
		}
		fmt.Fprintf(&out, `<text class="small" x="%d" y="%d">%s</text>`, placed.x-95, placed.y+5, html.EscapeString(shortID(detail)))
		fmt.Fprintf(&out, `<text class="small" x="%d" y="%d">%d sats · depth %d</text></g>`, placed.x-95, placed.y+25, n.ValueSats, placed.depth)
	}
	out.WriteString(`</svg>`)
	return []byte(out.String()), nil
}
