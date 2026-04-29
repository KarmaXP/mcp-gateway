package rpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
)

const JSONRPCVersion = "2.0"

var (
	ErrInvalidRequest = errors.New("rpc: invalid JSON-RPC request")
	ErrNotObject      = errors.New("rpc: body must be a JSON object")
)

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	ID      json.RawMessage `json:"id,omitempty"`
}

func (r *Request) IsNotification() bool {
	return len(r.ID) == 0
}

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
	if req.JSONRPC != JSONRPCVersion {
		return nil, fmt.Errorf("%w: jsonrpc must be %q", ErrInvalidRequest, JSONRPCVersion)
	}
	if req.Method == "" {
		return nil, fmt.Errorf("%w: method required", ErrInvalidRequest)
	}
	if len(req.ID) > 0 && string(req.ID) == "null" {
		return nil, fmt.Errorf("%w: id must be omitted for notifications, not null", ErrInvalidRequest)
	}
	return &req, nil
}

type ErrorObject struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *ErrorObject    `json:"error,omitempty"`
}

func NewResult(id json.RawMessage, result json.RawMessage) *Response {
	return &Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Result:  result,
	}
}

func NewError(id json.RawMessage, code int, message string, data json.RawMessage) *Response {
	return &Response{
		JSONRPC: JSONRPCVersion,
		ID:      id,
		Error: &ErrorObject{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}

func (r *Response) Marshal() ([]byte, error) {
	return json.Marshal(r)
}

func ParseResponse(raw []byte) (*Response, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: empty body", ErrInvalidRequest)
	}
	if raw[0] != '{' {
		return nil, ErrNotObject
	}
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("rpc: decode response: %w", err)
	}
	if resp.JSONRPC != JSONRPCVersion {
		return nil, fmt.Errorf("%w: jsonrpc must be %q", ErrInvalidRequest, JSONRPCVersion)
	}
	if resp.Error == nil && resp.Result == nil {
		return nil, fmt.Errorf("%w: response must include result or error", ErrInvalidRequest)
	}
	return &resp, nil
}

func MarshalRequest(req *Request) ([]byte, error) {
	ver := req.JSONRPC
	if ver == "" {
		ver = JSONRPCVersion
	}
	type wire struct {
		JSONRPC string          `json:"jsonrpc"`
		Method  string          `json:"method"`
		Params  json.RawMessage `json:"params,omitempty"`
		ID      json.RawMessage `json:"id,omitempty"`
	}
	return json.Marshal(wire{
		JSONRPC: ver,
		Method:  req.Method,
		Params:  req.Params,
		ID:      req.ID,
	})
}
