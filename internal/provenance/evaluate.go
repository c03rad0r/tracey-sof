package provenance

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"tracey-sof/internal/model"
)

const (
	documentedStatus          = "documented"
	partiallyDocumentedStatus = "partially_documented"
	undocumentedStatus        = "undocumented"
	completeTraceStatus       = "complete"
	incompleteTraceStatus     = "incomplete"
)

type graphEdgeKey struct {
	from string
	to   string
	kind string
}

type evaluationGraph struct {
	nodes        map[string]*model.GraphNode
	edges        map[graphEdgeKey]model.GraphEdge
	outgoing     map[string][]string
	boundaries   map[string]model.Boundary
	transactions map[string]model.Transaction
}

func newEvaluationGraph(trace model.Trace) *evaluationGraph {
	return &evaluationGraph{
		nodes:        make(map[string]*model.GraphNode),
		edges:        make(map[graphEdgeKey]model.GraphEdge),
		outgoing:     make(map[string][]string),
		boundaries:   make(map[string]model.Boundary),
		transactions: trace.Transactions,
	}
}

func (g *evaluationGraph) transaction(txid string) *model.GraphNode {
	return g.ensureNode(transactionNodeID(txid), "transaction")
}

func (g *evaluationGraph) output(outpoint model.Outpoint) *model.GraphNode {
	return g.ensureNode(outpointNodeID(outpoint), "output")
}

func (g *evaluationGraph) ensureNode(id, kind string) *model.GraphNode {
	if node, exists := g.nodes[id]; exists {
		return node
	}
	node := &model.GraphNode{ID: id, Kind: kind}
	if kind == "transaction" {
		node.TxID = strings.TrimPrefix(id, "tx:")
	} else {
		node.Outpoint = strings.TrimPrefix(id, "out:")
	}
	g.nodes[id] = node
	return node
}

func (g *evaluationGraph) addEdge(from, to, kind string) {
	key := graphEdgeKey{from: from, to: to, kind: kind}
	g.edges[key] = model.GraphEdge{From: from, To: to, Kind: kind}
	g.outgoing[from] = append(g.outgoing[from], to)
}

