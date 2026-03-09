// Package plugin implements the plugin host that launches plugin binaries
// as subprocesses and bridges bidirectional JSON-RPC 2.0 to the Cartograph
// data source abstractions.
//
// Plugins are standalone executables that communicate over stdin/stdout.
// The protocol starts with a handshake line, then switches to newline-
// delimited JSON-RPC 2.0 (RawFramer). See the protocol reference in
// cloudgraph-plugin-system-plan.md for details.
package plugin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/realxen/cartograph/internal/jsonrpc2"
)

// Protocol constants.
const (
	// ProtocolVersion is the current handshake protocol version.
	ProtocolVersion = "1"

	// MagicCookieKey is the environment variable that prevents accidental
	// direct execution of plugin binaries. The plugin should check for this
	// on startup and refuse to run if it is not set.
	MagicCookieKey = "CARTOGRAPH_PLUGIN_MAGIC_COOKIE"

	// MagicCookieValue is the expected value for the magic cookie.
	MagicCookieValue = "cartograph-plugin-v1"

	// defaultHandshakeTimeout is the maximum time to wait for the plugin
	// to write its handshake line.
	defaultHandshakeTimeout = 30 * time.Second

	// defaultShutdownTimeout is the maximum time to wait for the plugin
	// process to exit after the JSON-RPC connection is closed.
	defaultShutdownTimeout = 5 * time.Second

	// stderrBufferSize is the maximum line length for stderr capture.
	stderrBufferSize = 64 * 1024
)

// Errors returned by the process launcher.
var (
	ErrHandshakeTimeout = errors.New("plugin: handshake timeout")
	ErrHandshakeInvalid = errors.New("plugin: invalid handshake")
	ErrProtocolMismatch = errors.New("plugin: protocol version mismatch")
)

// LaunchOptions configures plugin process launch behavior.
type LaunchOptions struct {
	// Env is additional environment variables for the plugin process.
	// The magic cookie is always added automatically.
	Env []string

	// HandshakeTimeout is how long to wait for the handshake line.
	// Default: 30 seconds.
	HandshakeTimeout time.Duration

	// ShutdownTimeout is how long to wait for the process to exit after
	// the JSON-RPC connection is closed. Default: 5 seconds.
	ShutdownTimeout time.Duration

	// Handler is the JSON-RPC handler for plugin-to-host calls and
	// notifications (config_get, cache_get, emit_node, log, etc.).
	Handler jsonrpc2.Handler

	// Stderr receives lines from the plugin's stderr. Each line is
	// delivered with the plugin name prefix already removed.
	// If nil, stderr is discarded.
	Stderr func(pluginName string, line string)
}

// PluginProcess represents a running plugin subprocess.
type PluginProcess struct {
	// Conn is the bidirectional JSON-RPC 2.0 connection to the plugin.
	Conn *jsonrpc2.Connection

	name    string
	version string

	cmd             *exec.Cmd
	stdin           io.WriteCloser
	shutdownTimeout time.Duration
	stderrDone      chan struct{} // closed when stderr goroutine exits
	once            sync.Once     // guards shutdown
	waitErr         error         // result of cmd.Wait()
	waitDone        chan struct{} // closed when cmd.Wait() returns
}

// Name returns the plugin's self-reported name from the handshake.
func (p *PluginProcess) Name() string { return p.name }

// Version returns the plugin's self-reported version from the handshake.
func (p *PluginProcess) Version() string { return p.version }

// LaunchPlugin starts a plugin binary as a subprocess, performs the protocol
// handshake, and returns a PluginProcess with an active JSON-RPC 2.0
// connection. The caller must call Close() when done.
func LaunchPlugin(ctx context.Context, binaryPath string, opts LaunchOptions) (*PluginProcess, error) {
	if opts.HandshakeTimeout == 0 {
		opts.HandshakeTimeout = defaultHandshakeTimeout
	}
	if opts.ShutdownTimeout == 0 {
		opts.ShutdownTimeout = defaultShutdownTimeout
	}

	cmd := exec.CommandContext(ctx, binaryPath)
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("%s=%s", MagicCookieKey, MagicCookieValue),
	)
	cmd.Env = append(cmd.Env, opts.Env...)

	stdinPipe, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin: stdin pipe: %w", err)
	}
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin: stdout pipe: %w", err)
	}
	stderrPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("plugin: stderr pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("plugin: start: %w", err)
	}

	// If anything fails after Start(), we must kill the process.
	cleanup := func() {
		_ = stdinPipe.Close()
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}

	name, version, err := readHandshake(stdoutPipe, opts.HandshakeTimeout)
	if err != nil {
		cleanup()
		return nil, err
	}

	stderrDone := make(chan struct{})
	go drainStderr(stderrPipe, name, opts.Stderr, stderrDone)

	// The bufio.Reader from readHandshake may have buffered data beyond
	// the handshake line, so we pass the buffered reader through.
	transport := &pluginTransport{
		reader: stdoutPipe,
		writer: stdinPipe,
		closeFn: func() error {
			return stdinPipe.Close()
		},
	}

	handler := opts.Handler
	if handler == nil {
		handler = jsonrpc2.HandlerFunc(func(_ context.Context, _ *jsonrpc2.Request) (any, error) {
			return nil, jsonrpc2.ErrMethodNotFound
		})
	}

	conn := jsonrpc2.NewConnection(ctx, transport, jsonrpc2.ConnectionOptions{
		Handler: handler,
	})

	p := &PluginProcess{
		Conn:            conn,
		name:            name,
		version:         version,
		cmd:             cmd,
		stdin:           stdinPipe,
		shutdownTimeout: opts.ShutdownTimeout,
		stderrDone:      stderrDone,
		waitDone:        make(chan struct{}),
	}

	go func() {
		p.waitErr = cmd.Wait()
		close(p.waitDone)
	}()

	return p, nil
}

