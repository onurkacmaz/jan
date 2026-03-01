package daemon

import (
	"context"
	"errors"
	"fmt"
	"jan/internal/config"
	"jan/internal/supervisor"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
)

const (
	stopTimeout      = 3 * time.Second
	stopPollInterval = 100 * time.Millisecond
)

var ErrDaemonNotRunning = errors.New("daemon not running")

type ServiceStatus struct {
	Service      string
	Running      bool
	DaemonPID    int
	ChildPID     int
	LastExitCode *int
}

type CleanReport struct {
	RemovedPID     int
	RemovedStatus  int
	SkippedRunning int
	Failed         int
}

func Start(configPath string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	pidPath := daemonPIDPath(cfg.Name)
	existingPID, err := readPID(pidPath)
	if err == nil {
		running, runErr := isPIDRunning(existingPID)
		if runErr != nil {
			return runErr
		}
		if running {
			return fmt.Errorf("service %q already running (daemon pid %d)", cfg.Name, existingPID)
		}

		_ = removePID(pidPath)
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	exe, err := os.Executable()
	if err != nil {
		return err
	}

	cmd := exec.Command(exe, "__daemon", "-c", configPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_RDWR, 0o644)
	if err != nil {
		return err
	}
	defer devNull.Close()

	cmd.Stdin = devNull
	cmd.Stdout = devNull
	cmd.Stderr = devNull

	if err := cmd.Start(); err != nil {
		return err
	}

	if err := writePID(pidPath, cmd.Process.Pid); err != nil {
		_ = cmd.Process.Kill()
		return err
	}

	time.Sleep(100 * time.Millisecond)
	running, err := isPIDRunning(cmd.Process.Pid)
	if err != nil {
		return err
	}
	if !running {
		_ = removePID(pidPath)
		return fmt.Errorf("daemon failed to start")
	}

	return nil
}

func RunDaemon(configPath string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	pidPath := daemonPIDPath(cfg.Name)
	daemonPID := os.Getpid()
	if err := writePID(pidPath, daemonPID); err != nil {
		return err
	}
	defer func() {
		_ = removePIDIfOwned(pidPath, daemonPID)
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := supervisor.Run(ctx, cfg); err != nil {
		return err
	}

	if ctx.Err() != nil {
		return nil
	}

	return nil
}

func Stop(configPath string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	return stopByService(cfg.Name)
}

func Restart(configPath string) error {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return err
	}

	if err := stopByService(cfg.Name); err != nil && !errors.Is(err, ErrDaemonNotRunning) {
		return err
	}

	return Start(configPath)
}

func StopAll() (stopped, failed int, err error) {
	paths, err := filepath.Glob("/tmp/jan_*.daemon.pid")
	if err != nil {
		return 0, 0, err
	}

	for _, path := range paths {
		service, ok := serviceFromDaemonPIDPath(path)
		if !ok {
			continue
		}

		stopErr := stopByService(service)
		if stopErr == nil {
			stopped++
			continue
		}

		if errors.Is(stopErr, ErrDaemonNotRunning) {
			_ = removePID(path)
			continue
		}

		failed++
	}

	if failed > 0 {
		return stopped, failed, fmt.Errorf("%d service(s) failed to stop", failed)
	}

	return stopped, failed, nil
}

func Clean(force bool, removeStatus bool) (CleanReport, error) {
	pidPaths, err := filepath.Glob("/tmp/jan_*.pid")
	if err != nil {
		return CleanReport{}, err
	}

	sort.Strings(pidPaths)

	report := CleanReport{}
	for _, path := range pidPaths {
		pid, readErr := readPID(path)
		if readErr != nil {
			if rmErr := removePID(path); rmErr != nil {
				report.Failed++
				continue
			}
			report.RemovedPID++
			continue
		}

		running, runErr := isPIDRunning(pid)
		if runErr != nil {
			report.Failed++
			continue
		}
		if running && !force {
			report.SkippedRunning++
			continue
		}

		if rmErr := removePID(path); rmErr != nil {
			report.Failed++
			continue
		}
		report.RemovedPID++
	}

	if removeStatus {
		statusPaths, globErr := filepath.Glob("/tmp/jan_*.status")
		if globErr != nil {
			return report, globErr
		}

		sort.Strings(statusPaths)
		for _, path := range statusPaths {
			if rmErr := os.Remove(path); rmErr != nil {
				if errors.Is(rmErr, os.ErrNotExist) {
					continue
				}
				report.Failed++
				continue
			}
			report.RemovedStatus++
		}
	}

	if report.Failed > 0 {
		return report, fmt.Errorf("%d file(s) failed to clean", report.Failed)
	}

	return report, nil
}

func stopByService(service string) error {
	pidPath := daemonPIDPath(service)
	pid, err := readPID(pidPath)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("service %q: %w", service, ErrDaemonNotRunning)
	}
	if err != nil {
		return err
	}

	running, err := isPIDRunning(pid)
	if err != nil {
		return err
	}
	if !running {
		_ = removePID(pidPath)
		return fmt.Errorf("service %q: %w", service, ErrDaemonNotRunning)
	}

	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil && err != syscall.ESRCH {
		return err
	}

	if waitUntilStopped(pid, stopTimeout) {
		_ = removePID(pidPath)
		return nil
	}

	if err := syscall.Kill(pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return err
	}

	if !waitUntilStopped(pid, stopTimeout) {
		return fmt.Errorf("service %q daemon did not stop (pid %d)", service, pid)
	}

	_ = removePID(pidPath)
	return nil
}

