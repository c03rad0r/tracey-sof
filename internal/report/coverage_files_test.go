package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"tracey-sof/internal/model"
	webassets "tracey-sof/web"
)

func TestCoverageAtomicWriteSuccessAndFilesystemErrors(t *testing.T) {
	t.Run("creates parents and replaces atomically", func(t *testing.T) {
		root := t.TempDir()
		name := filepath.Join(root, "nested", "reports", "result.txt")
		coverageWriteFile(t, name, []byte("old contents that are longer"))
		want := []byte("new\x00contents\n")
		if err := AtomicWrite(name, want, 0o640); err != nil {
			t.Fatal(err)
		}
		got, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("contents = %q, want %q", got, want)
		}
		info, err := os.Stat(name)
		if err != nil {
			t.Fatal(err)
		}
		if gotMode := info.Mode().Perm(); gotMode != 0o640 {
			t.Errorf("mode = %04o, want 0640", gotMode)
		}
		matches, err := filepath.Glob(filepath.Join(filepath.Dir(name), ".report-*.tmp"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 0 {
			t.Errorf("temporary files left behind: %v", matches)
		}
	})

	t.Run("parent component is a file", func(t *testing.T) {
		root := t.TempDir()
		parent := filepath.Join(root, "not-a-directory")
		coverageWriteFile(t, parent, []byte("file"))
		if err := AtomicWrite(filepath.Join(parent, "result.txt"), []byte("data"), 0o600); err == nil {
			t.Fatal("AtomicWrite accepted a regular file as its parent")
		}
	})

	t.Run("temporary file cannot be created", func(t *testing.T) {
		root := t.TempDir()
		directory := filepath.Join(root, "read-only")
		if err := os.Mkdir(directory, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(directory, 0o700) })
		if err := AtomicWrite(filepath.Join(directory, "result.txt"), []byte("data"), 0o600); err == nil {
			t.Fatal("AtomicWrite created a temporary file in a read-only directory")
		}
	})

	t.Run("rename failure cleans temporary file", func(t *testing.T) {
		root := t.TempDir()
		destination := filepath.Join(root, "destination")
		if err := os.Mkdir(destination, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := AtomicWrite(destination, []byte("data"), 0o600); err == nil {
			t.Fatal("AtomicWrite replaced a directory with a regular file")
		}
		matches, err := filepath.Glob(filepath.Join(root, ".report-*.tmp"))
		if err != nil {
			t.Fatal(err)
		}
		if len(matches) != 0 {
			t.Errorf("failed rename left temporary files: %v", matches)
		}
	})
}

func TestCoverageCSVBytesRoundTripAndEmptyReport(t *testing.T) {
	report := testReport()
	report.UTXOs[0].Address = "address,with comma"
	report.UTXOs[0].PurchaseIDs = []string{"buy-1", "buy-2"}
	report.UTXOs[0].OwnerIDs = []string{"owner\"quoted"}
	report.UTXOs[0].Labels = []string{"first line\nsecond line", "plain"}
	report.UTXOs[0].Notes = []string{"note, one", "note \"two\""}
	report.UTXOs = append(report.UTXOs, model.Target{TxID: strings.Repeat("a", 64), Vout: 42, ValueSats: -7, Status: "undocumented", Trace: "incomplete"})
	data, err := CSVBytes(report)
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		t.Fatalf("generated CSV does not parse: %v\n%s", err, data)
	}
	if len(records) != 3 {
		t.Fatalf("CSV has %d records, want header and two rows", len(records))
	}
	for i, record := range records {
		if len(record) != 13 {
			t.Errorf("record %d has %d columns, want 13", i, len(record))
		}
	}
	first := records[1]
	checks := map[int]string{
		0:  report.UTXOs[0].Key(),
		1:  report.UTXOs[0].TxID,
		2:  "0",
		3:  "address,with comma",
		4:  "100",
		5:  "documented",
		6:  "complete",
		7:  "buy-1|buy-2",
		8:  "owner\"quoted",
		9:  "first line\nsecond line|plain",
		10: "note, one|note \"two\"",
	}
	for column, want := range checks {
		if first[column] != want {
			t.Errorf("column %d = %q, want %q", column, first[column], want)
		}
	}
	if records[2][1] != strings.Repeat("a", 64) || records[2][2] != "42" || records[2][4] != "-7" {
		t.Errorf("second row was not encoded correctly: %q", records[2])
	}

	empty, err := CSVBytes(model.Report{})
	if err != nil {
		t.Fatal(err)
	}
	emptyRecords, err := csv.NewReader(bytes.NewReader(empty)).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(emptyRecords) != 1 || !reflect.DeepEqual(emptyRecords[0], records[0]) {
		t.Errorf("empty report CSV = %q, want only the standard header", emptyRecords)
	}
}

