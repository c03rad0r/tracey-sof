package report

import (
	"strconv"
	"strings"

	"tracey-sof/internal/bank"
	"tracey-sof/internal/model"
)

// StatementEvents builds the events that need bank-statement proof: every
// purchase record plus every unaccounted inbound exchange payout.
func StatementEvents(report model.Report) []bank.Event {
	events := make([]bank.Event, 0, len(report.Purchases)+8)
	for _, purchase := range report.Purchases {
		txid, outpoint := "", ""
		if len(purchase.TxIDs) > 0 {
			txid = purchase.TxIDs[0]
		}
		if len(purchase.Outpoints) > 0 {
			outpoint = purchase.Outpoints[0].String()
		}
		fiatAmount, fiatCurrency := "", ""
		if purchase.Fiat != nil {
			fiatAmount, fiatCurrency = purchase.Fiat.Amount, purchase.Fiat.Currency
		}
		event := bank.Event{
			Kind: "purchase", ID: purchase.ID, Date: purchaseUTCDate(purchase), DateTime: purchase.Date,
			Provider: purchase.Provider, TxID: txid, Outpoint: outpoint,
			AmountSats: optionSatsValue(purchase.AmountSats), Height: purchase.Height,
			FiatAmount: fiatAmount, FiatCurrency: fiatCurrency,
		}
		for _, artifact := range purchase.Artifacts {
			if strings.HasPrefix(artifact.Path, "evidence/bank/") {
				event.BankEvidence = true
				event.BankArtifact = artifact.Path
				event.BankSHA = artifact.SHA256
				event.BankDate = purchase.StatementDate
				break
			}
		}
		events = append(events, event)
	}
	for _, gap := range ExchangeGaps(report) {
		events = append(events, bank.Event{
			Kind: "inbound_payout", ID: gap.TxID + ":" + gap.Outpoint, Date: gap.DateUTC,
			DateTime: gap.DateTime, Provider: "unknown exchange", TxID: gap.TxID, Outpoint: gap.Outpoint,
			AmountSats: gap.AmountSats, Height: gap.Height,
		})
	}
	return events
}

func optionSatsValue(value *int64) int64 {
	if value == nil {
		return 0
	}
	return *value
}

func recordsByKey(cache bank.Cache) map[string]bank.MatchRecord {
	records := make(map[string]bank.MatchRecord, len(cache.Records))
	for _, record := range cache.Records {
		records[record.Key()] = record
	}
	return records
}

func statementStatus(record bank.MatchRecord, exists bool) (status, action string) {
	switch {
	case exists && record.Status == "matched" && record.Confirmed:
		return "matched_bank_evidence", "none"
	case exists && record.Status == "matched":
		return "candidate_needs_confirmation", "corroborate amount or explicitly confirm before applying"
	case exists && record.Status == "ambiguous":
		return "ambiguous_bank_match", "review bank candidates"
	default:
		return "request_physical_statement", "request physical statement"
	}
}

// StatementRequestsCSVBytes writes one row per purchase/payout event, marking
// bank-matched rows and placeholders for events that need a physical statement.
func StatementRequestsCSVBytes(report model.Report, cache bank.Cache) ([]byte, error) {
	return csvBytes(report, func(writer reportCSVWriter, current model.Report) error {
		return writeStatementRequestsCSV(writer, current, cache)
	})
}

func writeStatementRequestsCSV(writer reportCSVWriter, report model.Report, cache bank.Cache) error {
	header := []string{"event_type", "event_id", "event_date", "statement_month", "provider",
		"txid", "outpoint", "amount_sats", "fiat_amount", "fiat_currency",
		"bank", "status", "bank_booking_date", "bank_reference", "evidence_sha256", "action"}
	if err := writer.Write(header); err != nil {
		return err
	}
	records := recordsByKey(cache)
	for _, event := range StatementEvents(report) {
		record, exists := records[event.Key()]
		status, action := statementStatus(record, exists)
		if event.BankEvidence && status == "request_physical_statement" {
			status, action = "matched_bank_evidence", "none"
		}
		month := ""
		if len(event.Date) >= 7 {
			month = event.Date[:7]
		}
		fiatAmount := event.FiatAmount
		fiatCurrency := event.FiatCurrency
		if exists && record.Status == "matched" && record.Confirmed {
			fiatAmount, fiatCurrency = record.BankAmountEUR, record.BankCurrency
		}
		if action == "request physical statement" && month != "" {
			action = "request physical statement for " + month
		}
		sha := ""
		if exists && record.Status == "matched" && record.Confirmed {
			sha = cache.SHA256
		}
		if event.BankEvidence {
			sha = event.BankSHA
		}
		bankDate := record.BankDate
		if event.BankEvidence {
			bankDate = event.BankDate
		}
		row := []string{event.Kind, event.ID, event.Date, month, event.Provider,
			event.TxID, event.Outpoint, strconv.FormatInt(event.AmountSats, 10),
			fiatAmount, fiatCurrency, "local bank", status,
			bankDate, record.BankReference, sha, action}
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func writeBankMatchesCSV(writer reportCSVWriter, cache bank.Cache) error {
	header := []string{"event_type", "event_id", "event_date", "txid", "outpoint",
		"amount_sats", "status", "confirmed", "bank_booking_date", "bank_amount_eur", "bank_currency",
		"bank_reference", "evidence_sha256"}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, record := range cache.Records {
		sha := ""
		if record.Status == "matched" && record.Confirmed {
			sha = cache.SHA256
		}
		row := []string{record.Kind, record.ID, record.Date, record.TxID, record.Outpoint,
			strconv.FormatInt(record.AmountSats, 10), record.Status, strconv.FormatBool(record.Confirmed), record.BankDate,
			record.BankAmountEUR, record.BankCurrency, record.BankReference, sha}
		if err := writer.Write(row); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

// BankMatchesCSVBytes writes the bank-match cache as a CSV.
func BankMatchesCSVBytes(cache bank.Cache) ([]byte, error) {
	return csvBytes(model.Report{}, func(writer reportCSVWriter, _ model.Report) error {
		return writeBankMatchesCSV(writer, cache)
	})
}

// StatementRequestSummary returns matched and request counts.
func StatementRequestSummary(report model.Report, cache bank.Cache) (int, int) {
	records := recordsByKey(cache)
	matched, requests := 0, 0
	for _, event := range StatementEvents(report) {
		record, exists := records[event.Key()]
		if (exists && record.Status == "matched" && record.Confirmed) || event.BankEvidence {
			matched++
		} else {
			requests++
		}
	}
	return matched, requests
}
