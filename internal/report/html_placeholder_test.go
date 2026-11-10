package report

import (
	"strings"
	"testing"
)

func TestHTMLAcceptsLiteralPlaceholderTextInUserEvidence(t *testing.T) {
	report := testReport()
	literal := strings.Join([]string{"/*__CSS__*/", "/*__GRAPH_JS__*/", "/*__APP_JS__*/", "/*__REPORT_JSON__*/", "/*__SERVER_CONFIG__*/"}, " / ")
	report.Purchases[0].Provider = literal
	report.Purchases[0].Notes = literal
	data, err := HTMLBytes(report, false)
	if err != nil {
		t.Fatalf("literal user evidence must not be treated as an unresolved template: %v", err)
	}
	if !strings.Contains(string(data), literal) {
		t.Fatal("literal evidence was not preserved")
	}
}
