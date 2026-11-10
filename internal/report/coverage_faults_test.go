package report

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

type reportFaultTemp struct {
	fail    string
	closed  int
	written []byte
}

func (f *reportFaultTemp) Chmod(os.FileMode) error {
	if f.fail == "chmod" {
		return errors.New("chmod failure")
	}
	return nil
}
func (f *reportFaultTemp) Write(data []byte) (int, error) {
	if f.fail == "write" {
		return 0, errors.New("write failure")
	}
	f.written = append([]byte(nil), data...)
	return len(data), nil
}
func (f *reportFaultTemp) Sync() error {
	if f.fail == "sync" {
		return errors.New("sync failure")
	}
	return nil
}
func (f *reportFaultTemp) Close() error {
	f.closed++
	if f.fail == "close" {
		return errors.New("close failure")
	}
	return nil
}

func TestCoverageAtomicTempFailureStages(t *testing.T) {
	for _, stage := range []string{"chmod", "write", "sync", "close"} {
		t.Run(stage, func(t *testing.T) {
			file := &reportFaultTemp{fail: stage}
			if err := writeAtomicTemp(file, []byte("data"), 0o600); err == nil {
				t.Fatalf("%s failure was accepted", stage)
			}
			if stage != "close" && file.closed != 1 {
				t.Fatalf("file was not closed after %s failure", stage)
			}
		})
	}
	file := &reportFaultTemp{}
	if err := writeAtomicTemp(file, []byte("data"), 0o600); err != nil || file.closed != 1 || string(file.written) != "data" {
		t.Fatalf("successful temp write = %+v, %v", file, err)
	}
}

type reportFaultCSVWriter struct {
	writeErrAt int
	flushErr   error
	writes     int
}

func (w *reportFaultCSVWriter) Write([]string) error {
	w.writes++
	if w.writes == w.writeErrAt {
		return errors.New("writer failure")
	}
	return nil
}
func (w *reportFaultCSVWriter) Flush()       {}
func (w *reportFaultCSVWriter) Error() error { return w.flushErr }

func TestCoverageCSVWriterFailureStages(t *testing.T) {
	report := testReport()
	for _, writeErrAt := range []int{1, 2} {
		writer := &reportFaultCSVWriter{writeErrAt: writeErrAt}
		if err := writeCSV(writer, report); err == nil {
			t.Fatalf("writeCSV accepted failure at write %d", writeErrAt)
		}
	}
	if err := writeCSV(&reportFaultCSVWriter{flushErr: errors.New("flush failure")}, report); err == nil {
		t.Fatal("writeCSV accepted flush failure")
	}
	if err := writeEvidenceCSV(&reportFaultCSVWriter{writeErrAt: 1}, report); err == nil {
		t.Fatal("evidence header failure accepted")
	}
	report.Evidence = []model.EvidenceStatus{{Kind: "address", Value: "x"}}
	if err := writeEvidenceCSV(&reportFaultCSVWriter{writeErrAt: 2}, report); err == nil {
		t.Fatal("evidence row failure accepted")
	}
	if err := writeEvidenceCSV(&reportFaultCSVWriter{flushErr: errors.New("flush failure")}, report); err == nil {
		t.Fatal("evidence flush failure accepted")
	}
	if _, err := csvBytes(report, func(reportCSVWriter, model.Report) error { return errors.New("encoder") }); err == nil {
		t.Fatal("csv encoder failure accepted")
	}
	if _, err := evidenceCSVBytes(report, func(reportCSVWriter, model.Report) error { return errors.New("encoder") }); err == nil {
		t.Fatal("evidence encoder failure accepted")
	}
}

func TestCoverageHTMLMarshalFailureStages(t *testing.T) {
	calls := 0
	failing := func(any) ([]byte, error) { calls++; return nil, errors.New("marshal failure") }
	if _, err := htmlBytes(testReport(), false, failing); err == nil || calls != 1 {
		t.Fatal("report marshal failure not handled")
	}
	calls = 0
	secondFails := func(value any) ([]byte, error) {
		calls++
		if calls == 1 {
			return []byte("{}"), nil
		}
		return nil, errors.New("config marshal failure")
	}
	if _, err := htmlBytes(testReport(), true, secondFails); err == nil || calls != 2 {
		t.Fatal("config marshal failure not handled")
	}
}

func validStructuredSteps() structuredSteps {
	data := func() ([]byte, error) { return []byte("ok"), nil }
	return structuredSteps{
		json: data, csv: data, evidenceCSV: data, html: data, svg: data,
		atomicWrite: func(string, []byte, os.FileMode) error { return nil },
	}
}

