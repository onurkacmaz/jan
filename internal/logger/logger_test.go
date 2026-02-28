package logger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveLogDir(t *testing.T) {
	setJANLogDir(t, "")

	got := ResolveLogDir()
	if got != defaultSystemLogDir {
		t.Fatalf("ResolveLogDir() = %q, want %q", got, defaultSystemLogDir)
	}

	custom := filepath.Join(t.TempDir(), "logs")
	setJANLogDir(t, "  "+custom+"  ")

	got = ResolveLogDir()
	if got != custom {
		t.Fatalf("ResolveLogDir() with env = %q, want %q", got, custom)
	}
}

func TestResolveLogPath(t *testing.T) {
	setJANLogDir(t, "")

	customPath := filepath.Join(t.TempDir(), "custom.log")
	got := ResolveLogPath("demo", "  "+customPath+"  ")
	if got != customPath {
		t.Fatalf("ResolveLogPath() custom = %q, want %q", got, customPath)
	}

	envDir := filepath.Join(t.TempDir(), "env-logs")
	setJANLogDir(t, envDir)
	got = ResolveLogPath("demo", "")
	want := filepath.Join(envDir, "demo.log")
	if got != want {
		t.Fatalf("ResolveLogPath() default = %q, want %q", got, want)
	}
}

func TestCandidateLogPaths(t *testing.T) {
	service := "svc"

	setJANLogDir(t, "")
	got := CandidateLogPaths(service, "")
	if len(got) != 2 {
		t.Fatalf("CandidateLogPaths() len = %d, want 2 (%v)", len(got), got)
	}
	if got[0] != filepath.Join(defaultSystemLogDir, service+".log") {
		t.Fatalf("primary = %q", got[0])
	}
	if got[1] != filepath.Join(fallbackLogDir, service+".log") {
		t.Fatalf("fallback = %q", got[1])
	}

	envDir := filepath.Join(t.TempDir(), "env")
	setJANLogDir(t, envDir)
	got = CandidateLogPaths(service, "")
	if len(got) != 1 {
		t.Fatalf("CandidateLogPaths() with env len = %d, want 1 (%v)", len(got), got)
	}
	if got[0] != filepath.Join(envDir, service+".log") {
		t.Fatalf("env primary = %q", got[0])
	}

	customPath := filepath.Join(t.TempDir(), "custom.log")
	got = CandidateLogPaths(service, "  "+customPath+"  ")
	if len(got) != 1 || got[0] != customPath {
		t.Fatalf("CandidateLogPaths() with custom = %v, want [%q]", got, customPath)
	}
}

func TestOpenServiceLog_CustomPath(t *testing.T) {
	setJANLogDir(t, "")

	path := filepath.Join(t.TempDir(), "nested", "service.log")
	f, err := OpenServiceLog("demo", path)
	if err != nil {
		t.Fatalf("OpenServiceLog() error = %v", err)
	}
	t.Cleanup(func() { _ = CloseServiceLog(f) })

	if _, err := f.WriteString("hello\n"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := CloseServiceLog(f); err != nil {
		t.Fatalf("CloseServiceLog() error = %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}
	if !strings.Contains(string(raw), "hello") {
		t.Fatalf("log content = %q, want to contain %q", string(raw), "hello")
	}
}

func TestOpenServiceLog_UsesEnvDir(t *testing.T) {
	envDir := filepath.Join(t.TempDir(), "logs")
	setJANLogDir(t, envDir)

	service := "env-service"
	f, err := OpenServiceLog(service, "")
	if err != nil {
		t.Fatalf("OpenServiceLog() error = %v", err)
	}
	t.Cleanup(func() { _ = CloseServiceLog(f) })

	if _, err := f.WriteString("line\n"); err != nil {
		t.Fatalf("WriteString() error = %v", err)
	}
	if err := CloseServiceLog(f); err != nil {
		t.Fatalf("CloseServiceLog() error = %v", err)
	}

	path := filepath.Join(envDir, service+".log")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("Stat(%q) error = %v", path, err)
	}
}

func TestCloseServiceLog_Nil(t *testing.T) {
	if err := CloseServiceLog(nil); err != nil {
		t.Fatalf("CloseServiceLog(nil) error = %v", err)
	}
}

func TestShouldFallbackToTmp(t *testing.T) {
	permissionErr := fmt.Errorf("wrap: %w", os.ErrPermission)

	setJANLogDir(t, "")
	primary := filepath.Join(defaultSystemLogDir, "svc.log")
	if !shouldFallbackToTmp(primary, permissionErr) {
		t.Fatalf("shouldFallbackToTmp() = false, want true")
	}

	setJANLogDir(t, filepath.Join(t.TempDir(), "env"))
	if shouldFallbackToTmp(primary, permissionErr) {
		t.Fatalf("shouldFallbackToTmp() with env = true, want false")
	}

	setJANLogDir(t, "")
	otherPrimary := filepath.Join(t.TempDir(), "svc.log")
	if shouldFallbackToTmp(otherPrimary, permissionErr) {
		t.Fatalf("shouldFallbackToTmp() for non-system dir = true, want false")
	}

	if shouldFallbackToTmp(primary, errors.New("some error")) {
		t.Fatalf("shouldFallbackToTmp() for non-permission error = true, want false")
	}
}

func setJANLogDir(t *testing.T, value string) {
	t.Helper()

	prev, had := os.LookupEnv("JAN_LOG_DIR")
	t.Cleanup(func() {
		if had {
			_ = os.Setenv("JAN_LOG_DIR", prev)
			return
		}
		_ = os.Unsetenv("JAN_LOG_DIR")
	})

	if strings.TrimSpace(value) == "" {
		if err := os.Unsetenv("JAN_LOG_DIR"); err != nil {
			t.Fatalf("Unsetenv() error = %v", err)
		}
		return
	}

	if err := os.Setenv("JAN_LOG_DIR", value); err != nil {
		t.Fatalf("Setenv() error = %v", err)
	}
}
