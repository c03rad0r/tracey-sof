package input

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

func edgeWriteFile(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func edgeRequireError(t *testing.T, err error, contains string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q", contains)
	}
	if !strings.Contains(err.Error(), contains) {
		t.Fatalf("error %q does not contain %q", err, contains)
	}
}

func edgePurchasesWithArtifacts(artifacts ...model.Artifact) model.PurchaseFile {
	file := validPurchases()
	file.Purchases[0].Artifacts = artifacts
	return file
}

func TestEdgeInputLoadingFailures(t *testing.T) {
	t.Run("read failure", func(t *testing.T) {
		_, err := LoadOwned(t.TempDir())
		edgeRequireError(t, err, "read ")
	})

	t.Run("malformed trailing JSON", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "annotations.json")
		edgeWriteFile(t, path, `{"schema_version":1} {`)
		_, err := LoadAnnotations(path)
		edgeRequireError(t, err, "trailing data")
	})

	t.Run("owned validation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "owned.json")
		edgeWriteFile(t, path, `{}`)
		_, err := LoadOwned(path)
		edgeRequireError(t, err, "unsupported schema_version")
	})

	t.Run("purchases decode", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "purchases.json")
		edgeWriteFile(t, path, `{"schema_version":1,`)
		_, err := LoadPurchases(path, false)
		edgeRequireError(t, err, "decode ")
	})

	t.Run("purchases validation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "purchases.json")
		edgeWriteFile(t, path, `{}`)
		_, err := LoadPurchases(path, false)
		edgeRequireError(t, err, "unsupported schema_version")
	})

	t.Run("annotation stat failure", func(t *testing.T) {
		dir := t.TempDir()
		parent := filepath.Join(dir, "regular-file")
		edgeWriteFile(t, parent, "not a directory")
		_, err := LoadAnnotations(filepath.Join(parent, "annotations.json"))
		edgeRequireError(t, err, "stat ")
	})

	t.Run("annotation validation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "annotations.json")
		edgeWriteFile(t, path, `{"schema_version":1,"transactions":{"bad":{}}}`)
		_, err := LoadAnnotations(path)
		edgeRequireError(t, err, "invalid annotated txid")
	})
}

func TestEdgeSaveAnnotationsDestinationFailures(t *testing.T) {
	t.Run("invalid annotations", func(t *testing.T) {
		err := SaveAnnotations(filepath.Join(t.TempDir(), "annotations.json"), model.AnnotationFile{})
		edgeRequireError(t, err, "unsupported schema_version")
	})

	t.Run("directory creation", func(t *testing.T) {
		parent := filepath.Join(t.TempDir(), "regular-file")
		edgeWriteFile(t, parent, "not a directory")
		err := SaveAnnotations(filepath.Join(parent, "annotations.json"), EmptyAnnotations())
		edgeRequireError(t, err, "create annotation directory")
	})

	t.Run("temporary file creation", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "read-only")
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o500); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
		err := SaveAnnotations(filepath.Join(dir, "annotations.json"), EmptyAnnotations())
		if err == nil {
			t.Skip("directory permissions are not enforced for this user")
		}
		edgeRequireError(t, err, "create annotation temporary file")
	})

	t.Run("replacement failure cleans temporary file", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "annotations.json")
		if err := os.Mkdir(path, 0o700); err != nil {
			t.Fatal(err)
		}
		err := SaveAnnotations(path, EmptyAnnotations())
		edgeRequireError(t, err, "replace annotations")
		entries, readErr := os.ReadDir(dir)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if len(entries) != 1 || entries[0].Name() != "annotations.json" {
			t.Fatalf("temporary files were left behind: %v", entries)
		}
	})
}

func TestEdgeArtifactVerificationFailures(t *testing.T) {
	digest := fmt.Sprintf("%x", sha256.Sum256([]byte("receipt")))

	t.Run("duplicate artifact", func(t *testing.T) {
		dir := t.TempDir()
		edgeWriteFile(t, filepath.Join(dir, "receipt.txt"), "receipt")
		file := edgePurchasesWithArtifacts(
			model.Artifact{Path: "receipt.txt", SHA256: digest},
			model.Artifact{Path: "receipt.txt", SHA256: digest},
		)
		err := ValidatePurchases(file, dir, true)
		edgeRequireError(t, err, "duplicates artifact")
	})

	t.Run("missing artifact root", func(t *testing.T) {
		file := edgePurchasesWithArtifacts(model.Artifact{Path: "receipt.txt", SHA256: digest})
		err := ValidatePurchases(file, filepath.Join(t.TempDir(), "missing"), true)
		edgeRequireError(t, err, "open artifact root")
	})

	t.Run("missing artifact", func(t *testing.T) {
		file := edgePurchasesWithArtifacts(model.Artifact{Path: "receipt.txt", SHA256: digest})
		err := ValidatePurchases(file, t.TempDir(), true)
		edgeRequireError(t, err, "open artifact receipt.txt")
	})

	t.Run("mismatched artifact", func(t *testing.T) {
		dir := t.TempDir()
		edgeWriteFile(t, filepath.Join(dir, "receipt.txt"), "different")
		file := edgePurchasesWithArtifacts(model.Artifact{Path: "receipt.txt", SHA256: digest})
		err := ValidatePurchases(file, dir, true)
		edgeRequireError(t, err, "sha256 mismatch")
	})

	t.Run("artifact read failure", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.Mkdir(filepath.Join(dir, "receipt-dir"), 0o700); err != nil {
			t.Fatal(err)
		}
		file := edgePurchasesWithArtifacts(model.Artifact{Path: "receipt-dir", SHA256: digest})
		err := ValidatePurchases(file, dir, true)
		edgeRequireError(t, err, "hash artifact")
	})
}

func TestEdgeRemainingValidationBranches(t *testing.T) {
	const canonical = "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4"
	if err := validateAddress(canonical); err != nil {
		t.Fatalf("valid lowercase address rejected: %v", err)
	}
	edgeRequireError(t, validateAddress(strings.ToUpper(canonical)), "canonical mainnet form")
	edgeRequireError(t, validateXpub("xpub-not-a-key"), "invalid mainnet extended public key")

	annotations := EmptyAnnotations()
	annotations.PurchaseLinks = []model.PurchaseLink{{
		Target:     "not-a-transaction-outpoint-or-address",
		PurchaseID: "purchase",
		Decision:   "confirmed",
	}}
	edgeRequireError(t, ValidateAnnotations(annotations), "invalid purchase-link target")
}
