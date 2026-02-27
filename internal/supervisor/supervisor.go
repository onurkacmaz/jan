package supervisor

import (
	"context"
	"errors"
	"fmt"
	"jan/internal/config"
	"jan/internal/logger"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode"
)

const shutdownGracePeriod = 2 * time.Second

type State struct {
	Service    string
	Pid        int
	Running    bool
	ExitCode   *int
	LastError  string
	RetryCount int
}

var (
	stateMu sync.Mutex
	state   State
)

func CurrentState() State {
	stateMu.Lock()
	defer stateMu.Unlock()

	s := state
	if state.ExitCode != nil {
		code := *state.ExitCode
		s.ExitCode = &code
	}

	return s
}

func Run(ctx context.Context, cfg *config.Config) error {
	releaseLock, err := acquirePIDLock(cfg.Name)
	if err != nil {
		return err
	}
	defer releaseLock()

	logFile, err := logger.OpenServiceLog(cfg.Name)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	defer logFile.Close()

	retryCount := 0
	setRetryCount(retryCount)

	for {
		if ctx.Err() != nil {
			return nil
		}

		code, runErr, canceled, err := runOnce(ctx, cfg, logFile, retryCount)
		if err != nil {
			return err
		}

		if canceled {
			return nil
		}

		if shouldRestart(cfg.RestartPolicy, code) {
			if !canRetry(cfg.MaxRetries, retryCount) {
				if runErr != nil {
					return fmt.Errorf(
						"max retries reached (%d), last exit code: %d: %w",
						cfg.MaxRetries,
						code,
						runErr,
					)
				}
				return fmt.Errorf("max retries reached (%d), last exit code: %d", cfg.MaxRetries, code)
			}

			retryCount++
			setRetryCount(retryCount)

			if waitBackoff(ctx, cfg.BackoffMS) {
				return nil
			}

			continue
		}

		return runErr
	}
}

func runOnce(
	ctx context.Context,
	cfg *config.Config,
	logFile *os.File,
	retryCount int,
) (int, error, bool, error) {
	cmd := exec.Command(cfg.Command, cfg.Args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if cfg.Workdir != "" {
		cmd.Dir = cfg.Workdir
	}

	cmd.Env = mergedEnv(cfg.Env)

	if err := cmd.Start(); err != nil {
		return -1, nil, false, err
	}

	setRunningState(cfg.Name, cmd.Process.Pid, retryCount)

	waitCh := make(chan error, 1)
	go func() { waitCh <- cmd.Wait() }()

	select {
	case err := <-waitCh:
		updateExitState(cfg.Name, cmd.ProcessState, err)
		return exitCode(cmd.ProcessState), err, false, nil
	case <-ctx.Done():
		requestGracefulStop(cmd.Process)
		err := waitWithTimeout(waitCh, shutdownGracePeriod, cmd.Process)
		updateExitState(cfg.Name, cmd.ProcessState, err)
		return exitCode(cmd.ProcessState), err, true, nil
	}
}

func requestGracefulStop(process *os.Process) {
	if process == nil {
		return
	}
	_ = process.Signal(syscall.SIGTERM)
}

func waitWithTimeout(waitCh <-chan error, timeout time.Duration, process *os.Process) error {
	if timeout <= 0 {
		return <-waitCh
	}

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case err := <-waitCh:
		return err
	case <-timer.C:
		if process != nil {
			_ = process.Kill()
		}
		return <-waitCh
	}
}

func shouldRestart(policy string, code int) bool {
	switch policy {
	case config.RestartAlways:
		return true
	case config.RestartOnFailure:
		return code != 0
	case config.RestartNever:
		return false
	default:
		return false
	}
}

func canRetry(maxRetries int, retryCount int) bool {
	if maxRetries == 0 {
		return true
	}

	return retryCount < maxRetries
}

func exitCode(ps *os.ProcessState) int {
	if ps == nil {
		return -1
	}
	return ps.ExitCode()
}

func waitBackoff(ctx context.Context, backoffMS int) bool {
	if backoffMS <= 0 {
		return false
	}

	timer := time.NewTimer(time.Duration(backoffMS) * time.Millisecond)
	defer timer.Stop()

	select {
	case <-timer.C:
		return false
	case <-ctx.Done():
		return true
	}
}

func mergedEnv(extra map[string]string) []string {
	base := os.Environ()
	if len(extra) == 0 {
		return base
	}

	merged := make([]string, 0, len(base)+len(extra))
	seen := make(map[string]struct{}, len(base))
	for _, item := range base {
		k, _, ok := strings.Cut(item, "=")
		if ok {
			seen[k] = struct{}{}
		}
		merged = append(merged, item)
	}

	for k, v := range extra {
		if _, exists := seen[k]; exists {
			replaced := false
			for i, item := range merged {
				key, _, ok := strings.Cut(item, "=")
				if ok && key == k {
					merged[i] = fmt.Sprintf("%s=%s", k, v)
					replaced = true
					break
				}
			}
			if replaced {
				continue
			}
		}
		merged = append(merged, fmt.Sprintf("%s=%s", k, v))
	}

	return merged
}

