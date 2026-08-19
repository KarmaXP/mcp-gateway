package rpc

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

const (
	JSONRPCVersion = "2.0"

	jsonObjectStartByte = '{'
	jsonRawNull = "null"

	idKeyNumberPrefix = "n:"
	idKeyStringPrefix = "s:"
)

var (
	ErrInvalidRequest = errors.New("rpc: invalid JSON-RPC request")
	ErrNotObject = errors.New("rpc: body must be a JSON object")
	ErrUncorrelatableID = errors.New("id must be a string or a number")
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
	if raw[0] != jsonObjectStartByte {
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
	if len(req.ID) > 0 && string(req.ID) == jsonRawNull {
		return nil, fmt.Errorf("%w: id must be omitted for notifications, not null", ErrInvalidRequest)
	}
	if len(req.ID) > 0 {
		if _, err := CanonicalIDKey(req.ID); err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalidRequest, err)
		}
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
	ID      json.RawMessage `json:"id"`
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
	if len(r.ID) > 0 {
		return json.Marshal(r)
	}
	out := *r
	out.ID = json.RawMessage(jsonRawNull)
	return json.Marshal(&out)
}

func ParseResponse(raw []byte) (*Response, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, fmt.Errorf("%w: empty body", ErrInvalidRequest)
	}
	if raw[0] != jsonObjectStartByte {
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
	if resp.Error != nil && resp.Result != nil {
		return nil, fmt.Errorf("%w: response must not include both result and error", ErrInvalidRequest)
	}
	return &resp, nil
}

func MarshalRequest(req *Request) ([]byte, error) {
	if req == nil {
		return nil, fmt.Errorf("%w: nil request", ErrInvalidRequest)
	}
	out := *req
	if out.JSONRPC == "" {
		out.JSONRPC = JSONRPCVersion
	}
	return json.Marshal(&out)
}

func CanonicalIDKey(id json.RawMessage) (string, error) {
	trimmed := bytes.TrimSpace(id)
	if len(trimmed) == 0 || string(trimmed) == jsonRawNull {
		return "", ErrUncorrelatableID
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var decoded any
	if err := dec.Decode(&decoded); err != nil {
		return "", fmt.Errorf("%w: %w", ErrUncorrelatableID, err)
	}
	switch v := decoded.(type) {
	case string:
		return idKeyStringPrefix + v, nil
	case json.Number:
		return canonicalNumberIDKey(v)
	default:
		return "", ErrUncorrelatableID
	}
}

func canonicalNumberIDKey(n json.Number) (string, error) {
	if i, err := strconv.ParseInt(n.String(), 10, 64); err == nil {
		return idKeyNumberPrefix + strconv.FormatInt(i, 10), nil
	}
	f, err := strconv.ParseFloat(n.String(), 64)
	if err != nil {
		return "", fmt.Errorf("%w: %w", ErrUncorrelatableID, err)
	}
	return idKeyNumberPrefix + strconv.FormatFloat(f, 'g', -1, 64), nil
}
