package fixtures_test

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"tracey-sof/internal/core"
	"tracey-sof/internal/fixtures"
	"tracey-sof/internal/model"
)

const (
	fixtureCoinbase = "0437cd7f8525ceed2324359c2d0ba26006d92d856a9c20fa0241106ee5a597c9"
	fixtureTransfer = "f4184fc596403b9d638783cf57adfe4c75c605f6356fbc91338530e9831e9e16"
	fixtureSegWit   = "a38d861a0584254429a8e84747c0d203ddada49d6355bbf9de92e4fffc230c47"
)

func publicFixture(t *testing.T, txid string) fixtures.Fixture {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "mainnet", txid+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var fixture fixtures.Fixture
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	return fixture
}

func writeFixture(t *testing.T, dir, name string, fixture fixtures.Fixture) {
	t.Helper()
	data, err := json.Marshal(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadDirReturnsSortedCompleteFixturesDeterministically(t *testing.T) {
	dir := t.TempDir()
	want := []fixtures.Fixture{
		publicFixture(t, fixtureTransfer),
		publicFixture(t, fixtureCoinbase),
		publicFixture(t, fixtureSegWit),
	}
	for _, fixture := range want {
		writeFixture(t, dir, fixture.TxID+".json", fixture)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("ignored"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "directory.json"), 0o700); err != nil {
		t.Fatal(err)
	}

	first, err := fixtures.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	second, err := fixtures.LoadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("repeated loads differ:\nfirst:  %#v\nsecond: %#v", first, second)
	}
	sort.Slice(want, func(i, j int) bool { return want[i].TxID < want[j].TxID })
	if !reflect.DeepEqual(first, want) {
		t.Fatalf("loaded fixtures do not preserve metadata or sorted order\ngot:  %#v\nwant: %#v", first, want)
	}
}

func TestLoadDirRejectsMalformedIncompleteAndNoncanonicalFixtures(t *testing.T) {
	valid := publicFixture(t, fixtureTransfer)
	other := publicFixture(t, fixtureCoinbase)
	tests := []struct {
		name    string
		file    string
		fixture fixtures.Fixture
		wantErr string
	}{
		{name: "unsupported schema", file: valid.TxID + ".json", fixture: func() fixtures.Fixture { f := valid; f.SchemaVersion++; return f }(), wantErr: "incomplete fixture"},
		{name: "missing txid", file: valid.TxID + ".json", fixture: func() fixtures.Fixture { f := valid; f.TxID = ""; return f }(), wantErr: "incomplete fixture"},
		{name: "missing hex", file: valid.TxID + ".json", fixture: func() fixtures.Fixture { f := valid; f.Hex = ""; return f }(), wantErr: "incomplete fixture"},
		{name: "invalid transaction hex", file: valid.TxID + ".json", fixture: func() fixtures.Fixture { f := valid; f.Hex = "not-hex"; return f }(), wantErr: "txid or transaction bytes are inconsistent"},
		{name: "transaction txid mismatch", file: other.TxID + ".json", fixture: func() fixtures.Fixture { f := valid; f.TxID = other.TxID; return f }(), wantErr: "txid or transaction bytes are inconsistent"},
		{name: "uppercase noncanonical txid", file: strings.ToUpper(valid.TxID) + ".json", fixture: func() fixtures.Fixture { f := valid; f.TxID = strings.ToUpper(f.TxID); return f }(), wantErr: "txid or transaction bytes are inconsistent"},
		{name: "filename mismatch", file: "renamed.json", fixture: valid, wantErr: "txid or transaction bytes are inconsistent"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			writeFixture(t, dir, test.file, test.fixture)
			loaded, err := fixtures.LoadDir(dir)
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Fatalf("LoadDir() = %#v, %v; want error containing %q", loaded, err, test.wantErr)
			}
		})
	}

	t.Run("malformed JSON", func(t *testing.T) {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte(`{"schema_version":`), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := fixtures.LoadDir(dir)
		if err == nil || !strings.Contains(err.Error(), "broken.json:") {
			t.Fatalf("LoadDir() error = %v; want filename-qualified JSON error", err)
		}
	})
}