func StartAll(dir string) (started, skipped, failed int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, 0, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := strings.ToLower(entry.Name())
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		configPath := filepath.Join(dir, entry.Name())
		startErr := Start(configPath)
		if startErr == nil {
			started++
			continue
		}

		if strings.Contains(startErr.Error(), "already running") {
			skipped++
			continue
		}

		failed++
	}

	if failed > 0 {
		return started, skipped, failed, fmt.Errorf("%d service(s) failed to start", failed)
	}

	return started, skipped, failed, nil
}

func RestartAll(dir string) (restarted, started, failed int, err error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, 0, 0, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := strings.ToLower(entry.Name())
		if !strings.HasSuffix(name, ".yaml") && !strings.HasSuffix(name, ".yml") {
			continue
		}

		configPath := filepath.Join(dir, entry.Name())
		cfg, cfgErr := loadConfig(configPath)
		if cfgErr != nil {
			failed++
			continue
		}

		wasRunning := false
		stopErr := stopByService(cfg.Name)
		if stopErr == nil {
			wasRunning = true
		} else if !errors.Is(stopErr, ErrDaemonNotRunning) {
			failed++
			continue
		}

		startErr := Start(configPath)
		if startErr != nil {
			failed++
			continue
		}

		if wasRunning {
			restarted++
		} else {
			started++
		}
	}

	if failed > 0 {
		return restarted, started, failed, fmt.Errorf("%d service(s) failed to restart", failed)
	}

	return restarted, started, failed, nil
}

func Status(configPath string) (ServiceStatus, error) {
	cfg, err := loadConfig(configPath)
	if err != nil {
		return ServiceStatus{}, err
	}

	return statusByService(cfg.Name)
}

func StatusByService(service string) (ServiceStatus, error) {
	return statusByService(service)
}

func StatusAll() ([]ServiceStatus, error) {
	serviceSet := map[string]struct{}{}

	daemonPIDPaths, err := filepath.Glob("/tmp/jan_*.daemon.pid")
	if err != nil {
		return nil, err
	}
	for _, path := range daemonPIDPaths {
		if service, ok := serviceFromDaemonPIDPath(path); ok {
			serviceSet[service] = struct{}{}
		}
	}

	childPIDPaths, err := filepath.Glob("/tmp/jan_*.child.pid")
	if err != nil {
		return nil, err
	}
	for _, path := range childPIDPaths {
		if service, ok := serviceFromChildPIDPath(path); ok {
			serviceSet[service] = struct{}{}
		}
	}

	statusPaths, err := filepath.Glob("/tmp/jan_*.status")
	if err != nil {
		return nil, err
	}
	for _, path := range statusPaths {
		if service, ok := serviceFromStatusPath(path); ok {
			serviceSet[service] = struct{}{}
		}
	}

	services := make([]string, 0, len(serviceSet))
	for service := range serviceSet {
		services = append(services, service)
	}
	sort.Strings(services)

	result := make([]ServiceStatus, 0, len(services))
	for _, service := range services {
		st, statusErr := statusByService(service)
		if statusErr != nil {
			return nil, statusErr
		}

		result = append(result, st)
	}

	return result, nil
}

