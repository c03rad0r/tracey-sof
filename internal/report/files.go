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
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
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

func CSVBytes(report model.Report) ([]byte, error) {
	var out strings.Builder
	writer := csv.NewWriter(&out)
	header := []string{"outpoint", "txid", "vout", "address", "value_sats", "documentation_status", "trace_status", "purchase_ids", "owner_ids", "labels", "notes"}
	if err := writer.Write(header); err != nil {
		return nil, err
	}
	for _, utxo := range report.UTXOs {
		record := []string{utxo.Key(), utxo.TxID, strconv.FormatUint(uint64(utxo.Vout), 10), utxo.Address,
			strconv.FormatInt(utxo.ValueSats, 10), utxo.Status, utxo.Trace,
			strings.Join(utxo.PurchaseIDs, "|"), strings.Join(utxo.OwnerIDs, "|"), strings.Join(utxo.Labels, "|"), strings.Join(utxo.Notes, "|")}
		if err := writer.Write(record); err != nil {
			return nil, err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

func HTMLBytes(report model.Report, server bool) ([]byte, error) {
	data, err := json.Marshal(report)
	if err != nil {
		return nil, err
	}
	config, err := json.Marshal(map[string]any{"server": server, "save_url": "/api/annotations"})
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

func WriteStructured(report model.Report, outDir string, server bool) error {
	jsonData, err := JSONBytes(report)
	if err != nil {
		return err
	}
	csvData, err := CSVBytes(report)
	if err != nil {
		return err
	}
	htmlData, err := HTMLBytes(report, server)
	if err != nil {
		return err
	}
	svgData, err := SVGBytes(report)
	if err != nil {
		return err
	}
	files := map[string][]byte{"report.json": jsonData, "report.csv": csvData, "report.html": htmlData, "report.svg": svgData}
	// Fixed order makes partial failure behavior clear and tests deterministic.
	for _, name := range []string{"report.json", "report.csv", "report.svg", "report.html"} {
		if err := AtomicWrite(filepath.Join(outDir, name), files[name], 0o600); err != nil {
			return fmt.Errorf("write %s: %w", name, err)
		}
	}
	return nil
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
