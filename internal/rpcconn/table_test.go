package rpcconn

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

func TestFailAllGivesEveryWaiterTheReason(t *testing.T) {
	t.Parallel()
	table := NewTable("test")
	first, err := table.Register(json.RawMessage(`1`))
	require.NoError(t, err)
	second, err := table.Register(json.RawMessage(`2`))
	require.NoError(t, err)

	disconnected := errors.New("upstream disconnected")
	table.FailAll(disconnected)

	_, err = first.Wait(context.Background())
	require.ErrorIs(t, err, disconnected)
	_, err = second.Wait(context.Background())
	require.ErrorIs(t, err, disconnected)
}

func TestFailAllWithoutAReasonStillFailsTheWaiter(t *testing.T) {
	t.Parallel()
	table := NewTable("test")
	call, err := table.Register(json.RawMessage(`1`))
	require.NoError(t, err)

	table.FailAll(nil)

	resp, err := call.Wait(context.Background())
	require.Nil(t, resp)
	require.ErrorContains(t, err, "test: upstream unavailable",
		"a waiter must never be handed a nil response and a nil error")
}

func TestDeliverHandsTheResponseToItsWaiter(t *testing.T) {
	t.Parallel()
	table := NewTable("test")
	call, err := table.Register(json.RawMessage(`1`))
	require.NoError(t, err)

	sent := &rpc.Response{JSONRPC: rpc.JSONRPCVersion, ID: json.RawMessage(`1`), Result: json.RawMessage(`{"ok":true}`)}
	table.Deliver(sent)

	got, err := call.Wait(context.Background())
	require.NoError(t, err)
	require.Equal(t, sent, got)
}

func TestDeliverLeavesOtherWaitersAloneWhenNobodyExpectsTheID(t *testing.T) {
	t.Parallel()
	table := NewTable("test")
	_, err := table.Register(json.RawMessage(`1`))
	require.NoError(t, err)

	table.Deliver(&rpc.Response{JSONRPC: rpc.JSONRPCVersion, ID: json.RawMessage(`7`)})

	require.True(t, table.InFlight(json.RawMessage(`1`)),
		"an unsolicited response must not disturb the calls in flight")
}

func TestDeliverCorrelatesARespelledID(t *testing.T) {
	t.Parallel()
	table := NewTable("test")
	call, err := table.Register(json.RawMessage(`1`))
	require.NoError(t, err)

	table.Deliver(&rpc.Response{
		JSONRPC: rpc.JSONRPCVersion,
		ID:      json.RawMessage(`1.0`),
		Result:  json.RawMessage(`{"ok":true}`),
	})

	got, err := call.Wait(context.Background())
	require.NoError(t, err)
	require.NotNil(t, got, "id 1 echoed as 1.0 must still reach the caller waiting on 1")
}

func TestRegisterRefusesADuplicateID(t *testing.T) {
	t.Parallel()
	table := NewTable("test")
	_, err := table.Register(json.RawMessage(`1`))
	require.NoError(t, err)
	_, err = table.Register(json.RawMessage(`1`))
	require.ErrorContains(t, err, "duplicate jsonrpc id")
}

func TestReleaseFreesTheIDForReuse(t *testing.T) {
	t.Parallel()
	table := NewTable("test")
	call, err := table.Register(json.RawMessage(`1`))
	require.NoError(t, err)
	call.Release()
	_, err = table.Register(json.RawMessage(`1`))
	require.NoError(t, err)
}

func TestWaitHonoursTheCallerContext(t *testing.T) {
	t.Parallel()
	table := NewTable("test")
	call, err := table.Register(json.RawMessage(`1`))
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err = call.Wait(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
