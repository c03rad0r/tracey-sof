package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const coverageGateHelperEnvironment = "TRACEY_SOF_COVERAGEGATE_HELPER"

func TestCoverageGateSubprocess(t *testing.T) {
	if os.Getenv(coverageGateHelperEnvironment) != "1" {
		return
	}
	separator := -1
	for i, argument := range os.Args {
		if argument == "--" {
			separator = i
			break
		}
	}
	if separator < 0 {
		os.Exit(99)
	}
	os.Args = append([]string{"coveragegate"}, os.Args[separator+1:]...)
	main()
	os.Exit(0)
}

func executeCoverageGate(t *testing.T, arguments ...string) (int, string, string) {
	t.Helper()
	commandArguments := append([]string{"-test.run=^TestCoverageGateSubprocess$", "--"}, arguments...)
	command := exec.Command(os.Args[0], commandArguments...)
	command.Env = append(os.Environ(), coverageGateHelperEnvironment+"=1")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	if err == nil {
		return 0, stdout.String(), stderr.String()
	}
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) {
		t.Fatalf("start coveragegate subprocess: %v", err)
	}
	return exitError.ExitCode(), stdout.String(), stderr.String()
}

func writeCoverageProfile(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "coverage.out")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestCoverageGateValidProfiles(t *testing.T) {
	tests := []struct {
		name       string
		profile    string
		minimum    string
		wantOutput string
	}{
		{
			name:       "statement weighted exact threshold",
			profile:    "mode: atomic\ntracey-sof/a.go:1.1,2.1 8 2\ntracey-sof/b.go:3.1,4.1 2 0\n",
			minimum:    "80",
			wantOutput: "First-party statement coverage: 80.00% (8/10 statements)\n",
		},
		{
			name:       "multiple hits and no mode header",
			profile:    "tracey-sof/a.go:1.1,2.1 1 9\ntracey-sof/b.go:3.1,4.1 3 1\n",
			minimum:    "100",
			wantOutput: "First-party statement coverage: 100.00% (4/4 statements)\n",
		},
		{
			name:       "zero threshold accepts uncovered statements",
			profile:    "mode: set\ntracey-sof/a.go:1.1,2.1 5 0\n",
			minimum:    "0",
			wantOutput: "First-party statement coverage: 0.00% (0/5 statements)\n",
		},
		{
			name:       "compiler zero-statement rows are ignored",
			profile:    "mode: set\ntracey-sof/a.go:1.1,2.1 0 6\ntracey-sof/a.go:2.1,3.1 1 1\n",
			minimum:    "100",
			wantOutput: "First-party statement coverage: 100.00% (1/1 statements)\n",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := writeCoverageProfile(t, test.profile)
			exitCode, stdout, stderr := executeCoverageGate(t, "-profile", profile, "-minimum", test.minimum)
			if exitCode != 0 || stdout != test.wantOutput || stderr != "" {
				t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
			}
		})
	}
}

func TestCoverageGateRejectsBadInput(t *testing.T) {
	tests := []struct {
		name       string
		profile    string
		wantStderr string
	}{
		{name: "no statements", profile: "mode: atomic\n", wantStderr: "coverage profile contains no statements"},
		{name: "blank profile", profile: "", wantStderr: "coverage profile contains no statements"},
		{name: "malformed line", profile: "mode: set\nnot a coverage record with too many fields\n", wantStderr: "invalid coverage line 2"},
		{name: "statement count is not numeric", profile: "mode: set\na.go:1.1,2.1 many 1\n", wantStderr: "invalid coverage data on line 2"},
		{name: "hit count is not numeric", profile: "mode: set\na.go:1.1,2.1 1 often\n", wantStderr: "invalid coverage data on line 2"},
		{name: "negative statements", profile: "mode: set\na.go:1.1,2.1 -1 1\n", wantStderr: "invalid coverage data on line 2"},
		{name: "negative hit count", profile: "mode: set\na.go:1.1,2.1 1 -1\n", wantStderr: "invalid coverage data on line 2"},
		{name: "scanner error", profile: "mode: set\n" + strings.Repeat("x", 70*1024) + "\n", wantStderr: "token too long"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			profile := writeCoverageProfile(t, test.profile)
			exitCode, _, stderr := executeCoverageGate(t, "-profile", profile)
			if exitCode != 1 || !strings.Contains(stderr, test.wantStderr) {
				t.Fatalf("exit=%d stderr=%q, want substring %q", exitCode, stderr, test.wantStderr)
			}
		})
	}
}

func TestCoverageGateThresholdAndFileErrors(t *testing.T) {
	t.Run("below threshold", func(t *testing.T) {
		profile := writeCoverageProfile(t, "mode: set\na.go:1.1,2.1 1 1\nb.go:1.1,2.1 1 0\n")
		exitCode, stdout, stderr := executeCoverageGate(t, "-profile", profile, "-minimum", "50.01")
		if exitCode != 1 || !strings.Contains(stdout, "50.00% (1/2 statements)") || !strings.Contains(stderr, "statement coverage 50.00% is below required 50.01%") {
			t.Fatalf("exit=%d stdout=%q stderr=%q", exitCode, stdout, stderr)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "missing.cover")
		exitCode, _, stderr := executeCoverageGate(t, "-profile", missing)
		if exitCode != 1 || !strings.Contains(stderr, missing) {
			t.Fatalf("exit=%d stderr=%q", exitCode, stderr)
		}
	})

	t.Run("read error", func(t *testing.T) {
		exitCode, _, stderr := executeCoverageGate(t, "-profile", t.TempDir())
		if exitCode != 1 || !strings.Contains(stderr, "is a directory") {
			t.Fatalf("exit=%d stderr=%q", exitCode, stderr)
		}
	})

	for _, test := range []struct {
		name       string
		arguments  []string
		wantCode   int
		wantStderr string
	}{
		{name: "required profile", wantCode: 2, wantStderr: "-profile is required"},
		{name: "unknown flag", arguments: []string{"-unknown"}, wantCode: 2, wantStderr: "flag provided but not defined"},
		{name: "invalid minimum", arguments: []string{"-minimum", "not-a-number"}, wantCode: 2, wantStderr: "invalid value"},
	} {
		t.Run(test.name, func(t *testing.T) {
			exitCode, _, stderr := executeCoverageGate(t, test.arguments...)
			if exitCode != test.wantCode || !strings.Contains(stderr, test.wantStderr) {
				t.Fatalf("exit=%d stderr=%q", exitCode, stderr)
			}
		})
	}
}

func TestCoverageGateHelp(t *testing.T) {
	exitCode, _, stderr := executeCoverageGate(t, "-h")
	if exitCode != 0 || !strings.Contains(stderr, "Usage of coveragegate") {
		t.Fatalf("exit=%d stderr=%q", exitCode, stderr)
	}
}
