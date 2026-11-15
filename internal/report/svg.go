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
	label string
}

type svgHeader struct {
	label string
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
			result = append(result, svgNode{node: byID[id], depth: d, row: row, x: width - 200 - d*380, y: 85 + row*170, label: fmt.Sprintf("depth %d", d)})
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].node.ID < result[j].node.ID })
	return result, width, height, nil
}

// layoutGraphHeight places one column per distinct block height present in the
// graph, skipping empty heights. x is strictly chronological (oldest left,
// newest right); nodes without a known height lead in their own column.
func layoutGraphHeight(graph model.Graph, heights map[string]int64, dates map[string]string) ([]svgNode, int, int, []svgHeader, error) {
	byID := map[string]model.GraphNode{}
	buckets := map[int64][]model.GraphNode{}
	keys := make([]int64, 0)
	for _, node := range graph.Nodes {
		if node.ID == "" || node.Kind == "" {
			return nil, 0, 0, nil, fmt.Errorf("graph contains a node without ID or kind")
		}
		if _, exists := byID[node.ID]; exists {
			return nil, 0, 0, nil, fmt.Errorf("duplicate graph node %s", node.ID)
		}
		byID[node.ID] = node
		height := heights[node.ID]
		if height < 0 {
			height = 0
		}
		if _, exists := buckets[height]; !exists {
			keys = append(keys, height)
		}
		buckets[height] = append(buckets[height], node)
	}
	for _, edge := range graph.Edges {
		if _, ok := byID[edge.From]; !ok {
			return nil, 0, 0, nil, fmt.Errorf("edge starts at missing node %s", edge.From)
		}
		if _, ok := byID[edge.To]; !ok {
			return nil, 0, 0, nil, fmt.Errorf("edge ends at missing node %s", edge.To)
		}
		if edge.Kind != "funded_by" && edge.Kind != "input" {
			return nil, 0, 0, nil, fmt.Errorf("unknown edge kind %q", edge.Kind)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	headers := make([]svgHeader, 0, len(keys))
	result := make([]svgNode, 0, len(graph.Nodes))
	maxRows := 0
	for column, height := range keys {
		nodes := buckets[height]
		sort.SliceStable(nodes, func(i, j int) bool {
			left, right := dates[nodes[i].ID], dates[nodes[j].ID]
			if left != right {
				return left < right
			}
			return nodes[i].ID < nodes[j].ID
		})
		x := 190 + column*380
		header := "unknown height"
		if height > 0 {
			header = commaInt(height)
			for _, node := range nodes {
				if dates[node.ID] != "" {
					header += " · " + dates[node.ID]
					break
				}
			}
		}
		headers = append(headers, svgHeader{label: header, x: x - 140, y: 20})
		if len(nodes) > maxRows {
			maxRows = len(nodes)
		}
		for row, node := range nodes {
			result = append(result, svgNode{node: node, depth: column, row: row, x: x, y: 110 + row*170})
		}
	}
	width := 380 * len(keys)
	height := 240
	if rows := 20 + 170*maxRows; rows > height {
		height = rows
	}
	sort.Slice(result, func(i, j int) bool { return result[i].node.ID < result[j].node.ID })
	return result, width, height, headers, nil
}

func commaInt(value int64) string {
	digits := strconv.FormatInt(value, 10)
	if len(digits) <= 3 {
		return digits
	}
	first := len(digits) % 3
	if first == 0 {
		first = 3
	}
	var out strings.Builder
	out.WriteString(digits[:first])
	for index := first; index < len(digits); index += 3 {
		out.WriteString(",")
		out.WriteString(digits[index : index+3])
	}
	return out.String()
}

// svgNodeDates resolves the best known calendar date for each node: block time,
// then purchase date, then a BIP-329 annotation time.
func svgNodeDates(report model.Report) map[string]string {
	purchases := map[string]model.Purchase{}
	for _, purchase := range report.Purchases {
		purchases[purchase.ID] = purchase
	}
	dates := make(map[string]string, len(report.Graph.Nodes))
	for _, node := range report.Graph.Nodes {
		date := ""
		if len(node.BlockTime) >= 10 {
			date = node.BlockTime[:10]
		}
		if date == "" {
			for _, id := range node.PurchaseIDs {
				if purchase, ok := purchases[id]; ok && len(purchase.Date) >= 10 {
					date = purchase.Date[:10]
					break
				}
			}
		}
		if date == "" {
			switch node.Kind {
			case "transaction":
				if len(report.Annotations.Transactions[node.TxID].Time) >= 10 {
					date = report.Annotations.Transactions[node.TxID].Time[:10]
				}
			default:
				if len(report.Annotations.Outpoints[node.Outpoint].Time) >= 10 {
					date = report.Annotations.Outpoints[node.Outpoint].Time[:10]
				} else if len(report.Annotations.Addresses[node.Address].Time) >= 10 {
					date = report.Annotations.Addresses[node.Address].Time[:10]
				}
			}
		}
		dates[node.ID] = date
	}
	return dates
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
	return svgBytes(report, nodes, width, height, nil)
}

// svgNodeHeights resolves the best known block height for each node: the
// graph node height, then a purchase record height, then a BIP-329 annotation.
func svgNodeHeights(report model.Report) map[string]int64 {
	purchases := map[string]model.Purchase{}
	for _, purchase := range report.Purchases {
		purchases[purchase.ID] = purchase
	}
	heights := make(map[string]int64, len(report.Graph.Nodes))
	for _, node := range report.Graph.Nodes {
		height := node.Height
		if height <= 0 {
			for _, id := range node.PurchaseIDs {
				if purchase, ok := purchases[id]; ok && purchase.Height > 0 {
					height = purchase.Height
					break
				}
			}
		}
		if height <= 0 {
			switch node.Kind {
			case "transaction":
				height = report.Annotations.Transactions[node.TxID].Height
			default:
				height = report.Annotations.Outpoints[node.Outpoint].Height
				if height <= 0 {
					height = report.Annotations.Addresses[node.Address].Height
				}
			}
		}
		heights[node.ID] = height
	}
	return heights
}

// SVGBytesHeight produces the same graph on a strictly chronological block axis.
func SVGBytesHeight(report model.Report) ([]byte, error) {
	nodes, width, height, headers, err := layoutGraphHeight(report.Graph, svgNodeHeights(report), svgNodeDates(report))
	if err != nil {
		return nil, err
	}
	return svgBytes(report, nodes, width, height, headers)
}

func svgBytes(report model.Report, nodes []svgNode, width, height int, headers []svgHeader) ([]byte, error) {
	position := map[string]svgNode{}
	for _, node := range nodes {
		position[node.node.ID] = node
	}
	annotations := newSVGAnnotations(report.Annotations)
	purchases := map[string]model.Purchase{}
	for _, purchase := range report.Purchases {
		purchases[purchase.ID] = purchase
	}
	var out strings.Builder
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" width="%d" height="%d" viewBox="0 0 %d %d" role="img" aria-labelledby="title desc">`, width, height, width, height)
	out.WriteString(`<title id="title">Source-of-funds transaction graph</title><desc id="desc">Exact transaction ancestry from current owned UTXOs. Purchase boundaries are evidence supplied by the user. Amounts show BTC and, when the wallet supplied it, the fiat value at transaction time.</desc>`)
	out.WriteString(`<defs><marker id="arrow" markerWidth="8" markerHeight="8" refX="7" refY="4" orient="auto"><path d="M0,0 L8,4 L0,8 z" fill="#777"/></marker><style>text{font-family:monospace;fill:#202124}.node{fill:#fff;stroke:#68707a;stroke-width:2}.owned{stroke:#16803b;stroke-width:4}.purchase{stroke:#ba1676;stroke-width:4}.terminal{fill:#fff2f2;stroke:#b42318}.edge{stroke:#777;stroke-width:1.5;fill:none;marker-end:url(#arrow)}.small{font-size:11px;fill:#4f5358}.tiny{font-size:10px;fill:#475569}.amount{font-size:11px;font-weight:bold}.title{font-size:12px;font-weight:bold}.column-header{font-size:13px;font-weight:bold;fill:#334155}.edge-vertical{stroke:#8a94a3;stroke-width:1.2}</style></defs>`)
	out.WriteString(`<rect width="100%" height="100%" fill="#fafafa"/>`)
	for _, header := range headers {
		fmt.Fprintf(&out, `<text class="column-header" x="%d" y="%d">%s</text>`, header.x, header.y, html.EscapeString(header.label))
	}
	for _, edge := range report.Graph.Edges {
		from, to := position[edge.From], position[edge.To]
		title := html.EscapeString(edge.Kind + " · " + strconv.FormatInt(edge.ValueSats, 10) + " sats")
		if from.x == to.x {
			fmt.Fprintf(&out, `<path class="edge edge-vertical" d="M%d %d L%d %d"><title>%s</title></path>`, from.x+140, from.y, from.x+140, to.y, title)
			continue
		}
		x1, x2 := from.x-150, to.x+150
		if x1 < x2 {
			x1, x2 = from.x+150, to.x-150
		}
		fmt.Fprintf(&out, `<path class="edge" d="M%d %d L%d %d"><title>%s</title></path>`, x1, from.y, x2, to.y, title)
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
		amount := n.ValueSats
		var purchase *model.Purchase
		for _, id := range n.PurchaseIDs {
			if candidate, ok := purchases[id]; ok {
				purchase = &candidate
				break
			}
		}
		if purchase != nil && purchase.AmountSats != nil && (n.Kind == "transaction" || n.ValueSats == 0) {
			amount = *purchase.AmountSats
		}
		if n.Kind != "transaction" || purchase != nil {
			fmt.Fprintf(&out, `<text class="amount" x="%d" y="%d">%s · %d sats</text>`, placed.x-140, placed.y+22, btcString(amount), amount)
		}
		if fiat := fiatString(annotation.Fiat); fiat != "" {
			when := ""
			if len(annotation.Time) >= 10 {
				when = " · " + annotation.Time[:10]
			}
			fmt.Fprintf(&out, `<text class="tiny" x="%d" y="%d">%s%s (wallet-recorded)</text>`, placed.x-140, placed.y+40, html.EscapeString(fiat), html.EscapeString(when))
		} else if purchase != nil {
			fmt.Fprintf(&out, `<text class="tiny" x="%d" y="%d">%s purchase</text>`, placed.x-140, placed.y+40, html.EscapeString(purchase.Provider))
		} else if rate := fiatString(annotation.Rate); rate != "" {
			fmt.Fprintf(&out, `<text class="tiny" x="%d" y="%d">rate %s/BTC (wallet-recorded)</text>`, placed.x-140, placed.y+40, html.EscapeString(rate))
		}
		stamp := ""
		if len(n.BlockTime) >= 10 {
			stamp = n.BlockTime[:10]
		} else if purchase != nil && len(purchase.Date) >= 10 {
			stamp = purchase.Date[:10]
		}
		footer := placed.label
		if stamp != "" && footer != "" {
			footer = stamp + " · " + footer
		} else if stamp != "" {
			footer = stamp
		}
		if footer != "" {
			fmt.Fprintf(&out, `<text class="tiny" x="%d" y="%d">%s</text>`, placed.x-140, placed.y+56, html.EscapeString(footer))
		}
		out.WriteString(`</g>`)
	}
	out.WriteString(`</svg>`)
	return []byte(out.String()), nil
}
