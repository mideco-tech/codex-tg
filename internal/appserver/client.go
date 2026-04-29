package appserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mideco-tech/codex-tg/internal/version"
)

type Event struct {
	Channel string         `json:"channel"`
	Method  string         `json:"method,omitempty"`
	Params  map[string]any `json:"params,omitempty"`
	ID      any            `json:"id,omitempty"`
}

type TurnStartOptions struct {
	CollaborationMode string
	Model             string
	ReasoningEffort   string
}

type ModelOption struct {
	ID                       string
	DisplayName              string
	Description              string
	DefaultReasoningEffort   string
	SupportedReasoningEffort []string
	IsDefault                bool
	Hidden                   bool
}

type CollaborationModeOption struct {
	Name            string
	Mode            string
	Model           string
	ReasoningEffort string
}

type rpcResponse struct {
	Result any
	Error  error
}

type Client struct {
	options        ClientOptions
	requestTimeout time.Duration

	mu              sync.Mutex
	transport       rpcTransport
	transportStatus TransportStatus
	pending         map[uint64]chan rpcResponse
	subscribers     []chan Event
	nextID          uint64
	started         bool
	readerDone      chan struct{}
	serverRequests  map[string]map[string]any
}

func NewClient(codexBin, listenURL, cwd string, requestTimeout time.Duration) *Client {
	mode := modeFromEndpoint(strings.TrimSpace(listenURL))
	if strings.TrimSpace(listenURL) == "" || strings.TrimSpace(listenURL) == "stdio://" {
		mode = TransportModeStdio
	}
	return NewClientWithOptions(ClientOptions{
		CodexBin:       codexBin,
		ListenURL:      listenURL,
		CWD:            cwd,
		RequestTimeout: requestTimeout,
		TransportMode:  mode,
	})
}

func NewClientWithOptions(options ClientOptions) *Client {
	if options.RequestTimeout <= 0 {
		options.RequestTimeout = 30 * time.Second
	}
	return &Client{
		options:        options,
		requestTimeout: options.RequestTimeout,
		pending:        map[uint64]chan rpcResponse{},
		serverRequests: map[string]map[string]any{},
		readerDone:     make(chan struct{}),
	}
}

func (c *Client) Start(ctx context.Context) error {
	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()

	transport, status, err := c.connectTransport(ctx)
	if err != nil {
		return err
	}

	c.mu.Lock()
	if c.started {
		c.mu.Unlock()
		_ = transport.Close()
		return nil
	}
	c.transport = transport
	c.transportStatus = status
	c.started = true
	c.readerDone = make(chan struct{})
	c.mu.Unlock()

	go c.readLoop()
	if _, err := c.Request(ctx, "initialize", map[string]any{
		"capabilities": map[string]any{"experimentalApi": true},
		"clientInfo": map[string]any{
			"name":    "codex-tg",
			"title":   "codex-tg Telegram bridge",
			"version": version.Version,
		},
	}); err != nil {
		_ = c.Close()
		return err
	}
	return c.Notify(ctx, "initialized", nil)
}

func (c *Client) connectTransport(ctx context.Context) (rpcTransport, TransportStatus, error) {
	options := c.options
	mode := NormalizeTransportMode(options.TransportMode)
	if mode == "" {
		mode = modeFromEndpoint(strings.TrimSpace(options.ListenURL))
		if mode == TransportModeStdio && strings.TrimSpace(options.ListenURL) == "" {
			mode = TransportModeAuto
		}
	}
	status := TransportStatus{RequestedMode: mode}
	switch mode {
	case TransportModeAuto, TransportModeDesktopBridge:
		transport, errorsOut, err := connectAutoTransport(ctx, options.CodexBin, options.ListenURL, options.CWD, options.Endpoint)
		status.ProbeErrors = errorsOut
		if err != nil {
			return nil, status, err
		}
		status.ActiveMode = transport.Name()
		status.Endpoint = transport.Endpoint()
		return transport, status, nil
	default:
		transport, err := transportForMode(mode, options.CodexBin, options.ListenURL, options.CWD, options.Endpoint)
		if err != nil {
			return nil, status, err
		}
		if err := transport.Start(ctx); err != nil {
			status.ProbeErrors = []string{err.Error()}
			return nil, status, err
		}
		status.ActiveMode = transport.Name()
		status.Endpoint = transport.Endpoint()
		return transport, status, nil
	}
}

