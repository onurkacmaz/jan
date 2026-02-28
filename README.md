# jan

`jan` is a small process supervisor written in Go.

<img src="assets/jan-mascot.png" alt="Jan mascot" width="360" />

## Prerequisites

- Go `1.25.0` (see `go.mod`)
- Unix-like OS (`/tmp` and POSIX signals are used)

## Install

One-line install (curl):

```bash
curl -fsSL https://raw.githubusercontent.com/onurkacmaz/jan/main/install.sh | sh
```

System-wide one-line install:

```bash
curl -fsSL https://raw.githubusercontent.com/onurkacmaz/jan/main/install.sh | sh -s -- --system
```

Remote installer expects release assets named `jan-<os>-<arch>` (for example `jan-linux-amd64`, `jan-darwin-arm64`).

Build binary in project root:

```bash
go build -o jan ./cmd/jan
```

Build binary with embedded version (tag/commit):

```bash
VERSION=$(git describe --tags --always --dirty 2>/dev/null || git rev-parse --short HEAD)
go build -ldflags "-X main.version=${VERSION}" -o jan ./cmd/jan
```

Or run directly without build:

```bash
go run ./cmd/jan --help
```

Build/install with Makefile:

```bash
make build
make install
make install-system
make uninstall
make uninstall-system
```

One-line uninstall (curl installer):

```bash
curl -fsSL https://raw.githubusercontent.com/onurkacmaz/jan/main/install.sh | sh -s -- --uninstall
curl -fsSL https://raw.githubusercontent.com/onurkacmaz/jan/main/install.sh | sh -s -- --system --uninstall
```

## Quick Start (under 10 minutes)

1. Create `config.yaml`:

```yaml
name: demo
command: /bin/sh
args:
  - -c
  - echo out; echo err 1>&2
log_path: logs/demo.log
restart_policy: never
max_retries: 0
backoff_ms: 0
```

2. Run service:

```bash
./jan run -c config.yaml
```

3. Check status:

```bash
./jan status -c config.yaml
```

4. Inspect logs:

```bash
cat logs/demo.log
```

## Commands

Show help:

```bash
jan help
```

Show version:

```bash
jan version
```

Run service in foreground:

```bash
jan run -c <config.yaml>
```

Start daemonized service(s):

```bash
jan start -c <config.yaml>
jan start -d <config_dir>
jan start
```

Stop daemonized service(s):

```bash
jan stop -c <config.yaml>
jan stop
```

Show status:

```bash
jan status -c <config.yaml>
jan status
```

Sample status outputs:

```bash
service=demo status=running daemon_pid=12345 child_pid=12346
service=demo status=stopped last_exit_code=1
service=demo status=stopped
```

Clean pid/status files:

```bash
jan clean
jan clean -f
jan clean --pid-only
```

Sample clean output:

```bash
removed_pid=3 removed_status=2 skipped_running=0 failed=0
```

Show log directory/path info:

```bash
jan log
jan log -c <config.yaml>
jan log -s <service>
```

Tail service log:

```bash
jan tail -c <config.yaml> -n 100
jan tail -s <service> -n 100
jan tail -s <service> -f
```

## Exit Codes

`jan` uses fixed exit codes so errors are easy to classify in scripts.

| Code | Class | Meaning |
|---|---|---|
| 0 | success | command completed successfully |
| 2 | usage | invalid command/arguments |
| 10 | config | config file read/parse/validation error |
| 20 | runtime | process/supervisor runtime error |
| 130 | signal | process stopped by shutdown signal (`SIGINT`/`SIGTERM`) |

## Example config

```yaml
name: demo
command: /bin/sh
args: ["-c", "echo out; echo err 1>&2"]
log_path: logs/demo.log
restart_policy: never
max_retries: 0
backoff_ms: 0
```

Supported `restart_policy` values:

- `always`
- `on-failure`
- `never`

`log_path` is optional. If omitted, jan writes to `/var/log/jan/<service>.log` (or `JAN_LOG_DIR` if set). If system log path is not writable, jan falls back to `/tmp/jan/logs/<service>.log`.

## Known Limitations

- Process state is local to a single running `jan` process.
- Status uses files under `/tmp` (`jan_<service>.daemon.pid`, `jan_<service>.child.pid`, `jan_<service>.status`).
- `status` can show `last_exit_code` only if service has been run at least once.
- `jan run` stays in foreground; use `jan start` for daemon mode.
