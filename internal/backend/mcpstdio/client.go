// Package mcpstdio implements backend.Backend over MCP stdio (newline-delimited JSON-RPC).
package mcpstdio

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"sync"

	"golang.org/x/sync/semaphore"

	"github.com/KarmaXP/mcp-gateway/internal/rpc"
)

// Client runs a subprocess MCP server and multiplexes JSON-RPC over stdin/stdout.
type Client struct {
	id      string
	prefix  string
	command []string
	env     []string

	lifecycle context.Context
	sem       *semaphore.Weighted

	startOnce sync.Once
	startErr  error

	writeMu sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	br      *bufio.Reader

	pendMu  sync.Mutex
	pending map[string]chan *rpc.Response
	readWG  sync.WaitGroup
}

// New starts no process until the first Call. cleanup kills the subprocess and waits for the reader.
// maxConcurrency defaults to 8 when <= 0.
func New(lifecycle context.Context, id, prefix string, command, extraEnv []string, maxConcurrency int64) (*Client, func(), error) {
	if lifecycle == nil {
		lifecycle = context.Background()
	}
	if len(command) == 0 {
		return nil, nil, fmt.Errorf("mcpstdio: empty command")
	}
	if maxConcurrency <= 0 {
		maxConcurrency = 8
	}
	c := &Client{
		id:        id,
		prefix:    prefix,
		command:   slices.Clone(command),
		env:       slices.Clone(extraEnv),
		lifecycle: lifecycle,
		sem:       semaphore.NewWeighted(maxConcurrency),
		pending:   make(map[string]chan *rpc.Response),
	}
	return c, func() { c.close() }, nil
}

func (c *Client) ID() string     { return c.id }
func (c *Client) Prefix() string { return c.prefix }

func (c *Client) ensure(ctx context.Context) error {
	c.startOnce.Do(func() { c.startErr = c.startLocked() })
	return c.startErr
}

func (c *Client) startLocked() error {
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

func (c *Client) readLoop() {
	for {
		line, err := c.br.ReadBytes('\n')
		if err != nil {
			return
		}
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		resp, err := rpc.ParseResponse(line)
		if err != nil {
			continue
		}
		key := idKey(resp.ID)
		if key == "" {
			continue
		}
		c.pendMu.Lock()
		ch := c.pending[key]
		delete(c.pending, key)
		c.pendMu.Unlock()
		if ch == nil {
			continue
		}
		select {
		case ch <- resp:
		default:
		}
	}
}

func idKey(id json.RawMessage) string {
	if len(id) == 0 {
		return ""
	}
	return string(id)
}

func (c *Client) writeLineLocked(payload []byte) error {
	if _, err := c.stdin.Write(payload); err != nil {
		return err
	}
	_, err := c.stdin.Write([]byte{'\n'})
	return err
}

// Call performs one JSON-RPC round-trip over stdio.
func (c *Client) Call(ctx context.Context, req *rpc.Request) (*rpc.Response, error) {
	if err := c.sem.Acquire(ctx, 1); err != nil {
		return nil, err
	}
	defer c.sem.Release(1)

	if err := c.ensure(ctx); err != nil {
		return nil, err
	}

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

	key := idKey(req.ID)
	if key == "" {
		return nil, fmt.Errorf("mcpstdio %s: missing jsonrpc id", c.id)
	}
	ch := make(chan *rpc.Response, 1)
	c.pendMu.Lock()
	c.pending[key] = ch
	c.pendMu.Unlock()
	defer func() {
		c.pendMu.Lock()
		if cur, ok := c.pending[key]; ok && cur == ch {
			delete(c.pending, key)
		}
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
	case resp := <-ch:
		return resp, nil
	}
}

func (c *Client) close() {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	c.readWG.Wait()
}
