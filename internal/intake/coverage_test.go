package intake

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

type faultIntakeFile struct {
	stage  string
	closed int
}

func (f *faultIntakeFile) Name() string { return "intake.tmp" }
func (f *faultIntakeFile) Chmod(os.FileMode) error {
	if f.stage == "chmod" {
		return errors.New("chmod failure")
	}
	return nil
}
func (f *faultIntakeFile) Write([]byte) (int, error) {
	if f.stage == "write" {
		return 0, errors.New("write failure")
	}
	return 1, nil
}
func (f *faultIntakeFile) Sync() error {
	if f.stage == "sync" {
		return errors.New("sync failure")
	}
	return nil
}
func (f *faultIntakeFile) Close() error {
	f.closed++
	if f.stage == "close" {
		return errors.New("close failure")
	}
	return nil
}

type faultIntakeStore struct {
	stage string
	file  *faultIntakeFile
}

func (s *faultIntakeStore) MkdirAll(string, os.FileMode) error {
	if s.stage == "mkdir" {
		return errors.New("mkdir failure")
	}
	return nil
}
func (s *faultIntakeStore) CreateTemp(string, string) (intakeTempFile, error) {
	if s.stage == "create" {
		return nil, errors.New("create failure")
	}
	s.file = &faultIntakeFile{stage: s.stage}
	return s.file, nil
}
func (s *faultIntakeStore) Remove(string) error { return nil }
func (s *faultIntakeStore) Rename(string, string) error {
	if s.stage == "rename" {
		return errors.New("rename failure")
	}
	return nil
}

func TestSaveFailureStages(t *testing.T) {
	file := validIntakeFile()
	for _, stage := range []string{"mkdir", "create", "chmod", "write", "sync", "close", "rename"} {
		t.Run(stage, func(t *testing.T) {
			store := &faultIntakeStore{stage: stage}
			if err := save("ignored", file, func(any, string, string) ([]byte, error) { return []byte("{}"), nil }, store); err == nil {
				t.Fatalf("%s failure was accepted", stage)
			}
			if (stage == "chmod" || stage == "write" || stage == "sync") && store.file.closed != 1 {
				t.Fatalf("temporary file not closed after %s failure", stage)
			}
		})
	}
	if err := save("ignored", file, func(any, string, string) ([]byte, error) { return nil, errors.New("marshal failure") }, &faultIntakeStore{}); err == nil {
		t.Fatal("marshal failure accepted")
	}
	if err := save("ignored", File{}, func(any, string, string) ([]byte, error) { return nil, nil }, &faultIntakeStore{}); err == nil {
		t.Fatal("invalid file was saved")
	}
}

func TestLoadFailureStages(t *testing.T) {
	valid := validIntakeFile()
	dir := t.TempDir()
	validPath := filepath.Join(dir, "valid.json")
	if err := Save(validPath, valid); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("directory load accepted")
	}
	oversized := filepath.Join(dir, "oversized.json")
	if err := os.WriteFile(oversized, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(oversized, maxFileBytes+1); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(oversized); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversized load = %v", err)
	}
	cases := []struct {
		name    string
		content string
		want    string
	}{
		{"decode", "{", "decode"},
		{"multiple", "{}{}", "multiple"},
		{"trailing", "{} x", "trailing"},
		{"validation", `{"schema_version":2,"generated_at":"2026-11-11T00:00:00Z","sources":[]}`, "schema_version"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(dir, test.name+".json")
			if err := os.WriteFile(path, []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(path); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("Load = %v", err)
			}
		})
	}
}

func TestValidationAdditionalBranches(t *testing.T) {
	file := validIntakeFile()
	file.Sources[0].Purchase = PurchaseProposal{ID: "bad id!"}
	if err := Validate(file); err == nil {
		t.Fatal("invalid purchase ID accepted")
	}
	file = validIntakeFile()
	file.Sources[0].Purchase = PurchaseProposal{AmountSats: int64Ptr(0)}
	if err := Validate(file); err == nil {
		t.Fatal("zero amount accepted")
	}
	file = validIntakeFile()
	file.Sources[0].Purchase = PurchaseProposal{Fiat: &model.Fiat{Amount: "bad", Currency: "EUR"}}
	if err := Validate(file); err == nil {
		t.Fatal("invalid fiat accepted")
	}
	if err := validateCandidateValue("bad", "x"); err == nil {
		t.Fatal("unknown candidate kind accepted")
	}
	if err := validateCandidateValue(KindOutpoint, testParent+":00"); err == nil {
		t.Fatal("non-canonical outpoint accepted")
	}
	if err := validateResolution(Resolution{}); err != nil {
		t.Fatalf("empty resolution = %v", err)
	}
	if err := validateResolution(Resolution{CurrentUTXOs: []CurrentUTXO{{Outpoint: "bad"}}}); err == nil {
		t.Fatal("invalid resolution outpoint accepted")
	}
	if err := validateResolution(Resolution{CurrentUTXOs: []CurrentUTXO{{Outpoint: testParent + ":0", ValueSats: -1}}}); err == nil {
		t.Fatal("negative resolution value accepted")
	}
	if file.SourceByID("missing") != nil || file.SourceByID(file.Sources[0].ID) == nil {
		t.Fatal("SourceByID failed")
	}
}

