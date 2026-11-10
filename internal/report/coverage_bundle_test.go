package report

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

func coverageWriteFile(t *testing.T, name string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(name), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func coverageDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func coverageBundleFixture(t *testing.T) (string, string, model.PurchaseFile, map[string][]byte) {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	outDir := filepath.Join(root, "out")
	if err := WriteStructured(testReport(), outDir, true); err != nil {
		t.Fatal(err)
	}
	coverageWriteFile(t, filepath.Join(outDir, "report.pdf"), []byte("%PDF-1.7\nverified test artifact\n"))
	coverageWriteFile(t, filepath.Join(dataDir, "owned.json"), []byte("{\n  \"schema_version\": 1\n}\n"))
	coverageWriteFile(t, filepath.Join(dataDir, "purchases.json"), []byte("{\n  \"schema_version\": 1, \"purchases\": []\n}\n"))
	coverageWriteFile(t, filepath.Join(dataDir, "annotations.json"), []byte("{\"schema_version\":1}\n"))
	invoice := []byte("invoice: first purchase\n\u0000binary-safe\n")
	receipt := []byte("second purchase receipt\n")
	coverageWriteFile(t, filepath.Join(dataDir, "evidence", "invoice #1.txt"), invoice)
	coverageWriteFile(t, filepath.Join(dataDir, "receipts", "receipt.bin"), receipt)
	purchases := model.PurchaseFile{SchemaVersion: model.SchemaVersion, Purchases: []model.Purchase{
		{ID: "buy-one", Artifacts: []model.Artifact{{Path: "evidence/invoice #1.txt", SHA256: coverageDigest(invoice)}}},
		{ID: "buy-two", Artifacts: []model.Artifact{{Path: "receipts/receipt.bin", SHA256: coverageDigest(receipt)}}},
	}}
	expected := map[string][]byte{
		"evidence/001-001-invoice #1.txt": invoice,
		"evidence/002-001-receipt.bin":    receipt,
		"inputs/owned.json":               []byte("{\n  \"schema_version\": 1\n}\n"),
		"inputs/purchases.json":           []byte("{\n  \"schema_version\": 1, \"purchases\": []\n}\n"),
		"inputs/annotations.json":         []byte("{\"schema_version\":1}\n"),
	}
	return dataDir, outDir, purchases, expected
}

func coverageReadZIP(t *testing.T, data []byte) ([]*zip.File, map[string][]byte) {
	t.Helper()
	reader, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("open generated ZIP: %v", err)
	}
	contents := make(map[string][]byte, len(reader.File))
	for _, entry := range reader.File {
		if _, duplicate := contents[entry.Name]; duplicate {
			t.Errorf("duplicate ZIP entry %q", entry.Name)
		}
		stream, err := entry.Open()
		if err != nil {
			t.Fatalf("open %q: %v", entry.Name, err)
		}
		var value bytes.Buffer
		if _, err := value.ReadFrom(stream); err != nil {
			t.Fatalf("read %q: %v", entry.Name, err)
		}
		if err := stream.Close(); err != nil {
			t.Fatalf("close %q: %v", entry.Name, err)
		}
		contents[entry.Name] = value.Bytes()
	}
	return reader.File, contents
}

func TestCoverageZIPBytesRejectsOversizedEntryName(t *testing.T) {
	_, err := zipBytes(map[string][]byte{strings.Repeat("x", 1<<16): []byte("data")})
	if err == nil {
		t.Fatal("ZIP entry with a name too large for its header was accepted")
	}
}

