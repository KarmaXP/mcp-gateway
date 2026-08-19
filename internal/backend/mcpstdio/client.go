package mcpstdio

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/semaphore"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

const (
	weightedSemaphoreTickets int64 = 1
	pendingJSONRPCChannelCap int = 1
)

type StdioMCPUpstream struct {
	id      string
	prefix  string
	command []string
	env     []string

	lifecycle context.Context
	sem       *semaphore.Weighted

	startOnce sync.Once
	startErr  error

	deadMu  sync.Mutex
	deadErr error

	closeOnce sync.Once
	waitOnce  sync.Once

	writeMu sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	br      *bufio.Reader

	droppedResponses atomic.Uint64

	pendMu     sync.Mutex
	pending    map[string]chan *rpc.Response
	pendingErr map[string]error
	readWG     sync.WaitGroup

	onNotifMu sync.Mutex
	onNotif   func(*rpc.Request)
}

func NewStdioMCPUpstream(lifecycle context.Context, id, prefix string, command, extraEnv []string, maxConcurrency int64) (*StdioMCPUpstream, func(), error) {
	if lifecycle == nil {
		lifecycle = context.Background()
	}
	if len(command) == 0 {
		return nil, nil, fmt.Errorf("mcpstdio: empty command")
	}
	if maxConcurrency <= 0 {
		maxConcurrency = defaults.UpstreamMaxConcurrency
	}
	c := &StdioMCPUpstream{
		id:         id,
		prefix:     prefix,
		command:    slices.Clone(command),
		env:        slices.Clone(extraEnv),
		lifecycle:  lifecycle,
		sem:        semaphore.NewWeighted(maxConcurrency),
		pending:    make(map[string]chan *rpc.Response),
		pendingErr: make(map[string]error),
	}
	return c, func() { c.close() }, nil
}

func (c *StdioMCPUpstream) ID() string     { return c.id }
func (c *StdioMCPUpstream) Prefix() string { return c.prefix }

func (c *StdioMCPUpstream) DroppedResponses() uint64 {
	return c.droppedResponses.Load()
}

func (c *StdioMCPUpstream) SetOnNotification(fn func(*rpc.Request)) {
	c.onNotifMu.Lock()
	c.onNotif = fn
	c.onNotifMu.Unlock()
}

func (c *StdioMCPUpstream) ensure(ctx context.Context) error {
	if err := c.deadError(); err != nil {
		return err
	}
	c.startOnce.Do(func() { c.startErr = c.startLocked() })
	if c.startErr != nil {
		return c.startErr
	}
	return c.deadError()
}

func (c *StdioMCPUpstream) deadError() error {
	c.deadMu.Lock()
	defer c.deadMu.Unlock()
	return c.deadErr
}

func (c *StdioMCPUpstream) markDead(err error) {
	if err == nil {
		err = errors.New("mcpstdio: upstream unavailable")
	}
	c.deadMu.Lock()
	if c.deadErr == nil {
		c.deadErr = err
	}
	c.deadMu.Unlock()
	c.failPending()
}

func (c *StdioMCPUpstream) failPending() {
	c.pendMu.Lock()
	defer c.pendMu.Unlock()
	for key, ch := range c.pending {
		delete(c.pending, key)
		close(ch)
	}
	clear(c.pendingErr)
}

func (c *StdioMCPUpstream) reapProcess() {
	c.waitOnce.Do(func() {
		if c.cmd != nil {
			_ = c.cmd.Wait()
		}
	})
}

func (c *StdioMCPUpstream) startLocked() error {
	cmd := exec.CommandContext(c.lifecycle, c.command[0], c.command[1:]...)
	base := slices.Clone(os.Environ())
	cmd.Env = append(base, c.env...)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcpstdio %s: stdin: %w", c.id, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcpstdio %s: stdout: %w", c.id, err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mcpstdio %s: start: %w", c.id, err)
	}
	c.cmd = cmd
	c.stdin = stdin
	c.br = bufio.NewReader(stdout)

	c.readWG.Add(1)
	go func() {
		defer c.readWG.Done()
		c.readLoop()
	}()
	return nil
}

