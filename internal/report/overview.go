package report

import (
	"fmt"
	"html"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"tracey-sof/internal/bank"
	"tracey-sof/internal/model"
)

type overviewEvent struct {
	height     int64
	date       string
	kind       string // purchase | transaction | output
	id         string
	txid       string
	outpoint   string
	address    string
	label      string
	boundary   string
	provider   string
	purchaseID string
	valueSats  int64
	fiat       string
	targets    []string
	flags      []string
}

func overviewEvents(report model.Report) []overviewEvent {
	return overviewEventsWith(report, ExchangeGaps(report), ExchangeOutflows(report), nil)
}

func overviewEventsWith(report model.Report, inbound, outbound []ExchangeGap, statementFlags map[string]string) []overviewEvent {
	purchases := map[string]model.Purchase{}
	byOutpoint := map[string]model.Purchase{}
	for _, purchase := range report.Purchases {
		purchases[purchase.ID] = purchase
		for _, outpoint := range purchase.Outpoints {
			byOutpoint[outpoint.String()] = purchase
		}
	}
	utxoOutpoints, utxoTxids := currentUTXOIndex(report)
	inboundTxids, inboundOutpoints := gapIndex(inbound)
	outboundTxids, _ := gapIndex(outbound)
	events := make([]overviewEvent, 0, len(report.Graph.Nodes)+len(report.Purchases))
	for _, node := range report.Graph.Nodes {
		if node.Kind == "output" && len(node.PurchaseIDs) > 0 {
			continue // represented by the purchase row below
		}
		event := overviewEvent{height: node.Height, date: gapDate(node), id: node.ID,
			label: node.Label, boundary: node.TerminalReason, valueSats: node.ValueSats, txid: node.TxID,
			outpoint: node.Outpoint, address: node.Address}
		if node.Kind == "transaction" {
			event.kind = "transaction"
			event.id = node.TxID
		} else {
			event.kind = "output"
			event.id = node.Outpoint
			if purchase, ok := byOutpoint[node.Outpoint]; ok && event.height == 0 {
				event.height = purchase.Height
			}
		}
		event.flags = overviewFlags(event, utxoOutpoints, utxoTxids, inboundTxids, inboundOutpoints, outboundTxids, statementFlags)
		events = append(events, event)
	}
	for _, purchase := range report.Purchases {
		event := overviewEvent{kind: "purchase", id: purchase.ID, date: purchaseUTCDate(purchase),
			height: purchase.Height, provider: purchase.Provider, purchaseID: purchase.ID,
			valueSats: optionSatsValue(purchase.AmountSats), txid: "", outpoint: "", label: purchase.Notes}
		if len(purchase.TxIDs) > 0 {
			event.txid = purchase.TxIDs[0]
		}
		if len(purchase.Outpoints) > 0 {
			event.outpoint = purchase.Outpoints[0].String()
		}
		if purchase.Fiat != nil {
			event.fiat = purchase.Fiat.Amount + " " + purchase.Fiat.Currency
		}
		event.flags = overviewFlags(event, utxoOutpoints, utxoTxids, inboundTxids, inboundOutpoints, outboundTxids, statementFlags)
		events = append(events, event)
	}
	sort.SliceStable(events, func(i, j int) bool {
		if events[i].height != events[j].height {
			if events[i].height == 0 {
				return false
			}
			if events[j].height == 0 {
				return true
			}
			return events[i].height > events[j].height
		}
		if events[i].kind != events[j].kind {
			order := map[string]int{"purchase": 0, "transaction": 1, "output": 2}
			return order[events[i].kind] < order[events[j].kind]
		}
		return events[i].id < events[j].id
	})
	return events
}

func currentUTXOIndex(report model.Report) (map[string]bool, map[string]bool) {
	outpoints := make(map[string]bool, len(report.UTXOs))
	txids := make(map[string]bool, len(report.UTXOs))
	for _, target := range report.UTXOs {
		outpoints[target.Key()] = true
		txids[target.TxID] = true
	}
	return outpoints, txids
}

func gapIndex(rows []ExchangeGap) (map[string]bool, map[string]bool) {
	txids := make(map[string]bool, len(rows))
	outpoints := make(map[string]bool, len(rows))
	for _, row := range rows {
		txids[row.TxID] = true
		outpoints[row.Outpoint] = true
	}
	return txids, outpoints
}

