package report

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"tracey-sof/internal/model"
)

// ExchangeGap describes an exchange-side transaction that still needs a
// purchase or sale record before provenance can be called complete.
type ExchangeGap struct {
	Direction      string
	DateUTC        string
	DateTime       string
	Height         int64
	TxID           string
	Outpoint       string
	AmountSats     int64
	Counterparty   string
	Label          string
	BoundaryReason string
	EvidenceStatus string
	Suggested      string
	Targets        []string
}

func exchangeProviders() []string {
	// Exchange labels are private user assertions. There is deliberately no
	// built-in provider list in the public repository: set SOF_EXCHANGE_LABELS
	// (for example in the private node.env) to enable outbound gap detection.
	raw := strings.TrimSpace(os.Getenv("SOF_EXCHANGE_LABELS"))
	providers := make([]string, 0)
	for _, part := range strings.Split(raw, ",") {
		if value := strings.TrimSpace(part); value != "" {
			providers = append(providers, strings.ToLower(value))
		}
	}
	return providers
}

func gapDate(node model.GraphNode) string {
	if len(node.BlockTime) >= 10 {
		return node.BlockTime[:10]
	}
	return ""
}

func nodeLabel(node model.GraphNode) string {
	return node.Label
}

// targetAncestry maps every graph node to the current UTXOs that reach it.
func targetAncestry(report model.Report) map[string][]string {
	nodes := make(map[string]model.GraphNode, len(report.Graph.Nodes))
	for _, node := range report.Graph.Nodes {
		nodes[node.ID] = node
	}
	outgoing := make(map[string][]string, len(report.Graph.Edges))
	for _, edge := range report.Graph.Edges {
		outgoing[edge.From] = append(outgoing[edge.From], edge.To)
	}
	result := map[string][]string{}
	for _, node := range report.Graph.Nodes {
		if !node.Owned {
			continue
		}
		seen := map[string]bool{node.ID: true}
		queue := []string{node.ID}
		for head := 0; head < len(queue); head++ {
			for _, next := range outgoing[queue[head]] {
				if !seen[next] {
					seen[next] = true
					queue = append(queue, next)
				}
			}
		}
		for id := range seen {
			result[id] = append(result[id], node.Outpoint)
		}
	}
	for id := range result {
		sort.Strings(result[id])
	}
	return result
}

// ExchangeGaps lists policy-stopped exchange-side payouts that have no matched
// purchase evidence. The list is empty when every ancestor branch reaches a
// documented purchase or coinbase.
func ExchangeGaps(report model.Report) []ExchangeGap {
	ancestry := targetAncestry(report)
	children := map[string][]model.GraphNode{}
	for _, edge := range report.Graph.Edges {
		if edge.Kind == "funded_by" {
			children[edge.To] = append(children[edge.To], graphNodeByID(report, edge.From))
		}
	}
	gaps := make([]ExchangeGap, 0)
	for _, node := range report.Graph.Nodes {
		reason := node.TerminalReason
		if reason == "" || reason == "purchase" || reason == "coinbase" {
			continue
		}
		if !strings.HasPrefix(reason, "policy_") && !strings.HasPrefix(reason, "max_") {
			continue
		}
		row := ExchangeGap{
			Direction: "inflow", DateUTC: gapDate(node), DateTime: node.BlockTime, Height: node.Height,
			TxID: node.TxID, Outpoint: node.Outpoint, Label: nodeLabel(node),
			BoundaryReason: reason, EvidenceStatus: "no_purchase_evidence",
			Suggested: "exchange withdrawal txid/outpoint", Targets: ancestry[node.ID],
		}
		if node.Kind == "output" {
			row.AmountSats = node.ValueSats
			row.Counterparty = node.Address
			if row.TxID == "" && len(node.Outpoint) > 64 {
				row.TxID = node.Outpoint[:64]
			}
			gaps = append(gaps, row)
			continue
		}
		outputs := children[node.ID]
		if len(outputs) == 0 {
			gaps = append(gaps, row)
			continue
		}
		for _, output := range outputs {
			if output.Outpoint == "" {
				continue
			}
			child := row
			child.Outpoint = output.Outpoint
			child.AmountSats = output.ValueSats
			child.Counterparty = output.Address
			if child.DateUTC == "" {
				child.DateUTC = gapDate(output)
			}
			gaps = append(gaps, child)
		}
	}
	sort.Slice(gaps, func(i, j int) bool {
		if gaps[i].DateUTC != gaps[j].DateUTC {
			return gaps[i].DateUTC < gaps[j].DateUTC
		}
		return gaps[i].TxID < gaps[j].TxID
	})
	return gaps
}

