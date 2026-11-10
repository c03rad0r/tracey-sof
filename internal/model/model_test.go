package model

import (
	"encoding/json"
	"testing"
)

func TestOutpoint(t *testing.T) {
	valid := "F4184FC596403B9D638783CF57ADFE4C75C605F6356FBC91338530E9831E9E16:2"
	outpoint, err := ParseOutpoint(valid)
	if err != nil {
		t.Fatal(err)
	}
	if outpoint.TxID != "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16" || outpoint.Vout != 2 || outpoint.String() != "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16:2" {
		t.Fatalf("unexpected outpoint: %+v", outpoint)
	}
	for _, invalid := range []string{"", "abc", "abcd:one", "abcd:1:2", "abcd:-1", "abcd:4294967296"} {
		if _, err := ParseOutpoint(invalid); err == nil {
			t.Errorf("expected %q to fail", invalid)
		}
	}
	target := Target{TxID: outpoint.TxID, Vout: 3}
	ref := TargetRef{TxID: outpoint.TxID, Vout: 4}
	if target.Key() == ref.Key() {
		t.Error("keys must include vout")
	}
}

func TestTargetJSONOutpoint(t *testing.T) {
	original := Target{TxID: "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16", Vout: 0, ValueSats: 10, Status: "documented", Trace: "complete"}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(data, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["outpoint"] != "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16:0" {
		t.Fatalf("outpoint missing: %s", data)
	}
	var decoded Target
	if err := json.Unmarshal(data, &decoded); err != nil || decoded.Key() != original.Key() {
		t.Fatalf("roundtrip failed: %+v, %v", decoded, err)
	}
	for _, bad := range []string{`{"txid":"f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16","vout":0}`, `{"txid":"f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16","vout":1,"outpoint":"f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16:0"}`, `{"txid":"f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16","vout":0,"outpoint":"bad"}`} {
		if err := json.Unmarshal([]byte(bad), &decoded); err == nil {
			t.Errorf("accepted inconsistent target: %s", bad)
		}
	}
	if _, err := json.Marshal(struct{ T Target }{original}); err != nil {
		t.Fatal(err)
	}
}

func TestStatusesAndShape(t *testing.T) {
	for _, valid := range []string{"documented", "partially_documented", "undocumented"} {
		if !ValidDocumentationStatus(valid) {
			t.Error(valid)
		}
	}
	for _, valid := range []string{"complete", "incomplete"} {
		if !ValidTraceStatus(valid) {
			t.Error(valid)
		}
	}
	if ValidDocumentationStatus("proven") || ValidTraceStatus("finished") {
		t.Error("invalid status accepted")
	}
	data, err := json.Marshal(Report{SchemaVersion: SchemaVersion, Network: "mainnet"})
	if err != nil || len(data) == 0 {
		t.Fatal(err)
	}
}
