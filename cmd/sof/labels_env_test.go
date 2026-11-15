package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBankLabelsAreScopedToEvidenceTargets(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	wrapper := filepath.Join(dir, "labels.mk")
	contents := fmt.Sprintf(`include %s
build: ; @true
ansible-check: ; @true
bank-import: ; @env | grep -E '^(MERCHANT|PROVIDER|SOF_EXCHANGE_LABELS)=' | sort
gaps: ; @env | grep -E '^(MERCHANT|PROVIDER|SOF_EXCHANGE_LABELS)=' | sort
provision: ; @env | grep -E '^(MERCHANT|PROVIDER|SOF_EXCHANGE_LABELS)=' | sort || true
`, filepath.Join(root, "Makefile"))
	if err := os.WriteFile(wrapper, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"bank-import", "gaps"} {
		t.Run(target, func(t *testing.T) {
			command := exec.CommandContext(context.Background(), "make", "-f", wrapper, target)
			command.Env = append(os.Environ(), "ENV_FILE=/dev/null", "MERCHANT=example-merchant", "PROVIDER=example-provider", "SOF_EXCHANGE_LABELS=example-exchange")
			output, err := command.CombinedOutput()
			if err != nil {
				t.Fatalf("%s: %v\n%s", target, err, output)
			}
			for _, want := range []string{"MERCHANT=example-merchant", "PROVIDER=example-provider", "SOF_EXCHANGE_LABELS=example-exchange"} {
				if !strings.Contains(string(output), want) {
					t.Errorf("%s output lacks %q: %s", target, want, output)
				}
			}
		})
	}
	t.Run("provision-does-not-inherit-labels", func(t *testing.T) {
		command := exec.CommandContext(context.Background(), "make", "-f", wrapper, "provision")
		command.Env = append(os.Environ(), "ENV_FILE=/dev/null", "MERCHANT=example-merchant", "PROVIDER=example-provider", "SOF_EXCHANGE_LABELS=example-exchange")
		output, err := command.CombinedOutput()
		if err != nil {
			t.Fatalf("provision: %v\n%s", err, output)
		}
		if strings.Contains(string(output), "example-merchant") || strings.Contains(string(output), "example-provider") || strings.Contains(string(output), "example-exchange") {
			t.Fatalf("provision inherited financial labels: %s", output)
		}
	})
}
