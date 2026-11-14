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
	width, height := 420+maxDepth*380, maxRows*170+40
	if maxRows == 0 {
		width, height = 600, 180
	}
	result := make([]svgNode, 0, len(byID))
	for d, ids := range columns {
		for row, id := range ids {
			result = append(result, svgNode{node: byID[id], depth: d, row: row, x: width - 200 - d*380, y: 85 + row*170})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].node.ID < result[j].node.ID })
	return result, width, height, nil
}

// svgAnnotations resolves wallet labels and fiat metadata for static export.
type svgAnnotations struct {
	transactions map[string]model.Annotation
	addresses    map[string]model.Annotation
	outpoints    map[string]model.Annotation
}

func newSVGAnnotations(file model.AnnotationFile) svgAnnotations {
	index := svgAnnotations{transactions: file.Transactions, addresses: file.Addresses, outpoints: file.Outpoints}
	return index
}

func (index svgAnnotations) forNode(node model.GraphNode) model.Annotation {
	if node.Kind == "transaction" {
		return index.transactions[node.TxID]
	}
	if annotation, ok := index.outpoints[node.Outpoint]; ok && (annotation.Label != "" || len(annotation.Fiat) > 0 || annotation.Time != "") {
		return annotation
	}
	return index.addresses[node.Address]
}

func btcString(sats int64) string {
	negative := sats < 0
	if negative {
		sats = -sats
	}
	sign := ""
	if negative {
		sign = "-"
	}
	return fmt.Sprintf("%s%d.%08d BTC", sign, sats/100000000, sats%100000000)
}

func fiatString(values map[string]string) string {
	if len(values) == 0 {
		return ""
	}
	currency := "USD"
	if _, ok := values[currency]; !ok {
		for key := range values {
			currency = key
			break
		}
	}
	prefix := ""
	if currency == "USD" {
		prefix = "$"
	}
	return prefix + values[currency] + " " + currency
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
	annotations := newSVGAnnotations(report.Annotations)
	var out strings.Builder
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-labelledby="title desc">`, width, height, width, height)
	out.WriteString(`<title id="title">Source-of-funds transaction graph</title><desc id="desc">Exact transaction ancestry from current owned UTXOs on the right to older transactions on the left. Purchase boundaries are evidence supplied by the user. Amounts show BTC and, when the wallet supplied it, the fiat value at transaction time.</desc>`)
	out.WriteString(`<defs><marker id="arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8 z" fill="#777"/></marker><style>text{font-family:monospace;fill:#202124}.node{fill:#fff;stroke:#68707a;stroke-width:2}.owned{stroke:#16803b;stroke-width:4}.purchase{stroke:#ba1676;stroke-width:4}.terminal{fill:#fff2f2;stroke:#b42318}.edge{stroke:#777;stroke-width:1.5;fill:none;marker-end:url(#arrow)}.small{font-size:11px;fill:#4f5358}.tiny{font-size:10px;fill:#475569}.amount{font-size:11px;font-weight:bold}.title{font-size:12px;font-weight:bold}</style></defs>`)
	out.WriteString(`<rect width="100%" height="100%" fill="#fafafa"/>`)
	for _, edge := range report.Graph.Edges {
		from, to := position[edge.From], position[edge.To]
		x1, x2 := from.x-150, to.x+150
		if x1 < x2 {
			x1, x2 = from.x+150, to.x-150
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
		annotation := annotations.forNode(n)
		label := annotation.Label
		if label == "" {
			label = n.Label
		}
		if label == "" {
			label = n.Outpoint
		}
		if label == "" {
			label = n.TxID
		}
		if label == "" {
			label = n.ID
		}
		fmt.Fprintf(&out, `<g><title>%s</title><rect class="%s" x="%d" y="%d" width="300" height="120" rx="10"/>`, html.EscapeString(svgTitle(n)), class, placed.x-150, placed.y-60)
		fmt.Fprintf(&out, `<text class="title" x="%d" y="%d">%s</text>`, placed.x-140, placed.y-38, html.EscapeString(shortID(label)))
		detail := n.Address
		if detail == "" {
			detail = n.TerminalReason
		}
		if detail != "" {
			fmt.Fprintf(&out, `<text class="small" x="%d" y="%d">%s</text>`, placed.x-140, placed.y-18, html.EscapeString(shortID(detail)))
		}
		identifier := n.TxID
		if n.Kind != "transaction" && n.Outpoint != "" && n.Address != "" {
			identifier = n.Outpoint
		}
		if identifier != "" {
			fmt.Fprintf(&out, `<text class="tiny" x="%d" y="%d">%s</text>`, placed.x-140, placed.y+2, html.EscapeString(shortID(identifier)))
		}
		fmt.Fprintf(&out, `<text class="amount" x="%d" y="%d">%s · %d sats</text>`, placed.x-140, placed.y+22, btcString(n.ValueSats), n.ValueSats)
		if fiat := fiatString(annotation.Fiat); fiat != "" {
			when := ""
			if len(annotation.Time) >= 10 {
				when = " · " + annotation.Time[:10]
			}
			fmt.Fprintf(&out, `<text class="tiny" x="%d" y="%d">%s%s (wallet-recorded)</text>`, placed.x-140, placed.y+40, html.EscapeString(fiat), html.EscapeString(when))
		} else if rate := fiatString(annotation.Rate); rate != "" {
			fmt.Fprintf(&out, `<text class="tiny" x="%d" y="%d">rate %s/BTC (wallet-recorded)</text>`, placed.x-140, placed.y+40, html.EscapeString(rate))
		}
		fmt.Fprintf(&out, `<text class="tiny" x="%d" y="%d">depth %d</text></g>`, placed.x-140, placed.y+56, placed.depth)
	}
	out.WriteString(`</svg>`)
	return []byte(out.String()), nil
}
