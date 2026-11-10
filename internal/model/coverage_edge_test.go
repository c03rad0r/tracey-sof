package model

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestEdgeParseOutpointInvalidVout(t *testing.T) {
	txid := strings.Repeat("0", 64)
	for _, vout := range []string{"", "one", "-1", "4294967296"} {
		t.Run(vout, func(t *testing.T) {
			if _, err := ParseOutpoint(txid + ":" + vout); err == nil {
				t.Fatalf("expected vout %q to be rejected", vout)
			}
		})
	}
}

func TestEdgeTargetRejectsMalformedJSON(t *testing.T) {
	var target Target
	if err := json.Unmarshal([]byte(`{"txid":123}`), &target); err == nil {
		t.Fatal("expected target JSON with a non-string txid to be rejected")
	}
}