func TestCoverageStructuredFailureStages(t *testing.T) {
	failure := func() ([]byte, error) { return nil, errors.New("step failure") }
	cases := []string{"json", "csv", "evidence", "html", "svg"}
	for _, stage := range cases {
		t.Run(stage, func(t *testing.T) {
			steps := validStructuredSteps()
			switch stage {
			case "json":
				steps.json = failure
			case "csv":
				steps.csv = failure
			case "evidence":
				steps.evidenceCSV = failure
			case "html":
				steps.html = failure
			case "svg":
				steps.svg = failure
			}
			if err := writeStructured(steps, t.TempDir()); err == nil {
				t.Fatalf("%s failure was accepted", stage)
			}
		})
	}
	for _, name := range []string{"report.json", "report.csv", "evidence.csv", "report.svg", "report.html"} {
		steps := validStructuredSteps()
		steps.atomicWrite = func(path string, _ []byte, _ os.FileMode) error {
			if filepath.Base(path) == name {
				return errors.New("atomic write failure")
			}
			return nil
		}
		if err := writeStructured(steps, t.TempDir()); err == nil || !strings.Contains(err.Error(), name) {
			t.Errorf("atomic failure for %s = %v", name, err)
		}
	}
	if err := writeStructured(validStructuredSteps(), t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

type reportFailingWriter struct {
	remaining int
	closeErr  error
}

func (w *reportFailingWriter) Write(data []byte) (int, error) {
	if w.remaining <= 0 {
		return 0, errors.New("zip write failure")
	}
	if len(data) > w.remaining {
		w.remaining = 0
		return 0, errors.New("zip partial write failure")
	}
	w.remaining -= len(data)
	return len(data), nil
}

type reportWriteCountFailWriter struct {
	failAt int
	writes int
}

func (w *reportWriteCountFailWriter) Write(data []byte) (int, error) {
	w.writes++
	if w.writes == w.failAt {
		return 0, errors.New("zip write call failure")
	}
	return len(data), nil
}

func TestCoverageZIPWriterFailures(t *testing.T) {
	files := map[string][]byte{"a.txt": []byte(strings.Repeat("x", 100))}
	for budget := 0; budget < 12; budget++ {
		_ = writeZIP(&reportFailingWriter{remaining: budget}, files)
	}
	for failAt := 1; failAt <= 6; failAt++ {
		writer := &reportWriteCountFailWriter{failAt: failAt}
		_ = writeZIP(writer, files)
	}
	large := make([]byte, 1<<20)
	for index := range large {
		large[index] = byte(index*31 + index/251)
	}
	if err := writeZIP(&reportWriteCountFailWriter{failAt: 2}, map[string][]byte{"large.bin": large}); err == nil {
		t.Fatal("writeZIP ignored a data-write failure")
	}
	if err := writeZIP(&reportFailingWriter{remaining: 100000}, nil); err != nil {
		t.Fatalf("empty ZIP: %v", err)
	}
}

type reportZIPWriter struct {
	headerErr error
	closeErr  error
	entry     io.Writer
}

func (w *reportZIPWriter) CreateHeader(*zip.FileHeader) (io.Writer, error) {
	if w.headerErr != nil {
		return nil, w.headerErr
	}
	return w.entry, nil
}
func (w *reportZIPWriter) Close() error { return w.closeErr }

type reportErrorWriter struct{ err error }

func (w reportErrorWriter) Write([]byte) (int, error) { return 0, w.err }

func TestCoverageZIPWriterFactoryFailureStages(t *testing.T) {
	files := map[string][]byte{"a.txt": []byte("data")}
	for _, test := range []struct {
		name   string
		writer *reportZIPWriter
	}{
		{"header", &reportZIPWriter{headerErr: errors.New("header failure")}},
		{"entry", &reportZIPWriter{entry: reportErrorWriter{err: errors.New("entry failure")}}},
		{"close", &reportZIPWriter{entry: io.Discard, closeErr: errors.New("close failure")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := writeZIPWith(io.Discard, files, func(io.Writer) zipArchiveWriter { return test.writer })
			if err == nil {
				t.Fatalf("%s failure was ignored", test.name)
			}
		})
	}
}

type reportEvidenceReader struct {
	data              []byte
	readErr, closeErr error
}

func (r *reportEvidenceReader) Read(p []byte) (int, error) {
	if r.readErr != nil {
		return 0, r.readErr
	}
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.data)
	r.data = r.data[n:]
	return n, nil
}
func (r *reportEvidenceReader) Close() error { return r.closeErr }

func TestCoverageVerifiedEvidenceReadErrors(t *testing.T) {
	if _, err := readVerifiedEvidence(&reportEvidenceReader{readErr: errors.New("read failure")}, "a", "hash"); err == nil {
		t.Fatal("read failure accepted")
	}
	if _, err := readVerifiedEvidence(&reportEvidenceReader{closeErr: errors.New("close failure")}, "a", "hash"); err == nil {
		t.Fatal("close failure accepted")
	}
	if _, err := readVerifiedEvidence(&reportEvidenceReader{data: []byte("data")}, "a", strings.Repeat("0", 64)); err == nil {
		t.Fatal("hash mismatch accepted")
	}
}

func TestCoverageBundleArchiveFailure(t *testing.T) {
	dataDir, outDir, purchases, _ := coverageBundleFixture(t)
	err := bundle(testReport(), purchases, dataDir, outDir, func(map[string][]byte) ([]byte, error) { return nil, errors.New("archive failure") })
	if err == nil || !strings.Contains(err.Error(), "archive failure") {
		t.Fatalf("archive failure = %v", err)
	}
	if err := Bundle(testReport(), purchases, dataDir, outDir); err != nil {
		t.Fatal(err)
	}
	if data, err := os.ReadFile(filepath.Join(outDir, "accountant.zip")); err != nil || !bytes.HasPrefix(data, []byte("PK")) {
		t.Fatalf("bundle wrapper output: %v", err)
	}
}
