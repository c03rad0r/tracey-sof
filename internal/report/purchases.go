package report

import (
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"tracey-sof/internal/model"
)

// purchaseLocation resolves the timezone used for statement dates. SOF_TIMEZONE
// may name any IANA zone; otherwise the host local zone is used.
func purchaseLocation() *time.Location {
	if name := strings.TrimSpace(os.Getenv("SOF_TIMEZONE")); name != "" {
		if location, err := time.LoadLocation(name); err == nil {
			return location
		}
	}
	return time.Local
}

func purchaseLocalDate(purchase model.Purchase, location *time.Location) string {
	if purchase.Date == "" {
		return ""
	}
	parsed, err := time.Parse(time.RFC3339, purchase.Date)
	if err != nil {
		return ""
	}
	return parsed.In(location).Format("2006-01-02")
}

func purchaseStatementDate(purchase model.Purchase, location *time.Location) string {
	if purchase.StatementDate != "" {
		return purchase.StatementDate
	}
	return purchaseLocalDate(purchase, location)
}

func purchaseUTCDate(purchase model.Purchase) string {
	if len(purchase.Date) >= 10 {
		return purchase.Date[:10]
	}
	return ""
}

// matchedPurchaseIDs returns the purchase records with at least one piece of
// evidence observed in the traced current-UTXO ancestry.
func matchedPurchaseIDs(report model.Report) map[string]bool {
	matched := map[string]bool{}
	for _, item := range report.Evidence {
		if item.Matched {
			for _, id := range item.PurchaseIDs {
				matched[id] = true
			}
		}
	}
	return matched
}

func optionSats(value *int64) string {
	if value == nil {
		return ""
	}
	return strconv.FormatInt(*value, 10)
}

func outpointList(outpoints []model.Outpoint) string {
	values := make([]string, 0, len(outpoints))
	for _, outpoint := range outpoints {
		values = append(values, outpoint.String())
	}
	return strings.Join(values, "|")
}

func joinUnique(values []string, separator string) string {
	seen := map[string]bool{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if value != "" && !seen[value] {
			seen[value] = true
			result = append(result, value)
		}
	}
	sort.Strings(result)
	return strings.Join(result, separator)
}

func purchasesCSVBytes(report model.Report, location *time.Location) ([]byte, error) {
	return csvBytes(report, func(writer reportCSVWriter, current model.Report) error {
		return writePurchasesCSV(writer, current, location)
	})
}

func writePurchasesCSV(writer reportCSVWriter, report model.Report, location *time.Location) error {
	return writePurchasesRows(writer, report, location, false)
}

func writePurchasesUnmatchedCSV(writer reportCSVWriter, report model.Report, location *time.Location) error {
	return writePurchasesRows(writer, report, location, true)
}

