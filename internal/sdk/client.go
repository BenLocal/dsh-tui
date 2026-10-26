// Package sdk drives `dsh --profile sdk`: newline-delimited JSON-RPC 2.0 over
// the child's stdio. Requests are matched to responses by id; everything the
// server sends without an id is a notification, delivered in arrival order.
package sdk

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// Options describe the dsh child process.
type Options struct {
	Bin  string   // dsh executable
	Args []string // e.g. --profile sdk --patch <file>
	Dir  string   // process cwd: the sandbox workspace root
	Env  []string // full environment
}

// Notification is a server message without an id.
type Notification struct {
	Method string
	Params json.RawMessage
}

// RPCError is a JSON-RPC error response.
type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string { return fmt.Sprintf("%s (code %d)", e.Message, e.Code) }

type frame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int64          `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// Client is one connection to one dsh sdk process.
type Client struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *tailBuffer

	writeMu sync.Mutex
	nextID  atomic.Int64

	mu      sync.Mutex
	pending map[int64]chan frame

	notes chan Notification
	done  chan struct{} // closed when the reader stops
	err   error         // why the reader stopped; read after done
}

// ErrClosed is returned for calls after the process went away.
var ErrClosed = errors.New("dsh sdk process exited")

// Start launches the process and begins reading its stdout.
func Start(opts Options) (*Client, error) {
	cmd := exec.Command(opts.Bin, opts.Args...)
	cmd.Dir = opts.Dir
	cmd.Env = opts.Env
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr := &tailBuffer{max: 16 << 10}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start %s: %w", opts.Bin, err)
	}
	c := &Client{
		cmd:     cmd,
		stdin:   stdin,
		stderr:  stderr,
		pending: map[int64]chan frame{},
		notes:   make(chan Notification, 256),
		done:    make(chan struct{}),
	}
	go c.read(stdout)
	return c, nil
}

func (c *Client) read(stdout io.Reader) {
	// Event lines carry whole messages and tool output: no line-length cap.
	r := bufio.NewReader(stdout)
	var err error
	for {
		var line []byte
		line, err = r.ReadBytes('\n')
		if len(bytes.TrimSpace(line)) > 0 {
			c.dispatch(line)
		}
		if err != nil {
			break
		}
	}
	waitErr := c.cmd.Wait()
	if errors.Is(err, io.EOF) {
		err = waitErr
	}
	c.mu.Lock()
	c.err = err
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()
	close(c.notes)
	close(c.done)
}

func (c *Client) dispatch(line []byte) {
	var f frame
	if json.Unmarshal(line, &f) != nil {
		return // the protocol ignores malformed lines
	}
	if f.ID == nil {
		if f.Method != "" {
			c.notes <- Notification{Method: f.Method, Params: f.Params}
		}
		return
	}
	if f.Method != "" {
		return // server→client requests are unused by the protocol
	}
	c.mu.Lock()
	ch := c.pending[*f.ID]
	delete(c.pending, *f.ID)
	c.mu.Unlock()
	if ch != nil {
		ch <- f
	}
}

// Notifications delivers server notifications; closed when the process exits.
func (c *Client) Notifications() <-chan Notification { return c.notes }

// Done is closed when the process has exited.
func (c *Client) Done() <-chan struct{} { return c.done }

// Err reports why the process exited, after Done.
func (c *Client) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Stderr returns the last bytes the process wrote to stderr.
func (c *Client) Stderr() string { return c.stderr.String() }

// Call sends one request and decodes its result into out (nil to discard).
func (c *Client) Call(ctx context.Context, method string, params, out any) error {
	id := c.nextID.Add(1)
	body, err := json.Marshal(frame{JSONRPC: "2.0", ID: &id, Method: method, Params: mustRaw(params)})
	if err != nil {
		return err
	}
	ch := make(chan frame, 1)
	c.mu.Lock()
	select {
	case <-c.done:
		c.mu.Unlock()
		return ErrClosed
	default:
	}
	c.pending[id] = ch
	c.mu.Unlock()

	c.writeMu.Lock()
	_, err = c.stdin.Write(append(body, '\n'))
	c.writeMu.Unlock()
	if err != nil {
		c.forget(id)
		return fmt.Errorf("%s: %w", method, ErrClosed)
	}

	select {
	case f, ok := <-ch:
		if !ok {
			return fmt.Errorf("%s: %w", method, ErrClosed)
		}
		if f.Error != nil {
			return fmt.Errorf("%s: %w", method, f.Error)
		}
		if out != nil && len(f.Result) > 0 {
			return json.Unmarshal(f.Result, out)
		}
		return nil
	case <-ctx.Done():
		c.forget(id)
		return fmt.Errorf("%s: %w", method, ctx.Err())
	}
}

func (c *Client) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// Close asks the server to shut down, then kills it if it lingers.
func (c *Client) Close(timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = c.Call(ctx, "shutdown", struct{}{}, nil)
	_ = c.stdin.Close() // EOF also shuts the sdk app down
	select {
	case <-c.done:
	case <-ctx.Done():
		_ = c.cmd.Process.Kill()
		<-c.done
	}
}

func mustRaw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		panic(err) // params are our own structs
	}
	return b
}

// tailBuffer keeps the last max bytes written.
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (t *tailBuffer) Write(p []byte) (int, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.buf = append(t.buf, p...)
	if over := len(t.buf) - t.max; over > 0 {
		t.buf = t.buf[over:]
	}
	return len(p), nil
}

func (t *tailBuffer) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.buf)
}