func TestReviewAdditionalBranches(t *testing.T) {
	file := validIntakeFile()
	if err := ApplyReview(&file, ReviewOptions{PurchaseID: "single-source"}); err != nil {
		t.Fatal(err)
	}
	twoSources := validIntakeFile()
	twoSources.Sources = append(twoSources.Sources, twoSources.Sources[0])
	twoSources.Sources[1].ID = "src-second"
	if err := ApplyReview(&twoSources, ReviewOptions{PurchaseID: "ambiguous"}); err == nil {
		t.Fatal("metadata edit without a source was accepted")
	}
	file = validIntakeFile()
	candidateID := file.Sources[0].Candidates[0].ID
	if err := ApplyReview(&file, ReviewOptions{Accept: candidateID, Role: RoleReceivedAt, Note: "reviewed", Fiat: &model.Fiat{Amount: "10.00", Currency: "EUR"}}); err != nil {
		t.Fatal(err)
	}
	if file.Sources[0].Candidates[0].Review.Note != "reviewed" || file.Sources[0].Purchase.Fiat == nil {
		t.Fatal("review note or fiat not applied")
	}
	file = validIntakeFile()
	file.Sources[0].Purchase = PurchaseProposal{ID: "bad id!"}
	if err := ApplyReview(&file, ReviewOptions{PurchaseID: "bad id!"}); err == nil {
		t.Fatal("invalid metadata edit accepted")
	}
	file = validIntakeFile()
	file.Sources[0].Candidates[0].Resolution = &Resolution{Valid: true, Canonical: testAddress, CurrentUTXOs: []CurrentUTXO{{Outpoint: testParent + ":0", ValueSats: 1}}, TxIDExists: true, OutpointExists: true, InTrace: true}
	lines := List(file)
	if len(lines) != 1 || !strings.Contains(lines[0], "current_utxos=1") || !strings.Contains(lines[0], "txid_exists=true") || !strings.Contains(lines[0], "outpoint_exists=true") || !strings.Contains(lines[0], "in_trace=true") {
		t.Fatalf("list = %+v", lines)
	}
	rejected := validIntakeFile()
	rejected.Sources[0].Candidates[0].Review.Decision = DecisionRejected
	if !strings.Contains(Summary(rejected), "1 rejected") {
		t.Fatalf("summary = %q", Summary(rejected))
	}
	pending := validIntakeFile()
	pending.Sources[0].Candidates[0].Review.Decision = DecisionPending
	if !strings.Contains(Summary(pending), "1 pending") {
		t.Fatalf("pending summary = %q", Summary(pending))
	}
}

