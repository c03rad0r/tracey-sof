package core

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"tracey-sof/internal/model"
)

type jsonIndenter func(value any, prefix, indent string) ([]byte, error)

type temporaryFile interface {
	Name() string
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type atomicFileStore interface {
	MkdirAll(string, os.FileMode) error
	CreateTemp(string, string) (temporaryFile, error)
	Remove(string) error
	Rename(string, string) error
}

type osAtomicFileStore struct{}

func (osAtomicFileStore) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (osAtomicFileStore) CreateTemp(dir, pattern string) (temporaryFile, error) {
	return os.CreateTemp(dir, pattern)
}

func (osAtomicFileStore) Remove(path string) error { return os.Remove(path) }

func (osAtomicFileStore) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func SaveTrace(path string, trace model.Trace) error {
	return saveTrace(path, trace, json.MarshalIndent, osAtomicFileStore{})
}

func saveTrace(path string, trace model.Trace, indent jsonIndenter, files atomicFileStore) error {
	data, err := indent(trace, "", "  ")
	if err != nil {
		return fmt.Errorf("encode trace: %w", err)
	}
	data = append(data, '\n')
	if err := files.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := files.CreateTemp(filepath.Dir(path), ".trace-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer files.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
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
	return files.Rename(name, path)
}

func LoadTrace(path string) (model.Trace, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return model.Trace{}, fmt.Errorf("read trace: %w", err)
	}
	var trace model.Trace
	if err := json.Unmarshal(data, &trace); err != nil {
		return model.Trace{}, fmt.Errorf("decode trace: %w", err)
	}
	if trace.SchemaVersion != model.SchemaVersion {
		return model.Trace{}, fmt.Errorf("unsupported trace schema %d", trace.SchemaVersion)
	}
	return trace, nil
}

func StringSnapshot(info NodeInfo) string {
	status := "synchronized"
	if info.InitialBlockDownload || info.Blocks < info.Headers {
		status = "synchronizing"
	}
	return fmt.Sprintf("Core version %d, chain=%s, height=%d/%d, %s, txindex=%t, pruned=%t",
		info.Version, info.Chain, info.Blocks, info.Headers, status, info.TxIndex, info.Pruned)
}
