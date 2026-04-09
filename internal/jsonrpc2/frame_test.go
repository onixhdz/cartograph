package jsonrpc2

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestRawFramerRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	framer := RawFramer()
	w := framer.Writer(&buf)
	r := framer.Reader(&buf)
	ctx := context.Background()

	// Write a notification.
	msg, err := NewNotification("ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(ctx, msg); err != nil {
		t.Fatal(err)
	}

	// Write a call.
	call, err := NewCall(Int64ID(1), "add", map[string]int{"a": 1})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(ctx, call); err != nil {
		t.Fatal(err)
	}

	// Read notification back.
	got, err := r.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	req, ok := got.(*Request)
	if !ok {
		t.Fatal("expected *Request")
	}
	if req.Method != "ping" {
		t.Errorf("Method = %q, want ping", req.Method)
	}
	if req.IsCall() {
		t.Error("notification should not be a call")
	}

	// Read call back.
	got, err = r.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	req, ok = got.(*Request)
	if !ok {
		t.Fatal("expected *Request")
	}
	if req.Method != "add" {
		t.Errorf("Method = %q, want add", req.Method)
	}
	if !req.IsCall() {
		t.Error("call should be a call")
	}
}

func TestRawFramerEOF(t *testing.T) {
	r := RawFramer().Reader(strings.NewReader(""))
	_, err := r.Read(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestRawFramerInvalidJSON(t *testing.T) {
	r := RawFramer().Reader(strings.NewReader("not json\n"))
	_, err := r.Read(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestRawFramerContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := RawFramer().Reader(strings.NewReader(`{"jsonrpc":"2.0","method":"foo"}`))
	_, err := r.Read(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}

	var buf bytes.Buffer
	w := RawFramer().Writer(&buf)
	msg, _ := NewNotification("x", nil)
	if err := w.Write(ctx, msg); !errors.Is(err, context.Canceled) {
		t.Errorf("Write with canceled context: expected context.Canceled, got %v", err)
	}
}

func TestHeaderFramerRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	framer := HeaderFramer()
	w := framer.Writer(&buf)
	r := framer.Reader(&buf)
	ctx := context.Background()

	// Write a call.
	call, err := NewCall(Int64ID(99), "multiply", map[string]int{"x": 5, "y": 3})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(ctx, call); err != nil {
		t.Fatal(err)
	}

	// Verify raw bytes contain Content-Length header.
	written := buf.String()
	if !strings.HasPrefix(written, "Content-Length:") {
		t.Errorf("expected Content-Length header, got %q", written[:min(len(written), 50)])
	}

	// Read it back.
	got, err := r.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	req, ok := got.(*Request)
	if !ok {
		t.Fatal("expected *Request")
	}
	if req.Method != "multiply" {
		t.Errorf("Method = %q, want multiply", req.Method)
	}
	if req.ID.String() != "99" {
		t.Errorf("ID = %v, want 99", req.ID)
	}
}

func TestHeaderFramerEOF(t *testing.T) {
	r := HeaderFramer().Reader(strings.NewReader(""))
	_, err := r.Read(context.Background())
	if !errors.Is(err, io.EOF) {
		t.Errorf("expected io.EOF, got %v", err)
	}
}

func TestHeaderFramerMissingContentLength(t *testing.T) {
	// Valid header line but no Content-Length.
	r := HeaderFramer().Reader(strings.NewReader("X-Custom: foo\r\n\r\n"))
	_, err := r.Read(context.Background())
	if err == nil {
		t.Fatal("expected error for missing Content-Length")
	}
}

func TestHeaderFramerInvalidContentLength(t *testing.T) {
	r := HeaderFramer().Reader(strings.NewReader("Content-Length: -5\r\n\r\n"))
	_, err := r.Read(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid Content-Length")
	}
}

func TestHeaderFramerTruncatedBody(t *testing.T) {
	// Claim 100 bytes but provide fewer.
	r := HeaderFramer().Reader(strings.NewReader("Content-Length: 100\r\n\r\n{\"short\": true}"))
	_, err := r.Read(context.Background())
	if err == nil {
		t.Fatal("expected error for truncated body")
	}
}

func TestHeaderFramerInvalidHeaderLine(t *testing.T) {
	r := HeaderFramer().Reader(strings.NewReader("no-colon\r\n\r\n"))
	_, err := r.Read(context.Background())
	if err == nil {
		t.Fatal("expected error for invalid header line")
	}
}

func TestHeaderFramerContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := HeaderFramer().Reader(strings.NewReader("Content-Length: 10\r\n\r\n0123456789"))
	_, err := r.Read(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestHeaderFramerMultipleMessages(t *testing.T) {
	var buf bytes.Buffer
	framer := HeaderFramer()
	w := framer.Writer(&buf)
	ctx := context.Background()

	// Write three messages.
	for i := int64(1); i <= 3; i++ {
		msg, err := NewCall(Int64ID(i), "echo", map[string]int64{"n": i})
		if err != nil {
			t.Fatal(err)
		}
		if err := w.Write(ctx, msg); err != nil {
			t.Fatal(err)
		}
	}

	// Read them back.
	r := framer.Reader(&buf)
	for i := int64(1); i <= 3; i++ {
		got, err := r.Read(ctx)
		if err != nil {
			t.Fatalf("read %d: %v", i, err)
		}
		req, ok := got.(*Request)
		if !ok {
			t.Fatalf("message %d: expected *Request", i)
		}
		if req.ID.String() != Int64ID(i).String() {
			t.Errorf("message %d: ID = %v, want %v", i, req.ID, i)
		}
	}
}

func TestRawFramerResponseRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	framer := RawFramer()
	w := framer.Writer(&buf)
	r := framer.Reader(&buf)
	ctx := context.Background()

	resp, err := NewResponse(Int64ID(5), map[string]bool{"ok": true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(ctx, resp); err != nil {
		t.Fatal(err)
	}

	got, err := r.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	response, ok := got.(*Response)
	if !ok {
		t.Fatal("expected *Response")
	}
	if response.ID.String() != "5" {
		t.Errorf("ID = %v, want 5", response.ID)
	}
	if response.Error != nil {
		t.Errorf("Error = %v, want nil", response.Error)
	}
}
