package core

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

var errCoveragePersistence = errors.New("injected persistence failure")

type coverageFaultFile struct {
	stage   string
	events  *[]string
	closes  int
	written []byte
}

func (f *coverageFaultFile) Name() string { return "coverage-saving.tmp" }

func (f *coverageFaultFile) Chmod(os.FileMode) error {
	*f.events = append(*f.events, "chmod")
	if f.stage == "chmod" {
		return errCoveragePersistence
	}
	return nil
}

func (f *coverageFaultFile) Write(data []byte) (int, error) {
	*f.events = append(*f.events, "write")
	if f.stage == "write" {
		return 0, errCoveragePersistence
	}
	f.written = append([]byte(nil), data...)
	return len(data), nil
}

func (f *coverageFaultFile) Sync() error {
	*f.events = append(*f.events, "sync")
	if f.stage == "sync" {
		return errCoveragePersistence
	}
	return nil
}

func (f *coverageFaultFile) Close() error {
	f.closes++
	*f.events = append(*f.events, "close")
	if f.stage == "close" {
		return errCoveragePersistence
	}
	return nil
}

type coverageAtomicStore struct {
	stage  string
	events []string
	file   *coverageFaultFile
}

func (s *coverageAtomicStore) MkdirAll(string, os.FileMode) error {
	if s.stage == "mkdir" {
		return errCoveragePersistence
	}
	return nil
}

func (s *coverageAtomicStore) CreateTemp(string, string) (temporaryFile, error) {
	if s.stage == "create" {
		return nil, errCoveragePersistence
	}
	s.file = &coverageFaultFile{stage: s.stage, events: &s.events}
	return s.file, nil
}

func (s *coverageAtomicStore) Remove(string) error {
	s.events = append(s.events, "remove")
	return nil
}

func (s *coverageAtomicStore) Rename(string, string) error {
	s.events = append(s.events, "rename")
	if s.stage == "rename" {
		return errCoveragePersistence
	}
	return nil
}

func TestCoverageAtomicSaveFailureStagesAndCleanup(t *testing.T) {
	saves := []struct {
		name string
		save func(string, jsonIndenter, atomicFileStore) error
	}{
		{name: "trace", save: func(path string, indent jsonIndenter, files atomicFileStore) error {
			return saveTrace(path, model.Trace{}, indent, files)
		}},
		{name: "source", save: func(path string, indent jsonIndenter, files atomicFileStore) error {
			return NewSource(nil, path).save(indent, files)
		}},
	}
	tests := []struct {
		stage  string
		events []string
	}{
		{stage: "mkdir"},
		{stage: "create"},
		{stage: "chmod", events: []string{"chmod", "close", "remove"}},
		{stage: "write", events: []string{"chmod", "write", "close", "remove"}},
		{stage: "sync", events: []string{"chmod", "write", "sync", "close", "remove"}},
		{stage: "close", events: []string{"chmod", "write", "sync", "close", "remove"}},
		{stage: "rename", events: []string{"chmod", "write", "sync", "close", "rename", "remove"}},
		{events: []string{"chmod", "write", "sync", "close", "rename", "remove"}},
	}
	for _, save := range saves {
		t.Run(save.name, func(t *testing.T) {
			for _, test := range tests {
				name := test.stage
				if name == "" {
					name = "success"
				}
				t.Run(name, func(t *testing.T) {
					files := &coverageAtomicStore{stage: test.stage}
					err := save.save(filepath.Join(t.TempDir(), "output.json"), json.MarshalIndent, files)
					if test.stage == "" {
						if err != nil {
							t.Fatalf("save failed: %v", err)
						}
					} else if !errors.Is(err, errCoveragePersistence) {
						t.Fatalf("save error = %v, want injected failure", err)
					}
					if !reflect.DeepEqual(files.events, test.events) {
						t.Errorf("operation order = %v, want %v", files.events, test.events)
					}
					wantCloses := 0
					if len(test.events) > 0 {
						wantCloses = 1
					}
					if test.stage != "create" && test.stage != "mkdir" {
						if files.file == nil || files.file.closes != wantCloses {
							t.Errorf("close calls = %+v, want %d", files.file, wantCloses)
						}
						if len(files.file.written) > 0 && files.file.written[len(files.file.written)-1] != '\n' {
							t.Error("saved content is not newline terminated")
						}
					}
				})
			}
		})
	}
}