// Close gracefully shuts down the plugin process:
//  1. Close the JSON-RPC connection (closes stdin, plugin sees EOF).
//  2. Wait for the process to exit within the shutdown timeout.
//  3. If it doesn't exit, send SIGKILL.
//  4. Always Wait() to prevent zombie processes.
func (p *PluginProcess) Close() error {
	var closeErr error
	p.once.Do(func() {
		// Close the JSON-RPC connection. This closes the stdin pipe,
		// which signals EOF to the plugin.
		if err := p.Conn.Close(); err != nil {
			closeErr = fmt.Errorf("plugin: close connection: %w", err)
		}

		// Wait for process exit with timeout.
		select {
		case <-p.waitDone:
			// Process exited on its own.
		case <-time.After(p.shutdownTimeout):
			// Timeout — kill the process.
			if p.cmd.Process != nil {
				_ = p.cmd.Process.Kill()
			}
			<-p.waitDone
		}

		// Wait for stderr goroutine to finish.
		<-p.stderrDone
	})
	return closeErr
}

// Wait blocks until the plugin process has fully exited.
func (p *PluginProcess) Wait() error {
	<-p.waitDone
	return p.waitErr
}

// readHandshake reads the first line from the plugin's stdout and parses
// the handshake: "<protocol-version>|<name>|<version>".
func readHandshake(r io.Reader, timeout time.Duration) (name string, version string, err error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	br := bufio.NewReaderSize(r, 4096)

	go func() {
		line, err := br.ReadString('\n')
		ch <- result{line: line, err: err}
	}()

	select {
	case <-time.After(timeout):
		return "", "", ErrHandshakeTimeout
	case res := <-ch:
		if res.err != nil {
			return "", "", fmt.Errorf("%w: read error: %w", ErrHandshakeInvalid, res.err)
		}
		return parseHandshake(strings.TrimRight(res.line, "\r\n"))
	}
}

// parseHandshake validates a handshake line: "<version>|<name>|<plugin-version>".
func parseHandshake(line string) (name string, version string, err error) {
	parts := strings.SplitN(line, "|", 3)
	if len(parts) != 3 {
		return "", "", fmt.Errorf("%w: expected 3 pipe-delimited fields, got %d: %q", ErrHandshakeInvalid, len(parts), line)
	}

	protoVersion := strings.TrimSpace(parts[0])
	name = strings.TrimSpace(parts[1])
	version = strings.TrimSpace(parts[2])

	if protoVersion != ProtocolVersion {
		return "", "", fmt.Errorf("%w: plugin uses protocol %q, host requires %q", ErrProtocolMismatch, protoVersion, ProtocolVersion)
	}

	if name == "" {
		return "", "", fmt.Errorf("%w: empty plugin name", ErrHandshakeInvalid)
	}

	if version == "" {
		return "", "", fmt.Errorf("%w: empty plugin version", ErrHandshakeInvalid)
	}

	return name, version, nil
}

// drainStderr reads lines from the plugin's stderr and forwards them to
// the logger function. This must run in a goroutine to prevent the plugin
// from blocking when the stderr pipe buffer fills.
func drainStderr(r io.Reader, pluginName string, logger func(string, string), done chan struct{}) {
	defer close(done)
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, stderrBufferSize), stderrBufferSize)
	for scanner.Scan() {
		line := scanner.Text()
		if logger != nil {
			logger(pluginName, line)
		}
	}
}

// pluginTransport adapts the plugin's stdout (read) and stdin (write) into
// an io.ReadWriteCloser for the JSON-RPC 2.0 connection.
type pluginTransport struct {
	reader  io.Reader
	writer  io.Writer
	closeFn func() error
}

func (t *pluginTransport) Read(p []byte) (int, error) {
	n, err := t.reader.Read(p)
	return n, err //nolint:wrapcheck
}

func (t *pluginTransport) Write(p []byte) (int, error) {
	n, err := t.writer.Write(p)
	return n, err //nolint:wrapcheck
}

func (t *pluginTransport) Close() error {
	return t.closeFn()
}
