package intake

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"tracey-sof/internal/model"
)

const DefaultMaxSourceBytes = 64 << 20

// CommandRunner and CommandLookup are seams for tests and for environments
// where OCR binaries are unavailable.
type CommandRunner func(context.Context, string, ...string) ([]byte, error)
type CommandLookup func(string) (string, error)

type Scanner struct {
	Runner       CommandRunner
	Lookup       CommandLookup
	Timeout      time.Duration
	MaxPages     int
	MaxBytes     int64
	Now          func() time.Time
	MkdirTemp    func(string, string) (string, error)
	Glob         func(string) ([]string, error)
	Rel          func(string, string) (string, error)
	ValidateFile func(File) error
}

func DefaultScanner() Scanner {
	return Scanner{
		Runner: func(ctx context.Context, name string, args ...string) ([]byte, error) {
			return exec.CommandContext(ctx, name, args...).CombinedOutput()
		},
		Lookup:       exec.LookPath,
		Timeout:      2 * time.Minute,
		MaxPages:     20,
		MaxBytes:     DefaultMaxSourceBytes,
		Now:          time.Now,
		MkdirTemp:    os.MkdirTemp,
		Glob:         filepath.Glob,
		Rel:          filepath.Rel,
		ValidateFile: Validate,
	}
}

func (s Scanner) Scan(ctx context.Context, inbox, dataDir string) (File, error) {
	if s.Runner == nil {
		s.Runner = DefaultScanner().Runner
	}
	if s.Lookup == nil {
		s.Lookup = DefaultScanner().Lookup
	}
	if s.Timeout <= 0 {
		s.Timeout = 2 * time.Minute
	}
	if s.MaxPages <= 0 {
		s.MaxPages = 20
	}
	if s.MaxBytes <= 0 {
		s.MaxBytes = DefaultMaxSourceBytes
	}
	if s.Now == nil {
		s.Now = time.Now
	}
	if s.MkdirTemp == nil {
		s.MkdirTemp = os.MkdirTemp
	}
	if s.Glob == nil {
		s.Glob = filepath.Glob
	}
	if s.Rel == nil {
		s.Rel = filepath.Rel
	}
	if s.ValidateFile == nil {
		s.ValidateFile = Validate
	}
	file := NewFile(s.Now())
	err := filepath.WalkDir(inbox, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		mediaType, ok := mediaTypeFor(path)
		if !ok {
			return nil
		}
		source, err := s.scanFile(ctx, path, dataDir, mediaType)
		if err != nil {
			return fmt.Errorf("scan %s: %w", path, err)
		}
		file.Sources = append(file.Sources, source)
		return nil
	})
	if err != nil {
		return file, err
	}
	sort.Slice(file.Sources, func(i, j int) bool { return file.Sources[i].Path < file.Sources[j].Path })
	if len(file.Sources) == 0 {
		return file, nil
	}
	if err := s.ValidateFile(file); err != nil {
		return file, err
	}
	return file, nil
}

func (s Scanner) scanFile(ctx context.Context, path, dataDir, mediaType string) (Source, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Source{}, err
	}
	if info.Size() > s.MaxBytes {
		return Source{}, fmt.Errorf("file exceeds %d bytes", s.MaxBytes)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Source{}, err
	}
	if containsPrivateMaterial(data) {
		return Source{}, fmt.Errorf("source contains private key material; remove it before intake")
	}
	contentHash := sha256.Sum256(data)
	rel, err := s.Rel(dataDir, path)
	if err != nil {
		return Source{}, err
	}
	rel = filepath.ToSlash(rel)
	if err := validateRelativePath(rel); err != nil {
		return Source{}, fmt.Errorf("source must be stored below the data directory: %w", err)
	}
	source := Source{
		ID:        sourceID(rel, hex.EncodeToString(contentHash[:])),
		Path:      rel,
		SHA256:    hex.EncodeToString(contentHash[:]),
		MediaType: mediaType,
	}
	var candidates []Candidate
	var methods []string
	switch {
	case mediaType == "application/pdf":
		text, used, err := s.extractPDF(ctx, path)
		if err != nil {
			return Source{}, err
		}
		methods = used
		candidates = ExtractText(text)
	case strings.HasPrefix(mediaType, "image/"):
		text, err := s.ocrImage(ctx, path)
		if err != nil {
			return Source{}, err
		}
		methods = []string{"tesseract"}
		candidates = ExtractText(text)
	case mediaType == "text/csv" || mediaType == "text/tab-separated-values":
		comma := ','
		if mediaType == "text/tab-separated-values" {
			comma = '\t'
		}
		candidates, err = ExtractCSV(data, comma)
		if err != nil {
			return Source{}, err
		}
		methods = []string{"structured-text"}
	default:
		methods = []string{"text"}
		candidates = ExtractText(string(data))
	}
	source.Extraction = methods
	source.Candidates = dedupeCandidates(candidates)
	for i := range source.Candidates {
		source.Candidates[i].ID = candidateID(source.ID, source.Candidates[i])
	}
	source.Purchase.ID = purchaseID(rel, source.SHA256)
	return source, nil
}

