package intake

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"tracey-sof/internal/model"
)

const (
	testAddress = "1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNa"
	testSegwit  = "bc1qw508d6qejxtdg4y5r3zarvary0c5xw7kv8f3t4"
	testTxID    = "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16"
	testParent  = "0437cd7f8525ceed2324359c2d0ba26006d92d856a9c20fa0241106ee5a597c9"
)

func TestExtractTextKindsAndDeduplication(t *testing.T) {
	text := "ATM receipt\naddress: " + testAddress + "\ntxid: " + testTxID + "\noutpoint " + testParent + ":1\nrepeat " + testAddress + "\ninvalid 1A1zP1eP5QGefi2DMPTfTL5SLmv7DivfNb\n"
	candidates := ExtractText(text)
	if len(candidates) != 3 {
		t.Fatalf("candidates = %+v", candidates)
	}
	if candidates[0].Kind != KindAddress || candidates[0].Value != testAddress || candidates[0].Line != 2 {
		t.Fatalf("address candidate = %+v", candidates[0])
	}
	if candidates[1].Kind != KindTxID || candidates[1].Value != testTxID || candidates[1].Confidence != ConfidenceMedium {
		t.Fatalf("txid candidate = %+v", candidates[1])
	}
	if candidates[2].Kind != KindOutpoint || candidates[2].Value != testParent+":1" || candidates[2].Confidence != ConfidenceHigh {
		t.Fatalf("outpoint candidate = %+v", candidates[2])
	}
}

func TestExtractTextCanonicalizesBech32AndOrdersMatches(t *testing.T) {
	text := strings.ToUpper(testSegwit) + "\n" + testTxID
	candidates := ExtractText(text)
	if len(candidates) != 2 || candidates[0].Kind != KindAddress || candidates[0].Value != testSegwit || candidates[0].Confidence != ConfidenceHigh {
		t.Fatalf("canonical candidates = %+v", candidates)
	}
	if candidates[1].Kind != KindTxID || candidates[1].Confidence != ConfidenceLow {
		t.Fatalf("plain txid confidence = %+v", candidates[1])
	}
}

func TestExtractTextRejectsMalformedValues(t *testing.T) {
	text := "bc1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqq " + strings.Repeat("A", 64) + " 3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy"
	candidates := ExtractText(text)
	if len(candidates) != 2 || candidates[0].Kind != KindTxID || candidates[0].Value != strings.Repeat("a", 64) || candidates[1].Value != "3J98t1WpEZ73CNmQviecrnyiWrnqRhWNLy" {
		t.Fatalf("malformed values accepted: %+v", candidates)
	}
}

func TestExtractCSVStructuredAndFallback(t *testing.T) {
	data := []byte("address,role,txid\n" + testAddress + ",received_at," + testTxID + "\n" + testSegwit + ",unknown,\n")
	candidates, err := ExtractCSV(data, ',')
	if err != nil {
		t.Fatal(err)
	}
	if len(candidates) != 3 || candidates[0].Role != RoleReceivedAt || candidates[1].Role != RoleUnknown {
		t.Fatalf("CSV candidates = %+v", candidates)
	}
	fallback, err := ExtractCSV([]byte("free text "+testAddress+"\n"), ',')
	if err != nil || len(fallback) != 1 {
		t.Fatalf("CSV fallback = %+v, %v", fallback, err)
	}
	if _, err := ExtractCSV([]byte("address\nnot-an-address\n"), ','); err == nil {
		t.Fatal("invalid structured address was accepted")
	}
	if _, err := ExtractCSV([]byte("txid\nbad\n"), ','); err == nil {
		t.Fatal("invalid structured txid was accepted")
	}
	if _, err := ExtractCSV([]byte("outpoint\nbad\n"), ','); err == nil {
		t.Fatal("invalid structured outpoint was accepted")
	}
	if _, err := ExtractCSV([]byte("address,\"unterminated\n"), ','); err == nil {
		t.Fatal("malformed CSV was accepted")
	}
}

