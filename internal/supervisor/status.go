package supervisor

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Status struct {
	Service      string
	Running      bool
	PID          int
	LastExitCode *int
}

func ServiceStatus(service string) (Status, error) {
	st := Status{
		Service: service,
	}

	path := pidFilePath(service)
	raw, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			return st, fmt.Errorf("read pid file %q: %w", path, err)
		}
		attachLastExitCode(&st, service)
		return st, nil
	}

	pidText := strings.TrimSpace(string(raw))
	pid, parseErr := strconv.Atoi(pidText)
	if parseErr != nil || pid <= 0 {
		attachLastExitCode(&st, service)
		return st, nil
	}

	running, runErr := isPIDRunning(pid)
	if runErr != nil {
		return st, fmt.Errorf("check pid %d from %q: %w", pid, path, runErr)
	}

	if running {
		st.Running = true
		st.PID = pid
		return st, nil
	}

	attachLastExitCode(&st, service)
	return st, nil
}

func attachLastExitCode(st *Status, service string) {
	code, err := readLastExitCode(service)
	if err != nil || code == nil {
		return
	}
	st.LastExitCode = code
}