func graphNodeByID(report model.Report, id string) model.GraphNode {
	for _, node := range report.Graph.Nodes {
		if node.ID == id {
			return node
		}
	}
	return model.GraphNode{}
}

// ExchangeOutflows lists provider-labelled transactions that send coins to a
// counterparty address and return change, matching the configured exchange
// labels. They need sale/deposit records, not purchase evidence.
func ExchangeOutflows(report model.Report) []ExchangeGap {
	providers := exchangeProviders()
	spent := map[string]bool{}
	for _, edge := range report.Graph.Edges {
		if edge.Kind == "input" {
			spent[edge.To] = true
		}
	}
	rows := make([]ExchangeGap, 0)
	for _, node := range report.Graph.Nodes {
		if node.Kind != "transaction" {
			continue
		}
		label := strings.ToLower(node.Label)
		provider := ""
		for _, candidate := range providers {
			if strings.Contains(label, candidate) {
				provider = candidate
				break
			}
		}
		if provider == "" {
			continue
		}
		sent := int64(0)
		counterparty := make([]string, 0)
		for _, edge := range report.Graph.Edges {
			if edge.Kind != "funded_by" || edge.To != node.ID {
				continue
			}
			output := graphNodeByID(report, edge.From)
			label := strings.ToLower(outputAnnotation(report, output))
			if spent[output.ID] || strings.Contains(label, "change") {
				continue
			}
			sent += output.ValueSats
			if output.Address != "" {
				counterparty = append(counterparty, output.Address)
			}
		}
		if sent == 0 {
			continue
		}
		rows = append(rows, ExchangeGap{
			Direction: "outflow", DateUTC: gapDate(node), Height: node.Height, TxID: node.TxID,
			AmountSats: sent, Counterparty: strings.Join(counterparty, "|"), Label: node.Label,
			EvidenceStatus: "no_sale_evidence", Suggested: "sale/deposit confirmation",
			Targets: targetAncestry(report)[node.ID],
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].DateUTC != rows[j].DateUTC {
			return rows[i].DateUTC < rows[j].DateUTC
		}
		return rows[i].TxID < rows[j].TxID
	})
	return rows
}

func outputAnnotation(report model.Report, node model.GraphNode) string {
	if annotation, ok := report.Annotations.Outpoints[node.Outpoint]; ok && annotation.Label != "" {
		return annotation.Label
	}
	if annotation, ok := report.Annotations.Addresses[node.Address]; ok && annotation.Label != "" {
		return annotation.Label
	}
	return ""
}

type exchangeMonthRow struct {
	direction string
	key       string
	count     int
	totalSats int64
}