func TestScannerTextAndCSV(t *testing.T) {
	root := t.TempDir()
	inbox := filepath.Join(root, "data", "inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, "receipt.txt"), []byte("paid to "+testAddress+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, "list.csv"), []byte("outpoint,role\n"+testParent+":0,purchase_outpoint\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, "ignore.bin"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := DefaultScanner().Scan(context.Background(), inbox, filepath.Join(root, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Sources) != 2 || file.Sources[0].Path != "inbox/list.csv" || file.Sources[1].Path != "inbox/receipt.txt" {
		t.Fatalf("sources = %+v", file.Sources)
	}
	if file.Sources[0].Candidates[0].Role != RolePurchaseOutpoint || file.Sources[1].Candidates[0].Role != RoleUnknown {
		t.Fatalf("candidate roles = %+v", file.Sources)
	}
	if file.Sources[1].Purchase.ID == "" || file.Sources[1].SHA256 == "" {
		t.Fatalf("source metadata = %+v", file.Sources[1])
	}
}

func TestScannerPDFTextAndOCRPaths(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	inbox := filepath.Join(dataDir, "inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	pdfPath := filepath.Join(inbox, "receipt.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	lookup := func(string) (string, error) { return "/fake", nil }
	textRunner := func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "pdftotext" {
			return []byte("address " + testAddress), nil
		}
		return nil, errors.New("unexpected")
	}
	scanner := DefaultScanner()
	scanner.Lookup = lookup
	scanner.Runner = textRunner
	source, err := scanner.scanFile(context.Background(), pdfPath, dataDir, "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Extraction) != 1 || source.Extraction[0] != "pdftotext" || len(source.Candidates) != 1 {
		t.Fatalf("PDF text source = %+v", source)
	}

	ocrRunner := func(_ context.Context, name string, args ...string) ([]byte, error) {
		switch name {
		case "pdftotext":
			return []byte("  "), nil
		case "pdftoppm":
			prefix := args[len(args)-1]
			if err := os.WriteFile(prefix+"-1.png", []byte("png"), 0o600); err != nil {
				return nil, err
			}
			return nil, nil
		case "tesseract":
			return []byte("txid " + testTxID), nil
		default:
			return nil, errors.New("unexpected")
		}
	}
	scanner.Runner = ocrRunner
	source, err = scanner.scanFile(context.Background(), pdfPath, dataDir, "application/pdf")
	if err != nil {
		t.Fatal(err)
	}
	if len(source.Extraction) != 2 || source.Extraction[0] != "pdftoppm" || len(source.Candidates) != 1 {
		t.Fatalf("PDF OCR source = %+v", source)
	}

	imagePath := filepath.Join(inbox, "receipt.png")
	if err := os.WriteFile(imagePath, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err = scanner.scanFile(context.Background(), imagePath, dataDir, "image/png")
	if err != nil || len(source.Candidates) != 1 || source.Extraction[0] != "tesseract" {
		t.Fatalf("image OCR source = %+v, %v", source, err)
	}
}

func TestScannerErrorPaths(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	inbox := filepath.Join(dataDir, "inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	scanner := DefaultScanner()
	scanner.Lookup = func(string) (string, error) { return "", errors.New("missing") }
	if _, err := scanner.Scan(context.Background(), filepath.Join(root, "missing"), dataDir); err == nil {
		t.Fatal("missing inbox accepted")
	}
	pdfPath := filepath.Join(inbox, "receipt.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.scanFile(context.Background(), pdfPath, dataDir, "application/pdf"); err == nil || !strings.Contains(err.Error(), "pdftotext failed") {
		t.Fatalf("missing OCR binaries = %v", err)
	}
	textPath := filepath.Join(inbox, "receipt.txt")
	if err := os.WriteFile(textPath, []byte("too large"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.scanFile(context.Background(), textPath, filepath.Join(root, "other"), "text/plain"); err == nil || !strings.Contains(err.Error(), "below the data directory") {
		t.Fatalf("outside data directory = %v", err)
	}
	scanner.MaxBytes = DefaultMaxSourceBytes
	privatePath := filepath.Join(inbox, "private.txt")
	if err := os.WriteFile(privatePath, []byte("xprv-secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.scanFile(context.Background(), privatePath, dataDir, "text/plain"); err == nil || !strings.Contains(err.Error(), "private key material") {
		t.Fatalf("private material = %v", err)
	}
	scanner.MaxBytes = 1
	if _, err := scanner.scanFile(context.Background(), textPath, dataDir, "text/plain"); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized file = %v", err)
	}
}

func TestScannerPDFFallbackVariants(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	inbox := filepath.Join(dataDir, "inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	pdfPath := filepath.Join(inbox, "receipt.pdf")
	if err := os.WriteFile(pdfPath, []byte("%PDF-1.7\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scanner := DefaultScanner()
	scanner.Lookup = func(string) (string, error) { return "/fake", nil }
	scanner.Runner = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("command failed") }
	if _, _, err := scanner.extractPDF(context.Background(), pdfPath); err == nil || !strings.Contains(err.Error(), "OCR fallback failed") {
		t.Fatalf("failed OCR fallback = %v", err)
	}
	scanner.Runner = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "pdftotext" {
			return nil, errors.New("text failed")
		}
		return nil, errors.New("ocr failed")
	}
	if _, _, err := scanner.extractPDF(context.Background(), pdfPath); err == nil || !strings.Contains(err.Error(), "pdftotext failed") {
		t.Fatalf("failed text and OCR = %v", err)
	}
	scanner.Lookup = func(name string) (string, error) {
		if name == "pdftoppm" {
			return "", errors.New("missing")
		}
		return "/fake", nil
	}
	if _, _, err := scanner.extractPDF(context.Background(), pdfPath); err == nil || !strings.Contains(err.Error(), "pdftoppm is not installed") {
		t.Fatalf("missing pdftoppm = %v", err)
	}
	scanner.Lookup = func(name string) (string, error) {
		if name == "tesseract" {
			return "", errors.New("missing")
		}
		return "/fake", nil
	}
	if _, _, err := scanner.extractPDF(context.Background(), pdfPath); err == nil || !strings.Contains(err.Error(), "tesseract is not installed") {
		t.Fatalf("missing tesseract = %v", err)
	}
	scanner.Lookup = func(string) (string, error) { return "/fake", nil }
	scanner.Runner = func(_ context.Context, name string, _ ...string) ([]byte, error) {
		if name == "pdftotext" {
			return []byte(""), nil
		}
		return nil, nil
	}
	if _, _, err := scanner.extractPDF(context.Background(), pdfPath); err == nil || !strings.Contains(err.Error(), "produced no page images") {
		t.Fatalf("empty page images = %v", err)
	}
}

func TestScannerMetadataAndIdentifiers(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	inbox := filepath.Join(dataDir, "inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(inbox, "ATM Receipt 2024!.txt")
	if err := os.WriteFile(path, []byte(testAddress), 0o600); err != nil {
		t.Fatal(err)
	}
	scanner := DefaultScanner()
	scanner.Now = func() time.Time { return time.Unix(0, 0) }
	file, err := scanner.Scan(context.Background(), inbox, dataDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(file.Sources) != 1 || !strings.HasPrefix(file.Sources[0].Purchase.ID, "atm-receipt-2024-") {
		t.Fatalf("generated purchase ID = %+v", file.Sources)
	}
	if !strings.HasPrefix(file.Sources[0].Candidates[0].ID, "cand-") || file.SchemaVersion != SchemaVersion || file.GeneratedAt != "1970-01-01T00:00:00Z" {
		t.Fatalf("generated metadata = %+v", file)
	}
	for _, media := range []string{"a.pdf", "a.png", "a.jpg", "a.jpeg", "a.tif", "a.tiff", "a.webp", "a.csv", "a.tsv", "a.json", "a.txt", "a.text"} {
		if _, ok := mediaTypeFor(media); !ok {
			t.Errorf("media type missing for %s", media)
		}
	}
	if _, ok := mediaTypeFor("a.bin"); ok {
		t.Error("unsupported media type accepted")
	}
}

func TestSaveLoadAndValidation(t *testing.T) {
	file := validIntakeFile()
	path := filepath.Join(t.TempDir(), "purchase-intake.json")
	if err := Save(path, file); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Sources) != 1 || loaded.Sources[0].Candidates[0].Value != testAddress {
		t.Fatalf("loaded file = %+v", loaded)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestValidationFailures(t *testing.T) {
	base := validIntakeFile()
	cases := []struct {
		name   string
		mutate func(*File)
		want   string
	}{
		{"schema", func(f *File) { f.SchemaVersion = 2 }, "schema_version"},
		{"generated", func(f *File) { f.GeneratedAt = "bad" }, "generated_at"},
		{"no sources", func(f *File) { f.Sources = nil }, "at least one"},
		{"source id", func(f *File) { f.Sources[0].ID = "" }, "source ID"},
		{"duplicate source", func(f *File) { f.Sources = append(f.Sources, f.Sources[0]) }, "duplicated"},
		{"path", func(f *File) { f.Sources[0].Path = "../x" }, "unsafe"},
		{"sha", func(f *File) { f.Sources[0].SHA256 = "bad" }, "sha256"},
		{"sha hex", func(f *File) { f.Sources[0].SHA256 = strings.Repeat("z", 64) }, "sha256"},
		{"media", func(f *File) { f.Sources[0].MediaType = "" }, "media_type"},
		{"extraction empty", func(f *File) { f.Sources[0].Extraction = nil }, "no extraction"},
		{"extraction blank", func(f *File) { f.Sources[0].Extraction = []string{" "} }, "empty extraction"},
		{"proposal", func(f *File) { f.Sources[0].Purchase.Date = "bad" }, "invalid purchase date"},
		{"candidate id", func(f *File) { f.Sources[0].Candidates[0].ID = "" }, "candidate ID"},
		{"candidate kind", func(f *File) { f.Sources[0].Candidates[0].Kind = "bad" }, "invalid kind"},
		{"candidate value", func(f *File) { f.Sources[0].Candidates[0].Value = "bad" }, "invalid mainnet address"},
		{"candidate role", func(f *File) { f.Sources[0].Candidates[0].Role = "bad" }, "invalid candidate role"},
		{"role kind", func(f *File) { f.Sources[0].Candidates[0].Role = RolePurchaseTxID }, "requires kind"},
		{"confidence", func(f *File) { f.Sources[0].Candidates[0].Confidence = "bad" }, "invalid confidence"},
		{"decision", func(f *File) { f.Sources[0].Candidates[0].Review.Decision = "bad" }, "invalid review decision"},
		{"resolution", func(f *File) { f.Sources[0].Candidates[0].Resolution = &Resolution{Valid: true} }, "requires canonical"},
		{"resolution outpoint", func(f *File) {
			f.Sources[0].Candidates[0].Resolution = &Resolution{Valid: true, Canonical: testAddress, CurrentUTXOs: []CurrentUTXO{{Outpoint: "bad"}}}
		}, "invalid resolved outpoint"},
		{"resolution value", func(f *File) {
			f.Sources[0].Candidates[0].Resolution = &Resolution{Valid: true, Canonical: testAddress, CurrentUTXOs: []CurrentUTXO{{Outpoint: testParent + ":0", ValueSats: -1}}}
		}, "negative value"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			file := base
			file.Sources = append([]Source(nil), base.Sources...)
			file.Sources[0].Candidates = append([]Candidate(nil), base.Sources[0].Candidates...)
			test.mutate(&file)
			if err := Validate(file); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Validate() = %v, want %q", err, test.want)
			}
		})
	}
}

func TestCandidateValidationVariants(t *testing.T) {
	file := validIntakeFile()
	file.Sources[0].Candidates[0].Kind = KindTxID
	file.Sources[0].Candidates[0].Value = testTxID
	file.Sources[0].Candidates[0].Role = RolePurchaseTxID
	if err := Validate(file); err != nil {
		t.Fatal(err)
	}
	file.Sources[0].Candidates[0].Kind = KindOutpoint
	file.Sources[0].Candidates[0].Value = testParent + ":0"
	file.Sources[0].Candidates[0].Role = RolePurchaseOutpoint
	if err := Validate(file); err != nil {
		t.Fatal(err)
	}
	file.Sources[0].Candidates[0].Value = testParent
	if err := Validate(file); err == nil {
		t.Fatal("non-canonical outpoint accepted")
	}
	file.Sources[0].Candidates[0].Kind = KindTxID
	file.Sources[0].Candidates[0].Value = strings.ToUpper(testTxID)
	if err := Validate(file); err == nil {
		t.Fatal("uppercase txid accepted")
	}
}

func TestPromoteAndReview(t *testing.T) {
	base := validIntakeFile()
	base.Sources[0].Candidates[0].Role = RoleReceivedAt
	base.Sources[0].Candidates[0].Review.Decision = DecisionAccepted
	base.Sources[0].Purchase = PurchaseProposal{ID: "atm-1", Date: "2024-01-31T14:05:00Z", Provider: "ATM"}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, base.Sources[0].Path), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	promoted, err := Promote(base, model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{}}, root, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(promoted.Purchases) != 1 || promoted.Purchases[0].ID != "atm-1" || len(promoted.Purchases[0].Addresses) != 1 || len(promoted.Purchases[0].Artifacts) != 1 {
		t.Fatalf("promoted = %+v", promoted)
	}
	if _, err := Promote(base, promoted, root, false); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate promotion = %v", err)
	}
	if _, err := Promote(base, promoted, root, true); err != nil {
		t.Fatalf("replace promotion = %v", err)
	}

	options := ReviewOptions{Accept: base.Sources[0].Candidates[0].ID, Role: RoleReceivedAt, PurchaseID: "atm-2", Date: "2024-01-31T14:05:00Z", Provider: "ATM", AmountSats: int64Ptr(1000)}
	if err := ApplyReview(&base, options); err != nil {
		t.Fatal(err)
	}
	if base.Sources[0].Purchase.ID != "atm-2" || base.Sources[0].Candidates[0].Review.Decision != DecisionAccepted {
		t.Fatalf("reviewed file = %+v", base.Sources[0])
	}
	lines := List(base)
	if len(lines) != 1 || !strings.Contains(lines[0], "accepted") {
		t.Fatalf("list = %+v", lines)
	}
	if !strings.Contains(Summary(base), "1 accepted") {
		t.Fatalf("summary = %q", Summary(base))
	}
}

func TestReviewAndPromotionFailures(t *testing.T) {
	base := validIntakeFile()
	candidateID := base.Sources[0].Candidates[0].ID
	cases := []struct {
		name    string
		options ReviewOptions
		want    string
	}{
		{"multiple actions", ReviewOptions{Accept: candidateID, Reject: candidateID}, "only one"},
		{"missing candidate", ReviewOptions{Accept: "missing"}, "not found"},
		{"no action", ReviewOptions{}, "no review action"},
		{"role kind", ReviewOptions{Accept: candidateID, Role: RolePurchaseTxID}, "requires kind"},
		{"promotable", ReviewOptions{Accept: candidateID}, "non-promotable"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			file := base
			if err := ApplyReview(&file, test.options); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("ApplyReview = %v", err)
			}
		})
	}
	file := base
	if err := ApplyReview(&file, ReviewOptions{Reject: candidateID, Note: "operator"}); err != nil {
		t.Fatal(err)
	}
	if err := ApplyReview(&file, ReviewOptions{Pending: candidateID}); err != nil {
		t.Fatal(err)
	}
	file = base
	file.Sources[0].Candidates[0].Role = RoleReceivedAt
	file.Sources[0].Candidates[0].Review.Decision = DecisionAccepted
	file.Sources[0].Purchase.ID = "atm-1"
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, file.Sources[0].Path), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	file.Sources[0].SHA256 = shaOf("data")
	if _, err := Promote(file, model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{}}, root, false); err != nil {
		t.Fatal(err)
	}
	file.Sources[0].Purchase.ID = ""
	if _, err := Promote(file, model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{}}, root, false); err == nil || !strings.Contains(err.Error(), "no purchase ID") {
		t.Fatalf("missing purchase ID = %v", err)
	}
	file = base
	file.Sources[0].Candidates[0].Role = RolePaidTo
	file.Sources[0].Candidates[0].Review.Decision = DecisionAccepted
	if _, err := Promote(file, model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{}}, root, false); err == nil || !strings.Contains(err.Error(), "non-promotable") {
		t.Fatalf("paid-to promotion = %v", err)
	}
}

