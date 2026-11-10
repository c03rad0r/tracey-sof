package input

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"tracey-sof/internal/model"
)

const maxInputBytes = 64 << 20

type jsonIndenter func(value any, prefix, indent string) ([]byte, error)

type temporaryFile interface {
	Name() string
	Chmod(os.FileMode) error
	Write([]byte) (int, error)
	Sync() error
	Close() error
}

type annotationFileStore interface {
	MkdirAll(string, os.FileMode) error
	CreateTemp(string, string) (temporaryFile, error)
	Remove(string) error
	Rename(string, string) error
}

type osAnnotationFileStore struct{}

func (osAnnotationFileStore) MkdirAll(path string, perm os.FileMode) error {
	return os.MkdirAll(path, perm)
}

func (osAnnotationFileStore) CreateTemp(dir, pattern string) (temporaryFile, error) {
	return os.CreateTemp(dir, pattern)
}

func (osAnnotationFileStore) Remove(path string) error { return os.Remove(path) }

func (osAnnotationFileStore) Rename(oldPath, newPath string) error {
	return os.Rename(oldPath, newPath)
}

func loadJSON(path string, dst any) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxInputBytes+1))
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}
	if len(data) > maxInputBytes {
		return fmt.Errorf("%s exceeds %d bytes", path, maxInputBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("decode %s: %w", path, err)
	}
	var trailing any
	if err := dec.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return fmt.Errorf("decode %s: multiple JSON values", path)
		}
		return fmt.Errorf("decode %s trailing data: %w", path, err)
	}
	return nil
}

func LoadOwned(path string) (model.Owned, error) {
	var owned model.Owned
	if err := loadJSON(path, &owned); err != nil {
		return owned, err
	}
	if err := ValidateOwned(owned); err != nil {
		return owned, fmt.Errorf("validate %s: %w", path, err)
	}
	return owned, nil
}

func LoadPurchases(path string, verifyArtifacts bool) (model.PurchaseFile, error) {
	var file model.PurchaseFile
	if err := loadJSON(path, &file); err != nil {
		return file, err
	}
	if err := ValidatePurchases(file, filepath.Dir(path), verifyArtifacts); err != nil {
		return file, fmt.Errorf("validate %s: %w", path, err)
	}
	return file, nil
}

func LoadAnnotations(path string) (model.AnnotationFile, error) {
	file := model.AnnotationFile{SchemaVersion: model.SchemaVersion}
	if _, err := os.Stat(path); err != nil {
		if os.IsNotExist(err) {
			return file, nil
		}
		return file, fmt.Errorf("stat %s: %w", path, err)
	}
	if err := loadJSON(path, &file); err != nil {
		return file, err
	}
	if err := ValidateAnnotations(file); err != nil {
		return file, fmt.Errorf("validate %s: %w", path, err)
	}
	return file, nil
}

func SaveAnnotations(path string, file model.AnnotationFile) error {
	return saveAnnotations(path, file, json.MarshalIndent, osAnnotationFileStore{})
}

func saveAnnotations(path string, file model.AnnotationFile, indent jsonIndenter, files annotationFileStore) error {
	if err := ValidateAnnotations(file); err != nil {
		return err
	}
	data, err := indent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("encode annotations: %w", err)
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if err := files.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create annotation directory: %w", err)
	}
	tmp, err := files.CreateTemp(dir, ".annotations-*.tmp")
	if err != nil {
		return fmt.Errorf("create annotation temporary file: %w", err)
	}
	name := tmp.Name()
	defer files.Remove(name)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("set annotation permissions: %w", err)
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return fmt.Errorf("write annotations: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("sync annotations: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close annotations: %w", err)
	}
	if err := files.Rename(name, path); err != nil {
		return fmt.Errorf("replace annotations: %w", err)
	}
	return nil
}