func TestPromotionAdditionalBranches(t *testing.T) {
	base := validIntakeFile()
	if _, err := Promote(base, model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{}}, t.TempDir(), false); err == nil {
		t.Fatal("promotion without accepted candidates succeeded")
	}
	base.Sources[0].Candidates[0].Role = RolePurchaseOutpoint
	base.Sources[0].Candidates[0].Value = testParent + ":0"
	base.Sources[0].Candidates[0].Review.Decision = DecisionAccepted
	base.Sources[0].Purchase = PurchaseProposal{ID: "atm-outpoint", AmountSats: int64Ptr(1000), Fiat: &model.Fiat{Amount: "10.00", Currency: "EUR"}}
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "inbox"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, base.Sources[0].Path), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	promoted, err := Promote(base, model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{}}, root, false)
	if err != nil || len(promoted.Purchases[0].Outpoints) != 1 || promoted.Purchases[0].AmountSats == nil {
		t.Fatalf("outpoint promotion = %+v, %v", promoted, err)
	}
	base.Sources[0].Candidates[0].Value = testParent + ":1"
	base.Sources[0].Candidates[0].Role = RolePurchaseTxID
	base.Sources[0].Purchase.ID = "atm-tx"
	base.Sources[0].Candidates[0].Value = testTxID
	if _, err := Promote(base, model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{}}, root, false); err != nil {
		t.Fatalf("txid promotion = %v", err)
	}
	if len(uniqueOutpoints([]model.Outpoint{{TxID: testParent, Vout: 0}, {TxID: testParent, Vout: 0}})) != 1 {
		t.Fatal("duplicate outpoints not removed")
	}
	if len(uniqueOutpoints([]model.Outpoint{{TxID: testParent, Vout: 1}, {TxID: testParent, Vout: 0}})) != 2 {
		t.Fatal("distinct outpoints not sorted")
	}
	invalid := validIntakeFile()
	invalid.Sources[0].Candidates[0].Role = RolePurchaseOutpoint
	invalid.Sources[0].Candidates[0].Value = "bad"
	invalid.Sources[0].Candidates[0].Review.Decision = DecisionAccepted
	if _, err := Promote(invalid, model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{}}, root, false); err == nil {
		t.Fatal("invalid outpoint promotion accepted")
	}
	empty := validIntakeFile()
	empty.Sources[0].Candidates[0].Role = RoleReceivedAt
	empty.Sources[0].Candidates[0].Value = ""
	empty.Sources[0].Candidates[0].Review.Decision = DecisionAccepted
	if _, err := Promote(empty, model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{}}, root, false); err == nil {
		t.Fatal("empty evidence promotion accepted")
	}
	missingArtifact := validIntakeFile()
	missingArtifact.Sources[0].Candidates[0].Role = RoleReceivedAt
	missingArtifact.Sources[0].Candidates[0].Review.Decision = DecisionAccepted
	if _, err := Promote(missingArtifact, model.PurchaseFile{SchemaVersion: 1, Purchases: []model.Purchase{}}, t.TempDir(), false); err == nil {
		t.Fatal("missing artifact accepted")
	}
}

