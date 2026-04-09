// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Adapted from golang.org/x/tools/internal/jsonrpc2_v2 for the Cartograph
// plugin protocol. Simplified for unidirectional-call + bidirectional-call
// patterns over stdin/stdout.

package jsonrpc2

import (
	"encoding/json"
	"errors"
)

// Standard JSON-RPC 2.0 error codes.
// See https://www.jsonrpc.org/specification#error_object
var (
	// ErrParse is used when invalid JSON was received by the server.
	ErrParse = NewError(-32700, "JSON RPC parse error")
	// ErrInvalidRequest is used when the JSON sent is not a valid Request object.
	ErrInvalidRequest = NewError(-32600, "JSON RPC invalid request")
	// ErrMethodNotFound should be returned by the handler when the method does
	// not exist / is not available.
	ErrMethodNotFound = NewError(-32601, "JSON RPC method not found")
	// ErrInvalidParams should be returned by the handler when method
	// parameter(s) were invalid.
	ErrInvalidParams = NewError(-32602, "JSON RPC invalid params")
	// ErrInternal indicates a failure to process a call correctly.
	ErrInternal = NewError(-32603, "JSON RPC internal error")
)

const wireVersion = "2.0"

// wireCombined has all the fields of both Request and Response.
// We decode into this and then determine which message type it is.
type wireCombined struct {
	VersionTag string          `json:"jsonrpc"`
	ID         any             `json:"id,omitempty"`
	Method     string          `json:"method,omitempty"`
	Params     json.RawMessage `json:"params,omitempty"`
	Result     json.RawMessage `json:"result,omitempty"`
	Error      *WireError      `json:"error,omitempty"`
}

// WireError represents a structured error in a Response.
type WireError struct {
	// Code is an error code indicating the type of failure.
	Code int64 `json:"code"`
	// Message is a short description of the error.
	Message string `json:"message"`
	// Data is optional structured data containing additional information about the error.
	Data json.RawMessage `json:"data,omitempty"`
}

// NewError returns an error that will encode on the wire correctly.
// The standard codes are made available from this package, this function should
// only be used to build errors for application specific codes as allowed by the
// specification.
func NewError(code int64, message string) error {
	return &WireError{
		Code:    code,
		Message: message,
	}
}

func (err *WireError) Error() string {
	return err.Message
}

func (err *WireError) Is(other error) bool {
	w, ok := other.(*WireError)
	if !ok {
		return false
	}
	return err.Code == w.Code
}

// toWireError converts an error to a WireError suitable for the wire.
func toWireError(err error) *WireError {
	if err == nil {
		return nil
	}
	// Direct type check (not errors.As) — only pass through if err itself
	// is a *WireError, not if it wraps one. Wrapped errors get a new
	// WireError with the full message but the inner error's code.
	if w, ok := err.(*WireError); ok { //nolint:errorlint
		return w
	}
	result := &WireError{Message: err.Error()}
	var wrapped *WireError
	if errors.As(err, &wrapped) {
		result.Code = wrapped.Code
	}
	return result
}
