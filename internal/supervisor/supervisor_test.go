package supervisor

import (
	"context"
	"errors"
	"jan/internal/config"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestRunSetsPIDWhileRunning(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command test is unix-specific")
	}
	cleanupServiceFiles(t, "pid-test")
	defer cleanupServiceFiles(t, "pid-test")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cfg := &config.Config{
		Name:          "pid-test",
		Command:       "/bin/sh",
		Args:          []string{"-c", "sleep 2"},
		RestartPolicy: config.RestartNever,
	}

	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()

	state, ok := waitForState(2*time.Second, func(s State) bool {
		return s.Running && s.Pid > 0
	})
	if !ok {
		t.Fatalf("state was never running with pid > 0; got %+v", CurrentState())
	}

	if !state.Running || state.Pid <= 0 {
		t.Fatalf("unexpected running state: %+v", state)
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() returned error after cancel: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Run() did not return after context cancel")
	}
}

func TestRunCapturesExitState(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command test is unix-specific")
	}
	cleanupServiceFiles(t, "exit-test")
	defer cleanupServiceFiles(t, "exit-test")

	cfg := &config.Config{
		Name:          "exit-test",
		Command:       "/bin/sh",
		Args:          []string{"-c", "exit 7"},
		RestartPolicy: config.RestartNever,
	}

	err := Run(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected Run() to return error for non-zero exit")
	}

	if !strings.Contains(err.Error(), "exit status") {
		t.Fatalf("unexpected error: %v", err)
	}

	state := CurrentState()
	if state.Running {
		t.Fatalf("state should not be running after exit: %+v", state)
	}
	if state.ExitCode == nil {
		t.Fatalf("state exit code is nil: %+v", state)
	}
	if *state.ExitCode != 7 {
		t.Fatalf("exit code = %d, want 7", *state.ExitCode)
	}
	if state.LastError == "" {
		t.Fatalf("last error should not be empty after non-zero exit: %+v", state)
	}
}

func TestRunRestartPolicyAlways(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command test is unix-specific")
	}
	cleanupServiceFiles(t, "always-policy")
	defer cleanupServiceFiles(t, "always-policy")

	marker := filepath.Join(t.TempDir(), "always.marker")
	script := "/bin/sh"
	args := []string{"-c", "echo run >> " + marker}
	cfg := &config.Config{
		Name:          "always-policy",
		Command:       script,
		Args:          args,
		RestartPolicy: config.RestartAlways,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	if err := Run(ctx, cfg); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	runs := countLines(t, marker)
	if runs < 2 {
		t.Fatalf("always policy should restart at least once, got %d run(s)", runs)
	}
}

func TestRunRestartPolicyOnFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command test is unix-specific")
	}

	t.Run("does not restart on success", func(t *testing.T) {
		cleanupServiceFiles(t, "onfailure-success")
		defer cleanupServiceFiles(t, "onfailure-success")
		marker := filepath.Join(t.TempDir(), "onfailure-success.marker")
		cfg := &config.Config{
			Name:          "onfailure-success",
			Command:       "/bin/sh",
			Args:          []string{"-c", "echo run >> " + marker + "; exit 0"},
			RestartPolicy: config.RestartOnFailure,
		}

		if err := Run(context.Background(), cfg); err != nil {
			t.Fatalf("Run() error = %v", err)
		}

		runs := countLines(t, marker)
		if runs != 1 {
			t.Fatalf("on-failure with exit 0 should run once, got %d", runs)
		}
	})

	t.Run("restarts on failure", func(t *testing.T) {
		cleanupServiceFiles(t, "onfailure-fail")
		defer cleanupServiceFiles(t, "onfailure-fail")
		marker := filepath.Join(t.TempDir(), "onfailure-fail.marker")
		cfg := &config.Config{
			Name:          "onfailure-fail",
			Command:       "/bin/sh",
			Args:          []string{"-c", "echo run >> " + marker + "; exit 1"},
			RestartPolicy: config.RestartOnFailure,
		}

		ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
		defer cancel()

		if err := Run(ctx, cfg); err != nil {
			t.Fatalf("Run() error = %v", err)
		}

		runs := countLines(t, marker)
		if runs < 2 {
			t.Fatalf("on-failure with non-zero exit should restart, got %d run(s)", runs)
		}
	})
}

