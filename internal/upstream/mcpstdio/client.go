package mcpstdio

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"sync/atomic"

	"golang.org/x/sync/semaphore"

	"github.com/KarmaXP/mcp-gateway/internal/defaults"
	"github.com/KarmaXP/mcp-gateway/internal/rpc"
	"github.com/KarmaXP/mcp-gateway/internal/rpcconn"
	"github.com/KarmaXP/mcp-gateway/internal/upstream/framing"
)

const (
	weightedSemaphoreTickets int64 = 1
)

type proc struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	stderr io.ReadCloser
	br     *bufio.Reader
}

type writeRequest struct {
	payload []byte
	done    chan error
}

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

	procMu  sync.Mutex
	proc    atomic.Pointer[proc]
	writes  chan writeRequest
	writeWG sync.WaitGroup
	stopped chan struct{}

	calls  *rpcconn.Table
	readWG sync.WaitGroup

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
		id:        id,
		prefix:    prefix,
		command:   slices.Clone(command),
		env:       slices.Clone(extraEnv),
		lifecycle: lifecycle,
		sem:       semaphore.NewWeighted(maxConcurrency),
		writes:    make(chan writeRequest, maxConcurrency),
		stopped:   make(chan struct{}),
		calls:     rpcconn.NewTable("mcpstdio " + id),
	}
	return c, func() { c.close() }, nil
}

func (c *StdioMCPUpstream) ID() string     { return c.id }
func (c *StdioMCPUpstream) Prefix() string { return c.prefix }

func (c *StdioMCPUpstream) SetOnNotification(fn func(*rpc.Request)) {
	c.onNotifMu.Lock()
	c.onNotif = fn
	c.onNotifMu.Unlock()
}

func (c *StdioMCPUpstream) ensure(ctx context.Context) error {
	if err := c.deadError(); err != nil {
		return err
	}
	c.startOnce.Do(func() { c.startErr = c.start() })
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
	c.calls.FailAll(c.deadError())
}

func (c *StdioMCPUpstream) reapProcess() {
	c.waitOnce.Do(func() {
		if p := c.proc.Load(); p != nil {
			_ = p.cmd.Wait()
		}
	})
}

func (c *StdioMCPUpstream) start() error {
	cmd := exec.CommandContext(c.lifecycle, c.command[0], c.command[1:]...)
	cmd.Env = c.childEnv()

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("mcpstdio %s: stdin: %w", c.id, err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("mcpstdio %s: stderr: %w", c.id, err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("mcpstdio %s: stdout: %w", c.id, err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("mcpstdio %s: start: %w", c.id, err)
	}
	p := &proc{cmd: cmd, stdin: stdin, stdout: stdout, stderr: stderr, br: bufio.NewReader(stdout)}

	c.procMu.Lock()
	select {
	case <-c.stopped:
		c.procMu.Unlock()
		_ = stdin.Close()
		_ = stdout.Close()
		_ = stderr.Close()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
		return fmt.Errorf("mcpstdio %s: closed", c.id)
	default:
	}
	c.proc.Store(p)
	c.readWG.Add(1)
	c.readWG.Add(1)
	c.writeWG.Add(1)
	c.procMu.Unlock()

	go func() {
		defer c.readWG.Done()
		c.readLoop(p)
	}()
	go func() {
		defer c.readWG.Done()
		c.logStderr(p.stderr)
	}()
	go func() {
		defer c.writeWG.Done()
		c.writeLoop(p)
	}()
	return nil
}

func (c *StdioMCPUpstream) logStderr(r io.Reader) {
	br := bufio.NewReader(r)
	for {
		line, err := framing.ReadLineCapped(br, defaults.MaxUpstreamStderrLineBytes)
		if text := strings.TrimSpace(string(line)); text != "" {
			slog.Warn("upstream stderr", "upstream_id", c.id, "line", text)
		}
		if err != nil {
			return
		}
	}
}

func (c *StdioMCPUpstream) childEnv() []string {
	env := make([]string, 0, len(defaults.UpstreamStdioInheritedEnv)+len(c.env))
	for _, key := range defaults.UpstreamStdioInheritedEnv {
		if value, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+value)
		}
	}
	return append(env, c.env...)
}

func (c *StdioMCPUpstream) readLoop(p *proc) {
	defer c.onReaderExit()
	for {
		line, err := framing.ReadFrame(p.br, defaults.MaxUpstreamFrameBytes)
		if err != nil {
			if errors.Is(err, framing.ErrFrameTooLarge) {
				c.markDead(fmt.Errorf("mcpstdio %s: %w", c.id, err))
			}
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
		c.calls.Deliver(resp)
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

func (c *StdioMCPUpstream) writeLoop(p *proc) {
	for {
		select {
		case <-c.stopped:
			return
		case <-c.lifecycle.Done():
			return
		case req := <-c.writes:
			req.done <- writeFrame(p.stdin, req.payload)
		}
	}
}

func writeFrame(w io.Writer, payload []byte) error {
	if _, err := w.Write(payload); err != nil {
		return err
	}
	_, err := w.Write([]byte{'\n'})
	return err
}

func (c *StdioMCPUpstream) writeLine(ctx context.Context, payload []byte) error {
	if err := c.deadError(); err != nil {
		return err
	}
	req := writeRequest{payload: payload, done: make(chan error, 1)}
	select {
	case c.writes <- req:
	case <-ctx.Done():
		return ctx.Err()
	case <-c.stopped:
		return c.deadError()
	}
	select {
	case err := <-req.done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
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
		if err := c.writeLine(ctx, body); err != nil {
			return nil, err
		}
		return nil, nil
	}

	call, err := c.calls.Register(req.ID)
	if err != nil {
		return nil, err
	}
	defer call.Release()

	if err := c.writeLine(ctx, body); err != nil {
		return nil, err
	}
	return call.Wait(ctx)
}

func (c *StdioMCPUpstream) close() {
	c.closeOnce.Do(func() {
		c.markDead(fmt.Errorf("mcpstdio %s: closed", c.id))
		c.procMu.Lock()
		close(c.stopped)
		p := c.proc.Load()
		c.procMu.Unlock()
		if p != nil {
			_ = p.stdin.Close()
			_ = p.stdout.Close()
			_ = p.stderr.Close()
			if p.cmd.Process != nil {
				_ = p.cmd.Process.Kill()
			}
		}
		c.readWG.Wait()
		c.writeWG.Wait()
		c.reapProcess()
	})
}
