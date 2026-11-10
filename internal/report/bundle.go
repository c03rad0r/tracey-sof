package report

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"tracey-sof/internal/model"
)

const accountantReadme = `SOURCE-OF-FUNDS REPORT

This bundle separates blockchain facts from user-supplied purchase evidence.
Exact transaction inputs and previous outputs are blockchain facts. Purchase
IDs, invoice files, associated addresses, labels, and notes are assertions
supplied by the account holder and should be reviewed with the accountant.

Bitcoin does not identify which individual satoshis from mixed transaction
inputs entered each output. No FIFO, LIFO, pro-rata, change, or common-ownership
allocation has been applied. A partially documented UTXO therefore lists its
contributing purchase IDs and remaining unknown/incomplete ancestry without
assigning an artificial amount to each purchase.

"documented" means every traversed branch reached a purchase identified by
the user. "partially_documented" means at least one purchase contributes but
other branches remain. "incomplete" means tracing could not finish; it does not
prove that purchase documentation is absent. Coinbase is an origin, but it is
not treated as an ATM/exchange purchase.

The chain snapshot and producing software version appear in report.json. Raw transactions can be independently
retrieved by txid from a full Bitcoin Core node. Artifacts below evidence/ are
copies of user-selected local files. manifest.sha256 covers the export.
`

func manifest(files map[string][]byte) []byte {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var out strings.Builder
	for _, name := range names {
		sum := sha256.Sum256(files[name])
		fmt.Fprintf(&out, "%s  %s\n", hex.EncodeToString(sum[:]), name)
	}
	return []byte(out.String())
}

type zipArchiveWriter interface {
	CreateHeader(*zip.FileHeader) (io.Writer, error)
	Close() error
}

func newZIPArchiveWriter(out io.Writer) zipArchiveWriter { return zip.NewWriter(out) }

func writeZIPWith(out io.Writer, files map[string][]byte, open func(io.Writer) zipArchiveWriter) error {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	writer := open(out)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		header.SetModTime(time.Date(1980, 1, 1, 0, 0, 0, 0, time.UTC))
		header.SetMode(0o600)
		entry, err := writer.CreateHeader(header)
		if err != nil {
			return err
		}
		if _, err := entry.Write(files[name]); err != nil {
			return err
		}
	}
	return writer.Close()
}

func writeZIP(out io.Writer, files map[string][]byte) error {
	return writeZIPWith(out, files, newZIPArchiveWriter)
}

func zipBytes(files map[string][]byte) ([]byte, error) {
	var out bytes.Buffer
	if err := writeZIP(&out, files); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

type evidenceFile interface {
	io.Reader
	io.Closer
}

func readVerifiedEvidence(file evidenceFile, artifactPath, expectedSHA256 string) ([]byte, error) {
	sum := sha256.New()
	data, err := io.ReadAll(io.TeeReader(file, sum))
	closeErr := file.Close()
	if err != nil {
		return nil, err
	}
	if closeErr != nil {
		return nil, closeErr
	}
	if actual := hex.EncodeToString(sum.Sum(nil)); actual != expectedSHA256 {
		return nil, fmt.Errorf("evidence %s changed after validation", artifactPath)
	}
	return data, nil
}

// Bundle exports existing generated reports and verified evidence artifacts.
func Bundle(report model.Report, purchases model.PurchaseFile, dataDir, outDir string) error {
	return bundle(report, purchases, dataDir, outDir, zipBytes)
}

type zipArchiveFunc func(map[string][]byte) ([]byte, error)

func bundle(report model.Report, purchases model.PurchaseFile, dataDir, outDir string, archive zipArchiveFunc) error {
	files := map[string][]byte{"README-accountant.txt": []byte(accountantReadme)}
	for _, name := range []string{"report.json", "report.csv", "evidence.csv", "report.html", "report.svg", "report.pdf"} {
		data, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			return fmt.Errorf("bundle requires %s; run audit and PDF generation first: %w", name, err)
		}
		files[name] = data
	}
	root, err := os.OpenRoot(dataDir)
	if err != nil {
		return err
	}
	defer root.Close()
	for i, purchase := range purchases.Purchases {
		for j, artifact := range purchase.Artifacts {
			f, err := root.Open(artifact.Path)
			if err != nil {
				return fmt.Errorf("open evidence %s: %w", artifact.Path, err)
			}
			data, err := readVerifiedEvidence(f, artifact.Path, artifact.SHA256)
			if err != nil {
				return err
			}
			name := path.Join("evidence", fmt.Sprintf("%03d-%03d-%s", i+1, j+1, filepath.Base(artifact.Path)))
			files[name] = data
		}
	}
	for _, name := range []string{"owned.json", "purchases.json"} {
		data, err := os.ReadFile(filepath.Join(dataDir, name))
		if err != nil {
			return fmt.Errorf("bundle requires %s: %w", name, err)
		}
		files["inputs/"+name] = data
	}
	if data, err := os.ReadFile(filepath.Join(dataDir, "annotations.json")); err == nil {
		files["inputs/annotations.json"] = data
	} else if !os.IsNotExist(err) {
		return err
	}
	files["manifest.sha256"] = manifest(files)
	data, err := archive(files)
	if err != nil {
		return err
	}
	return AtomicWrite(filepath.Join(outDir, "accountant.zip"), data, 0o600)
}
