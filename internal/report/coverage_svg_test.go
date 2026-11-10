package report

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

func TestCoverageSVGEmptyGraph(t *testing.T) {
	first, err := SVGBytes(model.Report{})
	if err != nil {
		t.Fatal(err)
	}
	second, err := SVGBytes(model.Report{Graph: model.Graph{Nodes: []model.GraphNode{}, Edges: []model.GraphEdge{}}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Error("nil and empty graph representations produced different SVG")
	}
	text := string(first)
	for _, want := range []string{`width="600" height="180" viewBox="0 0 600 180"`, `<title id="title">`, `<desc id="desc">`, `</svg>`} {
		if !strings.Contains(text, want) {
			t.Errorf("empty SVG missing %q", want)
		}
	}
	if strings.Contains(text, `<rect class="node`) || strings.Contains(text, `class="edge"`) {
		t.Error("empty graph contains a node or edge")
	}
	var document any
	if err := xml.Unmarshal(first, &document); err != nil {
		t.Fatalf("empty graph is not well-formed XML: %v", err)
	}
}

func TestCoverageSVGCompleteLayoutStaticAndDeterministic(t *testing.T) {
	graph := model.Graph{Nodes: []model.GraphNode{
		{ID: "out-current", Kind: "output", Owned: true, Outpoint: strings.Repeat("a", 64) + ":0", Address: "current-address", ValueSats: 125},
		{ID: "tx-current", Kind: "transaction", TxID: strings.Repeat("b", 64), ValueSats: 125, PurchaseIDs: []string{"buy-current"}, TerminalReason: "purchase"},
		{ID: "out-old-b", Kind: "output", Outpoint: strings.Repeat("c", 64) + ":1", PurchaseIDs: []string{"buy-old"}, TerminalReason: "incomplete", TerminalDetail: "node unavailable", ValueSats: 75},
		{ID: "out-old-a", Kind: "output", Outpoint: strings.Repeat("d", 64) + ":0", Address: "old-address", ValueSats: 50},
		{ID: "tx-origin", Kind: "transaction", TxID: strings.Repeat("e", 64), PurchaseIDs: []string{"buy-old", "buy-second"}, ValueSats: 125},
	}, Edges: []model.GraphEdge{
		{From: "out-current", To: "tx-current", Kind: "funded_by", ValueSats: 125},
		{From: "tx-current", To: "out-old-b", Kind: "input", ValueSats: 75},
		{From: "tx-current", To: "out-old-a", Kind: "input", ValueSats: 50},
		{From: "out-old-a", To: "tx-origin", Kind: "funded_by", ValueSats: 50},
		{From: "out-old-b", To: "tx-origin", Kind: "funded_by", ValueSats: 75},
	}}
	report := model.Report{Graph: graph}
	first, err := SVGBytes(report)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		got, err := SVGBytes(report)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, got) {
			t.Fatalf("connected static SVG changed on render %d", i+2)
		}
	}
	var document any
	if err := xml.Unmarshal(first, &document); err != nil {
		t.Fatalf("generated SVG is not well-formed XML: %v", err)
	}
	text := string(first)
	for _, unwanted := range []string{"<script", "javascript:", "onload=", "<foreignObject"} {
		if strings.Contains(strings.ToLower(text), strings.ToLower(unwanted)) {
			t.Errorf("static SVG contains active content %q", unwanted)
		}
	}
	for _, want := range []string{"class=\"node owned\"", "class=\"node purchase\"", "class=\"node purchase terminal\"", "input · 50 sats", "funded_by · 125 sats", "buy-old, buy-second", "node unavailable"} {
		if !strings.Contains(text, want) {
			t.Errorf("complete SVG missing %q", want)
		}
	}
	placed, width, height, err := layoutGraph(graph)
	if err != nil {
		t.Fatal(err)
	}
	if width <= 0 || height <= 0 || len(placed) != len(graph.Nodes) {
		t.Fatalf("layout returned nodes=%d width=%d height=%d", len(placed), width, height)
	}
	depths := map[string]int{}
	rows := map[string]int{}
	for _, node := range placed {
		depths[node.node.ID] = node.depth
		rows[node.node.ID] = node.row
	}
	for id, want := range map[string]int{"out-current": 0, "tx-current": 1, "out-old-a": 2, "out-old-b": 2, "tx-origin": 3} {
		if depths[id] != want {
			t.Errorf("depth[%s] = %d, want %d", id, depths[id], want)
		}
	}
	if rows["out-old-a"] >= rows["out-old-b"] {
		t.Errorf("same-depth rows are not sorted by ID: a=%d b=%d", rows["out-old-a"], rows["out-old-b"])
	}
}

func TestCoverageSVGDisconnectedLayoutDeterministicAndComplete(t *testing.T) {
	report := model.Report{Graph: model.Graph{Nodes: []model.GraphNode{
		{ID: "owned", Kind: "output", Owned: true, Label: "selected output"},
		{ID: "funding", Kind: "transaction", Label: "funding transaction"},
		{ID: "detached-a", Kind: "transaction", Label: "disconnected A"},
		{ID: "detached-b", Kind: "output", Label: "disconnected B"},
		{ID: "detached-c", Kind: "transaction", Label: "disconnected C"},
	}, Edges: []model.GraphEdge{
		{From: "owned", To: "funding", Kind: "funded_by", ValueSats: 1},
	}}}
	first, err := SVGBytes(report)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		next, err := SVGBytes(report)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, next) {
			t.Fatalf("disconnected graph layout changed at render %d\nfirst:\n%s\nnext:\n%s", i+2, first, next)
		}
	}
	placed, _, _, err := layoutGraph(report.Graph)
	if err != nil {
		t.Fatal(err)
	}
	if len(placed) != len(report.Graph.Nodes) {
		t.Fatalf("layout has %d nodes, want all %d including disconnected nodes", len(placed), len(report.Graph.Nodes))
	}
	for _, node := range placed {
		if node.x < 105 || node.y < 38 {
			t.Errorf("disconnected node %q is clipped at (%d,%d)", node.node.ID, node.x, node.y)
		}
	}
	text := string(first)
	for _, id := range []string{"selected output", "funding transaction", "disconnected A", "disconnected B", "disconnected C"} {
		if !strings.Contains(text, id) {
			t.Errorf("disconnected SVG lost node %q", id)
		}
	}
}

