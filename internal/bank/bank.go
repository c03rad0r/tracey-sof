// Package bank parses local bank exports and matches debit rows to on-chain
// purchase events. Nothing is sent off the machine.
package bank

import (
	"encoding/csv"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

// Transaction is one booked bank row.
type Transaction struct {
	BookingDate time.Time
	ValueDate   time.Time
	Name        string
	Purpose     string
	Text        string
	AmountCents int64
	Currency    string
}

func parseGermanAmount(value string) (int64, error) {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return 0, fmt.Errorf("empty amount")
	}
	cleaned = strings.ReplaceAll(cleaned, ".", "")
	cleaned = strings.ReplaceAll(cleaned, ",", ".")
	parsed, err := strconv.ParseFloat(cleaned, 64)
	if err != nil {
		return 0, err
	}
	return int64(roundHalfUp(parsed * 100)), nil
}

func roundHalfUp(value float64) float64 {
	if value >= 0 {
		return float64(int64(value + 0.5))
	}
	return float64(int64(value - 0.5))
}

func parseGermanDate(value string) (time.Time, error) {
	return time.Parse("02.01.2006", strings.TrimSpace(value))
}

// ParseCSV reads a bank CSV export (semicolon separated, UTF-8 BOM,
// German decimal comma). It accepts both the raw export and a file whose header
// has been renamed; only the documented columns are required.
func ParseCSV(reader io.Reader) ([]Transaction, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, fmt.Errorf("read bank csv: %w", err)
	}
	// The export uses decorative double quotes as literal text (for example
	// `"" Lastschrift ""`) instead of CSV quoting. Removing them keeps the
	// semicolon structure intact; the export does not quote delimiters.
	parser := csv.NewReader(strings.NewReader(strings.ReplaceAll(string(data), "\"", "")))
	parser.Comma = ';'
	parser.FieldsPerRecord = -1
	records, _ := parser.ReadAll()
	if len(records) < 2 {
		return nil, fmt.Errorf("bank csv has no data rows")
	}
	index := map[string]int{}
	for position, name := range records[0] {
		index[strings.TrimSpace(strings.TrimPrefix(name, "\ufeff"))] = position
	}
	column := func(record []string, name string) string {
		position, ok := index[name]
		if !ok || position >= len(record) {
			return ""
		}
		return strings.TrimSpace(record[position])
	}
	result := make([]Transaction, 0, len(records)-1)
	for _, record := range records[1:] {
		booking, err := parseGermanDate(column(record, "Buchungstag"))
		if err != nil {
			continue
		}
		amount, err := parseGermanAmount(column(record, "Betrag"))
		if err != nil {
			continue
		}
		valueDate := booking
		if parsed, err := parseGermanDate(column(record, "Valutadatum")); err == nil {
			valueDate = parsed
		}
		result = append(result, Transaction{
			BookingDate: booking, ValueDate: valueDate,
			Name: column(record, "Name Zahlungsbeteiligter"), Purpose: column(record, "Verwendungszweck"),
			Text: column(record, "Buchungstext"), AmountCents: amount, Currency: column(record, "Waehrung"),
		})
	}
	return result, nil
}

// AmountEUR formats cents as a decimal EUR string (for fiat records and CSVs).
func AmountEUR(cents int64) string {
	sign := ""
	if cents < 0 {
		sign = "-"
		cents = -cents
	}
	return fmt.Sprintf("%s%d.%02d", sign, cents/100, cents%100)
}

// Reference returns a short bank reference from the purpose text.
func (t Transaction) Reference() string {
	fields := strings.Fields(t.Purpose)
	if len(fields) > 1 {
		return fields[1]
	}
	return strings.TrimSpace(t.Purpose)
}

func (t Transaction) Blob() string {
	return strings.ToLower(strings.Join([]string{t.Name, t.Purpose, t.Text}, " "))
}
