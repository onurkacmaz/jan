package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseValidConfig(t *testing.T) {
	raw := []byte(`
name: jan-demo
command: /usr/bin/sleep
args:
  - "5"
workdir: /tmp
log_path: /tmp/jan-demo.log
env:
  APP_ENV: dev
  LOG_LEVEL: info
restart_policy: on-failure
max_retries: 3
backoff_ms: 500
`)

	cfg, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}

	if cfg.Name != "jan-demo" {
		t.Fatalf("Name = %q, want %q", cfg.Name, "jan-demo")
	}
	if cfg.Command != "/usr/bin/sleep" {
		t.Fatalf("Command = %q, want %q", cfg.Command, "/usr/bin/sleep")
	}
	if len(cfg.Args) != 1 || cfg.Args[0] != "5" {
		t.Fatalf("Args = %#v, want []string{\"5\"}", cfg.Args)
	}
	if cfg.Workdir != "/tmp" {
		t.Fatalf("Workdir = %q, want %q", cfg.Workdir, "/tmp")
	}
	if cfg.LogPath != "/tmp/jan-demo.log" {
		t.Fatalf("LogPath = %q, want %q", cfg.LogPath, "/tmp/jan-demo.log")
	}
	if cfg.Env["APP_ENV"] != "dev" || cfg.Env["LOG_LEVEL"] != "info" {
		t.Fatalf("Env = %#v, unexpected values", cfg.Env)
	}
	if cfg.RestartPolicy != RestartOnFailure {
		t.Fatalf("RestartPolicy = %q, want %q", cfg.RestartPolicy, RestartOnFailure)
	}
	if cfg.MaxRetries != 3 {
		t.Fatalf("MaxRetries = %d, want %d", cfg.MaxRetries, 3)
	}
	if cfg.BackoffMS != 500 {
		t.Fatalf("BackoffMS = %d, want %d", cfg.BackoffMS, 500)
	}
}

func TestLoadValidConfigFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	raw := `
name: jan-demo
command: /bin/echo
args: ["hello"]
workdir: /tmp
log_path: /tmp/jan-load.log
env:
  HELLO: world
restart_policy: always
max_retries: 5
backoff_ms: 200
`

	if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if cfg.RestartPolicy != RestartAlways {
		t.Fatalf("RestartPolicy = %q, want %q", cfg.RestartPolicy, RestartAlways)
	}
	if cfg.LogPath != "/tmp/jan-load.log" {
		t.Fatalf("LogPath = %q, want %q", cfg.LogPath, "/tmp/jan-load.log")
	}
}

func TestParseMissingName(t *testing.T) {
	raw := []byte(`
command: /bin/echo
restart_policy: never
max_retries: 0
backoff_ms: 0
`)

	_, err := Parse(raw)
	assertErrContains(t, err, "name")
}

func TestParseMissingCommand(t *testing.T) {
	raw := []byte(`
name: jan-demo
restart_policy: never
max_retries: 0
backoff_ms: 0
`)

	_, err := Parse(raw)
	assertErrContains(t, err, "command")
}

func TestParseInvalidRestartPolicy(t *testing.T) {
	raw := []byte(`
name: jan-demo
command: /bin/echo
restart_policy: sometimes
max_retries: 0
backoff_ms: 0
`)

	_, err := Parse(raw)
	assertErrContains(t, err, "restart_policy")
}

func TestParseNegativeMaxRetries(t *testing.T) {
	raw := []byte(`
name: jan-demo
command: /bin/echo
restart_policy: always
max_retries: -1
backoff_ms: 0
`)

	_, err := Parse(raw)
	assertErrContains(t, err, "max_retries")
}

func TestParseTrimsLogPath(t *testing.T) {
	raw := []byte(`
name: jan-demo
command: /bin/echo
log_path: "  /tmp/jan-trim.log   "
restart_policy: never
max_retries: 0
backoff_ms: 0
`)

	cfg, err := Parse(raw)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if cfg.LogPath != "/tmp/jan-trim.log" {
		t.Fatalf("LogPath = %q, want %q", cfg.LogPath, "/tmp/jan-trim.log")
	}
}

func assertErrContains(t *testing.T, err error, field string) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error containing %q, got nil", field)
	}
	if !strings.Contains(err.Error(), field) {
		t.Fatalf("error = %q, want it to contain %q", err.Error(), field)
	}
}