func TestExtractionAdditionalBranches(t *testing.T) {
	if candidates, err := ExtractCSV(nil, ','); err != nil || len(candidates) != 0 {
		t.Fatalf("empty CSV = %+v, %v", candidates, err)
	}
	if _, err := ExtractCSV([]byte("address,role\n"+testAddress+"\n"), ','); err != nil {
		t.Fatalf("short role row = %v", err)
	}
	if _, err := ExtractCSV([]byte("address,txid\n"+testAddress+"\n"), ','); err != nil {
		t.Fatalf("short kind row = %v", err)
	}
	if candidates, err := ExtractCSV([]byte("address\n\n"), ','); err != nil || len(candidates) != 0 {
		t.Fatalf("empty cell = %+v, %v", candidates, err)
	}
	if _, err := structuredCandidate(KindAddress, "", RoleUnknown, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := structuredCandidate("bad", "x", RoleUnknown, 1); err != nil {
		t.Fatalf("unknown kind should be handled by caller: %v", err)
	}
	if _, err := structuredCandidate(KindOutpoint, "bad", RoleUnknown, 1); err == nil {
		t.Fatal("invalid outpoint accepted")
	}
	if isLowerHex("") || isLowerHex("ABC") || !isLowerHex("ab") {
		t.Fatal("hex checks failed")
	}
	if _, err := structuredCandidate(KindAddress, "not-an-address", RoleUnknown, 1); err == nil {
		t.Fatal("invalid address accepted")
	}
	if _, err := structuredCandidate(KindTxID, "bad", RoleUnknown, 1); err == nil {
		t.Fatal("invalid txid accepted")
	}
	result := dedupeCandidates([]Candidate{{Kind: "", Value: "x"}, {Kind: KindAddress, Value: testAddress}, {Kind: KindAddress, Value: testAddress, Role: RoleReceivedAt}})
	if len(result) != 1 || result[0].Role != RoleReceivedAt || result[0].Confidence == "" || result[0].Review.Decision == "" {
		t.Fatalf("dedupe = %+v", result)
	}
	if purchaseID("---.txt", strings.Repeat("a", 64)) != "purchase-aaaaaa" {
		t.Fatalf("fallback purchase ID = %q", purchaseID("---.txt", strings.Repeat("a", 64)))
	}
	long := strings.Repeat("a", 150) + ".txt"
	if len(purchaseID(long, strings.Repeat("b", 64))) > 107 {
		t.Fatal("long purchase ID not truncated")
	}
	if lineAt("abc", 99) != 1 {
		t.Fatal("lineAt out-of-range offset failed")
	}
	if !overlaps([][2]int{{1, 3}}, 2, 4) || overlaps([][2]int{{1, 3}}, 3, 4) {
		t.Fatal("overlap checks failed")
	}
	if candidates := ExtractText(testParent + ":9999999999"); len(candidates) != 1 || candidates[0].Kind != KindTxID {
		t.Fatalf("overflowing outpoint handling = %+v", candidates)
	}
}

func TestScannerAdditionalBranches(t *testing.T) {
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	inbox := filepath.Join(dataDir, "inbox")
	if err := os.MkdirAll(inbox, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(inbox, "receipt.pdf")
	if err := os.WriteFile(path, []byte("%PDF"), 0o600); err != nil {
		t.Fatal(err)
	}
	scanner := DefaultScanner()
	scanner.Lookup = func(string) (string, error) { return "/fake", nil }
	scanner.MkdirTemp = func(string, string) (string, error) { return "", errors.New("temp failure") }
	if _, _, err := scanner.extractPDF(context.Background(), path); err == nil {
		t.Fatal("temp failure accepted")
	}
	scanner.MkdirTemp = func(string, string) (string, error) { return t.TempDir(), nil }
	scanner.Runner = func(context.Context, string, ...string) ([]byte, error) { return nil, nil }
	scanner.Glob = func(string) ([]string, error) { return nil, errors.New("glob failure") }
	if _, _, err := scanner.extractPDF(context.Background(), path); err == nil {
		t.Fatal("glob failure accepted")
	}
	scanner.Glob = filepath.Glob
	scanner.Runner = func(_ context.Context, name string, args ...string) ([]byte, error) {
		if name == "pdftotext" {
			return nil, nil
		}
		if name == "pdftoppm" {
			prefix := args[len(args)-1]
			return nil, os.WriteFile(prefix+"-1.png", []byte("png"), 0o600)
		}
		return nil, errors.New("tesseract failure")
	}
	if _, _, err := scanner.extractPDF(context.Background(), path); err == nil {
		t.Fatal("tesseract failure accepted")
	}
	if _, err := (Scanner{}).lookup("sh"); err != nil {
		t.Fatalf("default lookup = %v", err)
	}
	defaultScanner := Scanner{Runner: DefaultScanner().Runner}
	if _, err := defaultScanner.run(context.Background(), "sh", "-c", "true"); err != nil {
		t.Fatalf("default runner = %v", err)
	}
	if _, err := scanner.scanFile(context.Background(), inbox, dataDir, "text/plain"); err == nil {
		t.Fatal("directory source accepted")
	}
	textPath := filepath.Join(inbox, "receipt.txt")
	if err := os.WriteFile(textPath, []byte(testAddress), 0o600); err != nil {
		t.Fatal(err)
	}
	scanInbox := filepath.Join(dataDir, "text-only")
	if err := os.Mkdir(scanInbox, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(scanInbox, "receipt.txt"), []byte(testAddress), 0o600); err != nil {
		t.Fatal(err)
	}
	if file, err := (Scanner{}).Scan(context.Background(), scanInbox, dataDir); err != nil || len(file.Sources) != 1 {
		t.Fatalf("zero-value scanner = %+v, %v", file, err)
	}
	emptyInbox := filepath.Join(dataDir, "empty")
	if err := os.Mkdir(emptyInbox, 0o700); err != nil {
		t.Fatal(err)
	}
	if file, err := (Scanner{}).Scan(context.Background(), emptyInbox, dataDir); err != nil || len(file.Sources) != 0 {
		t.Fatalf("empty scan = %+v, %v", file, err)
	}
	badCSV := filepath.Join(inbox, "bad.csv")
	if err := os.WriteFile(badCSV, []byte("address\n\"unterminated\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := (Scanner{}).Scan(context.Background(), inbox, dataDir); err == nil {
		t.Fatal("walk scan error accepted")
	}
	if err := os.Remove(badCSV); err != nil {
		t.Fatal(err)
	}
	relScanner := DefaultScanner()
	relScanner.Rel = func(string, string) (string, error) { return "", errors.New("rel failure") }
	if _, err := relScanner.scanFile(context.Background(), textPath, dataDir, "text/plain"); err == nil {
		t.Fatal("relative path failure accepted")
	}
	validateScanner := DefaultScanner()
	validateScanner.ValidateFile = func(File) error { return errors.New("validate failure") }
	if _, err := validateScanner.Scan(context.Background(), scanInbox, dataDir); err == nil {
		t.Fatal("scan validation failure accepted")
	}
	tsvPath := filepath.Join(inbox, "list.tsv")
	if err := os.WriteFile(tsvPath, []byte("outpoint\trole\n"+testParent+":0\tpurchase_outpoint\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	tsvScanner := DefaultScanner()
	if source, err := tsvScanner.scanFile(context.Background(), tsvPath, dataDir, "text/tab-separated-values"); err != nil || source.Candidates[0].Value != testParent+":0" {
		t.Fatalf("TSV source = %+v, %v", source, err)
	}
	if _, err := structuredCandidate(KindAddress, testAddress, RolePurchaseTxID, 1); err == nil {
		t.Fatal("incompatible structured role accepted")
	}
}
