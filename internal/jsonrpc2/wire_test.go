package jsonrpc2

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
)

func TestWireErrorIs(t *testing.T) {
	tests := []struct {
		name string
		a, b error
		want bool
	}{
		{"same code", NewError(-32600, "a"), NewError(-32600, "b"), true},
		{"different code", NewError(-32600, "a"), NewError(-32601, "b"), false},
		{"non-wire error", NewError(-32600, "a"), errors.New("plain"), false},
		{"sentinel parse", ErrParse, NewError(-32700, "other"), true},
		{"sentinel method not found", ErrMethodNotFound, NewError(-32601, "x"), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := errors.Is(tt.a, tt.b); got != tt.want {
				t.Errorf("errors.Is(%v, %v) = %v, want %v", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestWireErrorMessage(t *testing.T) {
	err := NewError(-32600, "bad request")
	if err.Error() != "bad request" {
		t.Errorf("Error() = %q, want %q", err.Error(), "bad request")
	}
}

func TestToWireError(t *testing.T) {
	t.Run("nil", func(t *testing.T) {
		if got := toWireError(nil); got != nil {
			t.Errorf("toWireError(nil) = %v, want nil", got)
		}
	})

	t.Run("wire error passthrough", func(t *testing.T) {
		we := &WireError{Code: -32600, Message: "bad"}
		got := toWireError(we)
		if got != we {
			t.Errorf("toWireError should return the same pointer for *WireError")
		}
	})

	t.Run("plain error", func(t *testing.T) {
		got := toWireError(errors.New("boom"))
		if got.Code != 0 {
			t.Errorf("Code = %d, want 0", got.Code)
		}
		if got.Message != "boom" {
			t.Errorf("Message = %q, want %q", got.Message, "boom")
		}
	})

	t.Run("wrapped wire error", func(t *testing.T) {
		inner := &WireError{Code: -32601, Message: "not found"}
		wrapped := fmt.Errorf("wrapper: %w", inner)
		got := toWireError(wrapped)
		if got.Code != -32601 {
			t.Errorf("Code = %d, want -32601", got.Code)
		}
		if got.Message != "wrapper: not found" {
			t.Errorf("Message = %q, want %q", got.Message, "wrapper: not found")
		}
	})
}

func TestStandardErrorCodes(t *testing.T) {
	tests := []struct {
		name string
		err  error
		code int64
	}{
		{"parse", ErrParse, -32700},
		{"invalid request", ErrInvalidRequest, -32600},
		{"method not found", ErrMethodNotFound, -32601},
		{"invalid params", ErrInvalidParams, -32602},
		{"internal", ErrInternal, -32603},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			we := &WireError{}
			ok := errors.As(tt.err, &we)
			if !ok {
				t.Fatal("expected *WireError")
			}
			if we.Code != tt.code {
				t.Errorf("Code = %d, want %d", we.Code, tt.code)
			}
		})
	}
}

func TestEncodeDecodeNotification(t *testing.T) {
	type logParams struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	}
	msg, err := NewNotification("log", logParams{Level: "info", Message: "hello"})
	if err != nil {
		t.Fatal(err)
	}

	data, err := EncodeMessage(msg)
	if err != nil {
		t.Fatal(err)
	}

	// Verify wire format.
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if raw["jsonrpc"] != "2.0" {
		t.Errorf("jsonrpc = %v, want 2.0", raw["jsonrpc"])
	}
	if raw["method"] != "log" {
		t.Errorf("method = %v, want log", raw["method"])
	}
	if _, hasID := raw["id"]; hasID {
		t.Error("notification should not have id")
	}

	// Round-trip.
	decoded, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	req, ok := decoded.(*Request)
	if !ok {
		t.Fatal("expected *Request")
	}
	if req.IsCall() {
		t.Error("notification should not be a call")
	}
	if req.Method != "log" {
		t.Errorf("Method = %q, want %q", req.Method, "log")
	}

	var got logParams
	if err := json.Unmarshal(req.Params, &got); err != nil {
		t.Fatal(err)
	}
	if got.Level != "info" || got.Message != "hello" {
		t.Errorf("params = %+v, want {info hello}", got)
	}
}

func TestEncodeDecodeCallInt64ID(t *testing.T) {
	msg, err := NewCall(Int64ID(42), "add", map[string]int{"a": 1, "b": 2})
	if err != nil {
		t.Fatal(err)
	}

	data, err := EncodeMessage(msg)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	req, ok := decoded.(*Request)
	if !ok {
		t.Fatal("expected *Request")
	}
	if !req.IsCall() {
		t.Error("call should be a call")
	}
	if req.ID.String() != "42" {
		t.Errorf("ID = %v, want 42", req.ID)
	}
	if req.Method != "add" {
		t.Errorf("Method = %q, want %q", req.Method, "add")
	}
}

func TestEncodeDecodeCallStringID(t *testing.T) {
	msg, err := NewCall(StringID("abc-123"), "lookup", nil)
	if err != nil {
		t.Fatal(err)
	}

	data, err := EncodeMessage(msg)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	req, ok := decoded.(*Request)
	if !ok {
		t.Fatal("expected *Request")
	}
	if req.ID.String() != "abc-123" {
		t.Errorf("ID = %v, want abc-123", req.ID)
	}
}

func TestEncodeDecodeResponse(t *testing.T) {
	resp, err := NewResponse(Int64ID(7), map[string]string{"status": "ok"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	data, err := EncodeMessage(resp)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := decoded.(*Response)
	if !ok {
		t.Fatal("expected *Response")
	}
	if r.ID.String() != "7" {
		t.Errorf("ID = %v, want 7", r.ID)
	}
	if r.Error != nil {
		t.Errorf("Error = %v, want nil", r.Error)
	}

	var result map[string]string
	if err := json.Unmarshal(r.Result, &result); err != nil {
		t.Fatal(err)
	}
	if result["status"] != "ok" {
		t.Errorf("result = %v, want {status: ok}", result)
	}
}

func TestEncodeDecodeErrorResponse(t *testing.T) {
	resp, err := NewResponse(Int64ID(3), nil, NewError(-32601, "method not found"))
	if err != nil {
		t.Fatal(err)
	}

	data, err := EncodeMessage(resp)
	if err != nil {
		t.Fatal(err)
	}

	decoded, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	r, ok := decoded.(*Response)
	if !ok {
		t.Fatal("expected *Response")
	}
	if r.Error == nil {
		t.Fatal("expected error in response")
	}

	var we *WireError
	if !errors.As(r.Error, &we) {
		t.Fatal("expected *WireError")
	}
	if we.Code != -32601 {
		t.Errorf("Code = %d, want -32601", we.Code)
	}
}

func TestDecodeInvalidVersion(t *testing.T) {
	raw := `{"jsonrpc":"1.0","method":"foo"}`
	_, err := DecodeMessage([]byte(raw))
	if err == nil {
		t.Fatal("expected error for invalid version")
	}
}

func TestDecodeInvalidJSON(t *testing.T) {
	_, err := DecodeMessage([]byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDecodeMissingMethodAndID(t *testing.T) {
	raw := `{"jsonrpc":"2.0"}`
	_, err := DecodeMessage([]byte(raw))
	if err == nil {
		t.Fatal("expected error for message with no method and no id")
	}
}

func TestMakeID(t *testing.T) {
	tests := []struct {
		name    string
		input   any
		want    string
		wantErr bool
	}{
		{"nil", nil, "<nil>", false},
		{"float64", float64(42), "42", false},
		{"string", "abc", "abc", false},
		{"invalid type", true, "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			id, err := MakeID(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("MakeID(%v) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if !tt.wantErr && id.String() != tt.want {
				t.Errorf("MakeID(%v).String() = %q, want %q", tt.input, id.String(), tt.want)
			}
		})
	}
}

func TestIDValidity(t *testing.T) {
	var zero ID
	if zero.IsValid() {
		t.Error("zero ID should not be valid")
	}
	if Int64ID(1).Raw() != int64(1) {
		t.Error("Raw() should return underlying value")
	}
	if StringID("x").Raw() != "x" {
		t.Error("Raw() should return underlying value")
	}
}

func TestNewNotificationNilParams(t *testing.T) {
	msg, err := NewNotification("ping", nil)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Params != nil {
		t.Errorf("Params = %v, want nil", msg.Params)
	}

	data, err := EncodeMessage(msg)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := DecodeMessage(data)
	if err != nil {
		t.Fatal(err)
	}
	req, ok := decoded.(*Request)
	if !ok {
		t.Fatal("expected *Request")
	}
	if req.Method != "ping" {
		t.Errorf("Method = %q, want ping", req.Method)
	}
}