func exchangeMonths(rows []ExchangeGap) []exchangeMonthRow {
	months := map[string]*exchangeMonthRow{}
	order := make([]string, 0)
	for _, row := range rows {
		key := "undated"
		if len(row.DateUTC) >= 7 {
			key = row.DateUTC[:7]
		}
		composite := row.Direction + "\x00" + key
		entry, exists := months[composite]
		if !exists {
			entry = &exchangeMonthRow{direction: row.Direction, key: key}
			months[composite] = entry
			order = append(order, composite)
		}
		entry.count++
		entry.totalSats += row.AmountSats
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	result := make([]exchangeMonthRow, 0, len(order))
	for _, key := range order {
		result = append(result, *months[key])
	}
	return result
}

func writeExchangeGapsCSV(writer reportCSVWriter, rows []ExchangeGap) error {
	header := []string{"direction", "date_utc", "height", "txid", "outpoint",
		"amount_sats", "amount_btc", "counterparty_address", "label",
		"boundary_reason", "evidence_status", "suggested_record", "affected_utxos"}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, row := range rows {
		record := []string{row.Direction, row.DateUTC, strconv.FormatInt(row.Height, 10),
			row.TxID, row.Outpoint, strconv.FormatInt(row.AmountSats, 10),
			strings.TrimSuffix(btcString(row.AmountSats), " BTC"), row.Counterparty, row.Label,
			row.BoundaryReason, row.EvidenceStatus, row.Suggested, strings.Join(row.Targets, "|")}
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func exchangeGapsCSVBytes(rows []ExchangeGap) ([]byte, error) {
	return csvBytes(model.Report{}, func(writer reportCSVWriter, _ model.Report) error {
		return writeExchangeGapsCSV(writer, rows)
	})
}

func writeExchangeMonthsCSV(writer reportCSVWriter, rows []ExchangeGap) error {
	header := []string{"direction", "year", "month", "month_label", "tx_count", "total_sats", "total_btc"}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, row := range exchangeMonths(rows) {
		year, month := "", ""
		if row.key != "undated" {
			year, month = row.key[:4], row.key[5:7]
		}
		record := []string{row.direction, year, month, row.key, strconv.Itoa(row.count),
			strconv.FormatInt(row.totalSats, 10), strings.TrimSuffix(btcString(row.totalSats), " BTC")}
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func exchangeMonthsCSVBytes(rows []ExchangeGap) ([]byte, error) {
	return csvBytes(model.Report{}, func(writer reportCSVWriter, _ model.Report) error {
		return writeExchangeMonthsCSV(writer, rows)
	})
}

// ExchangeGapsCSVBytes and friends expose the generated gap lists.
func ExchangeGapsCSVBytes(report model.Report) ([]byte, error) {
	return exchangeGapsCSVBytes(ExchangeGaps(report))
}

func ExchangeOutflowsCSVBytes(report model.Report) ([]byte, error) {
	return exchangeGapsCSVBytes(ExchangeOutflows(report))
}

func ExchangeGapMonthsCSVBytes(report model.Report) ([]byte, error) {
	return exchangeMonthsCSVBytes(ExchangeGaps(report))
}

func ExchangeOutflowMonthsCSVBytes(report model.Report) ([]byte, error) {
	return exchangeMonthsCSVBytes(ExchangeOutflows(report))
}

// ExchangeGapMonths returns the number of distinct months with unaccounted
// inbound exchange payouts.
func ExchangeGapMonths(report model.Report) int {
	return len(exchangeMonths(ExchangeGaps(report)))
}

// GapCheck returns an error while unaccounted inbound exchange payouts remain.
func GapCheck(report model.Report) error {
	if gaps := ExchangeGaps(report); len(gaps) > 0 {
		return fmt.Errorf("documentation incomplete: %d unaccounted exchange payout(s) remain", len(gaps))
	}
	return nil
}

// ExchangeOutflowMonths returns the number of distinct months with unaccounted
// outbound exchange transfers.
func ExchangeOutflowMonths(report model.Report) int {
	return len(exchangeMonths(ExchangeOutflows(report)))
}

// GapCheckAll returns an error while either inbound payouts or outbound
// transfers remain unaccounted.
func GapCheckAll(report model.Report) error {
	inbound := len(ExchangeGaps(report))
	outbound := len(ExchangeOutflows(report))
	if inbound > 0 || outbound > 0 {
		return fmt.Errorf("documentation incomplete: %d unaccounted inbound payout(s), %d outbound transfer(s) without sale evidence", inbound, outbound)
	}
	return nil
}