// overviewFlags marks the rows that explain the chronology: current UTXOs and
// the transactions that the evidence-gap sections ask the reader to resolve.
func overviewFlags(event overviewEvent, utxoOutpoints, utxoTxids, inboundTxids, inboundOutpoints, outboundTxids map[string]bool, statementFlags map[string]string) []string {
	flags := make([]string, 0, 3)
	if event.outpoint != "" && utxoOutpoints[event.outpoint] {
		flags = append(flags, "unspent")
	}
	if event.kind == "transaction" && event.txid != "" && utxoTxids[event.txid] {
		flags = append(flags, "funds unspent output")
	}
	if (event.txid != "" && inboundTxids[event.txid]) || (event.outpoint != "" && inboundOutpoints[event.outpoint]) {
		flags = append(flags, "inbound gap")
	}
	if event.txid != "" && outboundTxids[event.txid] {
		flags = append(flags, "outbound gap")
	}
	if event.kind == "purchase" && statementFlags[event.purchaseID] != "" {
		flags = append(flags, "statement "+statementFlags[event.purchaseID])
	}
	return flags
}

func statementPurchaseFlags(cache bank.Cache) map[string]string {
	flags := make(map[string]string)
	for _, record := range cache.Records {
		if record.Kind != "purchase" || record.StatementFile == "" {
			continue
		}
		switch {
		case record.Status == "matched" && record.Confirmed:
			flags[record.ID] = "confirmed"
		case record.Status == "matched":
			flags[record.ID] = "candidate"
		case record.Status == "ambiguous":
			flags[record.ID] = "ambiguous"
		default:
			flags[record.ID] = "missing"
		}
	}
	return flags
}

func statementStatusLabel(record bank.MatchRecord) string {
	switch {
	case record.Status == "matched" && record.Confirmed:
		return "confirmed"
	case record.Status == "matched":
		return "candidate"
	case record.Status == "ambiguous":
		return "ambiguous"
	default:
		return "missing"
	}
}

type statementCoverageRow struct {
	month      string
	purchases  int
	sources    []bank.StatementSource
	confirmed  int
	candidates int
	missing    int
}

func statementCoverageRows(report model.Report, cache bank.Cache) []statementCoverageRow {
	rows := map[string]*statementCoverageRow{}
	rowFor := func(month string) *statementCoverageRow {
		if month == "" {
			month = "undated"
		}
		row, exists := rows[month]
		if !exists {
			row = &statementCoverageRow{month: month}
			rows[month] = row
		}
		return row
	}
	confirmedIDs := make(map[string]bool)
	for _, purchase := range report.Purchases {
		month := purchaseUTCDate(purchase)
		if len(month) >= 7 {
			month = month[:7]
		}
		rowFor(month).purchases++
		for _, artifact := range purchase.Artifacts {
			if strings.HasPrefix(artifact.Path, "evidence/bank/") {
				confirmedIDs[purchase.ID] = true
				break
			}
		}
	}
	for _, source := range cache.Statements {
		rowFor(source.Period).sources = append(rowFor(source.Period).sources, source)
	}
	seen := make(map[string]bool)
	for _, record := range cache.Records {
		if record.Kind != "purchase" {
			continue
		}
		month := record.StatementMonth
		if month == "" && len(record.Date) >= 7 {
			month = record.Date[:7]
		}
		row := rowFor(month)
		seen[record.ID] = true
		status := statementStatusLabel(record)
		if confirmedIDs[record.ID] {
			status = "confirmed"
		}
		switch status {
		case "confirmed":
			row.confirmed++
		case "candidate", "ambiguous":
			row.candidates++
		}
	}
	for _, purchase := range report.Purchases {
		if !confirmedIDs[purchase.ID] || seen[purchase.ID] {
			continue
		}
		month := purchaseUTCDate(purchase)
		if len(month) >= 7 {
			month = month[:7]
		}
		rowFor(month).confirmed++
	}
	result := make([]statementCoverageRow, 0, len(rows))
	for _, row := range rows {
		if row.confirmed+row.candidates < row.purchases {
			row.missing = row.purchases - row.confirmed - row.candidates
		}
		result = append(result, *row)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].month < result[j].month })
	return result
}

type statementEvidenceRow struct {
	id        string
	date      string
	sats      int64
	bank      string
	recipient string
	purpose   string
	reference string
	file      string
	page      int
	status    string
	sha       string
}