func (c *StdioMCPUpstream) readLoop() {
	defer c.onReaderExit()
	for {
		line, err := c.br.ReadBytes('\n')
		if err != nil {
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		c.dispatch(line)
	}
}

func (c *StdioMCPUpstream) onReaderExit() {
	c.reapProcess()
	c.markDead(fmt.Errorf("mcpstdio %s: process exited", c.id))
}

func (c *StdioMCPUpstream) dispatch(raw []byte) {
	resp, err := rpc.ParseResponse(raw)
	if err == nil {
		key, idErr := rpc.CanonicalIDKey(resp.ID)
		if idErr != nil {
			return
		}
		c.pendMu.Lock()
		ch := c.pending[key]
		delete(c.pending, key)
		c.pendMu.Unlock()
		if ch == nil {
			return
		}
		select {
		case ch <- resp:
		default:
			c.droppedResponses.Add(1)
			deliverErr := fmt.Errorf("mcpstdio %s: pending channel full for id %s", c.id, key)
			c.pendMu.Lock()
			c.pendingErr[key] = deliverErr
			c.pendMu.Unlock()
			close(ch)
		}
		return
	}
	req, err := rpc.ParseRequest(raw)
	if err != nil || !req.IsNotification() {
		return
	}
	c.onNotifMu.Lock()
	fn := c.onNotif
	c.onNotifMu.Unlock()
	if fn != nil {
		fn(req)
	}
}

func (c *StdioMCPUpstream) writeLineLocked(payload []byte) error {
	if err := c.deadError(); err != nil {
		return err
	}
	if _, err := c.stdin.Write(payload); err != nil {
		return err
	}
	_, err := c.stdin.Write([]byte{'\n'})
	return err
}

func (c *StdioMCPUpstream) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if err := c.sem.Acquire(ctx, weightedSemaphoreTickets); err != nil {
		return nil, err
	}
	defer c.sem.Release(weightedSemaphoreTickets)

	if err := c.ensure(ctx); err != nil {
		return nil, err
	}

	body, err := rpc.MarshalRequest(req)
	if err != nil {
		return nil, err
	}

	if req.IsNotification() {
		c.writeMu.Lock()
		err := c.writeLineLocked(body)
		c.writeMu.Unlock()
		if err != nil {
			return nil, err
		}
		return nil, nil
	}

	key, err := rpc.CanonicalIDKey(req.ID)
	if err != nil {
		return nil, fmt.Errorf("mcpstdio %s: jsonrpc id: %w", c.id, err)
	}
	ch := make(chan *rpc.Response, pendingJSONRPCChannelCap)
	c.pendMu.Lock()
	if _, exists := c.pending[key]; exists {
		c.pendMu.Unlock()
		return nil, fmt.Errorf("mcpstdio %s: duplicate jsonrpc id %q", c.id, key)
	}
	c.pending[key] = ch
	c.pendMu.Unlock()
	defer func() {
		c.pendMu.Lock()
		if cur, ok := c.pending[key]; ok && cur == ch {
			delete(c.pending, key)
		}
		delete(c.pendingErr, key)
		c.pendMu.Unlock()
	}()

	c.writeMu.Lock()
	err = c.writeLineLocked(body)
	c.writeMu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case resp, ok := <-ch:
		c.pendMu.Lock()
		deliverErr := c.pendingErr[key]
		delete(c.pendingErr, key)
		c.pendMu.Unlock()
		if deliverErr != nil {
			return nil, deliverErr
		}
		if !ok {
			return nil, c.deadError()
		}
		return resp, nil
	}
}

func (c *StdioMCPUpstream) close() {
	c.closeOnce.Do(func() {
		c.markDead(fmt.Errorf("mcpstdio %s: closed", c.id))
		if c.stdin != nil {
			_ = c.stdin.Close()
		}
		if c.cmd != nil && c.cmd.Process != nil {
			_ = c.cmd.Process.Kill()
		}
		c.readWG.Wait()
		c.reapProcess()
	})
}
