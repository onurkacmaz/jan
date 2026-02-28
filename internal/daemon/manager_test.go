package daemon

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestStatusAllRunningShowsDaemonAndChildPID(t *testing.T) {
	service := "daemon-status-running"
	cleanupDaemonTestFiles(t, service)
	defer cleanupDaemonTestFiles(t, service)

	currentPID := os.Getpid()
	writePIDFile(t, daemonPIDPath(service), currentPID)
	writePIDFile(t, supervisorPIDPath(service), currentPID)
	writePIDFile(t, childPIDPath(service), currentPID)

	statuses, err := StatusAll()
	if err != nil {
		t.Fatalf("StatusAll() error = %v", err)
	}

	st, ok := findServiceStatus(statuses, sanitizeServiceName(service))
	if !ok {
		t.Fatalf("service %q not found in status list", service)
	}
	if !st.Running {
		t.Fatalf("expected running status, got %+v", st)
	}
	if st.DaemonPID != currentPID {
		t.Fatalf("daemon pid = %d, want %d", st.DaemonPID, currentPID)
	}
	if st.ChildPID != currentPID {
		t.Fatalf("child pid = %d, want %d", st.ChildPID, currentPID)
	}
}

func TestStatusAllIgnoresStaleDaemonPID(t *testing.T) {
	service := "daemon-status-stale"
	cleanupDaemonTestFiles(t, service)
	defer cleanupDaemonTestFiles(t, service)

	const stalePID = 999999

	writePIDFile(t, daemonPIDPath(service), stalePID)
	writePIDFile(t, supervisorPIDPath(service), stalePID)
	writePIDFile(t, childPIDPath(service), stalePID)
	if err := os.WriteFile(statusPath(service), []byte("3\n"), 0o644); err != nil {
		t.Fatalf("write status file: %v", err)
	}

	statuses, err := StatusAll()
	if err != nil {
		t.Fatalf("StatusAll() error = %v", err)
	}

	st, ok := findServiceStatus(statuses, sanitizeServiceName(service))
	if !ok {
		t.Fatalf("service %q not found in status list", service)
	}
	if st.Running {
		t.Fatalf("expected stale daemon pid to be stopped, got %+v", st)
	}
	if st.DaemonPID != 0 {
		t.Fatalf("expected daemon pid 0 for stale state, got %d", st.DaemonPID)
	}
	if st.ChildPID != 0 {
		t.Fatalf("expected child pid 0 for stale daemon state, got %d", st.ChildPID)
	}
	if st.LastExitCode == nil || *st.LastExitCode != 3 {
		t.Fatalf("last exit code = %v, want 3", st.LastExitCode)
	}

	if _, statErr := os.Stat(daemonPIDPath(service)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected stale daemon pid file to be removed, stat err=%v", statErr)
	}
}

func cleanupDaemonTestFiles(t *testing.T, service string) {
	t.Helper()

	for _, path := range []string{
		daemonPIDPath(service),
		supervisorPIDPath(service),
		childPIDPath(service),
		statusPath(service),
	} {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("remove %q: %v", path, err)
		}
	}
}

func writePIDFile(t *testing.T, path string, pid int) {
	t.Helper()

	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d\n", pid)), 0o644); err != nil {
		t.Fatalf("write pid file %q: %v", path, err)
	}
}

func findServiceStatus(statuses []ServiceStatus, service string) (ServiceStatus, bool) {
	for _, st := range statuses {
		if st.Service == service {
			return st, true
		}
	}

	return ServiceStatus{}, false
}

func supervisorPIDPath(service string) string {
	return filepath.Join("/tmp", fmt.Sprintf("jan_%s.pid", sanitizeServiceName(service)))
}

func childPIDPath(service string) string {
	return filepath.Join("/tmp", fmt.Sprintf("jan_%s.child.pid", sanitizeServiceName(service)))
}

func statusPath(service string) string {
	return filepath.Join("/tmp", fmt.Sprintf("jan_%s.status", sanitizeServiceName(service)))
}
