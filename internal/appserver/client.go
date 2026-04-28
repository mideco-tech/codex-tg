package appserver

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type Event struct {
	Channel string         `json:"channel"`
	Method  string         `json:"method,omitempty"`
	Params  map[string]any `json:"params,omitempty"`
	ID      any            `json:"id,omitempty"`
}

type rpcResponse struct {
	Result any
	Error  error
}

type Client struct {
	codexBin       string
	listenURL      string
	cwd            string
	requestTimeout time.Duration

	mu             sync.Mutex
	cmd            *exec.Cmd
	stdin          io.WriteCloser
	stdout         io.ReadCloser
	stderr         io.ReadCloser
	pending        map[uint64]chan rpcResponse
	subscribers    []chan Event
	nextID         uint64
	stderrLines    []string
	started        bool
	readerDone     chan struct{}
	stderrDone     chan struct{}
	serverRequests map[string]map[string]any
}

func NewClient(codexBin, listenURL, cwd string, requestTimeout time.Duration) *Client {
	return &Client{
		codexBin:       codexBin,
		listenURL:      listenURL,
		cwd:            cwd,
		requestTimeout: requestTimeout,
		pending:        map[uint64]chan rpcResponse{},
		serverRequests: map[string]map[string]any{},
		readerDone:     make(chan struct{}),
		stderrDone:     make(chan struct{}),
	}
}

func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return nil
	}
	cmd, err := c.buildCommand()
	if err != nil {
		c.mu.Unlock()
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		c.mu.Unlock()
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.mu.Unlock()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		c.mu.Unlock()
		return err
	}
	if err := cmd.Start(); err != nil {
		c.mu.Unlock()
		return err
	}
	c.cmd = cmd
	c.stdin = stdin
	c.stdout = stdout
	c.stderr = stderr
	c.started = true
	c.readerDone = make(chan struct{})
	c.stderrDone = make(chan struct{})
	c.mu.Unlock()

	go c.readStdout()
	go c.readStderr()
	if _, err := c.Request(ctx, "initialize", map[string]any{
		"capabilities": map[string]any{"experimentalApi": true},
		"clientInfo": map[string]any{
			"name":    "codex-tg",
			"version": "0.1.0",
		},
	}); err != nil {
		return err
	}
	return c.Notify(ctx, "initialized", nil)
}

func (c *Client) Close() error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil
	}
	cmd := c.cmd
	stdin := c.stdin
	pending := c.pending
	c.pending = map[uint64]chan rpcResponse{}
	c.started = false
	c.cmd = nil
	c.stdin = nil
	c.stdout = nil
	c.stderr = nil
	c.mu.Unlock()

	if stdin != nil {
		_ = stdin.Close()
	}
	for _, ch := range pending {
		select {
		case ch <- rpcResponse{Error: errors.New("app-server closed before response")}:
		default:
		}
		close(ch)
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	return nil
}

func (c *Client) Subscribe() <-chan Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch := make(chan Event, 128)
	c.subscribers = append(c.subscribers, ch)
	return ch
}

func (c *Client) Request(ctx context.Context, method string, params map[string]any) (any, error) {
	c.mu.Lock()
	if !c.started || c.stdin == nil {
		c.mu.Unlock()
		return nil, errors.New("app-server is not running")
	}
	c.nextID++
	id := c.nextID
	reply := make(chan rpcResponse, 1)
	c.pending[id] = reply
	stdin := c.stdin
	c.mu.Unlock()

	message := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  method,
	}
	if params != nil {
		message["params"] = params
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(stdin, string(payload)+"\n"); err != nil {
		return nil, err
	}

	timeout := c.requestTimeout
	if deadline, ok := ctx.Deadline(); ok {
		timeout = time.Until(deadline)
	}
	if timeout <= 0 {
		timeout = c.requestTimeout
	}

	select {
	case response := <-reply:
		if response.Error != nil {
			return nil, response.Error
		}
		return response.Result, nil
	case <-time.After(timeout):
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, fmt.Errorf("request timeout for %s", method)
	case <-ctx.Done():
		c.mu.Lock()
		delete(c.pending, id)
		c.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (c *Client) Notify(ctx context.Context, method string, params map[string]any) error {
	c.mu.Lock()
	if !c.started || c.stdin == nil {
		c.mu.Unlock()
		return errors.New("app-server is not running")
	}
	stdin := c.stdin
	c.mu.Unlock()
	message := map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
	}
	if params != nil {
		message["params"] = params
	}
	payload, err := json.Marshal(message)
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdin, string(payload)+"\n")
	return err
}