func (c *Client) Close() error {
	c.mu.Lock()
	if !c.started {
		c.mu.Unlock()
		return nil
	}
	transport := c.transport
	pending := c.pending
	c.pending = map[uint64]chan rpcResponse{}
	c.started = false
	c.transport = nil
	c.mu.Unlock()

	for _, ch := range pending {
		select {
		case ch <- rpcResponse{Error: errors.New("app-server closed before response")}:
		default:
		}
		close(ch)
	}
	if transport != nil {
		return transport.Close()
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
	if !c.started || c.transport == nil {
		c.mu.Unlock()
		return nil, errors.New("app-server is not running")
	}
	c.nextID++
	id := c.nextID
	reply := make(chan rpcResponse, 1)
	c.pending[id] = reply
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
	if err := c.send(ctx, payload); err != nil {
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
	if !c.started || c.transport == nil {
		c.mu.Unlock()
		return errors.New("app-server is not running")
	}
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
	return c.send(ctx, payload)
}

func (c *Client) RespondServerRequest(ctx context.Context, requestID string, result map[string]any) error {
	c.mu.Lock()
	if !c.started || c.transport == nil {
		c.mu.Unlock()
		return errors.New("app-server is not running")
	}
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
	return c.send(ctx, payload)
}

func (c *Client) send(ctx context.Context, payload []byte) error {
	c.mu.Lock()
	transport := c.transport
	c.mu.Unlock()
	if transport == nil {
		return errors.New("app-server is not running")
	}
	return transport.Send(ctx, payload)
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

func (c *Client) TurnStart(ctx context.Context, threadID, message, cwd string, options TurnStartOptions) (map[string]any, error) {
	resolved, err := c.resolveTurnStartOptions(ctx, options)
	if err != nil {
		return nil, err
	}
	params, err := turnStartParams(threadID, message, cwd, resolved)
	if err != nil {
		return nil, err
	}
	result, err := c.Request(ctx, "turn/start", params)
	if err != nil {
		return nil, err
	}
	return asMap(result), nil
}

func turnStartParams(threadID, message, cwd string, options TurnStartOptions) (map[string]any, error) {
	params := map[string]any{
		"threadId": threadID,
		"input": []map[string]any{
			{"type": "text", "text": message, "text_elements": []any{}},
		},
	}
	if strings.TrimSpace(cwd) != "" {
		params["cwd"] = cwd
	}
	mode := normalizeCollaborationMode(options.CollaborationMode)
	if mode != "" {
		model := strings.TrimSpace(options.Model)
		if model == "" {
			return nil, fmt.Errorf("codex model is required for collaboration mode %q", mode)
		}
		settings := map[string]any{
			"model":                  model,
			"reasoning_effort":       normalizeReasoningEffort(options.ReasoningEffort),
			"developer_instructions": nil,
		}
		if settings["reasoning_effort"] == "" {
			settings["reasoning_effort"] = nil
		}
		params["collaborationMode"] = map[string]any{
			"mode":     mode,
			"settings": settings,
		}
	}
	return params, nil
}

func (c *Client) resolveTurnStartOptions(ctx context.Context, options TurnStartOptions) (TurnStartOptions, error) {
	options.CollaborationMode = normalizeCollaborationMode(options.CollaborationMode)
	options.Model = strings.TrimSpace(options.Model)
	options.ReasoningEffort = normalizeReasoningEffort(options.ReasoningEffort)
	if options.CollaborationMode == "" {
		return options, nil
	}
	if options.Model == "" {
		model, err := c.defaultModel(ctx)
		if err != nil {
			return options, fmt.Errorf("codex model is required for collaboration mode %q; choose one with /model or fix model/list: %w", options.CollaborationMode, err)
		}
		options.Model = model
	}
	if options.ReasoningEffort == "" {
		if effort, err := c.collaborationModeReasoningEffort(ctx, options.CollaborationMode); err == nil {
			options.ReasoningEffort = effort
		}
	}
	return options, nil
}

func (c *Client) defaultModel(ctx context.Context) (string, error) {
	models, err := c.ModelList(ctx, false)
	if err != nil {
		return "", err
	}
	first := ""
	for _, model := range models {
		if model.ID == "" {
			continue
		}
		if first == "" {
			first = model.ID
		}
		if model.IsDefault {
			return model.ID, nil
		}
	}
	if first != "" {
		return first, nil
	}
	return "", errors.New("model/list returned no models")
}

func (c *Client) collaborationModeReasoningEffort(ctx context.Context, mode string) (string, error) {
	modes, err := c.CollaborationModeList(ctx)
	if err != nil {
		return "", err
	}
	for _, preset := range modes {
		if normalizeCollaborationMode(preset.Mode) != mode {
			continue
		}
		return normalizeReasoningEffort(preset.ReasoningEffort), nil
	}
	return "", nil
}

func (c *Client) ModelList(ctx context.Context, includeHidden bool) ([]ModelOption, error) {
	params := map[string]any{"limit": 50}
	if includeHidden {
		params["includeHidden"] = true
	}
	result, err := c.Request(ctx, "model/list", params)
	if err != nil {
		return nil, err
	}
	return modelOptionsFromResult(result), nil
}

func (c *Client) CollaborationModeList(ctx context.Context) ([]CollaborationModeOption, error) {
	result, err := c.Request(ctx, "collaborationMode/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	return collaborationModeOptionsFromResult(result), nil
}

func modelOptionsFromResult(result any) []ModelOption {
	data, _ := asMap(result)["data"].([]any)
	out := make([]ModelOption, 0, len(data))
	for _, item := range data {
		model := asMap(item)
		id := strings.TrimSpace(stringValue(model["model"], stringValue(model["id"], "")))
		if id == "" {
			continue
		}
		out = append(out, ModelOption{
			ID:                       id,
			DisplayName:              strings.TrimSpace(stringValue(model["displayName"], "")),
			Description:              strings.TrimSpace(stringValue(model["description"], "")),
			DefaultReasoningEffort:   normalizeReasoningEffort(stringValue(model["defaultReasoningEffort"], "")),
			SupportedReasoningEffort: supportedReasoningEfforts(model["supportedReasoningEfforts"]),
			IsDefault:                boolValue(model["isDefault"]),
			Hidden:                   boolValue(model["hidden"]),
		})
	}
	return out
}

func collaborationModeOptionsFromResult(result any) []CollaborationModeOption {
	data, _ := asMap(result)["data"].([]any)
	out := make([]CollaborationModeOption, 0, len(data))
	for _, item := range data {
		preset := asMap(item)
		out = append(out, CollaborationModeOption{
			Name:            strings.TrimSpace(stringValue(preset["name"], "")),
			Mode:            normalizeCollaborationMode(stringValue(preset["mode"], "")),
			Model:           strings.TrimSpace(stringValue(preset["model"], "")),
			ReasoningEffort: normalizeReasoningEffort(firstStringValue(preset["reasoning_effort"], preset["reasoningEffort"])),
		})
	}
	return out
}

func supportedReasoningEfforts(value any) []string {
	items, _ := value.([]any)
	out := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, item := range items {
		option := asMap(item)
		effort := normalizeReasoningEffort(firstStringValue(option["reasoning_effort"], option["reasoningEffort"], item))
		if effort == "" {
			continue
		}
		if _, ok := seen[effort]; ok {
			continue
		}
		seen[effort] = struct{}{}
		out = append(out, effort)
	}
	return out
}

func normalizeCollaborationMode(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "plan", "plan_mode", "plan-mode":
		return "plan"
	case "default":
		return "default"
	default:
		return ""
	}
}

func normalizeReasoningEffort(value string) string {
	normalized := strings.TrimSpace(strings.ToLower(value))
	switch normalized {
	case "":
		return ""
	case "x-high", "x_high", "extra-high", "extra_high":
		return "xhigh"
	default:
		return normalized
	}
}

func firstStringValue(values ...any) string {
	for _, value := range values {
		if text := strings.TrimSpace(stringValue(value, "")); text != "" {
			return text
		}
	}
	return ""
}

func boolValue(value any) bool {
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		parsed, _ := strconv.ParseBool(typed)
		return parsed
	default:
		return false
	}
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
	if c.transport == nil {
		return nil
	}
	return c.transport.StderrTail()
}

func (c *Client) TransportStatus() TransportStatus {
	c.mu.Lock()
	defer c.mu.Unlock()
	status := c.transportStatus
	status.ProbeErrors = append([]string(nil), c.transportStatus.ProbeErrors...)
	return status
}

func (c *Client) ThreadLoadedList(ctx context.Context) ([]string, error) {
	result, err := c.Request(ctx, "thread/loaded/list", map[string]any{})
	if err != nil {
		return nil, err
	}
	payload := asMap(result)
	items, _ := payload["data"].([]any)
	if items == nil {
		items, _ = payload["threads"].([]any)
	}
	if items == nil {
		items, _ = payload["items"].([]any)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		text := strings.TrimSpace(stringValue(item, ""))
		if text == "" {
			text = strings.TrimSpace(stringValue(asMap(item)["id"], ""))
		}
		if text != "" {
			out = append(out, text)
		}
	}
	return out, nil
}

func (c *Client) readLoop() {
	defer close(c.readerDone)
	for {
		c.mu.Lock()
		transport := c.transport
		c.mu.Unlock()
		if transport == nil {
			return
		}
		line, err := transport.Recv(context.Background())
		if err != nil {
			if !errors.Is(err, io.EOF) {
				c.broadcast(Event{Channel: "transport_error", Params: map[string]any{"transport": transport.Name(), "error": err.Error()}})
			}
			return
		}
		var payload map[string]any
		if err := json.Unmarshal(line, &payload); err != nil {
			c.broadcast(Event{Channel: "transport_error", Params: map[string]any{"line": string(line), "error": err.Error()}})
			continue
		}
		c.handlePayload(payload)
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