func TestCoverageBundleVerifiedArtifactsReproducibleManifest(t *testing.T) {
	dataDir, outDir, purchases, expected := coverageBundleFixture(t)
	for _, name := range []string{"README-accountant.txt", "report.json", "report.csv", "report.html", "report.svg", "report.pdf"} {
		name := name
		var source string
		if name == "README-accountant.txt" {
			expected[name] = []byte(accountantReadme)
			continue
		}
		source = filepath.Join(outDir, name)
		data, err := os.ReadFile(source)
		if err != nil {
			t.Fatal(err)
		}
		expected[name] = data
	}

	zipPath := filepath.Join(outDir, "accountant.zip")
	if err := Bundle(testReport(), purchases, dataDir, outDir); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	entries, contents := coverageReadZIP(t, first)
	if err := Bundle(testReport(), purchases, dataDir, outDir); err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Error("identical bundle inputs produced different ZIP bytes")
	}
	secondEntries, secondContents := coverageReadZIP(t, second)
	if len(secondContents) != len(contents) {
		t.Fatalf("rebuilt bundle entry count changed: %d != %d", len(secondContents), len(contents))
	}

	expectedNames := make([]string, 0, len(expected)+1)
	for name := range expected {
		expectedNames = append(expectedNames, name)
	}
	expectedNames = append(expectedNames, "manifest.sha256")
	sort.Strings(expectedNames)
	if len(entries) != len(expectedNames) {
		t.Fatalf("ZIP contains %d files, want %d", len(entries), len(expectedNames))
	}
	for i, entry := range entries {
		if entry.Name != expectedNames[i] || secondEntries[i].Name != expectedNames[i] {
			t.Errorf("entry %d = %q, want sorted name %q", i, entry.Name, expectedNames[i])
		}
		if entry.Method != zip.Deflate {
			t.Errorf("%s uses method %d, want deflate", entry.Name, entry.Method)
		}
		if got := entry.Mode().Perm(); got != 0o600 {
			t.Errorf("%s mode = %04o, want 0600", entry.Name, got)
		}
		if got := entry.Modified.UTC().Format("2006-01-02T15:04:05Z07:00"); got != "1980-01-01T00:00:00Z" {
			t.Errorf("%s timestamp = %s, want reproducible epoch", entry.Name, got)
		}
	}
	for name, want := range expected {
		if got, ok := contents[name]; !ok || !bytes.Equal(got, want) {
			t.Errorf("bundle content %q = %q, want %q (present %v)", name, got, want, ok)
		}
	}

	manifestText := string(contents["manifest.sha256"])
	if strings.Contains(manifestText, "manifest.sha256") {
		t.Error("manifest must not checksum itself")
	}
	lines := strings.Split(strings.TrimSuffix(manifestText, "\n"), "\n")
	if len(lines) != len(expected) {
		t.Fatalf("manifest has %d entries, want %d", len(lines), len(expected))
	}
	manifestNames := make([]string, 0, len(expected))
	for name := range expected {
		manifestNames = append(manifestNames, name)
	}
	sort.Strings(manifestNames)
	for i, name := range manifestNames {
		want := fmt.Sprintf("%s  %s", coverageDigest(expected[name]), name)
		if lines[i] != want {
			t.Errorf("manifest line %d = %q, want %q", i, lines[i], want)
		}
	}
}