func writePurchasesRows(writer reportCSVWriter, report model.Report, location *time.Location, unmatchedOnly bool) error {
	header := []string{"purchase_id", "date_utc", "date_local", "statement_date",
		"matched_current_ancestry", "provider", "amount_sats", "amount_btc",
		"fiat_amount", "fiat_currency", "txids", "outpoints", "notes"}
	if unmatchedOnly {
		header = append(header, "match_status")
	}
	if err := writer.Write(header); err != nil {
		return err
	}
	matched := matchedPurchaseIDs(report)
	for _, purchase := range report.Purchases {
		isMatched := matched[purchase.ID]
		if unmatchedOnly && isMatched {
			continue
		}
		local := purchaseLocalDate(purchase, location)
		statement := purchaseStatementDate(purchase, location)
		fiatAmount, fiatCurrency := "", ""
		if purchase.Fiat != nil {
			fiatAmount, fiatCurrency = purchase.Fiat.Amount, purchase.Fiat.Currency
		}
		amountBTC := ""
		if purchase.AmountSats != nil {
			amountBTC = strings.TrimSuffix(btcString(*purchase.AmountSats), " BTC")
		}
		record := []string{purchase.ID, purchase.Date, local, statement,
			strconv.FormatBool(isMatched), purchase.Provider,
			optionSats(purchase.AmountSats), amountBTC, fiatAmount, fiatCurrency,
			strings.Join(purchase.TxIDs, "|"), outpointList(purchase.Outpoints), purchase.Notes}
		if unmatchedOnly {
			record = append(record, "not_in_current_ancestry")
		}
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

// PurchasesCSVBytes writes one row per purchase, including statement dates and
// exact on-chain identifiers for bank-statement matching.
func PurchasesCSVBytes(report model.Report) ([]byte, error) {
	return purchasesCSVBytes(report, purchaseLocation())
}

func purchasesUnmatchedCSVBytes(report model.Report, location *time.Location) ([]byte, error) {
	return csvBytes(report, func(writer reportCSVWriter, current model.Report) error {
		return writePurchasesUnmatchedCSV(writer, current, location)
	})
}

// PurchasesUnmatchedCSVBytes writes only purchases absent from the traced
// current-UTXO ancestry.
func PurchasesUnmatchedCSVBytes(report model.Report) ([]byte, error) {
	return purchasesUnmatchedCSVBytes(report, purchaseLocation())
}

type purchaseDay struct {
	statementDate string
	utcDates      []string
	providers     []string
	purchaseIDs   []string
	txids         []string
	count         int
	totalSats     int64
}

func purchaseDays(report model.Report, location *time.Location) []purchaseDay {
	days := map[string]*purchaseDay{}
	order := make([]string, 0)
	for _, purchase := range report.Purchases {
		key := purchaseStatementDate(purchase, location)
		day, exists := days[key]
		if !exists {
			day = &purchaseDay{statementDate: key}
			days[key] = day
			order = append(order, key)
		}
		day.count++
		if purchase.AmountSats != nil {
			day.totalSats += *purchase.AmountSats
		}
		day.utcDates = append(day.utcDates, purchaseUTCDate(purchase))
		day.providers = append(day.providers, purchase.Provider)
		day.purchaseIDs = append(day.purchaseIDs, purchase.ID)
		day.txids = append(day.txids, purchase.TxIDs...)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i] == "" {
			return order[j] != ""
		}
		if order[j] == "" {
			return false
		}
		return order[i] < order[j]
	})
	result := make([]purchaseDay, 0, len(order))
	for _, key := range order {
		result = append(result, *days[key])
	}
	return result
}

func purchaseDatesCSVBytes(report model.Report, location *time.Location) ([]byte, error) {
	return csvBytes(report, func(writer reportCSVWriter, current model.Report) error {
		return writePurchaseDatesCSV(writer, current, location)
	})
}

func writePurchaseDatesCSV(writer reportCSVWriter, report model.Report, location *time.Location) error {
	header := []string{"statement_date", "purchase_count", "total_sats", "total_btc",
		"providers", "purchase_ids", "txids", "date_utc_days"}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, day := range purchaseDays(report, location) {
		record := []string{day.statementDate, strconv.Itoa(day.count),
			strconv.FormatInt(day.totalSats, 10), strings.TrimSuffix(btcString(day.totalSats), " BTC"),
			joinUnique(day.providers, "|"), joinUnique(day.purchaseIDs, "|"),
			joinUnique(day.txids, "|"), joinUnique(day.utcDates, "|")}
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

// PurchaseDatesCSVBytes writes one row per statement day for bank-statement
// downloads, with totals and the matching purchase/tx identifiers.
func PurchaseDatesCSVBytes(report model.Report) ([]byte, error) {
	return purchaseDatesCSVBytes(report, purchaseLocation())
}

type purchaseMonthRow struct {
	key       string
	count     int
	totalSats int64
	providers []string
	ids       []string
}

func purchaseMonths(report model.Report, location *time.Location) []purchaseMonthRow {
	rows := map[string]*purchaseMonthRow{}
	order := make([]string, 0)
	for _, purchase := range report.Purchases {
		statement := purchaseStatementDate(purchase, location)
		key := "undated"
		if len(statement) >= 7 {
			key = statement[:7]
		}
		row, exists := rows[key]
		if !exists {
			row = &purchaseMonthRow{key: key}
			rows[key] = row
			order = append(order, key)
		}
		row.count++
		if purchase.AmountSats != nil {
			row.totalSats += *purchase.AmountSats
		}
		row.providers = append(row.providers, purchase.Provider)
		row.ids = append(row.ids, purchase.ID)
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i] == "undated" {
			return order[j] != "undated"
		}
		if order[j] == "undated" {
			return false
		}
		return order[i] < order[j]
	})
	result := make([]purchaseMonthRow, 0, len(order))
	for _, key := range order {
		result = append(result, *rows[key])
	}
	return result
}