func TestRunRestartPolicyNever(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command test is unix-specific")
	}
	cleanupServiceFiles(t, "never-policy")
	defer cleanupServiceFiles(t, "never-policy")

	marker := filepath.Join(t.TempDir(), "never.marker")
	cfg := &config.Config{
		Name:          "never-policy",
		Command:       "/bin/sh",
		Args:          []string{"-c", "echo run >> " + marker + "; exit 1"},
		RestartPolicy: config.RestartNever,
	}

	err := Run(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected Run() to return error for non-zero exit with never policy")
	}

	runs := countLines(t, marker)
	if runs != 1 {
		t.Fatalf("never policy should not restart, got %d run(s)", runs)
	}
}

func TestRunAppliesBackoffBetweenRestarts(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command test is unix-specific")
	}
	cleanupServiceFiles(t, "backoff-policy")
	defer cleanupServiceFiles(t, "backoff-policy")

	cfg := &config.Config{
		Name:          "backoff-policy",
		Command:       "/bin/sh",
		Args:          []string{"-c", "exit 1"},
		RestartPolicy: config.RestartAlways,
		MaxRetries:    1,
		BackoffMS:     120,
	}

	start := time.Now()
	err := Run(context.Background(), cfg)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected max retries error")
	}
	if !strings.Contains(err.Error(), "max retries reached") {
		t.Fatalf("unexpected error: %v", err)
	}
	if elapsed < 100*time.Millisecond {
		t.Fatalf("backoff was not applied, elapsed=%v", elapsed)
	}
}

func TestRunTracksRetryCount(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command test is unix-specific")
	}
	cleanupServiceFiles(t, "retry-count-policy")
	defer cleanupServiceFiles(t, "retry-count-policy")

	cfg := &config.Config{
		Name:          "retry-count-policy",
		Command:       "/bin/sh",
		Args:          []string{"-c", "exit 1"},
		RestartPolicy: config.RestartAlways,
		MaxRetries:    2,
	}

	err := Run(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected max retries error")
	}

	state := CurrentState()
	if state.RetryCount != 2 {
		t.Fatalf("retry count = %d, want 2", state.RetryCount)
	}
}

func TestRunStopsAfterMaxRetries(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command test is unix-specific")
	}
	cleanupServiceFiles(t, "max-retries-policy")
	defer cleanupServiceFiles(t, "max-retries-policy")

	marker := filepath.Join(t.TempDir(), "max-retries.marker")
	cfg := &config.Config{
		Name:          "max-retries-policy",
		Command:       "/bin/sh",
		Args:          []string{"-c", "echo run >> " + marker + "; exit 1"},
		RestartPolicy: config.RestartAlways,
		MaxRetries:    2,
	}

	err := Run(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected max retries error")
	}

	runs := countLines(t, marker)
	if runs != 3 {
		t.Fatalf("expected initial run + 2 retries = 3 runs, got %d", runs)
	}
}

func TestRunGracefulShutdownSendsSIGTERM(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signal test is unix-specific")
	}
	cleanupServiceFiles(t, "graceful-shutdown")
	defer cleanupServiceFiles(t, "graceful-shutdown")

	marker := filepath.Join(t.TempDir(), "graceful.marker")
	cfg := &config.Config{
		Name:    "graceful-shutdown",
		Command: "/bin/sh",
		Args: []string{
			"-c",
			"trap 'echo term >> " + marker + "; exit 0' TERM; while true; do sleep 0.1; done",
		},
		RestartPolicy: config.RestartNever,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()

	_, ok := waitForState(2*time.Second, func(s State) bool { return s.Running && s.Pid > 0 })
	if !ok {
		t.Fatal("process did not start in time")
	}

	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run() should return nil on graceful shutdown, got %v", err)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("Run() did not return after cancel")
	}

	raw, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", marker, err)
	}

	if !strings.Contains(string(raw), "term") {
		t.Fatalf("expected child to handle TERM signal, content=%q", string(raw))
	}
}

