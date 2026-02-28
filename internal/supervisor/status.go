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
	DaemonPID    int
	ChildPID     int
	LastExitCode *int
}

func ServiceStatus(service string) (Status, error) {
	st := Status{
		Service: service,
	}

	daemonPID, daemonRunning, err := activePIDFromOptionalFile(pidFilePath(service))
	if err != nil {
		return st, err
	}

	if daemonRunning {
		st.Running = true
		st.PID = daemonPID
		st.DaemonPID = daemonPID

		childPID, childRunning, childErr := activePIDFromOptionalFile(childPIDFilePath(service))
		if childErr != nil {
			return st, childErr
		}
		if childRunning {
			st.ChildPID = childPID
		}

		return st, nil
	}

	attachLastExitCode(&st, service)
	return st, nil
}

func activePIDFromOptionalFile(path string) (pid int, running bool, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("read pid file %q: %w", path, err)
	}

	pidText := strings.TrimSpace(string(raw))
	pid, parseErr := strconv.Atoi(pidText)
	if parseErr != nil || pid <= 0 {
		_ = os.Remove(path)
		return 0, false, nil
	}

	running, runErr := isPIDRunning(pid)
	if runErr != nil {
		return 0, false, fmt.Errorf("check pid %d from %q: %w", pid, path, runErr)
	}
	if !running {
		_ = os.Remove(path)
		return pid, false, nil
	}

	return pid, true, nil
}

func attachLastExitCode(st *Status, service string) {
	code, err := readLastExitCode(service)
	if err != nil || code == nil {
		return
	}
	st.LastExitCode = code
}
