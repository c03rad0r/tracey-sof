//go:build linux

package report

import (
	"os/signal"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCoverageAtomicWriteReportsFileSizeLimit(t *testing.T) {
	dir := t.TempDir()
	var original syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_FSIZE, &original); err != nil {
		t.Fatal(err)
	}
	limited := original
	limited.Cur = 0
	signal.Ignore(syscall.SIGXFSZ)
	if err := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &limited); err != nil {
		signal.Reset(syscall.SIGXFSZ)
		t.Fatal(err)
	}
	defer func() {
		_ = syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original)
		signal.Reset(syscall.SIGXFSZ)
	}()
	err := AtomicWrite(filepath.Join(dir, "report.json"), []byte("write beyond the file-size limit"), 0o600)
	restoreErr := syscall.Setrlimit(syscall.RLIMIT_FSIZE, &original)
	signal.Reset(syscall.SIGXFSZ)
	if restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if err == nil {
		t.Fatal("AtomicWrite ignored a file-size-limit write failure")
	}
	matches, globErr := filepath.Glob(filepath.Join(dir, ".report-*.tmp"))
	if globErr != nil {
		t.Fatal(globErr)
	}
	if len(matches) != 0 {
		t.Errorf("failed write left temporary files: %v", matches)
	}
}
