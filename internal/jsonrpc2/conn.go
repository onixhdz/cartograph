// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Adapted from golang.org/x/tools/internal/jsonrpc2_v2 for the Cartograph
// plugin protocol. Simplified: no Preempter, no Server/Listener, no idle
// timeout. Retains bidirectional call support, pending call tracking, and
// graceful shutdown.

package jsonrpc2

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
)

// ErrClosed is returned for calls made on a closed connection.
var ErrClosed = errors.New("jsonrpc2: connection closed")

// Handler handles incoming JSON-RPC requests on a connection.
type Handler interface {
	Handle(ctx context.Context, req *Request) (result any, err error)
}

// HandlerFunc adapts a standalone function to the Handler interface.
type HandlerFunc func(ctx context.Context, req *Request) (any, error)

func (f HandlerFunc) Handle(ctx context.Context, req *Request) (any, error) {
	return f(ctx, req)
}

// ConnectionOptions configures a Connection.
type ConnectionOptions struct {
	// Framer controls message framing. If nil, RawFramer is used.
	Framer Framer
	// Handler handles incoming requests and notifications. If nil, all
	// requests receive ErrMethodNotFound.
	Handler Handler
}

// Connection manages a bidirectional JSON-RPC 2.0 connection.
// Both sides can send requests and receive responses concurrently.
type Connection struct {
	seq    atomic.Int64 // atomic: next request ID
	reader Reader
	writer Writer
	closer io.Closer

	handler Handler

	mu       sync.Mutex
	closed   bool
	pending  map[ID]*asyncCall // outgoing calls awaiting responses
	done     chan struct{}     // closed when reader goroutine exits
	closeErr error             // error from closing the underlying transport
}

// asyncCall tracks a pending outgoing call.
type asyncCall struct {
	id   ID
	done chan struct{}
	resp *Response
}

// AsyncCall represents a pending outgoing call whose result can be awaited.
type AsyncCall struct {
	ac *asyncCall
}

// ID returns the request ID for this call.
func (ac *AsyncCall) ID() ID { return ac.ac.id }

// Await waits for the response and unmarshals the result into the provided value.
// If the remote returned an error, it is returned directly.
func (ac *AsyncCall) Await(ctx context.Context, result any) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("awaiting call %v: %w", ac.ac.id, ctx.Err())
	case <-ac.ac.done:
	}
	if ac.ac.resp.Error != nil {
		return ac.ac.resp.Error
	}
	if result == nil {
		return nil
	}
	if err := json.Unmarshal(ac.ac.resp.Result, result); err != nil {
		return fmt.Errorf("unmarshaling result for call %v: %w", ac.ac.id, err)
	}
	return nil
}

// NewConnection creates a bidirectional JSON-RPC 2.0 connection over the
// given ReadWriteCloser and starts processing incoming messages.
func NewConnection(ctx context.Context, rwc io.ReadWriteCloser, opts ConnectionOptions) *Connection {
	framer := opts.Framer
	if framer == nil {
		framer = RawFramer()
	}
	handler := opts.Handler
	if handler == nil {
		handler = defaultHandler{}
	}

	c := &Connection{
		reader:  framer.Reader(rwc),
		writer:  framer.Writer(rwc),
		closer:  rwc,
		handler: handler,
		pending: make(map[ID]*asyncCall),
		done:    make(chan struct{}),
	}

	go c.readIncoming(ctx)
	return c
}

type defaultHandler struct{}

func (defaultHandler) Handle(_ context.Context, req *Request) (any, error) {
	return nil, fmt.Errorf("%w: %q", ErrMethodNotFound, req.Method)
}

// Call sends a request to the peer and returns an AsyncCall that can be used
// to await the response.
func (c *Connection) Call(ctx context.Context, method string, params any) *AsyncCall {
	id := Int64ID(c.seq.Add(1))

	ac := &asyncCall{
		id:   id,
		done: make(chan struct{}),
	}

	call, err := NewCall(id, method, params)
	if err != nil {
		ac.resp = &Response{ID: id, Error: fmt.Errorf("marshaling call parameters: %w", err)}
		close(ac.done)
		return &AsyncCall{ac: ac}
	}

	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		ac.resp = &Response{ID: id, Error: ErrClosed}
		close(ac.done)
		return &AsyncCall{ac: ac}
	}
	c.pending[id] = ac
	c.mu.Unlock()

	if err := c.write(ctx, call); err != nil {
		c.mu.Lock()
		if _, ok := c.pending[id]; ok {
			delete(c.pending, id)
			c.mu.Unlock()
			ac.resp = &Response{ID: id, Error: err}
			close(ac.done)
		} else {
			c.mu.Unlock()
		}
	}

	return &AsyncCall{ac: ac}
}

// Notify sends a notification (no response expected) to the peer.
func (c *Connection) Notify(ctx context.Context, method string, params any) error {
	msg, err := NewNotification(method, params)
	if err != nil {
		return fmt.Errorf("marshaling notification parameters: %w", err)
	}
	return c.write(ctx, msg)
}

// Close gracefully shuts down the connection. It waits for the reader
// goroutine to exit.
func (c *Connection) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return c.Wait()
	}
	c.closed = true
	c.mu.Unlock()

	err := c.closer.Close()

	c.mu.Lock()
	c.closeErr = err
	c.mu.Unlock()

	<-c.done
	if err != nil {
		return fmt.Errorf("closing transport: %w", err)
	}
	return nil
}

// Wait blocks until the connection has fully shut down.
func (c *Connection) Wait() error {
	<-c.done
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeErr
}

// Done returns a channel that is closed when the connection shuts down.
func (c *Connection) Done() <-chan struct{} {
	return c.done
}

// write sends a single message, serializing access to the writer.
func (c *Connection) write(ctx context.Context, msg Message) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	c.mu.Unlock()
	if err := c.writer.Write(ctx, msg); err != nil {
		return fmt.Errorf("writing jsonrpc message: %w", err)
	}
	return nil
}

// readIncoming reads messages from the transport and dispatches them.
func (c *Connection) readIncoming(ctx context.Context) {
	defer func() {
		// Retire all pending calls with an error.
		c.mu.Lock()
		for id, ac := range c.pending {
			ac.resp = &Response{ID: id, Error: ErrClosed}
			close(ac.done)
		}
		c.pending = nil
		c.mu.Unlock()

		close(c.done)
	}()

	for {
		msg, err := c.reader.Read(ctx)
		if err != nil {
			return
		}

		switch msg := msg.(type) {
		case *Request:
			go c.handleRequest(ctx, msg)
		case *Response:
			c.handleResponse(msg)
		}
	}
}

// handleRequest dispatches an incoming request to the handler and, for calls,
// sends the response back.
func (c *Connection) handleRequest(ctx context.Context, req *Request) {
	result, err := c.handler.Handle(ctx, req)

	if !req.IsCall() {
		return
	}

	// For calls, we must send a response.
	if result == nil && err == nil {
		err = fmt.Errorf("%w: %q", ErrInternal, req.Method)
	}

	resp, respErr := NewResponse(req.ID, result, err)
	if respErr != nil {
		resp, _ = NewResponse(req.ID, nil, fmt.Errorf("%w: failed to marshal result for %q", ErrInternal, req.Method))
	}

	_ = c.write(ctx, resp)
}

// handleResponse matches an incoming response to a pending outgoing call.
func (c *Connection) handleResponse(resp *Response) {
	c.mu.Lock()
	ac, ok := c.pending[resp.ID]
	if ok {
		delete(c.pending, resp.ID)
	}
	c.mu.Unlock()

	if ok {
		ac.resp = resp
		close(ac.done)
	}
}