func TestCoverageSVGDisconnectedOwnedComponentsDeterministic(t *testing.T) {
	report := model.Report{Graph: model.Graph{Nodes: []model.GraphNode{
		{ID: "owned-c", Kind: "output", Owned: true},
		{ID: "owned-a", Kind: "output", Owned: true},
		{ID: "owned-b", Kind: "output", Owned: true},
	}}}
	first, err := SVGBytes(report)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		next, err := SVGBytes(report)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, next) {
			t.Fatalf("static SVG for disconnected owned components changed at render %d", i+2)
		}
	}
}

func TestCoverageSVGRejectsCycles(t *testing.T) {
	report := model.Report{Graph: model.Graph{
		Nodes: []model.GraphNode{{ID: "a", Kind: "output", Owned: true}, {ID: "b", Kind: "transaction"}},
		Edges: []model.GraphEdge{{From: "a", To: "b", Kind: "funded_by"}, {From: "b", To: "a", Kind: "input"}},
	}}
	_, err := SVGBytes(report)
	if err == nil || !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("cyclic graph error = %v", err)
	}
}

func TestCoverageSVGDisconnectedEdgeLayout(t *testing.T) {
	report := model.Report{Graph: model.Graph{
		Nodes: []model.GraphNode{{ID: "b", Kind: "transaction"}, {ID: "a", Kind: "output"}},
		Edges: []model.GraphEdge{{From: "a", To: "b", Kind: "input", ValueSats: 9}},
	}}
	first, err := SVGBytes(report)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 30; i++ {
		next, err := SVGBytes(report)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(first, next) {
			t.Fatalf("SVG with a disconnected edge changed at render %d", i+2)
		}
	}
	if !strings.Contains(string(first), "input · 9 sats") {
		t.Error("disconnected edge was not drawn")
	}
}

func TestCoverageSVGLongTextEscapingAndFallbacks(t *testing.T) {
	longLabel := `12345678901234<middle>` + strings.Repeat("x", 30) + `1234567`
	scriptLabel := `</script><script>alert("svg")</script>&`
	report := model.Report{Graph: model.Graph{Nodes: []model.GraphNode{
		{ID: "label-node", Kind: "output", Owned: true, Label: longLabel, Address: scriptLabel, PurchaseIDs: []string{"buy"}, TerminalReason: "incomplete", TerminalDetail: `<detail>&`},
		{ID: "outpoint-node", Kind: "output", Outpoint: "outpoint-fallback", TerminalReason: "reason-detail"},
		{ID: "txid-node", Kind: "transaction", TxID: "txid-fallback"},
		{ID: "id-fallback", Kind: "transaction"},
	}, Edges: []model.GraphEdge{
		{From: "label-node", To: "outpoint-node", Kind: "input", ValueSats: -5},
	},
	},
	}
	data, err := SVGBytes(report)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{
		html.EscapeString(shortID(longLabel)),
		html.EscapeString(shortID(scriptLabel)),
		"outpoint-fallback",
		"reason-detail",
		"txid-fallback",
		">id-fallback</text>",
		"input · -5 sats",
		html.EscapeString("<detail>&"),
	} {
		if !strings.Contains(text, want) {
			t.Errorf("SVG missing %q", want)
		}
	}
	if strings.Contains(text, scriptLabel) || strings.Contains(strings.ToLower(text), "<script") {
		t.Error("SVG contains unescaped user-supplied script markup")
	}
	var document any
	if err := xml.Unmarshal(data, &document); err != nil {
		t.Fatalf("escaped SVG is not well-formed XML: %v", err)
	}
	if got := shortID("123456789012345678901234"); got != "123456789012345678901234" {
		t.Errorf("24-byte ID was shortened: %q", got)
	}
}

func TestCoverageSVGDepthLimit(t *testing.T) {
	makeGraph := func(chainDepth int, disconnected bool) model.Graph {
		chainNodes := chainDepth + 1
		nodeCount := chainNodes
		if disconnected {
			nodeCount++
		}
		graph := model.Graph{Nodes: make([]model.GraphNode, nodeCount), Edges: make([]model.GraphEdge, chainDepth)}
		for i := range graph.Nodes {
			id := fmt.Sprintf("node-%05d", i)
			graph.Nodes[i] = model.GraphNode{ID: id, Kind: "transaction", Owned: i == 0}
			if i > 0 && i < chainNodes {
				graph.Edges[i-1] = model.GraphEdge{From: fmt.Sprintf("node-%05d", i-1), To: id, Kind: "input"}
			}
		}
		return graph
	}
	for _, test := range []struct {
		name         string
		chainDepth   int
		disconnected bool
	}{
		{name: "reachable chain", chainDepth: 10001},
		{name: "disconnected column beyond limit", chainDepth: 10000, disconnected: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, _, _, err := layoutGraph(makeGraph(test.chainDepth, test.disconnected))
			if err == nil || !strings.Contains(err.Error(), "graph depth limit exceeded") {
				t.Fatalf("depth-limit error = %v", err)
			}
		})
	}
}