func statusByService(service string) (ServiceStatus, error) {
	st := ServiceStatus{Service: service}

	daemonPID, daemonRunning, err := daemonPIDStatus(service)
	if err != nil {
		return st, err
	}

	supervisorStatus, err := supervisor.ServiceStatus(service)
	if err != nil {
		return st, err
	}
	st.LastExitCode = supervisorStatus.LastExitCode

	if daemonRunning {
		st.Running = true
		st.DaemonPID = daemonPID
		st.ChildPID = supervisorStatus.ChildPID
	}

	return st, nil
}

func loadConfig(configPath string) (*config.Config, error) {
	cfg, err := config.Load(configPath)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func daemonPIDPath(service string) string {
	return filepath.Join("/tmp", fmt.Sprintf("jan_%s.daemon.pid", sanitizeServiceName(service)))
}

func daemonPIDStatus(service string) (pid int, running bool, err error) {
	path := daemonPIDPath(service)
	pid, err = readPID(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, false, nil
		}
		_ = removePID(path)
		return 0, false, nil
	}

	running, err = isPIDRunning(pid)
	if err != nil {
		return 0, false, err
	}
	if !running {
		_ = removePID(path)
		return pid, false, nil
	}

	return pid, true, nil
}

func isPIDRunning(pid int) (bool, error) {
	err := syscall.Kill(pid, 0)
	switch err {
	case nil:
		return true, nil
	case syscall.EPERM:
		return true, nil
	case syscall.ESRCH:
		return false, nil
	default:
		return false, err
	}
}

func waitUntilStopped(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		running, err := isPIDRunning(pid)
		if err != nil {
			return false
		}
		if !running {
			return true
		}
		time.Sleep(stopPollInterval)
	}

	running, err := isPIDRunning(pid)
	return err == nil && !running
}

func readPID(path string) (int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}

	text := strings.TrimSpace(string(raw))
	pid, err := strconv.Atoi(text)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid in %q", path)
	}

	return pid, nil
}

func writePID(path string, pid int) error {
	return os.WriteFile(path, []byte(fmt.Sprintf("%d\n", pid)), 0o644)
}

func removePID(path string) error {
	err := os.Remove(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func removePIDIfOwned(path string, ownerPID int) error {
	pid, err := readPID(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return nil
	}
	if pid != ownerPID {
		return nil
	}

	return removePID(path)
}

func sanitizeServiceName(service string) string {
	service = strings.TrimSpace(service)
	if service == "" {
		return "service"
	}

	var b strings.Builder
	b.Grow(len(service))
	for _, r := range service {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('_')
	}

	if b.Len() == 0 {
		return "service"
	}

	return b.String()
}

func serviceFromDaemonPIDPath(path string) (string, bool) {
	name := filepath.Base(path)
	if !strings.HasPrefix(name, "jan_") || !strings.HasSuffix(name, ".daemon.pid") {
		return "", false
	}
	service := strings.TrimSuffix(strings.TrimPrefix(name, "jan_"), ".daemon.pid")
	if service == "" {
		return "", false
	}
	return service, true
}

func serviceFromStatusPath(path string) (string, bool) {
	name := filepath.Base(path)
	if !strings.HasPrefix(name, "jan_") || !strings.HasSuffix(name, ".status") {
		return "", false
	}
	service := strings.TrimSuffix(strings.TrimPrefix(name, "jan_"), ".status")
	if service == "" {
		return "", false
	}
	return service, true
}

func serviceFromChildPIDPath(path string) (string, bool) {
	name := filepath.Base(path)
	if !strings.HasPrefix(name, "jan_") || !strings.HasSuffix(name, ".child.pid") {
		return "", false
	}
	service := strings.TrimSuffix(strings.TrimPrefix(name, "jan_"), ".child.pid")
	if service == "" {
		return "", false
	}
	return service, true
}
