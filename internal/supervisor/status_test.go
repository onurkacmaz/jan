package supervisor

import (
	"context"
	"errors"
	"jan/internal/config"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestServiceStatusShowsRunningPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("status test is unix-specific")
	}

	service := "status-running"
	cleanupServiceFiles(t, service)
	defer cleanupServiceFiles(t, service)

	cfg := &config.Config{
		Name:          service,
		Command:       "/bin/sh",
		Args:          []string{"-c", "while true; do sleep 0.1; done"},
		RestartPolicy: config.RestartNever,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()

	ok := waitForCondition(2*time.Second, func() bool {
		s, err := ServiceStatus(service)
		return err == nil && s.Running && s.PID > 0
	})
	if !ok {
		st, _ := ServiceStatus(service)
		t.Fatalf("status did not report running pid, got %+v", st)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() returned error after cancel: %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("Run() did not stop after cancel")
	}
}

func TestServiceStatusShowsStoppedLastExitCode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("status test is unix-specific")
	}

	service := "status-stopped"
	cleanupServiceFiles(t, service)
	defer cleanupServiceFiles(t, service)

	cfg := &config.Config{
		Name:          service,
		Command:       "/bin/sh",
		Args:          []string{"-c", "exit 7"},
		RestartPolicy: config.RestartNever,
	}

	err := Run(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected Run() to return non-zero exit error")
	}

	st, err := ServiceStatus(service)
	if err != nil {
		t.Fatalf("ServiceStatus() error = %v", err)
	}
	if st.Running {
		t.Fatalf("expected stopped status, got running: %+v", st)
	}
	if st.LastExitCode == nil {
		t.Fatalf("expected last exit code, got nil: %+v", st)
	}
	if *st.LastExitCode != 7 {
		t.Fatalf("last exit code = %d, want 7", *st.LastExitCode)
	}
}

func TestServiceStatusStoppedWithoutLastExitCode(t *testing.T) {
	service := "status-empty"
	cleanupServiceFiles(t, service)
	defer cleanupServiceFiles(t, service)

	st, err := ServiceStatus(service)
	if err != nil {
		t.Fatalf("ServiceStatus() error = %v", err)
	}
	if st.Running {
		t.Fatalf("expected stopped status, got %+v", st)
	}
	if st.LastExitCode != nil {
		t.Fatalf("expected nil last exit code, got %+v", st)
	}
}

func cleanupServiceFiles(t *testing.T, service string) {
	t.Helper()

	pidPath := pidFilePath(service)
	raw, readErr := os.ReadFile(pidPath)
	if readErr == nil {
		pidText := strings.TrimSpace(string(raw))
		pid, convErr := strconv.Atoi(pidText)
		if convErr == nil && pid > 0 {
			_ = syscall.Kill(pid, syscall.SIGKILL)
		}
	}

	for _, path := range []string{pidPath, statusFilePath(service)} {
		err := os.Remove(path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("remove %q: %v", path, err)
		}
	}
}
