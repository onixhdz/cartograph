package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/realxen/cartograph/internal/query"
	"github.com/realxen/cartograph/internal/service"
	"github.com/realxen/cartograph/internal/sysutil"
)

// ServeCmd is a command group for managing the cartograph background service.
type ServeCmd struct {
	Start  ServeStartCmd  `cmd:"" default:"1" help:"Start the service (default when no subcommand given)."`
	Stop   ServeStopCmd   `cmd:"" help:"Stop a running background service."`
	Status ServeStatusCmd `cmd:"" help:"Check whether the background service is running."`
}

// ServeStartCmd starts a long-running HTTP service that holds in-memory
// graphs and serves the JSON API over a unix domain socket (or TCP
// fallback).
type ServeStartCmd struct {
	Socket  string `help:"Unix socket path (default: <data-dir>/service.sock)." type:"path"`
	NoIdle  bool   `help:"Disable idle auto-shutdown timer."`
	Timeout int    `help:"Idle timeout in minutes before auto-shutdown (0 = no timeout)." default:"30"`
	Detach  bool   `help:"Detach and run in the background (use --no-detach for foreground)." short:"d" default:"true" negatable:""`
}

func (c *ServeStartCmd) Run(cli *CLI) error {
	if c.Detach {
		return c.runDetached()
	}
	return c.runForeground(cli)
}

// runDetached re-execs as a detached child process and waits for it to
// become healthy, then prints the PID and returns.
func (c *ServeStartCmd) runDetached() error {
	dataDir := DefaultDataDir()

	if client := tryConnectExisting(dataDir); client != nil {
		fmt.Println("Service is already running.")
		return nil
	}

	lf := service.NewLockfile(dataDir)
	if lf.IsStale() {
		_ = lf.Release()
	}

	// Remove leftover socket so the child's Listen succeeds.
	socketPath := c.Socket
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}
	_ = os.Remove(socketPath)

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("serve: cannot find executable: %w", err)
	}

	_ = os.MkdirAll(dataDir, 0o750)
	logPath := DefaultLogPath()
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		logFile = nil
	}

	args := []string{"serve", "start", "--no-detach"}
	if c.NoIdle || c.Timeout == 0 {
		args = append(args, "--no-idle")
	} else {
		args = append(args, fmt.Sprintf("--timeout=%d", c.Timeout))
	}
	if c.Socket != "" {
		args = append(args, "--socket="+c.Socket)
	}

	cmd := exec.CommandContext(context.Background(), exe, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Stdin = nil
	cmd.SysProcAttr = sysutil.DetachProcAttr()
	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return fmt.Errorf("serve: start background process: %w", err)
	}
	_ = cmd.Process.Release()
	if logFile != nil {
		_ = logFile.Close()
	}

	// Wait for PID file (up to 5s).
	pidAppeared := false
	for range 100 {
		time.Sleep(50 * time.Millisecond)
		pid, _, _, err := lf.ReadFullInfo()
		if err == nil && pid > 0 {
			pidAppeared = true
			break
		}
	}
	if !pidAppeared {
		fmt.Fprintf(os.Stderr, "Service failed to start (no PID file after 5s).\n")
		printLogTail(logPath, 10)
		return errors.New("serve: background start timed out")
	}

	// Wait for connections (up to 10s).
	for range 50 {
		time.Sleep(200 * time.Millisecond)
		_, addr, network, err := lf.ReadFullInfo()
		if err == nil && addr != "" && isAlive(network, addr) {
			pid, _, _, _ := lf.ReadFullInfo()
			fmt.Printf("Service started (PID %d), listening on %s (%s).\n", pid, addr, network)
			fmt.Printf("  Log file: %s\n", logPath)
			return nil
		}
	}

	fmt.Fprintf(os.Stderr, "Service started but not accepting connections after 10s.\n")
	printLogTail(logPath, 10)
	return errors.New("serve: background service not ready")
}

func (c *ServeStartCmd) runForeground(_ *CLI) error {
	dataDir := DefaultDataDir()
	socketPath := c.Socket
	if socketPath == "" {
		socketPath = DefaultSocketPath()
	}

	if client := tryConnectExisting(dataDir); client != nil {
		fmt.Println("Service is already running.")
		return nil
	}

	lf := service.NewLockfile(dataDir)
	if lf.IsStale() {
		_ = lf.Release()
	}

	_ = os.MkdirAll(dataDir, 0o750)
	// Remove leftover socket file so Listen succeeds.
	_ = os.Remove(socketPath)

	if err := lf.Acquire(socketPath, "unix"); err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	srv, err := service.NewServer(socketPath, lf, dataDir)
	if err != nil {
		_ = lf.Release()
		return fmt.Errorf("serve: %w", err)
	}
	srv.SetBackendFactory(newServerBackendFactory(srv))

	if c.NoIdle || c.Timeout == 0 {
		srv.SetIdleTimeout(0)
	} else {
		srv.SetIdleTimeout(time.Duration(c.Timeout) * time.Minute)
	}

	// The server may have fallen back to TCP if unix sockets are
	// unavailable, so re-acquire with the actual network/address.
	if srv.Network != "" && srv.Network != "unix" {
		_ = lf.Release()
		if err := lf.Acquire(srv.Addr, srv.Network); err != nil {
			return fmt.Errorf("serve: update lockfile: %w", err)
		}
	}

	// Start accepting connections immediately so clients don't time out
	// while repos are loading.
	if err := srv.Start(); err != nil {
		_ = srv.Stop(context.Background())
		return fmt.Errorf("serve: %w", err)
	}

	fmt.Printf("Listening on %s (%s)\n", srv.Addr, srv.Network)
	if c.NoIdle || c.Timeout == 0 {
		fmt.Println("Idle timeout: disabled")
	} else {
		fmt.Printf("Idle timeout: %d minutes\n", c.Timeout)
	}

	// Warm up the embedding provider in the background so queries
	// that arrive during repo loading already have vector search.
	srv.WarmQueryProvider()

	// Load previously indexed repos so they're immediately queryable.
	// This runs after Start() so the server is already responsive
	// (queries for not-yet-loaded repos trigger lazy loading).
	fmt.Print("Loading indexed repositories...")
	srv.LoadAllFromRegistry()
	repos := srv.Repos()
	if len(repos) > 0 {
		fmt.Printf(" %d repo(s) loaded.\n", len(repos))
	} else {
		fmt.Println(" none found.")
	}

	// Resume any embedding jobs that were interrupted by a previous crash.
	srv.RecoverEmbedJobs()

	fmt.Println("Press Ctrl+C to stop.")

	// Block until SIGINT / SIGTERM or idle-timeout shutdown.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	select {
	case <-sigCh:
		fmt.Println("\nShutting down...")
	case <-srv.Done():
		fmt.Println("Idle timeout reached, shutting down...")
	}
	if err := srv.Stop(context.Background()); err != nil {
		return fmt.Errorf("serve: shutdown: %w", err)
	}
	fmt.Println("Stopped.")
	return nil
}

