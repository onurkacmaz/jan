package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveExitCodeUsage(t *testing.T) {
	err := run([]string{"unknown"})
	if code := resolveExitCode(err); code != exitCodeUsage {
		t.Fatalf("exit code = %d, want %d (err=%v)", code, exitCodeUsage, err)
	}
}

func TestResolveExitCodeConfig(t *testing.T) {
	err := run([]string{"run", "-c", "/tmp/not-found-config.yaml"})
	if code := resolveExitCode(err); code != exitCodeConfig {
		t.Fatalf("exit code = %d, want %d (err=%v)", code, exitCodeConfig, err)
	}
	if !strings.Contains(err.Error(), "config error") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestResolveExitCodeRuntime(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command test is unix-specific")
	}

	path := filepath.Join(t.TempDir(), "runtime.yaml")
	raw := []byte(`
name: runtime-test
command: /bin/sh
args: ["-c", "exit 1"]
restart_policy: never
`)

	if writeErr := os.WriteFile(path, raw, 0o644); writeErr != nil {
		t.Fatalf("WriteFile() error = %v", writeErr)
	}

	err := run([]string{"run", "-c", path})
	if err == nil {
		t.Fatal("expected run() error")
	}
	if code := resolveExitCode(err); code != exitCodeRuntime {
		t.Fatalf("exit code = %d, want %d (err=%v)", code, exitCodeRuntime, err)
	}
}

func TestResolveExitCodeSignal(t *testing.T) {
	err := fail(exitCodeSignal, "received shutdown signal")
	if code := resolveExitCode(err); code != exitCodeSignal {
		t.Fatalf("exit code = %d, want %d", code, exitCodeSignal)
	}
}
