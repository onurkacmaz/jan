package logger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	defaultSystemLogDir = "/var/log/jan"
	fallbackLogDir      = "/tmp/jan/logs"
)

func OpenServiceLog(service string, customPath string) (*os.File, error) {
	paths := CandidateLogPaths(service, customPath)
	if len(paths) == 0 {
		return nil, fmt.Errorf("no candidate log path resolved")
	}

	if strings.TrimSpace(customPath) != "" {
		return openLogFile(paths[0])
	}

	logFile, err := openLogFile(paths[0])
	if err == nil {
		return logFile, nil
	}

	if shouldFallbackToTmp(paths[0], err) && len(paths) > 1 {
		fallbackFile, fallbackErr := openLogFile(paths[1])
		if fallbackErr == nil {
			return fallbackFile, nil
		}
		return nil, fmt.Errorf(
			"failed to open log file (primary=%q fallback=%q): %w",
			paths[0],
			paths[1],
			fallbackErr,
		)
	}

	return nil, err
}

func openLogFile(logPath string) (*os.File, error) {
	logDir := filepath.Dir(logPath)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create log directory %q: %w", logDir, err)
	}

	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %q: %w", logPath, err)
	}
	return logFile, nil
}

func CloseServiceLog(logFile *os.File) error {
	if logFile != nil {
		return logFile.Close()
	}
	return nil
}

func ResolveLogDir() string {
	if v := strings.TrimSpace(os.Getenv("JAN_LOG_DIR")); v != "" {
		return v
	}

	return defaultSystemLogDir
}

func ResolveLogPath(service string, customPath string) string {
	if path := strings.TrimSpace(customPath); path != "" {
		return path
	}

	return filepath.Join(ResolveLogDir(), fmt.Sprintf("%s.log", service))
}

func CandidateLogPaths(service string, customPath string) []string {
	primary := ResolveLogPath(service, customPath)
	if strings.TrimSpace(customPath) != "" {
		return []string{primary}
	}

	if strings.TrimSpace(os.Getenv("JAN_LOG_DIR")) != "" {
		return []string{primary}
	}

	return []string{
		primary,
		filepath.Join(fallbackLogDir, fmt.Sprintf("%s.log", service)),
	}
}

func FallbackLogDir() string {
	return fallbackLogDir
}

func shouldFallbackToTmp(primaryPath string, err error) bool {
	if strings.TrimSpace(os.Getenv("JAN_LOG_DIR")) != "" {
		return false
	}

	if filepath.Clean(filepath.Dir(primaryPath)) != filepath.Clean(defaultSystemLogDir) {
		return false
	}

	return errors.Is(err, os.ErrPermission) || os.IsPermission(err)
}