func validIntakeFile() File {
	return File{
		SchemaVersion: SchemaVersion,
		GeneratedAt:   "2026-11-11T00:00:00Z",
		Sources: []Source{{
			ID:         "src-example00001",
			Path:       "inbox/receipt.txt",
			SHA256:     shaOf("data"),
			MediaType:  "text/plain",
			Extraction: []string{"text"},
			Candidates: []Candidate{{
				ID: "cand-example00001", Kind: KindAddress, Value: testAddress, Role: RoleUnknown,
				Confidence: ConfidenceHigh, Line: 1, Review: Review{Decision: DecisionPending},
			}},
			Purchase: PurchaseProposal{ID: "atm-example"},
		}},
	}
}

func shaOf(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func int64Ptr(value int64) *int64 { return &value }

func TestScannerLoadAndValidateJSONSource(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	inbox := filepath.Join(dataDir, "inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, "list.json"), []byte(`{"address":"`+testAddress+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := DefaultScanner().Scan(context.Background(), inbox, dataDir)
	if err != nil || len(file.Sources) != 1 || file.Sources[0].MediaType != "application/json" {
		t.Fatalf("JSON source = %+v, %v", file, err)
	}
}

func TestExtractCSVRoleValidation(t *testing.T) {
	if _, err := ExtractCSV([]byte("address,role\n"+testAddress+",purchase_txid\n"), ','); err == nil {
		t.Fatal("incompatible role accepted")
	}
	if _, err := ExtractCSV([]byte("address,role\n"+testAddress+",bogus\n"), ','); err != nil {
		t.Fatalf("unknown role should not fail extraction: %v", err)
	}
}

func TestScannerReadAndWalkErrors(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	inbox := filepath.Join(dataDir, "inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(inbox, "receipt.txt")
	if err := os.WriteFile(path, []byte(testAddress), 0o600); err != nil {
		t.Fatal(err)
	}
	scanner := DefaultScanner()
	scanner.Lookup = func(string) (string, error) { return "/fake", nil }
	scanner.Runner = func(context.Context, string, ...string) ([]byte, error) { return []byte(testAddress), nil }
	if _, err := scanner.scanFile(context.Background(), filepath.Join(inbox, "missing.txt"), dataDir, "text/plain"); err == nil {
		t.Fatal("missing source accepted")
	}
	scanner.Runner = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	if _, _, err := scanner.extractPDF(context.Background(), path); err == nil || !strings.Contains(err.Error(), "no embedded text") {
		t.Fatalf("empty PDF text and OCR = %v", err)
	}
}

func TestScannerCSVAndOCRReadErrors(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	inbox := filepath.Join(dataDir, "inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	csvPath := filepath.Join(inbox, "bad.csv")
	if err := os.WriteFile(csvPath, []byte("address\n\"unterminated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	scanner := DefaultScanner()
	if _, err := scanner.scanFile(context.Background(), csvPath, dataDir, "text/csv"); err == nil {
		t.Fatal("malformed CSV source accepted")
	}
	scanner.Lookup = func(string) (string, error) { return "/fake", nil }
	scanner.Runner = func(context.Context, string, ...string) ([]byte, error) { return nil, errors.New("ocr failed") }
	imagePath := filepath.Join(inbox, "receipt.png")
	if err := os.WriteFile(imagePath, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.scanFile(context.Background(), imagePath, dataDir, "image/png"); err == nil {
		t.Fatal("failed OCR accepted")
	}
}
