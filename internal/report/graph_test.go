package report

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tracey-sof/internal/model"
)

func graphTestReport() model.Report {
	root := model.Outpoint{TxID: strings.Repeat("a", 64), Vout: 0}
	child := model.Outpoint{TxID: strings.Repeat("b", 64), Vout: 1}
	return model.Report{
		UTXOs: []model.Target{
			{TxID: root.TxID, Vout: root.Vout},
			{TxID: "short", Vout: 3},
		},
		Graph: model.Graph{
			Nodes: []model.GraphNode{
				{ID: "out:" + root.String(), Kind: "output", Outpoint: root.String(), Owned: true},
				{ID: "tx:" + root.TxID, Kind: "transaction", TxID: root.TxID},
				{ID: "out:" + child.String(), Kind: "output", Outpoint: child.String()},
			},
			Edges: []model.GraphEdge{
				{From: "out:" + root.String(), To: "tx:" + root.TxID, Kind: "funded_by"},
				{From: "tx:" + root.TxID, To: "out:" + child.String(), Kind: "input"},
			},
		},
	}
}

func TestSelectGraphFocusesOneTarget(t *testing.T) {
	graph := graphTestReport().Graph
	focused := SelectGraph(graph, []string{"out:" + graph.Nodes[0].Outpoint})
	if len(focused.Nodes) != 3 || len(focused.Edges) != 2 {
		t.Fatalf("focused graph = %d nodes, %d edges", len(focused.Nodes), len(focused.Edges))
	}
	if empty := SelectGraph(graph, nil); len(empty.Nodes) != 0 || len(empty.Edges) != 0 {
		t.Fatalf("unknown root returned %d nodes", len(empty.Nodes))
	}
	if unknown := SelectGraph(graph, []string{"out:missing:0"}); len(unknown.Nodes) != 0 {
		t.Fatalf("missing root returned %d nodes", len(unknown.Nodes))
	}
}

func TestWriteGraphsCreatesAllAndFocusedFiles(t *testing.T) {
	dir := t.TempDir()
	written, err := WriteGraphs(graphTestReport(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(written) != 6 {
		t.Fatalf("wrote %d files, want 6", len(written))
	}
	for _, path := range written {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %04o", filepath.Base(path), info.Mode().Perm())
		}
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), "<svg") {
			t.Errorf("%s is not an SVG", filepath.Base(path))
		}
	}
	for _, name := range []string{"graph-all.svg", "graph-height-all.svg", "graph-short-3.svg", "graph-height-short-3.svg"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestWriteGraphsErrorPaths(t *testing.T) {
	failing := func(model.Report) ([]byte, error) { return nil, errors.New("render failed") }
	if _, err := writeGraphs(graphTestReport(), t.TempDir(), failing, failing); err == nil {
		t.Fatal("expected render error")
	}
	ok := func(model.Report) ([]byte, error) { return []byte("<svg/>"), nil }
	if _, err := writeGraphs(graphTestReport(), t.TempDir(), ok, failing); err == nil {
		t.Fatal("expected height render error")
	}
	calls := 0
	failsSecond := func(model.Report) ([]byte, error) {
		calls++
		if calls > 1 {
			return nil, errors.New("focused render failed")
		}
		return []byte("<svg/>"), nil
	}
	if _, err := writeGraphs(graphTestReport(), t.TempDir(), failsSecond, ok); err == nil {
		t.Fatal("expected focused render error")
	}
	calls = 0
	failsSecondTime := func(model.Report) ([]byte, error) {
		calls++
		if calls > 1 {
			return nil, errors.New("focused height render failed")
		}
		return []byte("<svg/>"), nil
	}
	if _, err := writeGraphs(graphTestReport(), t.TempDir(), ok, failsSecondTime); err == nil {
		t.Fatal("expected focused height render error")
	}
	parent := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(parent, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteGraphs(graphTestReport(), parent); err == nil {
		t.Fatal("expected atomic write error")
	}
	blocked := t.TempDir()
	if err := os.Mkdir(filepath.Join(blocked, "graph-short-3.svg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteGraphs(graphTestReport(), blocked); err == nil {
		t.Fatal("expected focused atomic write error")
	}
	blockedTime := t.TempDir()
	if err := os.Mkdir(filepath.Join(blockedTime, "graph-height-short-3.svg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteGraphs(graphTestReport(), blockedTime); err == nil {
		t.Fatal("expected focused height atomic write error")
	}
	blockedAllTime := t.TempDir()
	if err := os.Mkdir(filepath.Join(blockedAllTime, "graph-height-all.svg"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteGraphs(graphTestReport(), blockedAllTime); err == nil {
		t.Fatal("expected height-all atomic write error")
	}
}
