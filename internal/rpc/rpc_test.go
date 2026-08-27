package rpc

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestParseRequest(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","method":"tools/list","id":42}`)
	req, err := ParseRequest(raw)
	require.NoError(t, err)
	require.Equal(t, "tools/list", req.Method)
	require.False(t, req.IsNotification())
	require.JSONEq(t, "42", string(req.ID))
}

func TestParseRequestInvalid(t *testing.T) {
	tests := []struct {
		name   string
		raw    string
		wraps  error
		unwrap bool
	}{
		{"empty", "", ErrInvalidRequest, true},
		{"not_object", `[]`, ErrNotObject, true},
		{"wrong_jsonrpc", `{"jsonrpc":"1.0","method":"x","id":1}`, ErrInvalidRequest, true},
		{"missing_method", `{"jsonrpc":"2.0","id":1}`, ErrInvalidRequest, true},
		{"id_null", `{"jsonrpc":"2.0","method":"x","id":null}`, ErrInvalidRequest, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRequest([]byte(tc.raw))
			require.Error(t, err)
			if tc.unwrap {
				require.ErrorIs(t, err, tc.wraps)
			}
		})
	}
}

func TestParseRequestNotification(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","method":"notifications/initialized"}`)
	req, err := ParseRequest(raw)
	require.NoError(t, err)
	require.True(t, req.IsNotification())
}

func TestNewResultPreservesID(t *testing.T) {
	id := json.RawMessage(`"req-1"`)
	res := NewResult(id, json.RawMessage(`{"ok":true}`))
	b, err := res.Marshal()
	require.NoError(t, err)
	var out map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &out))
	require.JSONEq(t, `"req-1"`, string(out["id"]))
	require.JSONEq(t, `{"ok":true}`, string(out["result"]))
}

func TestParseResponseResult(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":7,"result":{"tools":[]}}`)
	resp, err := ParseResponse(raw)
	require.NoError(t, err)
	require.NotNil(t, resp.Result)
	require.Nil(t, resp.Error)
	require.JSONEq(t, `7`, string(resp.ID))
}

func TestParseResponseError(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":"x","error":{"code":-32001,"message":"handshake"}}`)
	resp, err := ParseResponse(raw)
	require.NoError(t, err)
	require.Nil(t, resp.Result)
	require.NotNil(t, resp.Error)
	require.Equal(t, -32001, resp.Error.Code)
	require.Equal(t, "handshake", resp.Error.Message)
}

func TestParseResponseInvalid(t *testing.T) {
	_, err := ParseResponse([]byte(`{}`))
	require.Error(t, err)
	_, err = ParseResponse([]byte(`{"jsonrpc":"2.0","id":1}`))
	require.Error(t, err)
}

func TestMarshalRequest(t *testing.T) {
	b, err := MarshalRequest(&Request{
		JSONRPC: JSONRPCVersion,
		Method:  "tools/list",
		ID:      json.RawMessage(`"g1"`),
	})
	require.NoError(t, err)
	require.JSONEq(t, `{"jsonrpc":"2.0","method":"tools/list","id":"g1"}`, string(b))
}

func TestNewErrorPreservesIDAndPayload(t *testing.T) {
	id := json.RawMessage(`99`)
	data := json.RawMessage(`{"hint":"x"}`)
	res := NewError(id, -32000, "boom", data)
	b, err := res.Marshal()
	require.NoError(t, err)
	var out struct {
		ID    json.RawMessage `json:"id"`
		Error struct {
			Code    int             `json:"code"`
			Message string          `json:"message"`
			Data    json.RawMessage `json:"data"`
		} `json:"error"`
	}
	require.NoError(t, json.Unmarshal(b, &out))
	require.JSONEq(t, `99`, string(out.ID))
	require.Equal(t, -32000, out.Error.Code)
	require.Equal(t, "boom", out.Error.Message)
	require.JSONEq(t, `{"hint":"x"}`, string(out.Error.Data))
}

