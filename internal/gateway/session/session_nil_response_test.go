package session

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/gateway/errcodes"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestEnqueueDispatchResponseRejectsNil(t *testing.T) {
	s := NewSession(context.Background(), "nil-resp", nil, nil)
	err := s.enqueueDispatchResponse(nil)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil rpc response")
}

func TestDeliverMuxResponseNilResponseEnqueuesGatewayError(t *testing.T) {
	s := NewSession(context.Background(), "deliver-nil", nil, nil)
	reqID := json.RawMessage(`5`)
	err := s.deliverMuxResponse(reqID, nil, nil, "tools/list failed")
	require.NoError(t, err)

	select {
	case raw := <-s.Out():
		var resp rpc.Response
		require.NoError(t, json.Unmarshal(raw, &resp))
		require.NotNil(t, resp.Error)
		require.Equal(t, errcodes.GatewayInternal, resp.Error.Code)
		require.Equal(t, "tools/list failed", resp.Error.Message)
	default:
		t.Fatal("expected JSON-RPC error on outbound channel")
	}
}

func TestDeliverMuxResponseMuxErrorEnqueuesGatewayError(t *testing.T) {
	s := NewSession(context.Background(), "deliver-err", nil, nil)
	reqID := json.RawMessage(`6`)
	err := s.deliverMuxResponse(reqID, nil, context.Canceled, "tools/call failed")
	require.NoError(t, err)

	select {
	case raw := <-s.Out():
		var resp rpc.Response
		require.NoError(t, json.Unmarshal(raw, &resp))
		require.NotNil(t, resp.Error)
		require.Equal(t, errcodes.GatewayInternal, resp.Error.Code)
		require.Equal(t, "tools/call failed", resp.Error.Message)
	default:
		t.Fatal("expected JSON-RPC error on outbound channel")
	}
}