func statementEvidenceRows(report model.Report, cache bank.Cache) []statementEvidenceRow {
	purchases := make(map[string]model.Purchase, len(report.Purchases))
	for _, purchase := range report.Purchases {
		purchases[purchase.ID] = purchase
	}
	rows := make([]statementEvidenceRow, 0)
	for _, record := range cache.Records {
		if record.Kind != "purchase" || record.StatementFile == "" {
			continue
		}
		purchase := purchases[record.ID]
		date := record.Date
		if date == "" {
			date = purchaseUTCDate(purchase)
		}
		rows = append(rows, statementEvidenceRow{
			id: record.ID, date: date, sats: optionSatsValue(purchase.AmountSats),
			bank:      strings.TrimSpace(record.BankDate + " " + record.BankAmountEUR + " " + record.BankCurrency),
			recipient: record.StatementRecipient, purpose: record.StatementPurpose,
			reference: record.StatementReference, file: filepath.Base(record.StatementFile),
			page: record.StatementPage, status: statementStatusLabel(record), sha: record.StatementSHA256,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].date != rows[j].date {
			return rows[i].date < rows[j].date
		}
		return rows[i].id < rows[j].id
	})
	return rows
}

func overviewHTML(report model.Report, cache bank.Cache) []byte {
	var out strings.Builder
	out.WriteString(`<!doctype html><html lang="en"><head><meta charset="utf-8"><title>Local audit overview</title><style>
@page { size: A4 portrait; margin: 12mm; }
body { font-family: system-ui, sans-serif; font-size: 10pt; color: #1b2430; line-height: 1.4; }
h1 { font-size: 20pt; margin: 0 0 2mm; }
h2 { font-size: 15pt; margin: 7mm 0 2mm; border-bottom: 1px solid #9aa7b4; padding-bottom: 1mm; break-after: avoid; }
h3 { font-size: 11.5pt; margin: 5mm 0 1.5mm; break-after: avoid; }
p { margin: 1.5mm 0; }
table { width: 100%; border-collapse: collapse; font-size: 9pt; table-layout: fixed; }
th, td { border: 1px solid #b8c2cc; padding: 1.4mm 1.2mm; vertical-align: top; text-align: left; }
th { background: #eef2f6; }
tr { break-inside: avoid; }
code { font-family: ui-monospace, monospace; font-size: 8pt; word-break: break-all; }
.summary { display: grid; grid-template-columns: repeat(3, 1fr); gap: 2mm; margin: 3mm 0; }
.card { border: 1px solid #b8c2cc; padding: 2mm; }
.warn { background: #fff6e8; border: 1px solid #d79a2b; padding: 2mm; margin: 2mm 0; }
.muted { color: #536171; }
td.num { text-align: right; white-space: nowrap; }
td.status { font-weight: 600; }
</style></head><body>`)
	out.WriteString("<h1>Local audit overview</h1>")
	out.WriteString(fmt.Sprintf(`<p class="muted">Generated %s · chain main · block %s (%d) · %d current UTXOs · %s sats</p>`,
		html.EscapeString(report.GeneratedAt), html.EscapeString(report.Chain.BestBlock), report.Chain.BestHeight,
		report.Summary.TotalUTXOs, strconv.FormatInt(report.Summary.TotalSats, 10)))

	out.WriteString(`<div class="summary">`)
	out.WriteString(fmt.Sprintf(`<div class="card"><strong>Documentation</strong><br>%d documented · %d partial · %d undocumented<br>%d incomplete traces</div>`,
		report.Summary.DocumentedUTXOs, report.Summary.PartialUTXOs, report.Summary.UndocumentedUTXOs, report.Summary.IncompleteUTXOs))
	inbound := ExchangeGaps(report)
	outbound := ExchangeOutflows(report)
	out.WriteString(fmt.Sprintf(`<div class="card"><strong>Evidence gaps</strong><br>%d inbound payout(s) · %d months<br>%d outbound transfer(s) · %d months</div>`,
		len(inbound), ExchangeGapMonths(report), len(outbound), ExchangeOutflowMonths(report)))
	matched, requests := StatementRequestSummary(report, cache)
	out.WriteString(fmt.Sprintf(`<div class="card"><strong>Bank proof</strong><br>%d matched statement rows<br>%d physical statement request(s)</div>`, matched, requests))
	out.WriteString(`</div>`)

	out.WriteString(`<h2>Evidence gaps to resolve first</h2>`)
	if len(inbound) == 0 && len(outbound) == 0 {
		out.WriteString(`<p>No exchange gaps remain.</p>`)
	} else {
		if len(inbound) > 0 {
			out.WriteString(`<h3>Inbound exchange payouts — need purchase/withdrawal evidence</h3>`)
			out.WriteString(`<table><thead><tr><th style="width:12%">Date</th><th style="width:8%">Height</th><th style="width:28%">Transaction</th><th style="width:9%">Sats</th><th style="width:23%">Counterparty</th><th style="width:20%">Action</th></tr></thead><tbody>`)
			for _, gap := range inbound {
				out.WriteString(fmt.Sprintf(`<tr><td>%s</td><td class="num">%d</td><td><code>%s</code></td><td class="num">%d</td><td><code>%s</code></td><td>Request exchange withdrawal record for this txid/outpoint.</td></tr>`,
					html.EscapeString(gap.DateUTC), gap.Height, html.EscapeString(gap.TxID), gap.AmountSats, html.EscapeString(gap.Counterparty)))
			}
			out.WriteString(`</tbody></table>`)
		}
		if len(outbound) > 0 {
			out.WriteString(`<h3>Outbound exchange transfers — need sale/deposit evidence</h3>`)
			out.WriteString(`<table><thead><tr><th style="width:12%">Date</th><th style="width:28%">Transaction</th><th style="width:9%">Sats</th><th style="width:24%">Counterparty</th><th style="width:27%">Label</th></tr></thead><tbody>`)
			for _, gap := range outbound {
				out.WriteString(fmt.Sprintf(`<tr><td>%s</td><td><code>%s</code></td><td class="num">%d</td><td><code>%s</code></td><td>%s</td></tr>`,
					html.EscapeString(gap.DateUTC), html.EscapeString(gap.TxID), gap.AmountSats, html.EscapeString(gap.Counterparty), html.EscapeString(gap.Label)))
			}
			out.WriteString(`</tbody></table>`)
		}
	}

	out.WriteString(`<h2>Bank statement coverage</h2>`)
	coverage := statementCoverageRows(report, cache)
	if len(coverage) == 0 {
		out.WriteString(`<p>No purchase months or PDF statements are available.</p>`)
	} else {
		out.WriteString(`<table><thead><tr><th style="width:12%">Month</th><th style="width:10%">Purchases</th><th style="width:30%">PDF statement</th><th style="width:14%">Confirmed</th><th style="width:16%">Candidates</th><th style="width:18%">Missing</th></tr></thead><tbody>`)
		for _, row := range coverage {
			files := make([]string, 0, len(row.sources))
			for _, source := range row.sources {
				label := strings.TrimSpace(source.Period)
				if label == "" {
					label = filepath.Base(source.File)
				}
				files = append(files, html.EscapeString(label))
			}
			statement := strings.Join(files, "<br>")
			if statement == "" {
				statement = "—"
			}
			out.WriteString(fmt.Sprintf(`<tr><td>%s</td><td class="num">%d</td><td><code>%s</code></td><td class="num">%d</td><td class="num">%d</td><td class="num">%d</td></tr>`,
				html.EscapeString(row.month), row.purchases, statement, row.confirmed, row.candidates, row.missing))
		}
		out.WriteString(`</tbody></table>`)
		out.WriteString(`<p class="muted">PDF statements are local evidence; they are not embedded in this document.</p>`)
	}

	out.WriteString(`<h2>Chronology — newest block first, sources at the bottom</h2>`)
	out.WriteString(`<p class="muted">One section per block height present in the trace. Purchases appear as source rows. Values are exact sats; fiat is wallet/bank-recorded when available.</p>`)
	out.WriteString(`<p class="muted"><strong>Status:</strong> <strong>unspent</strong> = current UTXO that is still unspent; <strong>funds unspent output</strong> = transaction creates a current UTXO; <strong>inbound gap</strong> = purchase/withdrawal evidence needed; <strong>outbound gap</strong> = sale/deposit evidence needed; <strong>statement confirmed/candidate/ambiguous</strong> = PDF statement match status. These are the same transactions listed in the evidence-gap sections above.</p>`)
	events := overviewEventsWith(report, inbound, outbound, statementPurchaseFlags(cache))
	currentHeight := int64(-1)
	for _, event := range events {
		if event.height != currentHeight {
			if currentHeight != -1 {
				out.WriteString(`</tbody></table>`)
			}
			currentHeight = event.height
			label := "undated"
			if event.height > 0 {
				label = fmt.Sprintf("block %s", commaInt(event.height))
			}
			date := ""
			for _, candidate := range events {
				if candidate.height == event.height && candidate.date != "" {
					date = candidate.date
					break
				}
			}
			if date != "" {
				label += " · " + date
			}
			out.WriteString(fmt.Sprintf(`<h3>%s</h3>`, html.EscapeString(label)))
			out.WriteString(`<table><thead><tr><th style="width:12%">Type</th><th style="width:21%">Identifier</th><th style="width:9%">Sats</th><th style="width:7%">Fiat</th><th style="width:15%">Status</th><th style="width:17%">Label / provider</th><th style="width:19%">Address / boundary</th></tr></thead><tbody>`)
		}
		identifier := event.outpoint
		if identifier == "" {
			identifier = event.txid
		}
		if identifier == "" {
			identifier = event.id
		}
		detail := event.address
		if detail == "" {
			detail = event.boundary
		}
		label := event.provider
		if event.label != "" {
			if label != "" {
				label += " · "
			}
			label += event.label
		}
		value := ""
		if event.valueSats > 0 {
			value = strconv.FormatInt(event.valueSats, 10)
		}
		status := strings.Join(event.flags, " · ")
		out.WriteString(fmt.Sprintf(`<tr><td>%s</td><td><code>%s</code></td><td class="num">%s</td><td>%s</td><td class="status">%s</td><td>%s</td><td><code>%s</code></td></tr>`,
			html.EscapeString(event.kind), html.EscapeString(shortID(identifier)), value,
			html.EscapeString(event.fiat), html.EscapeString(status), html.EscapeString(label), html.EscapeString(shortID(detail))))
	}
	if currentHeight != -1 {
		out.WriteString(`</tbody></table>`)
	}

	out.WriteString(`<h2>Statement evidence</h2>`)
	evidence := statementEvidenceRows(report, cache)
	if len(evidence) == 0 {
		out.WriteString(`<p>No PDF statement rows have been imported yet.</p>`)
	} else {
		out.WriteString(`<table><thead><tr><th style="width:18%">Purchase</th><th style="width:9%">Date</th><th style="width:8%">Sats</th><th style="width:17%">Bank row</th><th style="width:24%">Recipient / purpose</th><th style="width:14%">Statement</th><th style="width:10%">Status</th></tr></thead><tbody>`)
		for _, row := range evidence {
			detail := row.recipient
			if row.purpose != "" {
				if detail != "" {
					detail += " · "
				}
				detail += row.purpose
			}
			if row.reference != "" {
				detail += " · " + row.reference
			}
			statement := fmt.Sprintf("%s p.%d", shortID(row.file), row.page)
			if row.sha != "" {
				statement += " · " + shortID(row.sha)
			}
			out.WriteString(fmt.Sprintf(`<tr><td><code>%s</code></td><td>%s</td><td class="num">%d</td><td>%s</td><td>%s</td><td><code>%s</code></td><td class="status">%s</td></tr>`,
				html.EscapeString(shortID(row.id)), html.EscapeString(row.date), row.sats,
				html.EscapeString(row.bank), html.EscapeString(detail), html.EscapeString(statement), html.EscapeString(row.status)))
		}
		out.WriteString(`</tbody></table>`)
	}

	out.WriteString(`<h2>Current UTXOs</h2><table><thead><tr><th style="width:30%">Outpoint</th><th style="width:24%">Address</th><th style="width:10%">Sats</th><th style="width:12%">Documentation</th><th style="width:12%">Trace</th><th style="width:12%">Review</th></tr></thead><tbody>`)
	for _, utxo := range report.UTXOs {
		out.WriteString(fmt.Sprintf(`<tr><td><code>%s</code></td><td><code>%s</code></td><td class="num">%d</td><td>%s</td><td>%s</td><td>%s</td></tr>`,
			html.EscapeString(utxo.Outpoint.String()), html.EscapeString(utxo.Address), utxo.ValueSats,
			html.EscapeString(utxo.Status), html.EscapeString(utxo.Trace), strings.Join(utxo.ReviewReasons, ", ")))
	}
	out.WriteString(`</tbody></table>`)
	out.WriteString(`<p class="muted">Source graph: out/graph-height-all.svg (vector). Full detail: out/report.html and out/report.pdf. Statement placeholders: out/statement-requests.csv.</p>`)
	out.WriteString(`</body></html>`)
	return []byte(out.String())
}

// WriteOverview writes a print-oriented HTML overview with gaps first and a
// newest-to-oldest block chronology.
func WriteOverview(report model.Report, cache bank.Cache, outDir string) error {
	return AtomicWrite(filepath.Join(outDir, "overview.html"), overviewHTML(report, cache), 0o600)
}
