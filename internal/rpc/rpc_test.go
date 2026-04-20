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