func coverageScriptJSON(t *testing.T, html, id string) []byte {
	t.Helper()
	start := `<script id="` + id + `" type="application/json">`
	from := strings.Index(html, start)
	if from < 0 {
		t.Fatalf("embedded %s script start not found", id)
	}
	from += len(start)
	to := strings.Index(html[from:], "</script>")
	if to < 0 {
		t.Fatalf("embedded %s script end not found", id)
	}
	return []byte(html[from : from+to])
}

func TestCoverageHTMLServerModeAndScriptEscaping(t *testing.T) {
	report := testReport()
	payload := "</script><script>globalThis.compromised=true</script>&\u2028"
	report.Warnings = []string{payload}
	report.Graph.Nodes[0].Label = payload
	report.UTXOs[0].Notes = []string{payload}
	page, err := HTMLBytes(report, true)
	if err != nil {
		t.Fatal(err)
	}
	text := string(page)
	if strings.Contains(text, payload) {
		t.Fatal("raw script-breaking user input appears in generated HTML")
	}
	embedded := coverageScriptJSON(t, text, "report-data")
	lower := strings.ToLower(string(embedded))
	for _, unsafe := range []string{"</script", "<script", "<", ">", "&"} {
		if strings.Contains(lower, unsafe) {
			t.Errorf("embedded report JSON contains unsafe literal %q: %s", unsafe, embedded)
		}
	}
	var decoded model.Report
	if err := json.Unmarshal(embedded, &decoded); err != nil {
		t.Fatalf("embedded report data is not valid JSON: %v", err)
	}
	if len(decoded.Warnings) != 1 || decoded.Warnings[0] != payload || decoded.Graph.Nodes[0].Label != payload || decoded.UTXOs[0].Notes[0] != payload {
		t.Error("escaped user input did not survive JSON round trip")
	}

	configJSON := coverageScriptJSON(t, text, "server-config")
	var config struct {
		Server  bool   `json:"server"`
		SaveURL string `json:"save_url"`
	}
	if err := json.Unmarshal(configJSON, &config); err != nil {
		t.Fatalf("server configuration is not valid JSON: %v", err)
	}
	if !config.Server || config.SaveURL != "/api/annotations" {
		t.Errorf("server config = %+v, want server mode and annotation endpoint", config)
	}
}

func TestCoverageHTMLRejectsMalformedAssetTemplate(t *testing.T) {
	original := webassets.Index
	webassets.Index = `<html><script type="application/json">/*__REPORT_JSON__*/</script><!-- /*__REPORT_JSON__*/ --></html>`
	t.Cleanup(func() { webassets.Index = original })
	if _, err := HTMLBytes(testReport(), false); err == nil || !strings.Contains(err.Error(), "HTML asset placeholder") {
		t.Fatalf("HTMLBytes malformed-template error = %v", err)
	}
	if err := WriteStructured(testReport(), t.TempDir(), false); err == nil {
		t.Fatal("WriteStructured accepted a malformed HTML asset template")
	}
}

func TestCoverageLoadRejectsMalformedAndUnsupportedReports(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{name: "malformed JSON", data: `{"schema_version":`, want: "decode report"},
		{name: "missing version", data: `{}`, want: "unsupported report schema 0"},
		{name: "future version", data: `{"schema_version":2}`, want: "unsupported report schema 2"},
		{name: "malformed target", data: `{"schema_version":1,"utxos":[{"txid":"abc","vout":0,"outpoint":"bad"}]}`, want: "decode report"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			name := filepath.Join(t.TempDir(), "report.json")
			coverageWriteFile(t, name, []byte(test.data))
			_, err := Load(name)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load error = %v, want text %q", err, test.want)
			}
		})
	}

	t.Run("minimal current version", func(t *testing.T) {
		name := filepath.Join(t.TempDir(), "report.json")
		coverageWriteFile(t, name, []byte(`{"schema_version":`+strconv.Itoa(model.SchemaVersion)+`}`))
		got, err := Load(name)
		if err != nil {
			t.Fatal(err)
		}
		if got.SchemaVersion != model.SchemaVersion {
			t.Errorf("schema version = %d, want %d", got.SchemaVersion, model.SchemaVersion)
		}
	})
}

func TestCoverageWriteStructuredWrapsOutputErrors(t *testing.T) {
	for _, blocked := range []string{"report.json", "report.csv", "report.svg", "report.html"} {
		t.Run(blocked, func(t *testing.T) {
			outDir := t.TempDir()
			if err := os.Mkdir(filepath.Join(outDir, blocked), 0o700); err != nil {
				t.Fatal(err)
			}
			err := WriteStructured(testReport(), outDir, false)
			if err == nil || !strings.Contains(err.Error(), "write "+blocked) {
				t.Fatalf("WriteStructured error = %v, want write %s", err, blocked)
			}
		})
	}
}
