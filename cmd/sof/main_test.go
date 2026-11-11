package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

type alwaysFailWriter struct{ err error }

func (writer alwaysFailWriter) Write([]byte) (int, error) { return 0, writer.err }

func TestMainHelperProcess(t *testing.T) {
	if os.Getenv("TRACEY_SOF_MAIN_HELPER") != "1" {
		return
	}
	os.Args = []string{"sof", "unknown-main-command"}
	main()
}

func TestMainWritesErrorsAndExitsNonzero(t *testing.T) {
	command := exec.Command(os.Args[0], "-test.run=^TestMainHelperProcess$")
	command.Env = append(os.Environ(), "TRACEY_SOF_MAIN_HELPER=1")
	var stdout, stderr bytes.Buffer
	command.Stdout, command.Stderr = &stdout, &stderr
	err := command.Run()
	var exitError *exec.ExitError
	if !errors.As(err, &exitError) || exitError.ExitCode() != 1 {
		t.Fatalf("main subprocess error = %v", err)
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), `error: unknown command "unknown-main-command"`) {
		t.Fatalf("stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunRejectsMissingUnknownCommandsAndFlagErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := run(context.Background(), nil, &stdout, &stderr); err == nil || !strings.Contains(err.Error(), "usage: sof") {
		t.Fatalf("no-command error = %v", err)
	}
	if err := run(context.Background(), []string{"does-not-exist"}, &stdout, &stderr); err == nil || err.Error() != `unknown command "does-not-exist"` {
		t.Fatalf("unknown-command error = %v", err)
	}

	commands := []string{"validate", "node-check", "discover", "trace", "report", "audit", "ui", "export", "example", "test-node", "pdf", "intake", "resolve-intake", "review-intake", "promote-intake"}
	for _, command := range commands {
		t.Run(command, func(t *testing.T) {
			stdout.Reset()
			stderr.Reset()
			err := run(context.Background(), []string{command, "-definitely-unknown"}, &stdout, &stderr)
			if err == nil || !strings.Contains(err.Error(), "flag provided but not defined") {
				t.Fatalf("error = %v, stderr = %q", err, stderr.String())
			}
			if !strings.Contains(stderr.String(), "flag provided but not defined") {
				t.Fatalf("flag package did not write diagnostic to supplied stderr: %q", stderr.String())
			}
		})
	}
}

func TestPrintJSON(t *testing.T) {
	var output bytes.Buffer
	value := struct {
		Name string `json:"name"`
		N    int    `json:"n"`
	}{Name: "fixture", N: 3}
	if err := printJSON(&output, value); err != nil {
		t.Fatal(err)
	}
	if got, want := output.String(), "{\n  \"name\": \"fixture\",\n  \"n\": 3\n}\n"; got != want {
		t.Fatalf("JSON output:\n%s\nwant:\n%s", got, want)
	}
	if err := printJSON(&output, make(chan int)); err == nil {
		t.Fatal("unsupported JSON value was accepted")
	}
	writeErr := errors.New("write failed")
	if err := printJSON(alwaysFailWriter{err: writeErr}, value); !errors.Is(err, writeErr) {
		t.Fatalf("writer error = %v, want %v", err, writeErr)
	}
}

func TestEnvironmentAndTraceLimits(t *testing.T) {
	t.Setenv("TRACEY_TEST_ENVIRONMENT", "configured")
	if got := env("TRACEY_TEST_ENVIRONMENT"); got != "configured" {
		t.Fatalf("env = %q", got)
	}
	t.Setenv("TRACEY_TEST_ENVIRONMENT", "")
	if got := env("TRACEY_TEST_ENVIRONMENT"); got != "" {
		t.Fatalf("empty env = %q", got)
	}

	t.Run("defaults", func(t *testing.T) {
		t.Setenv("TRACE_MAX_DEPTH", "")
		t.Setenv("TRACE_MAX_TRANSACTIONS", "")
		limits, err := traceLimits(t.TempDir())
		if err != nil || limits.MaxDepth != 25 || limits.MaxTransactions != 10000 {
			t.Fatalf("limits = %+v, error = %v", limits, err)
		}
	})

	t.Run("overrides", func(t *testing.T) {
		t.Setenv("TRACE_MAX_DEPTH", "7")
		t.Setenv("TRACE_MAX_TRANSACTIONS", "42")
		limits, err := traceLimits(t.TempDir())
		if err != nil || limits.MaxDepth != 7 || limits.MaxTransactions != 42 {
			t.Fatalf("limits = %+v, error = %v", limits, err)
		}
	})

	for _, test := range []struct {
		name  string
		value string
	}{
		{name: "TRACE_MAX_DEPTH", value: "0"},
		{name: "TRACE_MAX_DEPTH", value: "-2"},
		{name: "TRACE_MAX_DEPTH", value: "not-a-number"},
		{name: "TRACE_MAX_TRANSACTIONS", value: "0"},
		{name: "TRACE_MAX_TRANSACTIONS", value: "-2"},
		{name: "TRACE_MAX_TRANSACTIONS", value: "999999999999999999999999999999"},
	} {
		t.Run(test.name+"="+test.value, func(t *testing.T) {
			t.Setenv("TRACE_MAX_DEPTH", "")
			t.Setenv("TRACE_MAX_TRANSACTIONS", "")
			t.Setenv(test.name, test.value)
			_, err := traceLimits(t.TempDir())
			if err == nil || !strings.Contains(err.Error(), test.name+" must be a positive integer") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}
