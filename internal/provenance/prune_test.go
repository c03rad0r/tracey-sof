package provenance

import (
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

func TestPruneBoundaryOutputs(t *testing.T) {
	txA, txB, txC := strings.Repeat("a", 64), strings.Repeat("b", 64), strings.Repeat("c", 64)
	g := newEvaluationGraph(model.Trace{})
	g.transaction(txB)
	output := func(n uint32) *model.GraphNode {
		return g.output(model.Outpoint{TxID: txA, Vout: n})
	}
	owned := output(0)
	owned.Owned = true
	referenced := output(1)
	pruned := output(2)
	terminal := output(3)
	terminal.TerminalReason = "max_depth"
	orphan := output(4)
	nonTxParent := output(5)
	for _, node := range []*model.GraphNode{owned, referenced, pruned, terminal} {
		g.addEdge(node.ID, "tx:"+txB, "funded_by")
	}
	g.addEdge(nonTxParent.ID, "out:not-a-transaction", "funded_by")
	g.addEdge("tx:"+txC, referenced.ID, "input")
	g.boundaries["tx:"+txB] = model.Boundary{NodeID: "tx:" + txB, Reason: "policy_output_count"}
	g.pruneBoundaryOutputs()
	for _, node := range []*model.GraphNode{owned, referenced, terminal, orphan, nonTxParent} {
		if _, exists := g.nodes[node.ID]; !exists {
			t.Errorf("kept output %s was pruned", node.ID)
		}
	}
	if _, exists := g.nodes[pruned.ID]; exists {
		t.Errorf("boundary sibling %s was retained", pruned.ID)
	}
	if _, exists := g.nestedEdge(pruned.ID); exists {
		t.Error("pruned output still has an edge")
	}
}

func (g *evaluationGraph) nestedEdge(id string) (model.GraphEdge, bool) {
	for _, edge := range g.edges {
		if edge.From == id || edge.To == id {
			return edge, true
		}
	}
	return model.GraphEdge{}, false
}

func TestPruneBoundaryOutputsKeepsOutpointBoundaryAndExpanded(t *testing.T) {
	txOut, txExpanded := strings.Repeat("d", 64), strings.Repeat("e", 64)
	other := strings.Repeat("f", 64)
	g := newEvaluationGraph(model.Trace{})
	g.transaction(txOut)
	first := g.output(model.Outpoint{TxID: txOut, Vout: 0})
	sibling := g.output(model.Outpoint{TxID: txOut, Vout: 1})
	g.addEdge(first.ID, "tx:"+txOut, "funded_by")
	g.addEdge(sibling.ID, "tx:"+txOut, "funded_by")
	if err := addBoundaryToGraph(g, model.Boundary{NodeID: first.ID, Reason: reasonPurchase, Outpoint: first.Outpoint, PurchaseIDs: []string{"purchase-1"}}); err != nil {
		t.Fatal(err)
	}
	g.pruneBoundaryOutputs()
	if _, exists := g.nodes[first.ID]; !exists {
		t.Error("boundary output was pruned")
	}
	if _, exists := g.nodes[sibling.ID]; exists {
		t.Error("unexpanded purchase sibling was retained")
	}

	expanded := newEvaluationGraph(model.Trace{})
	expanded.transaction(txExpanded)
	boundary := expanded.output(model.Outpoint{TxID: txExpanded, Vout: 0})
	kept := expanded.output(model.Outpoint{TxID: txExpanded, Vout: 1})
	prevout := expanded.output(model.Outpoint{TxID: other, Vout: 0})
	expanded.addEdge(boundary.ID, "tx:"+txExpanded, "funded_by")
	expanded.addEdge(kept.ID, "tx:"+txExpanded, "funded_by")
	expanded.addEdge("tx:"+txExpanded, prevout.ID, "input")
	if err := addBoundaryToGraph(expanded, model.Boundary{NodeID: boundary.ID, Reason: reasonPurchase, Outpoint: boundary.Outpoint, PurchaseIDs: []string{"purchase-2"}}); err != nil {
		t.Fatal(err)
	}
	expanded.pruneBoundaryOutputs()
	if _, exists := expanded.nodes[kept.ID]; !exists {
		t.Error("output of an expanded transaction was pruned")
	}
}
