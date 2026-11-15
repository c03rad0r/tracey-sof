// Package statement extracts booking rows from local bank-statement PDFs.
package statement

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"tracey-sof/internal/bank"
	"tracey-sof/internal/intake"
)

// Row is one booked transaction from a statement.
type Row struct {
	BookingDate time.Time
	AmountCents int64
	Currency    string
	Recipient   string
	Purpose     string
	Reference   string
	Page        int
	Raw         string
}

// Statement is one parsed PDF statement.
type Statement struct {
	Institution string
	AccountType string
	Period      string
	File        string
	SHA256      string
	Pages       int
	Extraction  []string
	Rows        []Row
}

var (
	statementPeriod = regexp.MustCompile(`Buchungen vom (\d{2}\.\d{2}\.\d{4})`)
	statementRow    = regexp.MustCompile(`^\s*(\d{2}\.\d{2}\.\d{4})\s{2,}(.+?)\s{2,}(\d{2}\.\d{2}\.\d{4})\s*\*?\s+(-?[\d.]+,\d{2})\s*$`)
	extractPDFText  = intake.ExtractPDFText
)

// Extract reads and parses one statement PDF. Only local tools are used.
func Extract(ctx context.Context, path, institution string) (Statement, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Statement{}, fmt.Errorf("read statement: %w", err)
	}
	sum := sha256.Sum256(data)
	text, extraction, err := extractPDFText(ctx, path)
	if err != nil {
		return Statement{}, err
	}
	statement := Statement{
		Institution: strings.ToLower(strings.TrimSpace(institution)),
		File:        filepath.Base(path),
		SHA256:      hex.EncodeToString(sum[:]),
		Pages:       strings.Count(text, "\f") + 1,
		Extraction:  extraction,
	}
	switch statement.Institution {
	case "de-bank":
		parseChecking(text, &statement)
	default:
		return Statement{}, fmt.Errorf("unsupported statement institution %q", institution)
	}
	return statement, nil
}

func parseChecking(text string, statement *Statement) {
	statement.AccountType = accountType(text)
	statement.Period = statementPeriodOf(text)
	var current *Row
	for pageIndex, page := range strings.Split(text, "\f") {
		for _, line := range strings.Split(page, "\n") {
			if match := statementRow.FindStringSubmatch(line); match != nil {
				booking, err := time.Parse("02.01.2006", match[1])
				if err != nil {
					continue
				}
				amount, err := bank.ParseAmountEUR(match[4])
				if err != nil {
					continue
				}
				row := Row{
					BookingDate: booking,
					AmountCents: amount,
					Currency:    "EUR",
					Recipient:   recipientOf(match[2]),
					Page:        pageIndex + 1,
					Raw:         strings.TrimSpace(line),
				}
				statement.Rows = append(statement.Rows, row)
				current = &statement.Rows[len(statement.Rows)-1]
				continue
			}
			if current != nil && detailLine(line) {
				current.Raw += "\n" + strings.TrimSpace(line)
			}
		}
	}
	for index := range statement.Rows {
		row := &statement.Rows[index]
		row.Purpose = fieldOf(row.Raw, "SVWZ+")
		row.Reference = firstFieldOf(row.Raw, "EREF+", "MREF+")
	}
}

func accountType(text string) string {
	switch {
	case strings.Contains(text, "Tagesgeld"), strings.Contains(text, "Konditionserh"):
		return "savings"
	case strings.Contains(text, "Kontoauszug") && strings.Contains(text, "Buchungstag"):
		return "checking"
	default:
		return ""
	}
}

// IsCheckingAccount reports whether the statement belongs to a transaction
// account that can contain purchase debits.
func (s Statement) IsCheckingAccount() bool {
	return s.AccountType == "checking"
}

func statementPeriodOf(text string) string {
	match := statementPeriod.FindStringSubmatch(text)
	if match == nil {
		return ""
	}
	parsed, err := time.Parse("02.01.2006", match[1])
	if err != nil {
		return ""
	}
	return parsed.Format("2006-01")
}

func detailLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	for _, prefix := range []string{"SVWZ+", "EREF+", "MREF+", "CRED+", "ABWA+"} {
		if strings.HasPrefix(trimmed, prefix) {
			return true
		}
	}
	return false
}

func recipientOf(text string) string {
	for _, marker := range []string{" SEPA-", " SEPA "} {
		if index := strings.Index(text, marker); index >= 0 {
			return strings.TrimSpace(text[:index])
		}
	}
	return strings.TrimSpace(text)
}

func fieldOf(text, marker string) string {
	index := strings.Index(text, marker)
	if index < 0 {
		return ""
	}
	value := text[index+len(marker):]
	for _, end := range []string{" EREF+", " MREF+", " CRED+", " ABWA+", "\n"} {
		if cut := strings.Index(value, end); cut >= 0 {
			value = value[:cut]
		}
	}
	return strings.TrimSpace(value)
}

func firstFieldOf(text string, markers ...string) string {
	for _, marker := range markers {
		if value := fieldOf(text, marker); value != "" {
			return value
		}
	}
	return ""
}

// Transactions converts rows to the bank matching type.
func (s Statement) Transactions() []bank.Transaction {
	transactions := make([]bank.Transaction, 0, len(s.Rows))
	for _, row := range s.Rows {
		transactions = append(transactions, bank.Transaction{
			BookingDate:     row.BookingDate,
			ValueDate:       row.BookingDate,
			Name:            row.Recipient,
			Purpose:         row.Purpose,
			Text:            row.Raw,
			AmountCents:     row.AmountCents,
			Currency:        row.Currency,
			SourceFile:      s.File,
			SourcePage:      row.Page,
			SourceSHA256:    s.SHA256,
			SourceMonth:     s.Period,
			SourceText:      row.Raw,
			SourceReference: row.Reference,
		})
	}
	return transactions
}