type coverageDiscoveryStore struct {
	stage string
	wrote bool
}

func (s *coverageDiscoveryStore) MkdirAll(string, os.FileMode) error {
	if s.stage == "mkdir" {
		return errCoveragePersistence
	}
	return nil
}

func (s *coverageDiscoveryStore) WriteFile(string, []byte, os.FileMode) error {
	s.wrote = true
	if s.stage == "write" {
		return errCoveragePersistence
	}
	return nil
}

func TestCoverageSaveEncodingAndDiscoveryPersistenceFailures(t *testing.T) {
	indentFailure := func(any, string, string) ([]byte, error) {
		return nil, errCoveragePersistence
	}
	path := filepath.Join(t.TempDir(), "output.json")
	atomicSaves := []struct {
		name string
		save func() error
	}{
		{name: "trace", save: func() error {
			return saveTrace(path, model.Trace{}, indentFailure, &coverageAtomicStore{})
		}},
		{name: "source", save: func() error {
			return NewSource(nil, path).save(indentFailure, &coverageAtomicStore{})
		}},
		{name: "annotations are in the input package", save: nil},
	}
	for _, save := range atomicSaves {
		if save.save == nil {
			continue
		}
		t.Run(save.name+" encoding", func(t *testing.T) {
			if err := save.save(); !errors.Is(err, errCoveragePersistence) {
				t.Errorf("save error = %v, want injected encoding failure", err)
			}
		})
	}

	targets := []model.TargetRef{{TxID: strings.Repeat("0", 64), ValueSats: 1}}
	for _, stage := range []string{"encode", "mkdir", "write", ""} {
		name := stage
		if name == "" {
			name = "success"
		}
		t.Run("discovery "+name, func(t *testing.T) {
			files := &coverageDiscoveryStore{stage: stage}
			indent := jsonIndenter(json.MarshalIndent)
			if stage == "encode" {
				indent = indentFailure
			}
			err := saveDiscovery(path, model.Chain{}, targets, indent, files)
			if stage == "" {
				if err != nil || !files.wrote {
					t.Fatalf("SaveDiscovery helper = %v, wrote=%t", err, files.wrote)
				}
			} else if !errors.Is(err, errCoveragePersistence) {
				t.Fatalf("SaveDiscovery helper error = %v", err)
			}
			if (stage == "encode" || stage == "mkdir") && files.wrote {
				t.Error("discovery was written after an earlier stage failed")
			}
		})
	}
}

func TestCoverageLoadDiscoveryRejectsEmptyOrUnsupportedData(t *testing.T) {
	for _, contents := range []string{
		`{"schema_version":1,"targets":[]}`,
		`{"schema_version":999,"targets":[{"txid":"0000000000000000000000000000000000000000000000000000000000000000"}]}`,
	} {
		path := filepath.Join(t.TempDir(), "discovery.json")
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := LoadDiscovery(path); err == nil || !strings.Contains(err.Error(), "empty or has an unsupported schema") {
			t.Errorf("LoadDiscovery() error = %v", err)
		}
	}
}

func TestCoverageSourceFetchPrefersPriorCachedTransaction(t *testing.T) {
	fixture := readFixtureFile(t, firstTransfer)
	tx, err := DecodeTransaction(fixture.Hex)
	if err != nil {
		t.Fatal(err)
	}
	client, server := rpcTestClient(t, func(request rpcRequest) (any, *RPCError) {
		if request.Method != "getrawtransaction" {
			t.Fatalf("unexpected RPC method %s", request.Method)
		}
		return coverageRawResult(tx, fixture), nil
	})
	defer server.Close()
	prior := tx
	prior.BlockTime++
	source := NewSource(client, "")
	source.transactions[tx.TxID] = prior
	got, err := source.fetch(t.Context(), tx.TxID, fixture.BlockHash, fixture.BlockHeight)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, prior) || !reflect.DeepEqual(source.transactions[tx.TxID], prior) {
		t.Errorf("fetch replaced the prior cache entry: got=%+v want=%+v", got, prior)
	}
}