// ServeStopCmd sends a termination signal to the running background
// service. It first tries SIGTERM for a graceful shutdown, then
// escalates to SIGKILL if the process doesn't exit within 5 seconds.
type ServeStopCmd struct{}

func (c *ServeStopCmd) Run(cli *CLI) error {
	dataDir := DefaultDataDir()
	lf := service.NewLockfile(dataDir)

	pid, _, _, err := lf.ReadFullInfo()
	if err != nil {
		return fmt.Errorf("no service is running: %w", err)
	}
	if pid <= 0 {
		fmt.Println("No service is running (no PID in lockfile).")
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("no service running (PID %d not found): %w", pid, err)
	}

	// Phase 1: graceful shutdown via SIGTERM (Unix) or Kill (Windows).
	fmt.Printf("Stopping service (PID %d)...\n", pid)
	_ = sysutil.SignalTerm(proc)
	if !sysutil.IsProcessRunning(pid) {
		// Process may already be gone — not an error.
		fmt.Println("Stopped.")
		_ = lf.Release()
		return nil
	}

	// Phase 2: wait up to 5 seconds for the process to exit.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if !sysutil.IsProcessRunning(pid) {
			fmt.Println("Stopped.")
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Phase 3: escalate to SIGKILL (force kill).
	fmt.Printf("Process %d did not exit gracefully, forcing...\n", pid)
	if err := proc.Kill(); err != nil {
		return fmt.Errorf("serve stop: kill: %w", err)
	}
	fmt.Println("Killed.")
	return nil
}

// ServeStatusCmd checks whether the background service is currently
// running and reports its PID, address, and uptime.
type ServeStatusCmd struct{}

func (c *ServeStatusCmd) Run(cli *CLI) error {
	dataDir := DefaultDataDir()
	lf := service.NewLockfile(dataDir)

	pid, addr, network, err := lf.ReadFullInfo()
	if err != nil {
		return fmt.Errorf("service not running: %w", err)
	}

	if !sysutil.IsProcessRunning(pid) {
		fmt.Printf("Service not running (stale lockfile, PID %d).\n", pid)
		return nil
	}

	alive := isAlive(network, addr)
	if alive {
		fmt.Printf("Service running (PID %d)\n", pid)
		fmt.Printf("  Listening on %s (%s)\n", addr, network)
	} else {
		fmt.Printf("Service process exists (PID %d) but not accepting connections on %s (%s).\n", pid, addr, network)
	}

	logPath := DefaultLogPath()
	if info, err := os.Stat(logPath); err == nil && info.Size() > 0 {
		fmt.Printf("  Log file: %s (%s)\n", logPath, humanSize(info.Size()))
	}

	return nil
}

// humanSize returns a human-readable size string.
func humanSize(b int64) string {
	switch {
	case b >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(b)/float64(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.1f KB", float64(b)/float64(1<<10))
	default:
		return fmt.Sprintf("%d B", b)
	}
}

// tryConnectExisting reads the lockfile and tries to connect to a
// service that's already running. Returns nil if none is available.
func tryConnectExisting(dataDir string) *service.Client {
	lf := service.NewLockfile(dataDir)
	_, addr, network, err := lf.ReadFullInfo()
	if err != nil || addr == "" {
		return nil
	}
	if !isAlive(network, addr) {
		return nil
	}
	return service.NewAutoClient(addr)
}

// isAlive checks if a service endpoint is accepting connections.
func isAlive(network, addr string) bool {
	conn, err := (&net.Dialer{Timeout: 200 * time.Millisecond}).DialContext(context.Background(), network, addr)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

// newServerBackendFactory returns a BackendFactory that creates
// query.Backend instances from the server's cached graphs and search
// indexes.
func newServerBackendFactory(s *service.Server) service.BackendFactory {
	return func(repo string) service.ToolBackend {
		g, idx, ok := s.GetRepoResources(repo)
		if !ok {
			return nil
		}
		return &query.Backend{
			Graph:    g,
			Index:    idx,
			EmbedDir: s.GetRepoDir(repo),
			EmbedFn:  s.QueryEmbed,
		}
	}
}

// printLogTail prints the last n lines of a log file to stderr.
func printLogTail(path string, n int) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		fmt.Fprintf(os.Stderr, "  log file: %s\n", path)
		return
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	fmt.Fprintf(os.Stderr, "  log (%s):\n", path)
	for _, l := range lines {
		fmt.Fprintf(os.Stderr, "    %s\n", l)
	}
}
