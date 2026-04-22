// Package rpc provides minimal JSON-RPC 2.0 types and validation for MCP host messages.
package rpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

// Version is the only supported jsonrpc field value for MCP host messages.
const Version = "2.0"

var (
	// ErrInvalidRequest wraps validation failures from ParseRequest.
	ErrInvalidRequest = errors.New("rpc: invalid JSON-RPC request")
	// ErrNotObject means the body was not a JSON object.
	ErrNotObject = errors.New("rpc: body must be a JSON object")
)

// Request is a JSON-RPC 2.0 request. Notifications omit ID.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

// IsNotification reports whether the message is a JSON-RPC notification (no id).
func (r *Request) IsNotification() bool {
	return len(r.ID) == 0
}

// ParseRequest validates and decodes a single JSON-RPC request from JSON bytes.
func ParseRequest(raw []byte) (*Request, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: empty body", ErrInvalidRequest)
	}
	if raw[0] != '{' {
		return nil, ErrNotObject
	}
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("rpc: decode: %w", err)
	}
	if req.JSONRPC != Version {
		return nil, fmt.Errorf("%w: jsonrpc must be %q", ErrInvalidRequest, Version)
	}
	if req.Method == "" {
		return nil, fmt.Errorf("%w: method required", ErrInvalidRequest)
	}
	// JSON-RPC 2.0: notifications omit "id". "id": null is invalid for requests.
	if len(req.ID) > 0 && string(req.ID) == "null" {
		return nil, fmt.Errorf("%w: id must be omitted for notifications, not null", ErrInvalidRequest)
	}
	return &req, nil
}

// ErrorObject is a JSON-RPC 2.0 error object.
type ErrorObject struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

// Response is a JSON-RPC 2.0 response. Exactly one of Result or Error should be set.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ErrorObject    `json:"error,omitempty"`
}

// NewResult builds a success response preserving the request id bytes.
func NewResult(id json.RawMessage, result json.RawMessage) *Response {
	return &Response{
		JSONRPC: Version,
		ID:      id,
		Result:  result,
	}
}

// NewError builds an error response preserving the request id bytes.
func NewError(id json.RawMessage, code int, message string, data json.RawMessage) *Response {
	return &Response{
		JSONRPC: Version,
		ID:      id,
		Error: &ErrorObject{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

// Marshal serializes the response to JSON.
func (r *Response) Marshal() ([]byte, error) {
	return json.Marshal(r)
}