func sortedTransactionIDs(transactions map[string]model.Transaction) []string {
	ids := make([]string, 0, len(transactions))
	for id := range transactions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func parseGraphOutpoint(value string) (model.Outpoint, error) {
	separator := strings.LastIndexByte(value, ':')
	if separator <= 0 || separator == len(value)-1 {
		return model.Outpoint{}, fmt.Errorf("invalid graph outpoint %q", value)
	}
	index, err := strconv.ParseUint(value[separator+1:], 10, 32)
	if err != nil {
		return model.Outpoint{}, fmt.Errorf("invalid graph outpoint %q: %w", value, err)
	}
	outpoint := model.Outpoint{TxID: value[:separator], Vout: uint32(index)}
	if outpoint.String() != value {
		return model.Outpoint{}, fmt.Errorf("non-canonical graph outpoint %q", value)
	}
	return outpoint, nil
}

func sortedUnique(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, value := range values {
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func addBoundaryToGraph(g *evaluationGraph, boundary model.Boundary) error {
	if boundary.NodeID == "" || boundary.Reason == "" {
		return fmt.Errorf("boundary requires a node_id and reason")
	}
	if _, exists := g.boundaries[boundary.NodeID]; exists {
		return fmt.Errorf("duplicate boundary for graph node %q", boundary.NodeID)
	}
	var node *model.GraphNode
	switch {
	case strings.HasPrefix(boundary.NodeID, "tx:"):
		txid := strings.TrimPrefix(boundary.NodeID, "tx:")
		if txid == "" {
			return fmt.Errorf("transaction boundary has an empty txid")
		}
		if boundary.Transaction != "" && boundary.Transaction != txid {
			return fmt.Errorf("boundary %q has inconsistent transaction %q", boundary.NodeID, boundary.Transaction)
		}
		node = g.transaction(txid)
	case strings.HasPrefix(boundary.NodeID, "out:"):
		outpointText := strings.TrimPrefix(boundary.NodeID, "out:")
		outpoint, parseErr := parseGraphOutpoint(outpointText)
		if parseErr != nil {
			return parseErr
		}
		if boundary.Outpoint != "" && boundary.Outpoint != outpointText {
			return fmt.Errorf("boundary %q has inconsistent outpoint %q", boundary.NodeID, boundary.Outpoint)
		}
		node = g.output(outpoint)
	default:
		return fmt.Errorf("boundary %q has an invalid node ID", boundary.NodeID)
	}
	if boundary.Reason == reasonPurchase {
		if len(boundary.PurchaseIDs) == 0 {
			return fmt.Errorf("purchase boundary %q has no purchase IDs", boundary.NodeID)
		}
		for _, id := range boundary.PurchaseIDs {
			if id == "" {
				return fmt.Errorf("purchase boundary %q has an empty purchase ID", boundary.NodeID)
			}
		}
	} else if len(boundary.PurchaseIDs) != 0 {
		return fmt.Errorf("non-purchase boundary %q unexpectedly has purchase IDs", boundary.NodeID)
	}
	boundary.PurchaseIDs = sortedUnique(boundary.PurchaseIDs)
	g.boundaries[boundary.NodeID] = boundary
	node.PurchaseIDs = append([]string(nil), boundary.PurchaseIDs...)
	node.TerminalReason = boundary.Reason
	node.TerminalDetail = boundary.Detail
	return nil
}

func nonCoinbaseInputCount(transaction model.Transaction) int {
	count := 0
	for _, input := range transaction.Inputs {
		if !input.Coinbase {
			count++
		}
	}
	return count
}

func buildEvaluationGraph(trace model.Trace, annotations model.AnnotationFile) (*evaluationGraph, error) {
	g := newEvaluationGraph(trace)
	for _, mapTxID := range sortedTransactionIDs(trace.Transactions) {
		transaction := trace.Transactions[mapTxID]
		if transaction.TxID == "" || transaction.TxID != mapTxID {
			return nil, fmt.Errorf("transaction map key %q does not match transaction txid %q", mapTxID, transaction.TxID)
		}
		g.transaction(mapTxID)
		seenOutputs := make(map[uint32]struct{}, len(transaction.Outputs))
		for _, output := range transaction.Outputs {
			if _, exists := seenOutputs[output.Index]; exists {
				return nil, fmt.Errorf("transaction %s has duplicate output index %d", mapTxID, output.Index)
			}
			seenOutputs[output.Index] = struct{}{}
			if output.ValueSats < 0 {
				return nil, fmt.Errorf("transaction %s output %d has a negative value", mapTxID, output.Index)
			}
			outpoint := model.Outpoint{TxID: mapTxID, Vout: output.Index}
			node := g.output(outpoint)
			node.Address = output.Address
			node.ValueSats = output.ValueSats
			node.ScriptType = output.ScriptType
			g.addEdge(node.ID, transactionNodeID(mapTxID), "funded_by")
		}
	}

	seenInputs := make(map[string]struct{}, len(trace.Edges))
	for _, edge := range trace.Edges {
		transaction, exists := trace.Transactions[edge.SpendingTxID]
		if !exists {
			return nil, fmt.Errorf("trace edge refers to missing spending transaction %q", edge.SpendingTxID)
		}
		if edge.InputIndex < 0 || edge.InputIndex >= len(transaction.Inputs) {
			return nil, fmt.Errorf("trace edge has invalid input index %d for transaction %s", edge.InputIndex, edge.SpendingTxID)
		}
		input := transaction.Inputs[edge.InputIndex]
		if input.Coinbase || input.PrevOut != edge.PrevOut {
			return nil, fmt.Errorf("trace edge does not match transaction %s input %d", edge.SpendingTxID, edge.InputIndex)
		}
		key := fmt.Sprintf("%s\x00%d", edge.SpendingTxID, edge.InputIndex)
		if _, exists := seenInputs[key]; exists {
			return nil, fmt.Errorf("duplicate trace edge for transaction %s input %d", edge.SpendingTxID, edge.InputIndex)
		}
		seenInputs[key] = struct{}{}
		if edge.PrevOut.TxID == "" {
			return nil, fmt.Errorf("trace edge for transaction %s input %d has an empty previous-output txid", edge.SpendingTxID, edge.InputIndex)
		}
		node := g.output(edge.PrevOut)
		g.addEdge(transactionNodeID(edge.SpendingTxID), node.ID, "input")
	}

	for _, target := range trace.Targets {
		if target.TxID == "" {
			return nil, fmt.Errorf("target has an empty txid")
		}
		if target.ValueSats < 0 {
			return nil, fmt.Errorf("target %s has a negative value", target.Key())
		}
		node := g.output(model.Outpoint{TxID: target.TxID, Vout: target.Vout})
		node.Owned = true
		if target.Address != "" {
			node.Address = target.Address
		}
		if target.ValueSats != 0 || node.ValueSats == 0 {
			node.ValueSats = target.ValueSats
		}
	}

	for _, boundary := range trace.Boundaries {
		if err := addBoundaryToGraph(g, boundary); err != nil {
			return nil, err
		}
	}

	for _, node := range g.nodes {
		if node.Kind == "transaction" {
			node.Label = annotations.Transactions[node.TxID].Label
			continue
		}
		outpointLabel := annotations.Outpoints[node.Outpoint].Label
		addressLabel := annotations.Addresses[node.Address].Label
		if outpointLabel != "" {
			node.Label = outpointLabel
		} else {
			node.Label = addressLabel
		}
	}
	for key, edge := range g.edges {
		if edge.Kind == "funded_by" {
			edge.ValueSats = g.nodes[edge.From].ValueSats
		} else {
			edge.ValueSats = g.nodes[edge.To].ValueSats
		}
		g.edges[key] = edge
	}
	for id := range g.outgoing {
		sort.Strings(g.outgoing[id])
	}
	return g, nil
}

type branchResult struct {
	purchaseIDs map[string]struct{}
	other       bool
	incomplete  bool
	cycle       bool
}

func newBranchResult() branchResult {
	return branchResult{purchaseIDs: make(map[string]struct{})}
}

func (r *branchResult) merge(other branchResult) {
	for id := range other.purchaseIDs {
		r.purchaseIDs[id] = struct{}{}
	}
	r.other = r.other || other.other
	r.incomplete = r.incomplete || other.incomplete
	r.cycle = r.cycle || other.cycle
}

func (g *evaluationGraph) evaluateTarget(target model.TargetRef) (branchResult, error) {
	const (
		visiting = 1
		done     = 2
	)
	states := make(map[string]int)
	memo := make(map[string]branchResult)
	var visit func(string) (branchResult, error)
	visit = func(nodeID string) (branchResult, error) {
		if boundary, exists := g.boundaries[nodeID]; exists {
			result := newBranchResult()
			switch boundary.Reason {
			case reasonPurchase:
				for _, id := range boundary.PurchaseIDs {
					result.purchaseIDs[id] = struct{}{}
				}
			case reasonCoinbase:
				result.other = true
			default:
				result.other = true
				result.incomplete = true
			}
			return result, nil
		}
		if states[nodeID] == visiting {
			result := newBranchResult()
			result.other = true
			result.incomplete = true
			result.cycle = true
			return result, nil
		}
		if states[nodeID] == done {
			return memo[nodeID], nil
		}
		node := g.nodes[nodeID]
		if node == nil {
			return branchResult{}, fmt.Errorf("graph refers to missing node %q", nodeID)
		}
		children := g.outgoing[nodeID]
		if node.Kind == "output" && len(children) != 1 {
			return branchResult{}, fmt.Errorf("reachable output %q has no unique funding transaction or terminal boundary", nodeID)
		}
		if node.Kind == "transaction" {
			transaction := g.transactions[node.TxID]
			expected := nonCoinbaseInputCount(transaction)
			if len(children) != expected {
				return branchResult{}, fmt.Errorf("reachable transaction %q has %d graph inputs, expected %d", nodeID, len(children), expected)
			}
			if expected == 0 {
				return branchResult{}, fmt.Errorf("reachable transaction %q has no origin", nodeID)
			}
		}
		states[nodeID] = visiting
		result := newBranchResult()
		for _, child := range children {
			childResult, err := visit(child)
			if err != nil {
				return branchResult{}, err
			}
			result.merge(childResult)
		}
		states[nodeID] = done
		memo[nodeID] = result
		return result, nil
	}
	return visit(outpointNodeID(model.Outpoint{TxID: target.TxID, Vout: target.Vout}))
}

func boundaryWarning(boundary model.Boundary) string {
	kind := "incomplete provenance boundary"
	if boundary.Reason == reasonCoinbase {
		kind = "non-purchase terminal"
	}
	warning := fmt.Sprintf("%s: %s: %s", boundary.NodeID, kind, boundary.Reason)
	if boundary.Detail != "" {
		warning += ": " + boundary.Detail
	}
	return warning
}

func addTargetSats(total, value int64) (int64, error) {
	if value > 0 && total > math.MaxInt64-value {
		return 0, fmt.Errorf("satoshis total overflows int64")
	}
	return total + value, nil
}

func finalizedGraph(g *evaluationGraph) model.Graph {
	graph := model.Graph{
		Nodes: make([]model.GraphNode, 0, len(g.nodes)),
		Edges: make([]model.GraphEdge, 0, len(g.edges)),
	}
	for _, node := range g.nodes {
		graph.Nodes = append(graph.Nodes, *node)
	}
	for _, edge := range g.edges {
		graph.Edges = append(graph.Edges, edge)
	}
	sort.Slice(graph.Nodes, func(i, j int) bool { return graph.Nodes[i].ID < graph.Nodes[j].ID })
	sort.Slice(graph.Edges, func(i, j int) bool {
		left, right := graph.Edges[i], graph.Edges[j]
		if left.From != right.From {
			return left.From < right.From
		}
		if left.To != right.To {
			return left.To < right.To
		}
		return left.Kind < right.Kind
	})
	return graph
}

// Evaluate converts an exact trace into an independent per-target assessment
// and a stable, complete report. No satoshis are allocated to purchase inputs.
func Evaluate(trace model.Trace, purchases []model.Purchase, annotations model.AnnotationFile, generatedAt time.Time) (model.Report, error) {
	graph, err := buildEvaluationGraph(trace, annotations)
	if err != nil {
		return model.Report{}, err
	}
	report := model.Report{
		SchemaVersion: model.SchemaVersion,
		Network:       trace.Network,
		GeneratedAt:   generatedAt.UTC().Format(time.RFC3339),
		Chain: model.Chain{
			BestBlock:  trace.BestBlock,
			BestHeight: trace.BestHeight,
		},
		Warnings:    make([]string, 0),
		Purchases:   append([]model.Purchase(nil), purchases...),
		UTXOs:       make([]model.Target, 0, len(trace.Targets)),
		Annotations: annotations,
	}
	warningSet := make(map[string]struct{}, len(trace.Boundaries))
	for _, boundary := range trace.Boundaries {
		if boundary.Reason != reasonPurchase {
			warningSet[boundaryWarning(boundary)] = struct{}{}
		}
	}

	for _, targetRef := range trace.Targets {
		result, err := graph.evaluateTarget(targetRef)
		if err != nil {
			return model.Report{}, fmt.Errorf("evaluate target %s: %w", targetRef.Key(), err)
		}
		target := model.Target{
			Outpoint:  model.Outpoint{TxID: targetRef.TxID, Vout: targetRef.Vout},
			TxID:      targetRef.TxID,
			Vout:      targetRef.Vout,
			Address:   targetRef.Address,
			ValueSats: targetRef.ValueSats,
			OwnerIDs:  append([]string(nil), targetRef.OwnerIDs...),
			Labels:    append([]string(nil), targetRef.Labels...),
			Trace:     completeTraceStatus,
		}
		var ids []string
		if len(result.purchaseIDs) > 0 {
			ids = make([]string, 0, len(result.purchaseIDs))
			for id := range result.purchaseIDs {
				ids = append(ids, id)
			}
			sort.Strings(ids)
		}
		target.PurchaseIDs = ids
		switch {
		case len(ids) == 0:
			target.Status = undocumentedStatus
		case result.other:
			target.Status = partiallyDocumentedStatus
		default:
			target.Status = documentedStatus
		}
		if result.incomplete {
			target.Trace = incompleteTraceStatus
		}
		if result.cycle {
			warningSet[targetRef.Key()+": cycle detected while evaluating provenance"] = struct{}{}
		}

		report.Summary.TotalUTXOs++
		report.Summary.TotalSats, err = addTargetSats(report.Summary.TotalSats, target.ValueSats)
		if err != nil {
			return model.Report{}, err
		}
		switch target.Status {
		case documentedStatus:
			report.Summary.DocumentedUTXOs++
			// This subset cannot overflow after TotalSats was checked above.
			report.Summary.DocumentedSats += target.ValueSats
		case partiallyDocumentedStatus:
			report.Summary.PartialUTXOs++
		case undocumentedStatus:
			report.Summary.UndocumentedUTXOs++
		}
		if target.Trace == incompleteTraceStatus {
			report.Summary.IncompleteUTXOs++
		}
		report.UTXOs = append(report.UTXOs, target)
	}

	sort.SliceStable(report.UTXOs, func(i, j int) bool {
		if report.UTXOs[i].TxID != report.UTXOs[j].TxID {
			return report.UTXOs[i].TxID < report.UTXOs[j].TxID
		}
		return report.UTXOs[i].Vout < report.UTXOs[j].Vout
	})
	for warning := range warningSet {
		report.Warnings = append(report.Warnings, warning)
	}
	sort.Strings(report.Warnings)
	report.Graph = finalizedGraph(graph)
	return report, nil
}
