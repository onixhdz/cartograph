// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Adapted from golang.org/x/tools/internal/jsonrpc2_v2 for the Cartograph
// plugin protocol.

package jsonrpc2

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Reader abstracts reading JSON-RPC messages from a transport.
// Each call to Read must return exactly one complete message or an error.
type Reader interface {
	Read(context.Context) (Message, error)
}

// Writer abstracts writing JSON-RPC messages to a transport.
// Each call to Write must send exactly one complete message or return an error.
type Writer interface {
	Write(context.Context, Message) error
}

// Framer wraps low-level byte streams into JSON-RPC message readers and writers.
type Framer interface {
	Reader(io.Reader) Reader
	Writer(io.Writer) Writer
}

// RawFramer returns a Framer that uses raw newline-delimited JSON.
// Messages are delimited by json.Decoder boundaries (no explicit newlines required,
// but each Encode appends a newline for readability/debugging).
// This is the primary framer for the plugin stdin/stdout transport.
func RawFramer() Framer { return rawFramer{} }

type rawFramer struct{}

func (rawFramer) Reader(r io.Reader) Reader {
	return &rawReader{in: json.NewDecoder(r)}
}

func (rawFramer) Writer(w io.Writer) Writer {
	return &rawWriter{out: w}
}

type rawReader struct {
	in *json.Decoder
}

func (r *rawReader) Read(ctx context.Context) (Message, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("reading raw frame: %w", ctx.Err())
	default:
	}
	var raw json.RawMessage
	if err := r.in.Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding raw frame: %w", err)
	}
	return DecodeMessage(raw)
}

type rawWriter struct {
	out io.Writer
}

func (w *rawWriter) Write(ctx context.Context, msg Message) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("writing raw frame: %w", ctx.Err())
	default:
	}
	data, err := EncodeMessage(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	data = append(data, '\n')
	if _, err = w.out.Write(data); err != nil {
		return fmt.Errorf("writing raw frame: %w", err)
	}
	return nil
}

// HeaderFramer returns a Framer that uses Content-Length headers (LSP-style).
// Each message is preceded by "Content-Length: <n>\r\n\r\n" followed by <n> bytes
// of JSON. Useful for debugging with LSP tooling.
func HeaderFramer() Framer { return headerFramer{} }

type headerFramer struct{}

func (headerFramer) Reader(r io.Reader) Reader {
	return &headerReader{in: bufio.NewReader(r)}
}

func (headerFramer) Writer(w io.Writer) Writer {
	return &headerWriter{out: w}
}

type headerReader struct {
	in *bufio.Reader
}

func (r *headerReader) Read(ctx context.Context) (Message, error) {
	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("reading header frame: %w", ctx.Err())
	default:
	}

	firstRead := true
	var contentLength int64
	for {
		line, err := r.in.ReadString('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				if firstRead && line == "" {
					return nil, io.EOF
				}
				err = io.ErrUnexpectedEOF
			}
			return nil, fmt.Errorf("failed reading header line: %w", err)
		}
		firstRead = false

		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		colon := strings.IndexRune(line, ':')
		if colon < 0 {
			return nil, fmt.Errorf("invalid header line %q", line)
		}
		name, value := line[:colon], strings.TrimSpace(line[colon+1:])
		switch name {
		case "Content-Length":
			if contentLength, err = strconv.ParseInt(value, 10, 32); err != nil {
				return nil, fmt.Errorf("failed parsing Content-Length: %v", value)
			}
			if contentLength <= 0 {
				return nil, fmt.Errorf("invalid Content-Length: %v", contentLength)
			}
		}
	}
	if contentLength == 0 {
		return nil, errors.New("missing Content-Length header")
	}
	data := make([]byte, contentLength)
	if _, err := io.ReadFull(r.in, data); err != nil {
		return nil, fmt.Errorf("reading header frame body: %w", err)
	}
	return DecodeMessage(data)
}

type headerWriter struct {
	out io.Writer
}

func (w *headerWriter) Write(ctx context.Context, msg Message) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("writing header frame: %w", ctx.Err())
	default:
	}
	data, err := EncodeMessage(msg)
	if err != nil {
		return fmt.Errorf("marshaling message: %w", err)
	}
	if _, err := fmt.Fprintf(w.out, "Content-Length: %v\r\n\r\n", len(data)); err != nil {
		return fmt.Errorf("writing Content-Length header: %w", err)
	}
	if _, err = w.out.Write(data); err != nil {
		return fmt.Errorf("writing header frame body: %w", err)
	}
	return nil
}