func TestLoadDirMissingEmptyAndReadFailures(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "absent")
	if loaded, err := fixtures.LoadDir(missing); err == nil || !errors.Is(err, os.ErrNotExist) || loaded != nil {
		t.Fatalf("LoadDir(missing) = %#v, %v; want os.ErrNotExist and nil fixtures", loaded, err)
	}

	empty := t.TempDir()
	if err := os.WriteFile(filepath.Join(empty, "README.md"), []byte("not JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(empty, "nested.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if loaded, err := fixtures.LoadDir(empty); err == nil || !strings.Contains(err.Error(), "no transaction fixtures found") || loaded != nil {
		t.Fatalf("LoadDir(empty) = %#v, %v; want no-fixtures error and nil fixtures", loaded, err)
	}

	unreadable := t.TempDir()
	if err := os.Symlink("absent-target.json", filepath.Join(unreadable, "dangling.json")); err != nil {
		t.Fatal(err)
	}
	if loaded, err := fixtures.LoadDir(unreadable); err == nil || !errors.Is(err, os.ErrNotExist) || loaded != nil {
		t.Fatalf("LoadDir(dangling JSON) = %#v, %v; want read failure", loaded, err)
	}
}

func TestNewSourceAndTransaction(t *testing.T) {
	loaded, err := fixtures.LoadDir(filepath.Join("..", "..", "testdata", "mainnet"))
	if err != nil {
		t.Fatal(err)
	}
	source, err := fixtures.NewSource(loaded)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != len(loaded) {
		t.Fatalf("NewSource() has %d transactions, want %d", len(source), len(loaded))
	}
	for _, fixture := range loaded {
		want, err := core.DecodeTransaction(fixture.Hex)
		if err != nil {
			t.Fatal(err)
		}
		want.BlockHash = fixture.BlockHash
		want.BlockHeight = fixture.BlockHeight
		want.BlockTime = fixture.BlockTime
		got, err := source.Transaction(context.Background(), fixture.TxID)
		if err != nil {
			t.Fatalf("Transaction(%s): %v", fixture.TxID, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Transaction(%s) = %#v, want %#v", fixture.TxID, got, want)
		}
	}

	missingID := strings.Repeat("0", 64)
	got, err := source.Transaction(context.Background(), missingID)
	if err == nil || err.Error() != "fixture transaction "+missingID+" not found" {
		t.Fatalf("missing Transaction() = %#v, %v; want explicit not-found error", got, err)
	}
	if !reflect.DeepEqual(got, model.Transaction{}) {
		t.Fatalf("missing Transaction() returned nonzero transaction: %#v", got)
	}
}

func TestNewSourceRejectsInvalidFixturesAndSupportsEmptyInput(t *testing.T) {
	valid := publicFixture(t, fixtureTransfer)
	other := publicFixture(t, fixtureCoinbase)
	t.Run("invalid bytes", func(t *testing.T) {
		fixture := valid
		fixture.Hex = "zz"
		if source, err := fixtures.NewSource([]fixtures.Fixture{fixture}); err == nil || source != nil {
			t.Fatalf("NewSource(invalid bytes) = %#v, %v; want nil source and error", source, err)
		}
	})
	t.Run("txid mismatch", func(t *testing.T) {
		fixture := valid
		fixture.TxID = other.TxID
		source, err := fixtures.NewSource([]fixtures.Fixture{fixture})
		if err == nil || !strings.Contains(err.Error(), "fixture txid mismatch for "+other.TxID) || source != nil {
			t.Fatalf("NewSource(mismatched txid) = %#v, %v; want mismatch error", source, err)
		}
	})
	t.Run("empty", func(t *testing.T) {
		source, err := fixtures.NewSource(nil)
		if err != nil || source == nil || len(source) != 0 {
			t.Fatalf("NewSource(nil) = %#v, %v; want initialized empty source", source, err)
		}
		if _, err := source.Transaction(context.Background(), fixtureTransfer); err == nil {
			t.Fatal("empty source unexpectedly returned a transaction")
		}
	})
}