func BenchmarkParseRequest(b *testing.B) {
	raw := []byte(`{"jsonrpc":"2.0","method":"tools/call","params":{"name":"upstream__tool","arguments":{"query":"hello"}},"id":"bench-1"}`)
	b.ReportAllocs()
	b.SetBytes(int64(len(raw)))

	for b.Loop() {
		req, err := ParseRequest(raw)
		if err != nil {
			b.Fatal(err)
		}
		if req.Method == "" {
			b.Fatal("empty method")
		}
	}
}

func TestParseRequestRejectsStructuredID(t *testing.T) {
	tests := []struct {
		name string
		raw  string
	}{
		{"object", `{"jsonrpc":"2.0","method":"x","id":{"evil":1}}`},
		{"array", `{"jsonrpc":"2.0","method":"x","id":[1,2]}`},
		{"bool", `{"jsonrpc":"2.0","method":"x","id":true}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRequest([]byte(tc.raw))
			require.Error(t, err)
			require.ErrorIs(t, err, ErrInvalidRequest)
		})
	}
}

func TestParseRequestAcceptsStringAndNumberID(t *testing.T) {
	for _, raw := range []string{
		`{"jsonrpc":"2.0","method":"x","id":1}`,
		`{"jsonrpc":"2.0","method":"x","id":"a"}`,
		`{"jsonrpc":"2.0","method":"x","id":-7}`,
	} {
		t.Run(raw, func(t *testing.T) {
			_, err := ParseRequest([]byte(raw))
			require.NoError(t, err)
		})
	}
}

func TestParseResponseRejectsResultAndErrorTogether(t *testing.T) {
	raw := []byte(`{"jsonrpc":"2.0","id":1,"result":{"ok":true},"error":{"code":-1,"message":"boom"}}`)
	_, err := ParseResponse(raw)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidRequest)
}

func TestNewErrorWithoutIDEmitsNullID(t *testing.T) {
	b, err := NewError(nil, -32700, "parse error", nil).Marshal()
	require.NoError(t, err)
	var out map[string]json.RawMessage
	require.NoError(t, json.Unmarshal(b, &out))
	id, present := out["id"]
	require.True(t, present, "JSON-RPC 2.0 requires an id member on every response")
	require.JSONEq(t, "null", string(id))
}

func TestMarshalRequestNilRequest(t *testing.T) {
	_, err := MarshalRequest(nil)
	require.Error(t, err)
	require.ErrorIs(t, err, ErrInvalidRequest)
}

func TestMarshalRequestDefaultsVersionAndOmitsIDForNotifications(t *testing.T) {
	b, err := MarshalRequest(&Request{Method: "notifications/initialized"})
	require.NoError(t, err)
	require.JSONEq(t, `{"jsonrpc":"2.0","method":"notifications/initialized"}`, string(b))
}

func TestCanonicalIDKey(t *testing.T) {
	tests := []struct {
		name string
		id   string
		want string
	}{
		{"integer", `1`, "n:1"},
		{"integer with trailing zero fraction", `1.0`, "n:1"},
		{"integer in exponent form", `1e0`, "n:1"},
		{"trailing zeros", `1.000`, "n:1"},
		{"surrounding whitespace", ` 1 `, "n:1"},
		{"negative", `-7`, "n:-7"},
		{"beyond float64 precision stays exact", `9007199254740993`, "n:9007199254740993"},
		{"fraction", `1.5`, "n:1.5"},
		{"string", `"abc"`, "s:abc"},
		{"string digits do not collide with the number", `"1"`, "s:1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CanonicalIDKey(json.RawMessage(tc.id))
			require.NoError(t, err)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestCanonicalIDKeyRejectsUncorrelatable(t *testing.T) {
	tests := []struct {
		name string
		id   string
	}{
		{"absent", ``},
		{"null", `null`},
		{"object", `{"a":1}`},
		{"array", `[1]`},
		{"bool", `true`},
		{"malformed", `{`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := CanonicalIDKey(json.RawMessage(tc.id))
			require.Error(t, err)
			require.ErrorIs(t, err, ErrUncorrelatableID)
		})
	}
}