func TestRunShutdownIsRaceSafe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command test is unix-specific")
	}
	cleanupServiceFiles(t, "race-safe-shutdown")
	defer cleanupServiceFiles(t, "race-safe-shutdown")

	cfg := &config.Config{
		Name:          "race-safe-shutdown",
		Command:       "/bin/sh",
		Args:          []string{"-c", "while true; do sleep 0.1; done"},
		RestartPolicy: config.RestartNever,
	}

	for i := 0; i < 5; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		done := make(chan error, 1)
		go func() { done <- Run(ctx, cfg) }()

		_, ok := waitForState(2*time.Second, func(s State) bool { return s.Running && s.Pid > 0 })
		if !ok {
			t.Fatalf("iteration %d: process did not start in time", i)
		}

		cancel()

		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("iteration %d: Run() error = %v", i, err)
			}
		case <-time.After(4 * time.Second):
			t.Fatalf("iteration %d: Run() did not return after cancel", i)
		}
	}
}

func TestRunPreventsSecondInstanceForSameService(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pid lock test is unix-specific")
	}
	cleanupServiceFiles(t, "pid-lock-same-service")
	defer cleanupServiceFiles(t, "pid-lock-same-service")

	cfg := &config.Config{
		Name:          "pid-lock-same-service",
		Command:       "/bin/sh",
		Args:          []string{"-c", "while true; do sleep 0.1; done"},
		RestartPolicy: config.RestartNever,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() { done <- Run(ctx, cfg) }()

	_, ok := waitForState(2*time.Second, func(s State) bool { return s.Running && s.Pid > 0 })
	if !ok {
		t.Fatal("first instance did not start in time")
	}

	path := pidFilePath(cfg.Name)
	if !waitForCondition(2*time.Second, func() bool {
		_, statErr := os.Stat(path)
		return statErr == nil
	}) {
		t.Fatalf("pid file %q was not created", path)
	}

	err := Run(context.Background(), cfg)
	if err == nil {
		t.Fatal("expected second instance to fail with pid lock")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("unexpected error for second instance: %v", err)
	}

	cancel()
	select {
	case runErr := <-done:
		if runErr != nil {
			t.Fatalf("first instance returned error on shutdown: %v", runErr)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("first instance did not stop after cancel")
	}
}

func TestRunRemovesPIDFileOnExit(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("pid lock test is unix-specific")
	}
	cleanupServiceFiles(t, "pid-lock-cleanup")
	defer cleanupServiceFiles(t, "pid-lock-cleanup")

	cfg := &config.Config{
		Name:          "pid-lock-cleanup",
		Command:       "/bin/sh",
		Args:          []string{"-c", "exit 0"},
		RestartPolicy: config.RestartNever,
	}

	path := pidFilePath(cfg.Name)
	_ = os.Remove(path)

	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("pid file should be removed after exit, stat err = %v", err)
	}
}

func TestRunWritesLogsToCustomPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell command test is unix-specific")
	}
	cleanupServiceFiles(t, "custom-log-path")
	defer cleanupServiceFiles(t, "custom-log-path")

	logPath := filepath.Join(t.TempDir(), "nested", "custom.log")
	cfg := &config.Config{
		Name:          "custom-log-path",
		Command:       "/bin/sh",
		Args:          []string{"-c", "echo custom-log-line"},
		RestartPolicy: config.RestartNever,
		LogPath:       logPath,
	}

	if err := Run(context.Background(), cfg); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	raw, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", logPath, err)
	}
	if !strings.Contains(string(raw), "custom-log-line") {
		t.Fatalf("custom log content missing, got %q", string(raw))
	}
}

func waitForState(timeout time.Duration, predicate func(State) bool) (State, bool) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s := CurrentState()
		if predicate(s) {
			return s, true
		}
		time.Sleep(20 * time.Millisecond)
	}

	return CurrentState(), false
}

func countLines(t *testing.T, path string) int {
	t.Helper()

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%q) error = %v", path, err)
	}

	content := strings.TrimSpace(string(raw))
	if content == "" {
		return 0
	}

	return len(strings.Split(content, "\n"))
}

func waitForCondition(timeout time.Duration, check func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}

	return check()
}
