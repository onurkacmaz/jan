package main

import (
	"io"
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

func TestHelpCommand(t *testing.T) {
	out := captureStdout(t, func() {
		if err := run([]string{"help"}); err != nil {
			t.Fatalf("run(help) error = %v", err)
		}
	})

	if !strings.Contains(out, "Usage:") {
		t.Fatalf("help output missing usage: %q", out)
	}
	if !strings.Contains(out, "version") {
		t.Fatalf("help output missing version command: %q", out)
	}
	if !strings.Contains(out, "tail") {
		t.Fatalf("help output missing tail command: %q", out)
	}
	if !strings.Contains(out, "restart") {
		t.Fatalf("help output missing restart command: %q", out)
	}
}

func TestVersionCommand(t *testing.T) {
	prev := version
	version = "test-version"
	t.Cleanup(func() { version = prev })

	out := captureStdout(t, func() {
		if err := run([]string{"version"}); err != nil {
			t.Fatalf("run(version) error = %v", err)
		}
	})

	if !strings.Contains(out, "jan version test-version") {
		t.Fatalf("unexpected version output: %q", out)
	}
}

func TestHelpAndVersionShorthands(t *testing.T) {
	helpOut := captureStdout(t, func() {
		if err := run([]string{"-h"}); err != nil {
			t.Fatalf("run(-h) error = %v", err)
		}
	})
	if !strings.Contains(helpOut, "Commands:") {
		t.Fatalf("unexpected -h output: %q", helpOut)
	}

	prev := version
	version = "test-short"
	t.Cleanup(func() { version = prev })

	versionOut := captureStdout(t, func() {
		if err := run([]string{"-v"}); err != nil {
			t.Fatalf("run(-v) error = %v", err)
		}
	})
	if !strings.Contains(versionOut, "jan version test-short") {
		t.Fatalf("unexpected -v output: %q", versionOut)
	}
}

func TestLogCommandShowsDefaultLogDirs(t *testing.T) {
	prev := os.Getenv("JAN_LOG_DIR")
	if err := os.Unsetenv("JAN_LOG_DIR"); err != nil {
		t.Fatalf("Unsetenv() error = %v", err)
	}
	t.Cleanup(func() {
		if prev == "" {
			_ = os.Unsetenv("JAN_LOG_DIR")
			return
		}
		_ = os.Setenv("JAN_LOG_DIR", prev)
	})

	out := captureStdout(t, func() {
		if err := run([]string{"log"}); err != nil {
			t.Fatalf("run(log) error = %v", err)
		}
	})

	if !strings.Contains(out, "log_dir=/var/log/jan") {
		t.Fatalf("unexpected log output: %q", out)
	}
	if !strings.Contains(out, "fallback_log_dir=/tmp/jan/logs") {
		t.Fatalf("unexpected fallback log output: %q", out)
	}
}

func TestTailCommandByService(t *testing.T) {
	service := "tail-test-service"
	path := filepath.Join("/tmp/jan/logs", service+".log")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, []byte("a\nb\nc\n"), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(path) })

	out := captureStdout(t, func() {
		if err := run([]string{"tail", "-s", service, "-n", "2"}); err != nil {
			t.Fatalf("run(tail) error = %v", err)
		}
	})

	if !strings.Contains(out, "b\nc\n") {
		t.Fatalf("unexpected tail output: %q", out)
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()

	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}

	os.Stdout = w
	defer func() {
		os.Stdout = orig
	}()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close writer error = %v", err)
	}

	raw, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("close reader error = %v", err)
	}

	return string(raw)
}