func TestCoverageBundleRejectsMissingAndChangedEvidence(t *testing.T) {
	tests := []struct {
		name    string
		prepare func(t *testing.T, dataDir string, purchases *model.PurchaseFile)
		want    string
	}{
		{
			name: "missing artifact",
			prepare: func(t *testing.T, dataDir string, purchases *model.PurchaseFile) {
				t.Helper()
				if err := os.Remove(filepath.Join(dataDir, purchases.Purchases[0].Artifacts[0].Path)); err != nil {
					t.Fatal(err)
				}
			},
			want: "open evidence",
		},
		{
			name: "tampered artifact",
			prepare: func(t *testing.T, dataDir string, purchases *model.PurchaseFile) {
				t.Helper()
				coverageWriteFile(t, filepath.Join(dataDir, purchases.Purchases[0].Artifacts[0].Path), []byte("altered after validation"))
			},
			want: "changed after validation",
		},
		{
			name: "stale digest",
			prepare: func(t *testing.T, dataDir string, purchases *model.PurchaseFile) {
				t.Helper()
				purchases.Purchases[0].Artifacts[0].SHA256 = strings.Repeat("0", 64)
			},
			want: "changed after validation",
		},
		{
			name: "artifact is unreadable directory",
			prepare: func(t *testing.T, dataDir string, purchases *model.PurchaseFile) {
				t.Helper()
				name := filepath.Join(dataDir, purchases.Purchases[0].Artifacts[0].Path)
				if err := os.Remove(name); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(name, 0o700); err != nil {
					t.Fatal(err)
				}
			},
			want: "is a directory",
		},
		{
			name: "path escapes data root",
			prepare: func(t *testing.T, dataDir string, purchases *model.PurchaseFile) {
				t.Helper()
				purchases.Purchases[0].Artifacts[0].Path = "../outside.txt"
			},
			want: "open evidence",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataDir, outDir, purchases, _ := coverageBundleFixture(t)
			test.prepare(t, dataDir, &purchases)
			err := Bundle(testReport(), purchases, dataDir, outDir)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Bundle error = %v, want text %q", err, test.want)
			}
			if _, statErr := os.Stat(filepath.Join(outDir, "accountant.zip")); !os.IsNotExist(statErr) {
				t.Errorf("invalid evidence left a bundle behind: %v", statErr)
			}
		})
	}
}

func TestCoverageBundleReportsAndInputsErrors(t *testing.T) {
	requiredReports := []string{"report.json", "report.csv", "report.html", "report.svg", "report.pdf"}
	for _, missing := range requiredReports {
		t.Run("missing "+missing, func(t *testing.T) {
			root := t.TempDir()
			for _, name := range requiredReports {
				if name != missing {
					coverageWriteFile(t, filepath.Join(root, name), []byte(name))
				}
			}
			err := Bundle(testReport(), model.PurchaseFile{}, t.TempDir(), root)
			if err == nil || !strings.Contains(err.Error(), "bundle requires "+missing) {
				t.Fatalf("Bundle error = %v", err)
			}
		})
	}

	t.Run("invalid data root", func(t *testing.T) {
		outDir := t.TempDir()
		for _, name := range requiredReports {
			coverageWriteFile(t, filepath.Join(outDir, name), []byte(name))
		}
		if err := Bundle(testReport(), model.PurchaseFile{}, filepath.Join(outDir, "absent"), outDir); err == nil {
			t.Fatal("missing data root accepted")
		}
	})

	for _, missing := range []string{"owned.json", "purchases.json", "annotations.json"} {
		t.Run("missing required input "+missing, func(t *testing.T) {
			dataDir, outDir, purchases, _ := coverageBundleFixture(t)
			if err := os.Remove(filepath.Join(dataDir, missing)); err != nil {
				t.Fatal(err)
			}
			err := Bundle(testReport(), purchases, dataDir, outDir)
			if missing == "annotations.json" {
				if err != nil {
					t.Fatalf("optional annotations should be omitted when absent: %v", err)
				}
				data, readErr := os.ReadFile(filepath.Join(outDir, "accountant.zip"))
				if readErr != nil {
					t.Fatal(readErr)
				}
				_, contents := coverageReadZIP(t, data)
				if _, exists := contents["inputs/annotations.json"]; exists {
					t.Error("absent optional annotations were bundled")
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), "bundle requires "+missing) {
				t.Fatalf("Bundle error = %v, want missing %s", err, missing)
			}
		})
	}

	t.Run("unreadable optional annotations", func(t *testing.T) {
		dataDir, outDir, purchases, _ := coverageBundleFixture(t)
		if err := os.Remove(filepath.Join(dataDir, "annotations.json")); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(filepath.Join(dataDir, "annotations.json"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := Bundle(testReport(), purchases, dataDir, outDir); err == nil {
			t.Fatal("directory named annotations.json accepted as an input file")
		}
	})
}
