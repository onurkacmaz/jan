package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"jan/internal/config"
	"jan/internal/daemon"
	"jan/internal/logger"
	"jan/internal/supervisor"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

const (
	exitCodeOK      = 0
	exitCodeUsage   = 2
	exitCodeConfig  = 10
	exitCodeRuntime = 20
	exitCodeSignal  = 130
)

var version = "dev"

type cliError struct {
	Code int
	Msg  string
	Err  error
}

func (e *cliError) Error() string {
	if e == nil {
		return ""
	}
	if e.Err == nil {
		return e.Msg
	}
	return fmt.Sprintf("%s: %v", e.Msg, e.Err)
}

func (e *cliError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func fail(code int, msg string) error {
	return &cliError{
		Code: code,
		Msg:  msg,
	}
}

func wrapFail(code int, msg string, err error) error {
	return &cliError{
		Code: code,
		Msg:  msg,
		Err:  err,
	}
}

func resolveExitCode(err error) int {
	if err == nil {
		return exitCodeOK
	}

	var cliErr *cliError
	if errors.As(err, &cliErr) {
		return cliErr.Code
	}

	return exitCodeRuntime
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(resolveExitCode(err))
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fail(
			exitCodeUsage,
			"usage: jan <run|status|start|stop|restart|clean|log|tail|help|version> [flags]",
		)
	}

	switch args[0] {
	case "help", "-h", "--help":
		return helpCmd(args[1:])
	case "version", "-v", "--version":
		return versionCmd(args[1:])
	case "run":
		return runCmd(args[1:])
	case "status":
		return statusCmd(args[1:])
	case "start":
		return startCmd(args[1:])
	case "stop":
		return stopCmd(args[1:])
	case "restart":
		return restartCmd(args[1:])
	case "clean":
		return cleanCmd(args[1:])
	case "log":
		return logCmd(args[1:])
	case "tail":
		return tailCmd(args[1:])
	case "__daemon":
		return daemonCmd(args[1:])
	default:
		return fail(exitCodeUsage, fmt.Sprintf("unknown command: %s", args[0]))
	}
}

func helpCmd(args []string) error {
	if len(args) > 0 {
		return fail(exitCodeUsage, "help command does not accept arguments")
	}

	fmt.Println("jan - lightweight process supervisor")
	fmt.Println("")
	fmt.Println("Usage:")
	fmt.Println("  jan <command> [flags]")
	fmt.Println("")
	fmt.Println("Commands:")
	fmt.Println("  run      run service in foreground (-c required)")
	fmt.Println("  start    start daemonized service(s) (-c <file> or -d <dir>, default /etc/jan)")
	fmt.Println("  stop     stop daemonized service(s) (-c optional, default all)")
	fmt.Println("  restart  restart daemonized service(s) (-c <file> or -d <dir>, default /etc/jan)")
	fmt.Println("  status   show service status (-c optional, default all)")
	fmt.Println("  clean    clean jan files under /tmp (-f, --pid-only)")
	fmt.Println("  log      show log directory/path info")
	fmt.Println("  tail     print/tail service logs (-s <service> or -c <config>)")
	fmt.Println("  version  print jan version")
	fmt.Println("  help     show this help")

	return nil
}

func versionCmd(args []string) error {
	if len(args) > 0 {
		return fail(exitCodeUsage, "version command does not accept arguments")
	}

	fmt.Printf("jan version %s\n", version)
	return nil
}