func (s Scanner) extractPDF(ctx context.Context, path string) (string, []string, error) {
	text, textErr := s.run(ctx, "pdftotext", "-layout", "-enc", "UTF-8", path, "-")
	if textErr == nil && strings.TrimSpace(string(text)) != "" {
		return string(text), []string{"pdftotext"}, nil
	}
	ocr, ocrErr := s.ocrPDF(ctx, path)
	if ocrErr != nil {
		if textErr != nil {
			return "", nil, fmt.Errorf("pdftotext failed: %v; OCR fallback failed: %w", textErr, ocrErr)
		}
		return "", nil, fmt.Errorf("PDF has no embedded text and OCR fallback failed: %w", ocrErr)
	}
	return ocr, []string{"pdftoppm", "tesseract"}, nil
}

func (s Scanner) ocrPDF(ctx context.Context, path string) (string, error) {
	if _, err := s.lookup("pdftoppm"); err != nil {
		return "", fmt.Errorf("pdftoppm is not installed")
	}
	if _, err := s.lookup("tesseract"); err != nil {
		return "", fmt.Errorf("tesseract is not installed")
	}
	dir, err := s.MkdirTemp("", "sof-intake-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	prefix := filepath.Join(dir, "page")
	if _, err := s.run(ctx, "pdftoppm", "-r", "200", "-png", "-f", "1", "-l", fmt.Sprint(s.MaxPages), path, prefix); err != nil {
		return "", err
	}
	pages, err := s.Glob(prefix + "*.png")
	if err != nil {
		return "", err
	}
	sort.Strings(pages)
	if len(pages) == 0 {
		return "", fmt.Errorf("pdftoppm produced no page images")
	}
	var result strings.Builder
	for _, page := range pages {
		text, err := s.ocrImage(ctx, page)
		if err != nil {
			return "", err
		}
		result.WriteString(text)
		result.WriteByte('\n')
	}
	return result.String(), nil
}

func (s Scanner) ocrImage(ctx context.Context, path string) (string, error) {
	out, err := s.run(ctx, "tesseract", path, "stdout")
	if err != nil {
		return "", err
	}
	return string(out), nil
}

func (s Scanner) lookup(name string) (string, error) {
	if s.Lookup == nil {
		return exec.LookPath(name)
	}
	return s.Lookup(name)
}

func (s Scanner) run(ctx context.Context, name string, args ...string) ([]byte, error) {
	if _, err := s.lookup(name); err != nil {
		return nil, fmt.Errorf("%s is not installed: %w", name, err)
	}
	callCtx := ctx
	if s.Timeout > 0 {
		var cancel context.CancelFunc
		callCtx, cancel = context.WithTimeout(ctx, s.Timeout)
		defer cancel()
	}
	out, err := s.Runner(callCtx, name, args...)
	if err != nil {
		return nil, fmt.Errorf("%s failed: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return out, nil
}

func containsPrivateMaterial(data []byte) bool {
	lower := bytes.ToLower(data)
	for _, marker := range [][]byte{[]byte("xprv"), []byte("yprv"), []byte("zprv"), []byte("tprv"), []byte("priv("), []byte("wif(")} {
		if bytes.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func mediaTypeFor(path string) (string, bool) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".pdf":
		return "application/pdf", true
	case ".png":
		return "image/png", true
	case ".jpg", ".jpeg":
		return "image/jpeg", true
	case ".tif", ".tiff":
		return "image/tiff", true
	case ".webp":
		return "image/webp", true
	case ".csv":
		return "text/csv", true
	case ".tsv":
		return "text/tab-separated-values", true
	case ".json":
		return "application/json", true
	case ".txt", ".text":
		return "text/plain", true
	default:
		return "", false
	}
}

func ExtractCSV(data []byte, comma rune) ([]Candidate, error) {
	reader := csv.NewReader(strings.NewReader(strings.TrimPrefix(string(data), "\uFEFF")))
	reader.Comma = comma
	reader.FieldsPerRecord = -1
	reader.TrimLeadingSpace = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse delimited input: %w", err)
	}
	if len(records) == 0 {
		return nil, nil
	}
	columns := mapColumns(records[0])
	if len(columns) == 0 {
		return ExtractText(string(data)), nil
	}
	var candidates []Candidate
	for rowIndex, record := range records[1:] {
		role := RoleUnknown
		if column, ok := columns["role"]; ok && column < len(record) {
			if value := strings.TrimSpace(strings.ToLower(record[column])); ValidRole(value) {
				role = value
			}
		}
		if role != RoleUnknown {
			if expected := KindForRole(role); expected != "" {
				if _, ok := columns[expected]; !ok {
					return nil, fmt.Errorf("line %d: role %q requires a %s column", rowIndex+2, role, expected)
				}
			}
		}
		for _, kind := range []string{KindAddress, KindTxID, KindOutpoint} {
			column, ok := columns[kind]
			if !ok || column >= len(record) {
				continue
			}
			candidateRole := RoleUnknown
			if KindForRole(role) == kind {
				candidateRole = role
			}
			values := splitCell(record[column])
			for _, value := range values {
				candidate, err := structuredCandidate(kind, value, candidateRole, rowIndex+2)
				if err != nil {
					return nil, err
				}
				candidates = append(candidates, candidate)
			}
		}
	}
	return candidates, nil
}

func mapColumns(header []string) map[string]int {
	columns := map[string]int{}
	for index, value := range header {
		normalized := strings.ToLower(strings.TrimSpace(value))
		normalized = strings.NewReplacer(" ", "_", "-", "_").Replace(normalized)
		switch normalized {
		case "address", "addresses", "wallet_address", "receive_address", "receiving_address":
			columns[KindAddress] = index
		case "txid", "txids", "transaction", "transaction_id", "transaction_hash":
			columns[KindTxID] = index
		case "outpoint", "outpoints":
			columns[KindOutpoint] = index
		case "role", "evidence_role":
			columns["role"] = index
		}
	}
	return columns
}

func structuredCandidate(kind, raw, role string, line int) (Candidate, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return Candidate{}, nil
	}
	switch kind {
	case KindAddress:
		canonical, ok := canonicalAddress(value)
		if !ok {
			return Candidate{}, fmt.Errorf("line %d has invalid address %q", line, value)
		}
		value = canonical
	case KindTxID:
		if len(value) != 64 || !isLowerHex(value) {
			return Candidate{}, fmt.Errorf("line %d has invalid txid %q", line, value)
		}
	case KindOutpoint:
		outpoint, err := model.ParseOutpoint(strings.ToLower(value))
		if err != nil || outpoint.String() != strings.ToLower(value) {
			return Candidate{}, fmt.Errorf("line %d has invalid outpoint %q", line, value)
		}
		value = outpoint.String()
	}
	if err := ValidateRoleForKind(kind, role); err != nil {
		return Candidate{}, fmt.Errorf("line %d: %w", line, err)
	}
	return Candidate{Kind: kind, Value: value, Role: role, Confidence: ConfidenceHigh, Line: line,
		Review: Review{Decision: DecisionPending}}, nil
}

func splitCell(value string) []string {
	fields := strings.FieldsFunc(value, func(r rune) bool {
		return r == ';' || r == '|' || r == ',' || r == ' ' || r == '\n' || r == '\t'
	})
	result := make([]string, 0, len(fields))
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field != "" {
			result = append(result, field)
		}
	}
	return result
}

