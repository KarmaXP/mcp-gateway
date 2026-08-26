package rpcconn

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

const responsesPerCall = 1

// Call is one in-flight request, carrying the reason it failed beside its channel.
type Call struct {
	table *Table
	key   string
	ch    chan *rpc.Response
	err   error
}

// Table correlates JSON-RPC responses with the callers waiting for them.
type Table struct {
	name  string
	mu    sync.Mutex
	calls map[string]*Call
}

func NewTable(name string) *Table {
	return &Table{name: name, calls: make(map[string]*Call)}
}

func (t *Table) Register(id json.RawMessage) (*Call, error) {
	key, err := rpc.CanonicalIDKey(id)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", t.name, err)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if _, exists := t.calls[key]; exists {
		return nil, fmt.Errorf("%s: duplicate jsonrpc id %s", t.name, key)
	}
	call := &Call{table: t, key: key, ch: make(chan *rpc.Response, responsesPerCall)}
	t.calls[key] = call
	return call, nil
}

// Deliver hands the response to its waiter, and ignores one nobody is waiting for.
func (t *Table) Deliver(resp *rpc.Response) {
	if resp == nil || len(resp.ID) == 0 {
		return
	}
	key, err := rpc.CanonicalIDKey(resp.ID)
	if err != nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	call := t.calls[key]
	if call == nil {
		return
	}
	delete(t.calls, key)
	call.ch <- resp
}

// FailAll gives every waiter the same reason before closing it, so none of them has to guess.
func (t *Table) FailAll(err error) {
	if err == nil {
		err = fmt.Errorf("%s: upstream unavailable", t.name)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for key, call := range t.calls {
		call.err = err
		delete(t.calls, key)
		close(call.ch)
	}
}

// InFlight reports whether a caller is still waiting for this id.
func (t *Table) InFlight(id json.RawMessage) bool {
	key, err := rpc.CanonicalIDKey(id)
	if err != nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.calls[key]
	return ok
}

func (c *Call) Wait(ctx context.Context) (*rpc.Response, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp := <-c.ch:
		c.table.mu.Lock()
		err := c.err
		c.table.mu.Unlock()
		if err != nil {
			return nil, err
		}
		return resp, nil
	}
}

// Release drops the reservation when the caller gives up before a response arrives.
func (c *Call) Release() {
	c.table.mu.Lock()
	if cur, ok := c.table.calls[c.key]; ok && cur == c {
		delete(c.table.calls, c.key)
	}
	c.table.mu.Unlock()
}
