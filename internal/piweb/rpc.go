package piweb

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
)

// rpcClient speaks the pi coding agent RPC protocol over a child process's
// stdin/stdout: LF-delimited JSON commands in, LF-delimited JSON responses and
// events out. Responses carry the request id; everything else is an event and
// is handed to onEvent.
type rpcClient struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *boundedBuffer

	writeMu sync.Mutex

	mu      sync.Mutex
	pending map[string]chan rpcResponse
	closed  bool
	waitErr error
	done    chan struct{}

	reqSeq  atomic.Int64
	onEvent func(raw []byte)
}

type rpcResponse struct {
	Command string          `json:"command"`
	Success bool            `json:"success"`
	Error   string          `json:"error"`
	Data    json.RawMessage `json:"data"`
}

// rpcScannerBuffer bounds a single protocol line; tool output is truncated by
// pi itself, but generous headroom avoids splitting valid records.
const rpcScannerBuffer = 32 << 20

var errRPCClosed = errors.New("pi rpc process is not running")

// startRPCClient launches the pi command and begins reading its event stream.
// onEvent receives every non-response line verbatim and must not block for
// long; it is called from the single read loop goroutine.
func startRPCClient(command []string, dir string, env []string, onEvent func(raw []byte)) (*rpcClient, error) {
	if len(command) == 0 {
		return nil, errors.New("empty pi command")
	}
	cmd := exec.Command(command[0], command[1:]...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := newBoundedBuffer(64 << 10)
	cmd.Stderr = stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", command[0], err)
	}

	c := &rpcClient{
		cmd:     cmd,
		stdin:   stdin,
		stderr:  stderr,
		pending: make(map[string]chan rpcResponse),
		done:    make(chan struct{}),
		onEvent: onEvent,
	}
	go c.readLoop(stdout)
	return c, nil
}

func (c *rpcClient) readLoop(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64<<10), rpcScannerBuffer)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) > 0 && line[len(line)-1] == '\r' {
			line = line[:len(line)-1]
		}
		if len(line) == 0 {
			continue
		}
		c.dispatch(line)
	}
	err := c.cmd.Wait()

	c.mu.Lock()
	c.closed = true
	c.waitErr = err
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.done)
}

// dispatch routes one protocol line: responses complete pending requests,
// dialog-style extension UI requests are auto-cancelled so the agent never
// blocks on a UI we do not provide, and everything is forwarded as an event.
func (c *rpcClient) dispatch(line []byte) {
	var head struct {
		Type   string `json:"type"`
		ID     string `json:"id"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(line, &head); err != nil {
		return
	}

	switch head.Type {
	case "response":
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			return
		}
		c.mu.Lock()
		ch, ok := c.pending[head.ID]
		if ok {
			delete(c.pending, head.ID)
		}
		c.mu.Unlock()
		if ok {
			ch <- resp
			close(ch)
		}
		return
	case "extension_ui_request":
		switch head.Method {
		case "select", "confirm", "input", "editor":
			cancel := map[string]any{
				"type":      "extension_ui_response",
				"id":        head.ID,
				"cancelled": true,
			}
			_ = c.writeLine(cancel)
		}
	}

	if c.onEvent != nil {
		buf := make([]byte, len(line))
		copy(buf, line)
		c.onEvent(buf)
	}
}

func (c *rpcClient) writeLine(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := c.stdin.Write(append(data, '\n')); err != nil {
		return fmt.Errorf("write to pi: %w", err)
	}
	return nil
}

// request sends a command with a correlation id and waits for its response.
func (c *rpcClient) request(ctx context.Context, cmd map[string]any) (rpcResponse, error) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return rpcResponse{}, errRPCClosed
	}
	id := fmt.Sprintf("pw-%d", c.reqSeq.Add(1))
	ch := make(chan rpcResponse, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	msg := make(map[string]any, len(cmd)+1)
	maps.Copy(msg, cmd)
	msg["id"] = id
	if err := c.writeLine(msg); err != nil {
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return rpcResponse{}, err
	}

	select {
	case resp, ok := <-ch:
		if !ok {
			return rpcResponse{}, fmt.Errorf("%w: %s", errRPCClosed, c.exitDetail())
		}
		return resp, nil
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return rpcResponse{}, ctx.Err()
	}
}

// call sends a command and decodes the response data into out (when non-nil),
// converting protocol-level failures into errors.
func (c *rpcClient) call(ctx context.Context, cmd map[string]any, out any) error {
	resp, err := c.request(ctx, cmd)
	if err != nil {
		return err
	}
	if !resp.Success {
		if resp.Error != "" {
			return fmt.Errorf("pi %s: %s", resp.Command, resp.Error)
		}
		return fmt.Errorf("pi %s failed", resp.Command)
	}
	if out != nil && len(resp.Data) > 0 {
		return json.Unmarshal(resp.Data, out)
	}
	return nil
}

func (c *rpcClient) alive() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.closed
}

func (c *rpcClient) exitDetail() string {
	detail := "process exited"
	if c.waitErr != nil {
		detail = c.waitErr.Error()
	}
	if tail := c.stderr.String(); tail != "" {
		detail += "; stderr: " + tail
	}
	return detail
}

// close terminates the child process group and waits for the read loop to
// finish. Safe to call multiple times.
func (c *rpcClient) close() {
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if !closed {
		_ = c.stdin.Close()
		if c.cmd.Process != nil {
			_ = syscall.Kill(-c.cmd.Process.Pid, syscall.SIGTERM)
		}
	}
	<-c.done
}

// boundedBuffer keeps the most recent writes up to a fixed size, so stderr
// from a failing pi process can be reported without unbounded growth.
type boundedBuffer struct {
	mu   sync.Mutex
	max  int
	data []byte
}

func newBoundedBuffer(max int) *boundedBuffer {
	return &boundedBuffer{max: max}
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.data = append(b.data, p...)
	if len(b.data) > b.max {
		b.data = b.data[len(b.data)-b.max:]
	}
	return len(p), nil
}

func (b *boundedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.data)
}
