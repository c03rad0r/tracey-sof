package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"tracey-sof/internal/model"
	webassets "tracey-sof/web"
)

type atomicTempFile interface {
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

func writeAtomicTemp(file atomicTempFile, data []byte, mode os.FileMode) error {
	if err := file.Chmod(mode); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func AtomicWrite(path string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".report-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if err := writeAtomicTemp(tmp, data, mode); err != nil {
		return err
	}
	return os.Rename(name, path)
}

func JSONBytes(value any) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encode JSON: %w", err)
	}
	return append(data, '\n'), nil
}

type reportCSVWriter interface {
	Write([]string) error
	Flush()
	Error() error
}

func writeCSV(writer reportCSVWriter, report model.Report) error {
	header := []string{"outpoint", "txid", "vout", "address", "value_sats", "documentation_status", "trace_status", "purchase_ids", "owner_ids", "labels", "notes", "review_required", "review_reasons"}
	if err := writer.Write(header); err != nil {
		return err
	}
	for _, utxo := range report.UTXOs {
		record := []string{utxo.Key(), utxo.TxID, strconv.FormatUint(uint64(utxo.Vout), 10), utxo.Address,
			strconv.FormatInt(utxo.ValueSats, 10), utxo.Status, utxo.Trace,
			strings.Join(utxo.PurchaseIDs, "|"), strings.Join(utxo.OwnerIDs, "|"), strings.Join(utxo.Labels, "|"), strings.Join(utxo.Notes, "|"),
			strconv.FormatBool(utxo.ReviewRequired), strings.Join(utxo.ReviewReasons, "|")}
		if err := writer.Write(record); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func csvBytes(report model.Report, encode func(reportCSVWriter, model.Report) error) ([]byte, error) {
	var out strings.Builder
	if err := encode(csv.NewWriter(&out), report); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

func CSVBytes(report model.Report) ([]byte, error) {
	return csvBytes(report, writeCSV)
}

func EvidenceCSVBytes(report model.Report) ([]byte, error) {
	return evidenceCSVBytes(report, writeEvidenceCSV)
}

func writeEvidenceCSV(writer reportCSVWriter, report model.Report) error {
	if err := writer.Write([]string{"kind", "value", "purchase_ids", "matched", "matched_node_id"}); err != nil {
		return err
	}
	for _, item := range report.Evidence {
		if err := writer.Write([]string{item.Kind, item.Value, strings.Join(item.PurchaseIDs, "|"), strconv.FormatBool(item.Matched), item.MatchedNodeID}); err != nil {
			return err
		}
	}
	writer.Flush()
	return writer.Error()
}

func evidenceCSVBytes(report model.Report, encode func(reportCSVWriter, model.Report) error) ([]byte, error) {
	var out strings.Builder
	if err := encode(csv.NewWriter(&out), report); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

type jsonMarshalFunc func(any) ([]byte, error)

func htmlBytes(report model.Report, server bool, marshal jsonMarshalFunc) ([]byte, error) {
	data, err := marshal(report)
	if err != nil {
		return nil, err
	}
	config, err := marshal(map[string]any{"server": server, "save_url": "/api/annotations"})
	if err != nil {
		return nil, err
	}
	page := webassets.Index
	for _, marker := range []string{"/*__CSS__*/", "/*__GRAPH_JS__*/", "/*__APP_JS__*/", "/*__REPORT_JSON__*/", "/*__SERVER_CONFIG__*/"} {
		if strings.Count(page, marker) != 1 {
			return nil, fmt.Errorf("HTML asset placeholder %s must occur exactly once in the template", marker)
		}
	}
	page = strings.Replace(page, "/*__CSS__*/", webassets.CSS, 1)
	page = strings.Replace(page, "/*__GRAPH_JS__*/", webassets.GraphJS, 1)
	page = strings.Replace(page, "/*__APP_JS__*/", webassets.AppJS, 1)
	page = strings.Replace(page, "/*__REPORT_JSON__*/", string(data), 1)
	page = strings.Replace(page, "/*__SERVER_CONFIG__*/", string(config), 1)
	return []byte(page), nil
}

func HTMLBytes(report model.Report, server bool) ([]byte, error) {
	return htmlBytes(report, server, json.Marshal)
}

type structuredSteps struct {
	json                     func() ([]byte, error)
	csv                      func() ([]byte, error)
	evidenceCSV              func() ([]byte, error)
	purchasesCSV             func() ([]byte, error)
	purchasesUnmatchedCSV    func() ([]byte, error)
	purchaseDatesCSV         func() ([]byte, error)
	purchaseMonthsCSV        func() ([]byte, error)
	purchaseYearsCSV         func() ([]byte, error)
	exchangeGapsCSV          func() ([]byte, error)
	exchangeOutflowsCSV      func() ([]byte, error)
	exchangeGapMonthsCSV     func() ([]byte, error)
	exchangeOutflowMonthsCSV func() ([]byte, error)
	html                     func() ([]byte, error)
	svg                      func() ([]byte, error)
	atomicWrite              func(string, []byte, os.FileMode) error
}

func writeStructured(steps structuredSteps, outDir string) error {
	jsonData, err := steps.json()
	if err != nil {
		return err
	}
	csvData, err := steps.csv()
	if err != nil {
		return err
	}
	evidenceCSV, err := steps.evidenceCSV()
	if err != nil {
		return err
	}
	purchasesCSV, err := steps.purchasesCSV()
	if err != nil {
		return err
	}
	purchaseDatesCSV, err := steps.purchaseDatesCSV()
	if err != nil {
		return err
	}
	purchasesUnmatchedCSV, err := steps.purchasesUnmatchedCSV()
	if err != nil {
		return err
	}
	purchaseMonthsCSV, err := steps.purchaseMonthsCSV()
	if err != nil {
		return err
	}
	purchaseYearsCSV, err := steps.purchaseYearsCSV()
	if err != nil {
		return err
	}
	exchangeGapsCSV, err := steps.exchangeGapsCSV()
	if err != nil {
		return err
	}
	exchangeOutflowsCSV, err := steps.exchangeOutflowsCSV()
	if err != nil {
		return err
	}
	exchangeGapMonthsCSV, err := steps.exchangeGapMonthsCSV()
	if err != nil {
		return err
	}
	exchangeOutflowMonthsCSV, err := steps.exchangeOutflowMonthsCSV()
	if err != nil {
		return err
	}
	htmlData, err := steps.html()
	if err != nil {
		return err
	}
	svgData, err := steps.svg()
	if err != nil {
		return err
	}
	files := map[string][]byte{"report.json": jsonData, "report.csv": csvData, "evidence.csv": evidenceCSV,
		"purchases.csv": purchasesCSV, "purchases-unmatched.csv": purchasesUnmatchedCSV,
		"purchase-dates.csv": purchaseDatesCSV, "purchase-months.csv": purchaseMonthsCSV,
		"purchase-years.csv":                purchaseYearsCSV,
		"exchange-payouts-unaccounted.csv":  exchangeGapsCSV,
		"exchange-outflows-unaccounted.csv": exchangeOutflowsCSV,
		"exchange-payout-months.csv":        exchangeGapMonthsCSV,
		"exchange-outflow-months.csv":       exchangeOutflowMonthsCSV,
		"report.html":                       htmlData, "report.svg": svgData}
	// Fixed order makes partial failure behavior clear and tests deterministic.
	for _, name := range []string{"report.json", "report.csv", "evidence.csv", "purchases.csv", "purchases-unmatched.csv", "purchase-dates.csv", "purchase-months.csv", "purchase-years.csv", "exchange-payouts-unaccounted.csv", "exchange-outflows-unaccounted.csv", "exchange-payout-months.csv", "exchange-outflow-months.csv", "report.svg", "report.html"} {
		if err := steps.atomicWrite(filepath.Join(outDir, name), files[name], 0o600); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
}

func WriteStructured(report model.Report, outDir string, server bool) error {
	return writeStructured(structuredSteps{
		json:                     func() ([]byte, error) { return JSONBytes(report) },
		csv:                      func() ([]byte, error) { return CSVBytes(report) },
		evidenceCSV:              func() ([]byte, error) { return EvidenceCSVBytes(report) },
		purchasesCSV:             func() ([]byte, error) { return PurchasesCSVBytes(report) },
		purchasesUnmatchedCSV:    func() ([]byte, error) { return PurchasesUnmatchedCSVBytes(report) },
		purchaseDatesCSV:         func() ([]byte, error) { return PurchaseDatesCSVBytes(report) },
		purchaseMonthsCSV:        func() ([]byte, error) { return PurchaseMonthsCSVBytes(report) },
		purchaseYearsCSV:         func() ([]byte, error) { return PurchaseYearsCSVBytes(report) },
		exchangeGapsCSV:          func() ([]byte, error) { return ExchangeGapsCSVBytes(report) },
		exchangeOutflowsCSV:      func() ([]byte, error) { return ExchangeOutflowsCSVBytes(report) },
		exchangeGapMonthsCSV:     func() ([]byte, error) { return ExchangeGapMonthsCSVBytes(report) },
		exchangeOutflowMonthsCSV: func() ([]byte, error) { return ExchangeOutflowMonthsCSVBytes(report) },
		html:                     func() ([]byte, error) { return HTMLBytes(report, server) },
		svg:                      func() ([]byte, error) { return SVGBytes(report) },
		atomicWrite:              AtomicWrite,
	}, outDir)
}

func Load(path string) (model.Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Report{}, fmt.Errorf("read report: %w", err)
	}
	var report model.Report
	if err := json.Unmarshal(data, &report); err != nil {
		return model.Report{}, fmt.Errorf("decode report: %w", err)
	}
	if report.SchemaVersion != model.SchemaVersion {
		return model.Report{}, fmt.Errorf("unsupported report schema %d", report.SchemaVersion)
	}
	return report, nil
}
