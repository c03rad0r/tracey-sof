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
}

func overviewEvents(report model.Report) []overviewEvent {
	purchases := map[string]model.Purchase{}
	byOutpoint := map[string]model.Purchase{}
	for _, purchase := range report.Purchases {
		purchases[purchase.ID] = purchase
		for _, outpoint := range purchase.Outpoints {
			byOutpoint[outpoint.String()] = purchase
		}
	}
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

	out.WriteString(`<h2>Chronology — newest block first, sources at the bottom</h2>`)
	out.WriteString(`<p class="muted">One section per block height present in the trace. Purchases appear as source rows. Values are exact sats; fiat is wallet/bank-recorded when available.</p>`)
	events := overviewEvents(report)
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
			out.WriteString(`<table><thead><tr><th style="width:11%">Type</th><th style="width:26%">Identifier</th><th style="width:11%">Sats</th><th style="width:10%">Fiat</th><th style="width:21%">Label / provider</th><th style="width:21%">Address / boundary</th></tr></thead><tbody>`)
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
		out.WriteString(fmt.Sprintf(`<tr><td>%s</td><td><code>%s</code></td><td class="num">%s</td><td>%s</td><td>%s</td><td><code>%s</code></td></tr>`,
			html.EscapeString(event.kind), html.EscapeString(shortID(identifier)), value,
			html.EscapeString(event.fiat), html.EscapeString(label), html.EscapeString(shortID(detail))))
	}
	if currentHeight != -1 {
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
