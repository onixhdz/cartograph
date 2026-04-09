package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	mcpserver "github.com/realxen/cartograph/internal/mcp"
	"github.com/realxen/cartograph/internal/service"
	"github.com/realxen/cartograph/internal/sysutil"
)

// ServeCmd is a command group for managing the cartograph background service.
type ServeCmd struct {
	Start  ServeStartCmd  `cmd:"" default:"1" help:"Start the service (default when no subcommand given)."`
	Stop   ServeStopCmd   `cmd:"" help:"Stop a running background service."`
	Status ServeStatusCmd `cmd:"" help:"Check whether the background service is running."`
	Logs   ServeLogsCmd   `cmd:"" help:"Print the service log file to stdout."`
}

// ServeStartCmd starts a long-running HTTP service that holds in-memory
// graphs and serves the JSON API over a unix domain socket (or TCP
// fallback).
type ServeStartCmd struct {
	Socket  string `help:"Unix socket path (default: <data-dir>/service.sock)." type:"path"`
	NoIdle  bool   `help:"Disable idle auto-shutdown timer."`
	Timeout int    `help:"Idle timeout in minutes before auto-shutdown (0 = no timeout)." default:"30"`
	Detach  bool   `help:"Detach and run in the background (use --no-detach for foreground)." short:"d" default:"true" negatable:""`
	NoMCP   bool   `help:"Disable the built-in MCP endpoint at /mcp."`
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
	if c.NoMCP {
		args = append(args, "--no-mcp")
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

func (c *ServeStartCmd) runForeground(cli *CLI) error {
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

	if !c.NoMCP {
		appVersion := cli.AppVersion
		if appVersion == "" {
			appVersion = "dev"
		}
		mcpBackend := &serverMCPClient{srv: srv}
		mcpSrv := mcpserver.NewServer(appVersion, mcpBackend)
		handler := sdkmcp.NewStreamableHTTPHandler(
			func(_ *http.Request) *sdkmcp.Server {
				return mcpSrv.SDKServer()
			},
			&sdkmcp.StreamableHTTPOptions{Stateless: true},
		)
		srv.EnableMCP(handler)
	}

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
	if !c.NoMCP {
		fmt.Println("MCP endpoint:  /mcp (Streamable HTTP)")
	}
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

// ServeLogsCmd prints the service log file to stdout. By default it
// dumps the entire file. Use --tail N to show only the last N lines,
// or --follow to continuously stream new lines as they are written.
type ServeLogsCmd struct {
	Tail   int  `help:"Show only the last N lines." short:"n" default:"0"`
	Follow bool `help:"Follow the log file for new output (like tail -f)." short:"f"`
}

func (c *ServeLogsCmd) Run(cli *CLI) error {
	logPath := DefaultLogPath()

	if c.Follow {
		return c.follow(logPath)
	}

	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no log file found at %s", logPath)
		}
		return fmt.Errorf("read log: %w", err)
	}
	if len(data) == 0 {
		fmt.Fprintln(os.Stderr, "Log file is empty.")
		return nil
	}

	if c.Tail > 0 {
		lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
		if c.Tail < len(lines) {
			lines = lines[len(lines)-c.Tail:]
		}
		for _, l := range lines {
			fmt.Println(l)
		}
		return nil
	}

	if _, err = os.Stdout.Write(data); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}

// follow streams the log file to stdout, printing new lines as they
// are appended. It exits on SIGINT/SIGTERM.
func (c *ServeLogsCmd) follow(logPath string) error {
	f, err := os.Open(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no log file found at %s", logPath)
		}
		return fmt.Errorf("open log: %w", err)
	}
	defer f.Close()

	// If --tail is also set, seek to show the last N lines first.
	if c.Tail > 0 {
		data, err := os.ReadFile(logPath)
		if err == nil && len(data) > 0 {
			lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
			if c.Tail < len(lines) {
				lines = lines[len(lines)-c.Tail:]
			}
			for _, l := range lines {
				fmt.Println(l)
			}
		}
		// Seek to end so follow picks up only new content.
		if _, err := f.Seek(0, 2); err != nil {
			return fmt.Errorf("seek: %w", err)
		}
	} else {
		// Print existing content, then follow.
		if _, err := io.Copy(os.Stdout, f); err != nil {
			return fmt.Errorf("read log: %w", err)
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	buf := make([]byte, 4096)
	for {
		select {
		case <-sigCh:
			return nil
		default:
		}

		n, readErr := f.Read(buf)
		if n > 0 {
			_, _ = os.Stdout.Write(buf[:n])
		}
		if readErr != nil {
			// EOF — poll for more data.
			time.Sleep(200 * time.Millisecond)
		}
	}
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
	return newQueryBackendFactory(s)
}

// serverMCPClient adapts *service.Server to the mcp.Client interface,
// allowing the MCP handler to call server backends directly without
// HTTP round-tripping.
type serverMCPClient struct {
	srv *service.Server
}

func (c *serverMCPClient) resolveBackend(repo string) (string, service.ToolBackend, error) {
	resolved, err := c.srv.ResolveRepoName(repo)
	if err != nil {
		return "", nil, fmt.Errorf("resolve repo: %w", err)
	}
	be, err := c.srv.GetBackend(resolved)
	if err != nil {
		return "", nil, fmt.Errorf("get backend: %w", err)
	}
	if be == nil {
		return "", nil, &service.APIError{
			Code:    service.ErrCodeRepoNotFound,
			Message: fmt.Sprintf("repository %q not indexed", resolved),
		}
	}
	return resolved, be, nil
}

func (c *serverMCPClient) Query(req service.QueryRequest) (*service.QueryResult, error) {
	repo, be, err := c.resolveBackend(req.Repo)
	if err != nil {
		return nil, err
	}
	req.Repo = repo
	res, err := be.Query(req)
	if err != nil {
		return nil, fmt.Errorf("query: %w", err)
	}
	return res, nil
}

func (c *serverMCPClient) Context(req service.ContextRequest) (*service.ContextResult, error) {
	repo, be, err := c.resolveBackend(req.Repo)
	if err != nil {
		return nil, err
	}
	req.Repo = repo
	res, err := be.Context(req)
	if err != nil {
		return nil, fmt.Errorf("context: %w", err)
	}
	return res, nil
}

func (c *serverMCPClient) Cypher(req service.CypherRequest) (*service.CypherResult, error) {
	repo, be, err := c.resolveBackend(req.Repo)
	if err != nil {
		return nil, err
	}
	req.Repo = repo
	res, err := be.Cypher(req)
	if err != nil {
		return nil, fmt.Errorf("cypher: %w", err)
	}
	return res, nil
}

func (c *serverMCPClient) Impact(req service.ImpactRequest) (*service.ImpactResult, error) {
	repo, be, err := c.resolveBackend(req.Repo)
	if err != nil {
		return nil, err
	}
	req.Repo = repo
	res, err := be.Impact(req)
	if err != nil {
		return nil, fmt.Errorf("impact: %w", err)
	}
	return res, nil
}

func (c *serverMCPClient) Cat(req service.CatRequest) (*service.CatResult, error) {
	resolved, err := c.srv.ResolveRepoName(req.Repo)
	if err != nil {
		return nil, fmt.Errorf("resolve repo: %w", err)
	}
	req.Repo = resolved
	cr := c.srv.GetContentResolver(req.Repo)
	if cr == nil {
		return nil, &service.APIError{
			Code:    service.ErrCodeRepoNotFound,
			Message: fmt.Sprintf("repository %q has no content resolver", req.Repo),
		}
	}
	lineStart, lineEnd, err := service.ParseLineRange(req.Lines)
	if err != nil {
		return nil, fmt.Errorf("parse line range: %w", err)
	}
	result := service.CatResult{Files: make([]service.CatFile, 0, len(req.Files))}
	for _, path := range req.Files {
		data, readErr := cr.ReadFile(path)
		if readErr != nil {
			result.Files = append(result.Files, service.CatFile{Path: path, Error: readErr.Error()})
			continue
		}
		content := string(data)
		lineCount := strings.Count(content, "\n")
		if !strings.HasSuffix(content, "\n") && len(content) > 0 {
			lineCount++
		}
		if lineStart > 0 && lineEnd > 0 {
			lines := strings.Split(content, "\n")
			if lineStart > len(lines) {
				lineStart = len(lines)
			}
			if lineEnd > len(lines) {
				lineEnd = len(lines)
			}
			content = strings.Join(lines[lineStart-1:lineEnd], "\n")
		}
		result.Files = append(result.Files, service.CatFile{
			Path:      path,
			Content:   content,
			LineCount: lineCount,
		})
	}
	return &result, nil
}

func (c *serverMCPClient) Schema(req service.SchemaRequest) (*service.SchemaResult, error) {
	repo, be, err := c.resolveBackend(req.Repo)
	if err != nil {
		return nil, err
	}
	req.Repo = repo
	res, err := be.Schema(req)
	if err != nil {
		return nil, fmt.Errorf("schema: %w", err)
	}
	return res, nil
}

func (c *serverMCPClient) Status() (*service.StatusResult, error) {
	return c.srv.BuildStatus(), nil
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