func isLowerHex(value string) bool {
	if len(value) == 0 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func dedupeCandidates(candidates []Candidate) []Candidate {
	result := make([]Candidate, 0, len(candidates))
	index := map[string]int{}
	for _, candidate := range candidates {
		if candidate.Kind == "" || candidate.Value == "" {
			continue
		}
		key := candidate.Kind + "\x00" + candidate.Value
		if existing, ok := index[key]; ok {
			if result[existing].Role == RoleUnknown && candidate.Role != RoleUnknown {
				result[existing].Role = candidate.Role
			}
			continue
		}
		if candidate.Role == "" {
			candidate.Role = RoleUnknown
		}
		if candidate.Confidence == "" {
			candidate.Confidence = ConfidenceMedium
		}
		if candidate.Review.Decision == "" {
			candidate.Review.Decision = DecisionPending
		}
		index[key] = len(result)
		result = append(result, candidate)
	}
	return result
}

func sourceID(rel, hash string) string {
	sum := sha256.Sum256([]byte(rel + "\x00" + hash))
	return "src-" + hex.EncodeToString(sum[:])[:12]
}

func candidateID(sourceID string, candidate Candidate) string {
	sum := sha256.Sum256([]byte(sourceID + "\x00" + candidate.Kind + "\x00" + candidate.Value))
	return "cand-" + hex.EncodeToString(sum[:])[:12]
}

func purchaseID(rel, hash string) string {
	base := strings.TrimSuffix(filepath.Base(rel), filepath.Ext(rel))
	var slug strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(base) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '_' {
			slug.WriteRune(r)
			lastDash = false
		} else if !lastDash {
			slug.WriteByte('-')
			lastDash = true
		}
	}
	value := strings.Trim(slug.String(), "-._")
	if value == "" {
		value = "purchase"
	}
	if len(value) > 100 {
		value = strings.Trim(value[:100], "-._")
	}
	return value + "-" + hash[:6]
}