func writePurchaseMonthsCSV(writer reportCSVWriter, report model.Report, location *time.Location) error {
	header := []string{"year", "month", "month_label", "purchase_count", "total_sats",
		"total_btc", "providers", "purchase_ids"}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, row := range purchaseMonths(report, location) {
		year, month := "", ""
		if row.key != "undated" {
			year, month = row.key[:4], row.key[5:7]
		}
		record := []string{year, month, row.key, strconv.Itoa(row.count),
			strconv.FormatInt(row.totalSats, 10), strings.TrimSuffix(btcString(row.totalSats), " BTC"),
			joinUnique(row.providers, "|"), joinUnique(row.ids, "|")}
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func purchaseMonthsCSVBytes(report model.Report, location *time.Location) ([]byte, error) {
	return csvBytes(report, func(writer reportCSVWriter, current model.Report) error {
		return writePurchaseMonthsCSV(writer, current, location)
	})
}

// PurchaseMonthsCSVBytes writes one row per purchase month.
func PurchaseMonthsCSVBytes(report model.Report) ([]byte, error) {
	return purchaseMonthsCSVBytes(report, purchaseLocation())
}

type purchaseYearRow struct {
	key       string
	count     int
	totalSats int64
}

func purchaseYears(report model.Report, location *time.Location) []purchaseYearRow {
	rows := map[string]*purchaseYearRow{}
	order := make([]string, 0)
	for _, purchase := range report.Purchases {
		statement := purchaseStatementDate(purchase, location)
		key := "undated"
		if len(statement) >= 4 {
			key = statement[:4]
		}
		row, exists := rows[key]
		if !exists {
			row = &purchaseYearRow{key: key}
			rows[key] = row
			order = append(order, key)
		}
		row.count++
		if purchase.AmountSats != nil {
			row.totalSats += *purchase.AmountSats
		}
	}
	sort.Slice(order, func(i, j int) bool {
		if order[i] == "undated" {
			return order[j] != "undated"
		}
		if order[j] == "undated" {
			return false
		}
		return order[i] < order[j]
	})
	result := make([]purchaseYearRow, 0, len(order))
	for _, key := range order {
		result = append(result, *rows[key])
	}
	return result
}

func writePurchaseYearsCSV(writer reportCSVWriter, report model.Report, location *time.Location) error {
	header := []string{"year", "purchase_count", "total_sats", "total_btc"}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, row := range purchaseYears(report, location) {
		record := []string{row.key, strconv.Itoa(row.count),
			strconv.FormatInt(row.totalSats, 10), strings.TrimSuffix(btcString(row.totalSats), " BTC")}
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func purchaseYearsCSVBytes(report model.Report, location *time.Location) ([]byte, error) {
	return csvBytes(report, func(writer reportCSVWriter, current model.Report) error {
		return writePurchaseYearsCSV(writer, current, location)
	})
}

// PurchaseYearsCSVBytes writes one row per purchase year.
func PurchaseYearsCSVBytes(report model.Report) ([]byte, error) {
	return purchaseYearsCSVBytes(report, purchaseLocation())
}