func (c *Client) RespondServerRequest(ctx context.Context, requestID string, result map[string]any) error {
	c.mu.Lock()
	if !c.started || c.stdin == nil {
		c.mu.Unlock()
		return errors.New("app-server is not running")
	}
	stdin := c.stdin
	delete(c.serverRequests, requestID)
	c.mu.Unlock()
	payload, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      requestID,
		"result":  result,
	})
	if err != nil {
		return err
	}
	_, err = io.WriteString(stdin, string(payload)+"\n")
	return err
}

func (c *Client) ThreadList(ctx context.Context, limit int, cursor string) (map[string]any, error) {
	params := map[string]any{"limit": limit, "sortKey": "updated_at"}
	if strings.TrimSpace(cursor) != "" {
		params["cursor"] = cursor
	}
	result, err := c.Request(ctx, "thread/list", params)
	if err != nil {
		return nil, err
	}
	return asMap(result), nil
}

func (c *Client) ThreadRead(ctx context.Context, threadID string, includeTurns bool) (map[string]any, error) {
	result, err := c.Request(ctx, "thread/read", map[string]any{
		"threadId":     threadID,
		"includeTurns": includeTurns,
	})
	if err != nil {
		return nil, err
	}
	return asMap(result), nil
}

func (c *Client) ThreadResume(ctx context.Context, threadID, cwd string) (map[string]any, error) {
	params := map[string]any{
		"threadId":               threadID,
		"persistExtendedHistory": true,
	}
	if strings.TrimSpace(cwd) != "" {
		params["cwd"] = cwd
	}
	result, err := c.Request(ctx, "thread/resume", params)
	if err != nil {
		return nil, err
	}
	return asMap(result), nil
}

func (c *Client) TurnStart(ctx context.Context, threadID, message, cwd string) (map[string]any, error) {
	params := map[string]any{
		"threadId": threadID,
		"input": []map[string]any{
			{"type": "text", "text": message, "text_elements": []any{}},
		},
	}
	if strings.TrimSpace(cwd) != "" {
		params["cwd"] = cwd
	}
	result, err := c.Request(ctx, "turn/start", params)
	if err != nil {
		return nil, err
	}
	return asMap(result), nil
}

func (c *Client) ThreadStart(ctx context.Context, cwd string) (map[string]any, error) {
	params := map[string]any{
		"experimentalRawEvents":  false,
		"persistExtendedHistory": true,
	}
	if strings.TrimSpace(cwd) != "" {
		params["cwd"] = cwd
	}
	result, err := c.Request(ctx, "thread/start", params)
	if err != nil {
		return nil, err
	}
	return asMap(result), nil
}

func (c *Client) TurnInterrupt(ctx context.Context, threadID, turnID string) error {
	_, err := c.Request(ctx, "turn/interrupt", map[string]any{
		"threadId": threadID,
		"turnId":   turnID,
	})
	return err
}

func (c *Client) TurnSteer(ctx context.Context, threadID, turnID, message string) (map[string]any, error) {
	result, err := c.Request(ctx, "turn/steer", map[string]any{
		"threadId":       threadID,
		"expectedTurnId": turnID,
		"input": []map[string]any{
			{
				"type":          "text",
				"text":          message,
				"text_elements": []any{},
			},
		},
	})
	if err != nil {
		return nil, err
	}
	return asMap(result), nil
}

func (c *Client) StderrTail() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]string, len(c.stderrLines))
	copy(out, c.stderrLines)
	return out
}

