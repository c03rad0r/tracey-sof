package intake

import (
	"regexp"
	"sort"
	"strings"

	"github.com/btcsuite/btcd/btcutil"
	"github.com/btcsuite/btcd/chaincfg"
	"tracey-sof/internal/model"
)

var (
	addressPattern  = regexp.MustCompile(`(?i)\b(?:bc1[02-9ac-hj-np-z]{11,87}|[13][1-9a-km-zA-HJ-NP-Z]{25,34})\b`)
	txidPattern     = regexp.MustCompile(`(?i)\b[0-9a-f]{64}\b`)
	outpointPattern = regexp.MustCompile(`(?i)\b[0-9a-f]{64}:[0-9]{1,10}\b`)
)

type textMatch struct {
	kind       string
	value      string
	start, end int
	line       int
	confidence string
}

func ExtractText(text string) []Candidate {
	var matches []textMatch
	var occupied [][2]int
	for _, span := range outpointPattern.FindAllStringIndex(text, -1) {
		raw := text[span[0]:span[1]]
		outpoint, err := model.ParseOutpoint(strings.ToLower(raw))
		if err != nil || outpoint.String() != strings.ToLower(raw) {
			continue
		}
		matches = append(matches, textMatch{KindOutpoint, outpoint.String(), span[0], span[1], lineAt(text, span[0]), ConfidenceHigh})
		occupied = append(occupied, [2]int{span[0], span[1]})
	}
	for _, span := range txidPattern.FindAllStringIndex(text, -1) {
		if overlaps(occupied, span[0], span[1]) {
			continue
		}
		value := strings.ToLower(text[span[0]:span[1]])
		confidence := ConfidenceLow
		if labelledTransaction(text, span[0]) {
			confidence = ConfidenceMedium
		}
		matches = append(matches, textMatch{KindTxID, value, span[0], span[1], lineAt(text, span[0]), confidence})
		occupied = append(occupied, [2]int{span[0], span[1]})
	}
	for _, span := range addressPattern.FindAllStringIndex(text, -1) {
		raw := text[span[0]:span[1]]
		canonical, ok := canonicalAddress(raw)
		if !ok {
			continue
		}
		matches = append(matches, textMatch{KindAddress, canonical, span[0], span[1], lineAt(text, span[0]), ConfidenceHigh})
		occupied = append(occupied, [2]int{span[0], span[1]})
	}
	sort.SliceStable(matches, func(i, j int) bool { return matches[i].start < matches[j].start })
	result := make([]Candidate, 0, len(matches))
	seen := map[string]bool{}
	for _, match := range matches {
		key := match.kind + "\x00" + match.value
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, Candidate{
			Kind:       match.kind,
			Value:      match.value,
			Role:       RoleUnknown,
			Confidence: match.confidence,
			Line:       match.line,
			Review:     Review{Decision: DecisionPending},
		})
	}
	return result
}

func canonicalAddress(value string) (string, bool) {
	decoded, err := btcutil.DecodeAddress(value, &chaincfg.MainNetParams)
	if err != nil || !decoded.IsForNet(&chaincfg.MainNetParams) {
		return "", false
	}
	return decoded.EncodeAddress(), true
}

func overlaps(occupied [][2]int, start, end int) bool {
	for _, span := range occupied {
		if start < span[1] && end > span[0] {
			return true
		}
	}
	return false
}

func lineAt(text string, offset int) int {
	if offset > len(text) {
		offset = len(text)
	}
	return strings.Count(text[:offset], "\n") + 1
}

func labelledTransaction(text string, offset int) bool {
	start := offset - 64
	if start < 0 {
		start = 0
	}
	prefix := strings.ToLower(text[start:offset])
	for _, marker := range []string{"txid", "tx:", "transaction", "tx "} {
		if strings.Contains(prefix, marker) {
			return true
		}
	}
	return false
}
