package input

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

type inputFaultFile struct {
	stage  string
	closed int
}

func (f *inputFaultFile) Name() string { return "annotation.tmp" }
func (f *inputFaultFile) Chmod(os.FileMode) error {
	if f.stage == "chmod" {
		return errors.New("chmod failure")
	}
	return nil
}
func (f *inputFaultFile) Write([]byte) (int, error) {
	if f.stage == "write" {
		return 0, errors.New("write failure")
	}
	return 1, nil
}
func (f *inputFaultFile) Sync() error {
	if f.stage == "sync" {
		return errors.New("sync failure")
	}
	return nil
}
func (f *inputFaultFile) Close() error {
	f.closed++
	if f.stage == "close" {
		return errors.New("close failure")
	}
	return nil
}

type inputFaultStore struct {
	stage string
	file  *inputFaultFile
}

func (s *inputFaultStore) MkdirAll(string, os.FileMode) error {
	if s.stage == "mkdir" {
		return errors.New("mkdir failure")
	}
	return nil
}
func (s *inputFaultStore) CreateTemp(string, string) (temporaryFile, error) {
	if s.stage == "create" {
		return nil, errors.New("create failure")
	}
	s.file = &inputFaultFile{stage: s.stage}
	return s.file, nil
}
func (s *inputFaultStore) Remove(string) error { return nil }
func (s *inputFaultStore) Rename(string, string) error {
	if s.stage == "rename" {
		return errors.New("rename failure")
	}
	return nil
}

func TestCoverageSaveAnnotationFailureStages(t *testing.T) {
	valid := EmptyAnnotations()
	for _, stage := range []string{"mkdir", "create", "chmod", "write", "sync", "close", "rename"} {
		t.Run(stage, func(t *testing.T) {
			store := &inputFaultStore{stage: stage}
			if err := saveAnnotations("ignored", valid, func(any, string, string) ([]byte, error) { return []byte("{}"), nil }, store); err == nil {
				t.Fatalf("%s failure was accepted", stage)
			}
			if stage == "chmod" || stage == "write" || stage == "sync" {
				if store.file.closed != 1 {
					t.Fatalf("temporary file not closed after %s failure", stage)
				}
			}
		})
	}
	if err := saveAnnotations("ignored", valid, func(any, string, string) ([]byte, error) { return nil, errors.New("encode failure") }, &inputFaultStore{}); err == nil {
		t.Fatal("marshal failure accepted")
	}
}

func TestCoverageInputSizeLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.json")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxInputBytes + 1); err != nil {
		file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadAnnotations(path); err == nil {
		t.Fatal("oversized JSON input was accepted")
	}
}

func TestCoverageArtifactCloseFailure(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "receipt"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	sum := "3a6eb0790f39ac87c94f3856b2dd2c5d110e6811602261a9a923d3bb23adc8b7"
	err := verifyArtifactWithClose(dir, model.Artifact{Path: "receipt", SHA256: sum}, func(*os.File) error { return errors.New("close failure") })
	if err == nil {
		t.Fatal("artifact close failure was accepted")
	}
}

func TestCoverageSavePurchasesAndJSONAtomicFailures(t *testing.T) {
	valid := model.PurchaseFile{SchemaVersion: model.SchemaVersion, Purchases: []model.Purchase{{ID: "purchase-one"}}}
	path := filepath.Join(t.TempDir(), "purchases.json")
	if err := SavePurchases(path, valid, false); err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadPurchases(path, false)
	if err != nil || len(loaded.Purchases) != 1 || loaded.Purchases[0].ID != "purchase-one" {
		t.Fatalf("purchases round trip = %+v, %v", loaded, err)
	}
	if err := SavePurchases(path, model.PurchaseFile{SchemaVersion: model.SchemaVersion}, false); err == nil {
		t.Fatal("empty purchases file was saved")
	}
	if err := savePurchases(path, valid, false, func(any, string, string) ([]byte, error) { return nil, errors.New("marshal failure") }, &inputFaultStore{}); err == nil {
		t.Fatal("purchase marshal failure accepted")
	}
	for _, stage := range []string{"mkdir", "create", "chmod", "write", "sync", "close", "rename"} {
		t.Run(stage, func(t *testing.T) {
			store := &inputFaultStore{stage: stage}
			if err := saveJSONAtomic("ignored", ".purchases-*.tmp", []byte("{}\n"), store); err == nil {
				t.Fatalf("%s failure was accepted", stage)
			}
			if (stage == "chmod" || stage == "write" || stage == "sync") && store.file.closed != 1 {
				t.Fatalf("temporary file not closed after %s failure", stage)
			}
		})
	}
	if !ValidTxID(strings.Repeat("a", 64)) || ValidTxID(strings.Repeat("A", 64)) || ValidTxID("bad") {
		t.Fatal("ValidTxID failed")
	}
	if ValidateMainnetAddress("1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa") != nil || ValidateMainnetAddress("bad") == nil {
		t.Fatal("ValidateMainnetAddress failed")
	}
}
