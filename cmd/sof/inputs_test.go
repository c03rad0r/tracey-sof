package main

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

func TestPrivateInputsRoot(t *testing.T) {
	t.Setenv("TRACEY_INPUTS", "/tmp/private-inputs")
	if got := privateInputsRoot(); got != "/tmp/private-inputs" {
		t.Fatalf("TRACEY_INPUTS root = %q", got)
	}
	t.Setenv("TRACEY_INPUTS", "   ")
	t.Setenv("HOME", "/home/example")
	if got := privateInputsRoot(); got != "/home/example/tracey-inputs" {
		t.Fatalf("HOME root = %q", got)
	}
}

func TestBankImportRequiresMerchantAndProvider(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
		want string
	}{
		{"merchant", []string{"bank-import", "-bank-file", "bank.csv"}, "-merchant"},
		{"provider", []string{"bank-import", "-bank-file", "bank.csv", "-merchant", "Example Merchant"}, "-provider"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			err := run(context.Background(), test.args, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}
}
