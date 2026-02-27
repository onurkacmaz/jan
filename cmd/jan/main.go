package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"jan/internal/config"
	"jan/internal/supervisor"
	"os"
	"os/signal"
	"syscall"
)

const (
	exitCodeOK      = 0
	exitCodeUsage   = 2
	exitCodeConfig  = 10
	exitCodeRuntime = 20
	exitCodeSignal  = 130
)

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
		return fail(exitCodeUsage, "usage: jan <run|status> -c <config.yaml>")
	}

	switch args[0] {
	case "run":
		return runCmd(args[1:])
	case "status":
		return statusCmd(args[1:])
	default:
		return fail(exitCodeUsage, fmt.Sprintf("unknown command: %s", args[0]))
	}
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
		return fail(exitCodeUsage, "missing required flag: -c")
	}

	cfg, err := config.Load(*configPath)
	if err != nil {
		return wrapFail(exitCodeConfig, "config error", err)
	}

	status, err := supervisor.ServiceStatus(cfg.Name)
	if err != nil {
		return wrapFail(exitCodeRuntime, "runtime error", err)
	}

	if status.Running {
		fmt.Printf("service=%s status=running pid=%d\n", status.Service, status.PID)
		return nil
	}

	if status.LastExitCode != nil {
		fmt.Printf(
			"service=%s status=stopped last_exit_code=%d\n",
			status.Service,
			*status.LastExitCode,
		)
		return nil
	}

	fmt.Printf("service=%s status=stopped\n", status.Service)
	return nil
}
