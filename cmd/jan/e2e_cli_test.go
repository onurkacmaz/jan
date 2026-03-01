package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestE2EStartStatusStop(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("daemon e2e is unix-specific")
	}

	bin := buildJanBinary(t)
	service := fmt.Sprintf("jan-e2e-%d", time.Now().UnixNano())
	cfgPath := writeConfig(t, service)
	t.Cleanup(func() { cleanupService(t, bin, cfgPath, service) })

	if out, err := runJan(bin, "start", "-c", cfgPath); err != nil {
		t.Fatalf("start failed: %v\n%s", err, out)
	}

	ok := waitForCondition(6*time.Second, func() bool {
		out, err := runJan(bin, "status", "-c", cfgPath)
		return err == nil &&
			strings.Contains(out, "status=running") &&
			strings.Contains(out, "daemon_pid=") &&
			strings.Contains(out, "child_pid=")
	})
	if !ok {
		out, _ := runJan(bin, "status", "-c", cfgPath)
		t.Fatalf("status did not reach running state, output=%q", out)
	}

	if out, err := runJan(bin, "stop", "-c", cfgPath); err != nil {
		t.Fatalf("stop failed: %v\n%s", err, out)
	}

	ok = waitForCondition(6*time.Second, func() bool {
		out, err := runJan(bin, "status", "-c", cfgPath)
		return err == nil && strings.Contains(out, "status=stopped")
	})
	if !ok {
		out, _ := runJan(bin, "status", "-c", cfgPath)
		t.Fatalf("status did not reach stopped state, output=%q", out)
	}
}

func TestE2ERestart(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("daemon e2e is unix-specific")
	}

	bin := buildJanBinary(t)
	service := fmt.Sprintf("jan-e2e-restart-%d", time.Now().UnixNano())
	cfgPath := writeConfig(t, service)
	t.Cleanup(func() { cleanupService(t, bin, cfgPath, service) })

	if out, err := runJan(bin, "start", "-c", cfgPath); err != nil {
		t.Fatalf("start failed: %v\n%s", err, out)
	}

	if ok := waitForCondition(6*time.Second, func() bool {
		out, err := runJan(bin, "status", "-c", cfgPath)
		return err == nil && strings.Contains(out, "status=running")
	}); !ok {
		out, _ := runJan(bin, "status", "-c", cfgPath)
		t.Fatalf("status did not reach running state before restart, output=%q", out)
	}

	if out, err := runJan(bin, "restart", "-c", cfgPath); err != nil {
		t.Fatalf("restart failed: %v\n%s", err, out)
	}

	if ok := waitForCondition(6*time.Second, func() bool {
		out, err := runJan(bin, "status", "-c", cfgPath)
		return err == nil && strings.Contains(out, "status=running")
	}); !ok {
		out, _ := runJan(bin, "status", "-c", cfgPath)
		t.Fatalf("status did not reach running state after restart, output=%q", out)
	}
}

func TestE2EStartDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("daemon e2e is unix-specific")
	}

	bin := buildJanBinary(t)
	serviceA := fmt.Sprintf("jan-e2e-dir-a-%d", time.Now().UnixNano())
	serviceB := fmt.Sprintf("jan-e2e-dir-b-%d", time.Now().UnixNano())

	configDir := t.TempDir()
	cfgA := writeConfigAt(t, configDir, "a.yaml", serviceA)
	cfgB := writeConfigAt(t, configDir, "b.yaml", serviceB)

	t.Cleanup(func() {
		cleanupService(t, bin, cfgA, serviceA)
		cleanupService(t, bin, cfgB, serviceB)
	})

	out, err := runJan(bin, "start", "-d", configDir)
	if err != nil {
		t.Fatalf("start -d failed: %v\n%s", err, out)
	}
	if !strings.Contains(out, "started=2") {
		t.Fatalf("unexpected start summary: %q", out)
	}

	okA := waitForCondition(6*time.Second, func() bool {
		outA, errA := runJan(bin, "status", "-c", cfgA)
		return errA == nil && strings.Contains(outA, "status=running")
	})
	okB := waitForCondition(6*time.Second, func() bool {
		outB, errB := runJan(bin, "status", "-c", cfgB)
		return errB == nil && strings.Contains(outB, "status=running")
	})
	if !okA || !okB {
		outA, _ := runJan(bin, "status", "-c", cfgA)
		outB, _ := runJan(bin, "status", "-c", cfgB)
		t.Fatalf("services did not reach running state\nA=%q\nB=%q", outA, outB)
	}
}

func buildJanBinary(t *testing.T) string {
	t.Helper()

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}

	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
	bin := filepath.Join(t.TempDir(), "jan-e2e-bin")

	cmd := exec.Command("go", "build", "-o", bin, "./cmd/jan")
	cmd.Dir = repoRoot
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go build failed: %v\n%s", err, string(output))
	}

	return bin
}

func runJan(bin string, args ...string) (string, error) {
	cmd := exec.Command(bin, args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

func writeConfig(t *testing.T, service string) string {
	t.Helper()
	return writeConfigAt(t, t.TempDir(), "config.yaml", service)
}

func writeConfigAt(t *testing.T, dir, name, service string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	raw := fmt.Sprintf(`
name: %s
command: /bin/sh
args: ["-c", "while true; do sleep 0.2; done"]
restart_policy: never
`, service)

	if err := os.WriteFile(path, []byte(strings.TrimSpace(raw)+"\n"), 0o644); err != nil {
		t.Fatalf("write config %q: %v", path, err)
	}

	return path
}

func cleanupService(t *testing.T, bin, cfgPath, service string) {
	t.Helper()

	_, _ = runJan(bin, "stop", "-c", cfgPath)

	for _, path := range []string{
		filepath.Join("/tmp", fmt.Sprintf("jan_%s.pid", service)),
		filepath.Join("/tmp", fmt.Sprintf("jan_%s.daemon.pid", service)),
		filepath.Join("/tmp", fmt.Sprintf("jan_%s.child.pid", service)),
		filepath.Join("/tmp", fmt.Sprintf("jan_%s.status", service)),
	} {
		_ = os.Remove(path)
	}
}

func waitForCondition(timeout time.Duration, check func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if check() {
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return check()
}
