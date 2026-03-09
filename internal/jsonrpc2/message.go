// Copyright 2018 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.
//
// Adapted from golang.org/x/tools/internal/jsonrpc2_v2 for the Cartograph
// plugin protocol.

package jsonrpc2

import (
	"encoding/json"
	"errors"
	"fmt"
)

// ID is a Request identifier, defined by the spec to be a string, integer, or null.
// https://www.jsonrpc.org/specification#request_object
type ID struct {
	value any
}

// StringID creates a new string request identifier.
func StringID(s string) ID { return ID{value: s} }

// Int64ID creates a new integer request identifier.
func Int64ID(i int64) ID { return ID{value: i} }

// IsValid returns true if the ID is a valid (non-null) identifier.
func (id ID) IsValid() bool { return id.value != nil }

// Raw returns the underlying value of the ID.
func (id ID) Raw() any { return id.value }

// String returns a human-readable representation of the ID.
func (id ID) String() string {
	if id.value == nil {
		return "<nil>"
	}
	return fmt.Sprint(id.value)
}

// MakeID coerces a Go value to an ID. The value is assumed to be the default
// JSON unmarshaling of a request identifier: nil, float64, or string.
func MakeID(v any) (ID, error) {
	switch v := v.(type) {
	case nil:
		return ID{}, nil
	case float64:
		return Int64ID(int64(v)), nil
	case string:
		return StringID(v), nil
	}
	return ID{}, fmt.Errorf("%w: invalid ID type %T", ErrParse, v)
}

// Message is the interface to all JSON-RPC 2.0 message types.
// The concrete types are *Request and *Response.
type Message interface {
	marshal(to *wireCombined)
}

// Request is a message sent to a peer to request behavior.
// If it has a valid ID it is a call; otherwise it is a notification.
type Request struct {
	// ID of this request, used to tie the Response back to the request.
	// Zero value (invalid) for notifications.
	ID ID
	// Method is the method name to invoke.
	Method string
	// Params is the raw JSON parameters of the method.
	Params json.RawMessage
}

// IsCall reports whether the request expects a response.
func (msg *Request) IsCall() bool { return msg.ID.IsValid() }

func (msg *Request) marshal(to *wireCombined) {
	to.ID = msg.ID.value
	to.Method = msg.Method
	to.Params = msg.Params
}

// Response is a message used as a reply to a call Request.
type Response struct {
	// ID of the request this is a response to.
	ID ID
	// Result is the content of the response (mutually exclusive with Error).
	Result json.RawMessage
	// Error is set only if the call failed.
	Error error
}

func (msg *Response) marshal(to *wireCombined) {
	to.ID = msg.ID.value
	to.Error = toWireError(msg.Error)
	to.Result = msg.Result
}

// NewCall constructs a new call Request (expects a response).
func NewCall(id ID, method string, params any) (*Request, error) {
	p, err := marshalToRaw(params)
	return &Request{ID: id, Method: method, Params: p}, err
}

// NewNotification constructs a new notification Request (no response expected).
func NewNotification(method string, params any) (*Request, error) {
	p, err := marshalToRaw(params)
	return &Request{Method: method, Params: p}, err
}

// NewResponse constructs a new Response. If err is set, result may be ignored.
func NewResponse(id ID, result any, rerr error) (*Response, error) {
	r, merr := marshalToRaw(result)
	return &Response{ID: id, Result: r, Error: rerr}, merr
}

// EncodeMessage encodes a Message to its JSON wire representation.
func EncodeMessage(msg Message) ([]byte, error) {
	wire := wireCombined{VersionTag: wireVersion}
	msg.marshal(&wire)
	data, err := json.Marshal(&wire)
	if err != nil {
		return data, fmt.Errorf("marshaling jsonrpc message: %w", err)
	}
	return data, nil
}

// DecodeMessage decodes a JSON wire representation into a Message.
func DecodeMessage(data []byte) (Message, error) {
	msg := wireCombined{}
	if err := json.Unmarshal(data, &msg); err != nil {
		return nil, fmt.Errorf("unmarshaling jsonrpc message: %w", err)
	}
	if msg.VersionTag != wireVersion {
		return nil, fmt.Errorf("invalid message version tag %q; expected %q", msg.VersionTag, wireVersion)
	}
	id, err := MakeID(msg.ID)
	if err != nil {
		return nil, err
	}
	if msg.Method != "" {
		return &Request{
			Method: msg.Method,
			ID:     id,
			Params: msg.Params,
		}, nil
	}
	if !id.IsValid() {
		return nil, errors.New("jsonrpc message has no method and no valid id")
	}
	resp := &Response{
		ID:     id,
		Result: msg.Result,
	}
	if msg.Error != nil {
		resp.Error = msg.Error
	}
	return resp, nil
}

func marshalToRaw(obj any) (json.RawMessage, error) {
	if obj == nil {
		return nil, nil
	}
	data, err := json.Marshal(obj)
	if err != nil {
		return nil, fmt.Errorf("marshaling to raw JSON: %w", err)
	}
	return json.RawMessage(data), nil
}
