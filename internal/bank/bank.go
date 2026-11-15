// Package bank parses local bank exports and matches debit rows to on-chain
// purchase events. Nothing is sent off the machine.
package bank

import (
	"encoding/csv"
	"fmt"
	"io"
	"math"
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

const maxBankCSVBytes = 64 << 20

func parseGermanAmount(value string) (int64, error) {
	cleaned := strings.TrimSpace(value)
	if cleaned == "" {
		return 0, fmt.Errorf("empty amount")
	}
	sign := int64(1)
	if cleaned[0] == '-' || cleaned[0] == '+' {
		if cleaned[0] == '-' {
			sign = -1
		}
		cleaned = cleaned[1:]
	}
	integer, fraction, found := strings.Cut(cleaned, ",")
	if !found {
		fraction = ""
	}
	integer = strings.ReplaceAll(integer, ".", "")
	if integer == "" || !decimalDigits(integer) || !decimalDigits(fraction) {
		return 0, fmt.Errorf("invalid amount %q", value)
	}
	if len(fraction) > 2 {
		return 0, fmt.Errorf("amount %q has sub-cent precision", value)
	}
	for len(fraction) < 2 {
		fraction += "0"
	}
	whole, err := strconv.ParseUint(integer, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid amount %q: %w", value, err)
	}
	fractional := uint64(0)
	if fraction != "" {
		// Validation above constrains the fraction to two decimal digits,
		// so this conversion cannot fail under those constraints.
		fractional, _ = strconv.ParseUint(fraction, 10, 8)
	}
	if whole > (math.MaxInt64-100)/100 {
		return 0, fmt.Errorf("amount %q is too large", value)
	}
	return sign * int64(whole*100+fractional), nil
}

func decimalDigits(value string) bool {
	if value == "" {
		return true
	}
	for _, digit := range value {
		if digit < '0' || digit > '9' {
			return false
		}
	}
	return true
}

func parseGermanDate(value string) (time.Time, error) {
	return time.Parse("02.01.2006", strings.TrimSpace(value))
}

// ParseCSV reads a bank CSV export (semicolon separated, UTF-8 BOM,
// German decimal comma). It accepts both the raw export and a file whose header
// has been renamed; only the documented columns are required.
func ParseCSV(reader io.Reader) ([]Transaction, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxBankCSVBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read bank csv: %w", err)
	}
	if len(data) > maxBankCSVBytes {
		return nil, fmt.Errorf("bank csv exceeds %d bytes", maxBankCSVBytes)
	}
	parser := csv.NewReader(strings.NewReader(string(data)))
	parser.Comma = ';'
	parser.FieldsPerRecord = -1
	records, err := parser.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse bank csv: %w", err)
	}
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
