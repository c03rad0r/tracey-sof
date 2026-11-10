package provenance

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"tracey-sof/internal/model"
)

func TestEvaluationGraphNodeIdentity(t *testing.T) {
	graph := newEvaluationGraph(model.Trace{})
	transaction := graph.transaction("transaction")
	if transaction.Kind != "transaction" || transaction.TxID != "transaction" || graph.transaction("transaction") != transaction {
		t.Fatalf("transaction node identity = %+v", transaction)
	}
	output := graph.output(testOutpoint("transaction", 2))
	if output.Kind != "output" || output.Outpoint != "transaction:2" || graph.output(testOutpoint("transaction", 2)) != output {
		t.Fatalf("output node identity = %+v", output)
	}
}

func TestEvaluationGraphMissingNodeAndMemoizedSharedBranches(t *testing.T) {
	t.Run("missing target node", func(t *testing.T) {
		graph := newEvaluationGraph(model.Trace{})
		if _, err := graph.evaluateTarget(testTarget("missing", 0)); err == nil {
			t.Fatal("missing graph node did not return an error")
		}
	})
	t.Run("DAG shared ancestry", func(t *testing.T) {
		txA, txB, txC, txD := testTxID("a"), testTxID("b"), testTxID("c"), testTxID("d")
		trace := model.Trace{
			Targets: []model.TargetRef{testTarget(txA, 0)},
			Transactions: map[string]model.Transaction{
				txA: testTransaction(txA, []model.Input{testInput(txB, 0), testInput(txC, 0)}),
				txB: testTransaction(txB, []model.Input{testInput(txD, 0)}),
				txC: testTransaction(txC, []model.Input{testInput(txD, 0)}),
				txD: testTransaction(txD, []model.Input{{Coinbase: true}}),
			},
			Edges: []model.TraceEdge{
				{SpendingTxID: txA, InputIndex: 0, PrevOut: testOutpoint(txB, 0)},
				{SpendingTxID: txA, InputIndex: 1, PrevOut: testOutpoint(txC, 0)},
				{SpendingTxID: txB, InputIndex: 0, PrevOut: testOutpoint(txD, 0)},
				{SpendingTxID: txC, InputIndex: 0, PrevOut: testOutpoint(txD, 0)},
			},
			Boundaries: []model.Boundary{{NodeID: transactionNodeID(txD), Reason: reasonCoinbase}},
		}
		report, err := Evaluate(trace, nil, model.AnnotationFile{}, time.Time{})
		if err != nil {
			t.Fatal(err)
		}
		if report.UTXOs[0].Status != undocumentedStatus || report.UTXOs[0].Trace != completeTraceStatus {
			t.Fatalf("shared nonpurchase DAG = %+v", report.UTXOs[0])
		}
	})
}

func TestFinalizedGraphSortsSameEndpointsByKind(t *testing.T) {
	graph := newEvaluationGraph(model.Trace{})
	graph.nodes["from"] = &model.GraphNode{ID: "from", Kind: "transaction"}
	graph.nodes["to"] = &model.GraphNode{ID: "to", Kind: "output"}
	graph.addEdge("from", "to", "input")
	graph.addEdge("from", "to", "funded_by")
	final := finalizedGraph(graph)
	if len(final.Edges) != 2 || final.Edges[0].Kind != "funded_by" || final.Edges[1].Kind != "input" {
		t.Fatalf("edge kind tie was not sorted: %+v", final.Edges)
	}
}

func TestBoundaryHelpersRejectRemainingMalformedValues(t *testing.T) {
	if _, err := parseGraphOutpoint("tx:not-a-number"); err == nil || !strings.Contains(err.Error(), "invalid graph outpoint") {
		t.Fatalf("nonnumeric outpoint error = %v", err)
	}
	graph := newEvaluationGraph(model.Trace{})
	if err := addBoundaryToGraph(graph, model.Boundary{NodeID: "tx:", Reason: reasonCoinbase}); err == nil {
		t.Fatal("empty transaction boundary accepted")
	}
}

func TestTracerPrimitiveDeduplicationAndSorting(t *testing.T) {
	txA, txB, txC := testTxID("a"), testTxID("b"), testTxID("c")
	tracer := &tracer{edges: make(map[string]model.TraceEdge)}
	edge := model.TraceEdge{SpendingTxID: txA, InputIndex: 0, PrevOut: testOutpoint(txB, 0)}
	tracer.addEdge(edge)
	tracer.addEdge(edge)
	if len(tracer.trace.Edges) != 1 {
		t.Fatalf("duplicate edge added: %v", tracer.trace.Edges)
	}
	tracer.trace.Edges = []model.TraceEdge{
		{SpendingTxID: txB, InputIndex: 0, PrevOut: testOutpoint(txA, 0)},
		{SpendingTxID: txA, InputIndex: 2, PrevOut: testOutpoint(txA, 0)},
		{SpendingTxID: txA, InputIndex: 1, PrevOut: testOutpoint(txC, 2)},
		{SpendingTxID: txA, InputIndex: 1, PrevOut: testOutpoint(txB, 2)},
		{SpendingTxID: txA, InputIndex: 1, PrevOut: testOutpoint(txB, 1)},
	}
	tracer.trace.Boundaries = []model.Boundary{
		{NodeID: "z", Reason: "z"},
		{NodeID: "a", Reason: "b"},
		{NodeID: "a", Reason: "a", Detail: "z"},
		{NodeID: "a", Reason: "a", Detail: "a"},
	}
	tracer.sortResults()
	gotEdges := make([]string, 0, len(tracer.trace.Edges))
	for _, edge := range tracer.trace.Edges {
		gotEdges = append(gotEdges, edge.SpendingTxID+":"+string(rune('0'+edge.InputIndex))+":"+edge.PrevOut.String())
	}
	wantOrder := []model.Outpoint{testOutpoint(txB, 1), testOutpoint(txB, 2), testOutpoint(txC, 2)}
	for index, want := range wantOrder {
		if tracer.trace.Edges[index].PrevOut != want {
			t.Fatalf("edge sort order = %v", gotEdges)
		}
	}
	gotBoundaries := make([]string, 0, len(tracer.trace.Boundaries))
	for _, boundary := range tracer.trace.Boundaries {
		gotBoundaries = append(gotBoundaries, boundary.NodeID+boundary.Reason+boundary.Detail)
	}
	if want := []string{"aaa", "aaz", "ab", "zz"}; !reflect.DeepEqual(gotBoundaries, want) {
		t.Fatalf("boundary sort = %v, want %v", gotBoundaries, want)
	}
}