func setRunningState(service string, pid int, retryCount int) {
	stateMu.Lock()
	defer stateMu.Unlock()

	state = State{
		Service:    sanitizeServiceName(service),
		Pid:        pid,
		Running:    true,
		ExitCode:   nil,
		LastError:  "",
		RetryCount: retryCount,
	}
}

func setRetryCount(retryCount int) {
	stateMu.Lock()
	defer stateMu.Unlock()

	state.RetryCount = retryCount
}

func updateExitState(service string, ps *os.ProcessState, err error) {
	stateMu.Lock()
	defer stateMu.Unlock()

	code := -1
	if ps != nil {
		code = ps.ExitCode()
	}

	state.Running = false
	state.Service = sanitizeServiceName(service)
	state.Pid = 0
	state.ExitCode = &code
	if err != nil {
		state.LastError = err.Error()
	} else {
		state.LastError = ""
	}

	_ = writeLastExitCode(service, code)
}

func acquirePIDLock(service string) (func(), error) {
	path := pidFilePath(service)
	currentPID := os.Getpid()

	for attempt := 0; attempt < 2; attempt++ {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if err == nil {
			if _, writeErr := fmt.Fprintf(f, "%d\n", currentPID); writeErr != nil {
				_ = f.Close()
				_ = os.Remove(path)
				return nil, fmt.Errorf("write pid file %q: %w", path, writeErr)
			}
			if closeErr := f.Close(); closeErr != nil {
				_ = os.Remove(path)
				return nil, fmt.Errorf("close pid file %q: %w", path, closeErr)
			}

			return func() {
				_ = removePIDFileIfOwned(path, currentPID)
			}, nil
		}

		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create pid file %q: %w", path, err)
		}

		running, existingPID, checkErr := activePIDFromFile(path)
		if checkErr != nil {
			return nil, checkErr
		}
		if running {
			return nil, fmt.Errorf("service %q already running (pid %d)", service, existingPID)
		}

		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return nil, fmt.Errorf("remove stale pid file %q: %w", path, removeErr)
		}
	}

	return nil, fmt.Errorf("unable to acquire pid lock for service %q", service)
}

func pidFilePath(service string) string {
	safe := sanitizeServiceName(service)
	return filepath.Join("/tmp", fmt.Sprintf("jan_%s.pid", safe))
}

func statusFilePath(service string) string {
	safe := sanitizeServiceName(service)
	return filepath.Join("/tmp", fmt.Sprintf("jan_%s.status", safe))
}

func sanitizeServiceName(service string) string {
	service = strings.TrimSpace(service)
	if service == "" {
		return "service"
	}

	var b strings.Builder
	b.Grow(len(service))
	for _, r := range service {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}

	if b.Len() == 0 {
		return "service"
	}

	return b.String()
}

func activePIDFromFile(path string) (bool, int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, 0, fmt.Errorf("read pid file %q: %w", path, err)
	}

	pidText := strings.TrimSpace(string(raw))
	if pidText == "" {
		return false, 0, nil
	}

	pid, err := strconv.Atoi(pidText)
	if err != nil || pid <= 0 {
		return false, 0, nil
	}

	running, runErr := isPIDRunning(pid)
	if runErr != nil {
		return false, pid, fmt.Errorf("check pid %d from %q: %w", pid, path, runErr)
	}

	return running, pid, nil
}

func isPIDRunning(pid int) (bool, error) {
	err := syscall.Kill(pid, 0)
	switch err {
	case nil:
		return true, nil
	case syscall.ESRCH:
		return false, nil
	case syscall.EPERM:
		return true, nil
	default:
		return false, err
	}
}

func removePIDFileIfOwned(path string, ownerPID int) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	pidText := strings.TrimSpace(string(raw))
	pid, parseErr := strconv.Atoi(pidText)
	if parseErr != nil {
		return nil
	}

	if pid != ownerPID {
		return nil
	}

	if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
		return removeErr
	}

	return nil
}

func writeLastExitCode(service string, code int) error {
	path := statusFilePath(service)
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", code)), 0o644)
}

func readLastExitCode(service string) (*int, error) {
	path := statusFilePath(service)
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read status file %q: %w", path, err)
	}

	text := strings.TrimSpace(string(raw))
	if text == "" {
		return nil, nil
	}

	code, err := strconv.Atoi(text)
	if err != nil {
		return nil, nil
	}

	return &code, nil
}