func logCmd(args []string) error {
	fs := flag.NewFlagSet("log", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("c", "", "path to config.yaml")
	service := fs.String("s", "", "service name")

	if err := fs.Parse(args); err != nil {
		return wrapFail(exitCodeUsage, "invalid arguments", err)
	}
	if *configPath != "" && *service != "" {
		return fail(exitCodeUsage, "use either -c or -s, not both")
	}

	if *configPath == "" && *service == "" {
		fmt.Printf("log_dir=%s\n", logger.ResolveLogDir())
		if strings.TrimSpace(os.Getenv("JAN_LOG_DIR")) == "" {
			fmt.Printf("fallback_log_dir=%s\n", logger.FallbackLogDir())
		}
		return nil
	}

	resolvedService := *service
	customPath := ""
	if *configPath != "" {
		cfg, err := config.Load(*configPath)
		if err != nil {
			return wrapFail(exitCodeConfig, "config error", err)
		}
		resolvedService = cfg.Name
		customPath = cfg.LogPath
	}

	candidates := logger.CandidateLogPaths(resolvedService, customPath)
	fmt.Printf("service=%s\n", resolvedService)
	fmt.Printf("log_path=%s\n", candidates[0])
	if len(candidates) > 1 {
		fmt.Printf("fallback_log_path=%s\n", candidates[1])
	}

	return nil
}

func tailCmd(args []string) error {
	fs := flag.NewFlagSet("tail", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("c", "", "path to config.yaml")
	service := fs.String("s", "", "service name")
	lines := fs.Int("n", 50, "number of lines")
	follow := fs.Bool("f", false, "follow log output")

	if err := fs.Parse(args); err != nil {
		return wrapFail(exitCodeUsage, "invalid arguments", err)
	}
	if *lines < 0 {
		return fail(exitCodeUsage, "line count must be non-negative")
	}
	if *configPath != "" && *service != "" {
		return fail(exitCodeUsage, "use either -c or -s, not both")
	}
	if *configPath == "" && *service == "" {
		return fail(exitCodeUsage, "one of -c or -s is required")
	}

	resolvedService := *service
	customPath := ""
	if *configPath != "" {
		cfg, err := config.Load(*configPath)
		if err != nil {
			return wrapFail(exitCodeConfig, "config error", err)
		}
		resolvedService = cfg.Name
		customPath = cfg.LogPath
	}

	logPath, err := resolveReadableLogPath(logger.CandidateLogPaths(resolvedService, customPath))
	if err != nil {
		return wrapFail(exitCodeRuntime, "runtime error", err)
	}

	if err := printLastLines(logPath, *lines); err != nil {
		return wrapFail(exitCodeRuntime, "runtime error", err)
	}

	if !*follow {
		return nil
	}

	if err := followLogFile(logPath); err != nil {
		return wrapFail(exitCodeRuntime, "runtime error", err)
	}

	return nil
}

func cleanCmd(args []string) error {
	fs := flag.NewFlagSet("clean", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	force := fs.Bool("f", false, "remove pid files even if process appears running")
	pidOnly := fs.Bool("pid-only", false, "clean only pid files; keep status files")

	if err := fs.Parse(args); err != nil {
		return wrapFail(exitCodeUsage, "invalid arguments", err)
	}

	report, err := daemon.Clean(*force, !*pidOnly)
	fmt.Printf(
		"removed_pid=%d removed_status=%d skipped_running=%d failed=%d\n",
		report.RemovedPID,
		report.RemovedStatus,
		report.SkippedRunning,
		report.Failed,
	)
	if err != nil {
		return wrapFail(exitCodeRuntime, "runtime error", err)
	}

	return nil
}

func stopCmd(args []string) error {
	fs := flag.NewFlagSet("stop", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("c", "", "path to config.yaml")

	if err := fs.Parse(args); err != nil {
		return wrapFail(exitCodeUsage, "invalid arguments", err)
	}
	if *configPath != "" {
		if err := daemon.Stop(*configPath); err != nil {
			return wrapFail(exitCodeRuntime, "runtime error", err)
		}
		return nil
	}

	stopped, failed, err := daemon.StopAll()
	fmt.Printf("stopped=%d failed=%d\n", stopped, failed)
	if err != nil {
		return wrapFail(exitCodeRuntime, "runtime error", err)
	}

	return nil
}

func restartCmd(args []string) error {
	fs := flag.NewFlagSet("restart", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("c", "", "path to config.yaml")
	configDir := fs.String("d", "", "path to config directory")

	if err := fs.Parse(args); err != nil {
		return wrapFail(exitCodeUsage, "invalid arguments", err)
	}
	if *configPath != "" {
		if *configDir != "" {
			return fail(exitCodeUsage, "use either -c or -d, not both")
		}
		if err := daemon.Restart(*configPath); err != nil {
			return wrapFail(exitCodeRuntime, "runtime error", err)
		}
		return nil
	}

	dir := *configDir
	if dir == "" {
		dir = "/etc/jan"
	}

	restarted, started, failed, err := daemon.RestartAll(dir)
	fmt.Printf("restarted=%d started=%d failed=%d\n", restarted, started, failed)
	if err != nil {
		return wrapFail(exitCodeRuntime, "runtime error", err)
	}

	return nil
}

func startCmd(args []string) error {
	fs := flag.NewFlagSet("start", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("c", "", "path to config.yaml")
	configDir := fs.String("d", "", "path to config directory")

	if err := fs.Parse(args); err != nil {
		return wrapFail(exitCodeUsage, "invalid arguments", err)
	}
	if *configPath != "" {
		if *configDir != "" {
			return fail(exitCodeUsage, "use either -c or -d, not both")
		}
		if err := daemon.Start(*configPath); err != nil {
			return wrapFail(exitCodeRuntime, "runtime error", err)
		}
		return nil
	}

	dir := *configDir
	if dir == "" {
		dir = "/etc/jan"
	}

	started, skipped, failed, err := daemon.StartAll(dir)
	fmt.Printf("started=%d skipped=%d failed=%d\n", started, skipped, failed)
	if err != nil {
		return wrapFail(exitCodeRuntime, "runtime error", err)
	}

	return nil
}

func daemonCmd(args []string) error {
	fs := flag.NewFlagSet("__daemon", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("c", "", "path to config.yaml")

	if err := fs.Parse(args); err != nil {
		return wrapFail(exitCodeUsage, "invalid arguments", err)
	}
	if *configPath == "" {
		return fail(exitCodeUsage, "missing required flag: -c")
	}

	if err := daemon.RunDaemon(*configPath); err != nil {
		return wrapFail(exitCodeRuntime, "runtime error", err)
	}

	return nil
}

func runCmd(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("c", "", "path to config.yaml")

	if err := fs.Parse(args); err != nil {
		return wrapFail(exitCodeUsage, "invalid arguments", err)
	}
	if *configPath == "" {
		return fail(exitCodeUsage, "missing required flag: -c")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return wrapFail(exitCodeConfig, "config error", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := supervisor.Run(ctx, cfg); err != nil {
		return wrapFail(exitCodeRuntime, "runtime error", err)
	}

	if ctx.Err() != nil {
		return fail(exitCodeSignal, "received shutdown signal")
	}

	return nil
}

func statusCmd(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	configPath := fs.String("c", "", "path to config.yaml")

	if err := fs.Parse(args); err != nil {
		return wrapFail(exitCodeUsage, "invalid arguments", err)
	}
	if *configPath == "" {
		statuses, err := daemon.StatusAll()
		if err != nil {
			return wrapFail(exitCodeRuntime, "runtime error", err)
		}
		if len(statuses) == 0 {
			fmt.Println("no services found")
			return nil
		}
		for _, st := range statuses {
			printServiceStatus(st)
		}
		return nil
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return wrapFail(exitCodeConfig, "config error", err)
	}

	st, err := daemon.StatusByService(cfg.Name)
	if err != nil {
		return wrapFail(exitCodeRuntime, "runtime error", err)
	}

	printServiceStatus(st)
	return nil
}

func printServiceStatus(st daemon.ServiceStatus) {
	if st.Running {
		fmt.Printf(
			"service=%s status=running daemon_pid=%d child_pid=%d\n",
			st.Service,
			st.DaemonPID,
			st.ChildPID,
		)
		return
	}

	if st.LastExitCode != nil {
		fmt.Printf(
			"service=%s status=stopped last_exit_code=%d\n",
			st.Service,
			*st.LastExitCode,
		)
		return
	}

	fmt.Printf("service=%s status=stopped\n", st.Service)
}

func resolveReadableLogPath(candidates []string) (string, error) {
	for _, path := range candidates {
		fi, err := os.Stat(path)
		if err != nil {
			continue
		}
		if fi.Mode().IsRegular() {
			return path, nil
		}
	}

	return "", fmt.Errorf("log file not found (checked: %s)", strings.Join(candidates, ", "))
}

func printLastLines(path string, lineCount int) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}

	if lineCount == 0 {
		return nil
	}

	lines := strings.Split(string(raw), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	start := len(lines) - lineCount
	if start < 0 {
		start = 0
	}
	for _, line := range lines[start:] {
		fmt.Println(line)
	}

	return nil
}

func followLogFile(path string) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	offset := len(raw)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			next, readErr := os.ReadFile(path)
			if readErr != nil {
				if errors.Is(readErr, os.ErrNotExist) {
					continue
				}
				return readErr
			}

			if len(next) < offset {
				offset = 0
			}

			if len(next) > offset {
				if _, writeErr := os.Stdout.Write(next[offset:]); writeErr != nil {
					return writeErr
				}
				offset = len(next)
			}
		}
	}
}