func (c *Client) buildCommand() (*exec.Cmd, error) {
	executable, err := exec.LookPath(c.codexBin)
	if err != nil {
		executable = c.codexBin
	}
	if runtime.GOOS == "windows" {
		ext := strings.ToLower(filepath.Ext(executable))
		if ext == ".cmd" || ext == ".bat" {
			command := fmt.Sprintf("%s app-server --listen %s", executable, c.listenURL)
			cmd := exec.Command(os.Getenv("ComSpec"), "/d", "/c", command)
			cmd.Dir = c.cwd
			return cmd, nil
		}
	}
	cmd := exec.Command(executable, "app-server", "--listen", c.listenURL)
	cmd.Dir = c.cwd
	return cmd, nil
}

func (c *Client) readStdout() {
	defer close(c.readerDone)
	c.mu.Lock()
	stdout := c.stdout
	c.mu.Unlock()
	if stdout == nil {
		return
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var payload map[string]any
		if err := json.Unmarshal(line, &payload); err != nil {
			c.broadcast(Event{Channel: "transport_error", Params: map[string]any{"line": string(line), "error": err.Error()}})
			continue
		}
		c.handlePayload(payload)
	}
	if err := scanner.Err(); err != nil {
		c.broadcast(Event{Channel: "transport_error", Params: map[string]any{"stream": "stdout", "error": err.Error()}})
	}
}

func (c *Client) readStderr() {
	defer close(c.stderrDone)
	c.mu.Lock()
	stderr := c.stderr
	c.mu.Unlock()
	if stderr == nil {
		return
	}
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 512), 64*1024)
	for scanner.Scan() {
		line := scanner.Text()
		c.mu.Lock()
		c.stderrLines = append(c.stderrLines, line)
		if len(c.stderrLines) > 100 {
			c.stderrLines = c.stderrLines[len(c.stderrLines)-100:]
		}
		c.mu.Unlock()
	}
}

func (c *Client) handlePayload(payload map[string]any) {
	if id, ok := payload["id"]; ok {
		if _, hasResult := payload["result"]; hasResult || payload["error"] != nil {
			responseID := uint64FromAny(id)
			c.mu.Lock()
			reply := c.pending[responseID]
			delete(c.pending, responseID)
			c.mu.Unlock()
			if reply != nil {
				if payload["error"] != nil {
					reply <- rpcResponse{Error: fmt.Errorf("%v", payload["error"])}
				} else {
					reply <- rpcResponse{Result: payload["result"]}
				}
				close(reply)
			}
			return
		}
		if method, ok := payload["method"].(string); ok {
			requestID := fmt.Sprintf("%v", id)
			params := asMap(payload["params"])
			c.mu.Lock()
			c.serverRequests[requestID] = params
			c.mu.Unlock()
			c.broadcast(Event{Channel: "server_request", Method: method, Params: params, ID: id})
			return
		}
	}
	method, _ := payload["method"].(string)
	params := asMap(payload["params"])
	if strings.EqualFold(method, "serverRequest/resolved") {
		if requestID := fmt.Sprintf("%v", params["requestId"]); requestID != "" {
			c.mu.Lock()
			delete(c.serverRequests, requestID)
			c.mu.Unlock()
		}
	}
	c.broadcast(Event{Channel: "notification", Method: method, Params: params})
}

func (c *Client) broadcast(event Event) {
	c.mu.Lock()
	subs := append([]chan Event(nil), c.subscribers...)
	c.mu.Unlock()
	for _, subscriber := range subs {
		select {
		case subscriber <- event:
		default:
		}
	}
}

func asMap(value any) map[string]any {
	if value == nil {
		return map[string]any{}
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	return map[string]any{}
}

func uint64FromAny(value any) uint64 {
	switch typed := value.(type) {
	case float64:
		return uint64(typed)
	case int:
		return uint64(typed)
	case int64:
		return uint64(typed)
	case uint64:
		return typed
	default:
		return 0
	}
}
