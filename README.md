# jan

`jan` is a small process supervisor written in Go.

## Prerequisites

- Go `1.25.0` (see `go.mod`)
- Unix-like OS (`/tmp` and POSIX signals are used)

## Install

Build binary in project root:

```bash
go build -o jan ./cmd/jan
```

Or run directly without build:

```bash
go run ./cmd/jan --help
```

## Quick Start (under 10 minutes)

1. Create `config.yaml`:

```yaml
name: demo
command: /bin/sh
args:
  - -c
  - echo out; echo err 1>&2
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

Run service:

```bash
jan run -c <config.yaml>
```

Show service status:

```bash
jan status -c <config.yaml>
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
restart_policy: never
max_retries: 0
backoff_ms: 0
```

Supported `restart_policy` values:

- `always`
- `on-failure`
- `never`

## Known Limitations

- Process state is local to a single running `jan` process.
- Status uses files under `/tmp` (`jan_<service>.pid` and `jan_<service>.status`).
- `status` can show `last_exit_code` only if service has been run at least once.
- No daemon mode yet; `jan run` stays in foreground.
