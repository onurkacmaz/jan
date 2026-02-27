package logger

import (
	"fmt"
	"os"
)

func OpenServiceLog(service string) (*os.File, error) {
	err := os.MkdirAll("logs", 0755)
	if err != nil {
		return nil, fmt.Errorf("failed to create logs directory: %w", err)
	}

	logFile, err := os.OpenFile(fmt.Sprintf("logs/%s.log", service), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
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
