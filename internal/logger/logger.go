package logger

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func OpenServiceLog(service string) (*os.File, error) {
	logDir, err := resolveLogDir()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve log directory: %w", err)
	}

	err = os.MkdirAll(logDir, 0o755)
	if err != nil {
		return nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	logPath := filepath.Join(logDir, fmt.Sprintf("%s.log", service))
	logFile, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file: %w", err)
	}
	return logFile, nil
}

func CloseServiceLog(logFile *os.File) error {
	if logFile != nil {
		return logFile.Close()
	}
	return nil
}

func resolveLogDir() (string, error) {
	if v := strings.TrimSpace(os.Getenv("JAN_LOG_DIR")); v != "" {
		return v, nil
	}

	root, err := projectRootFromWD()
	if err != nil {
		return "logs", nil
	}

	return filepath.Join(root, "logs"), nil
}

func projectRootFromWD() (string, error) {
	dir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, nil
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", errors.New("go.mod not found in current or parent directories")
		}

		dir = parent
	}
}
