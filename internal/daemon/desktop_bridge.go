package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/config"
	"github.com/mideco-tech/codex-tg/internal/model"
	"github.com/mideco-tech/codex-tg/internal/version"
)

const desktopBridgeProtocol = "experimental-jsonl"

type DesktopBridgeOptions struct {
	RuntimeDir string
	PairingDir string
	ID         string
	AppName    string
	BundleID   string
}

type DesktopBridgeStatus struct {
	Enabled          bool
	Registered       bool
	ConnectedClients int
	PairingPath      string
	SocketPath       string
	LastError        string
	Protocol         string
	ProtocolReady    bool
}

func (s DesktopBridgeStatus) StateLabel() string {
	switch {
	case !s.Enabled && !s.Registered:
		return "disabled"
	case s.Registered && s.ConnectedClients > 0 && s.ProtocolReady:
		return "registered/connected"
	case s.Registered && s.ConnectedClients > 0:
		return "registered/protocol unverified"
	case s.Registered:
		return "registered/not connected"
	case s.LastError != "":
		return "not registered"
	default:
		return "not registered"
	}
}

type DesktopBridge struct {
	service *Service
	options DesktopBridgeOptions

	mu          sync.Mutex
	listener    net.Listener
	clients     map[net.Conn]struct{}
	pairingPath string
	socketPath  string
	lastError   string
	registered  bool
	protocolOK  bool
	closed      bool
}

func defaultDesktopBridgeOptions(paths config.Paths) DesktopBridgeOptions {
	runtimeDir := filepath.Join(paths.DataDir, "desktop-bridge")
	pairingDir := filepath.Join(runtimeDir, "app_pairing_extensions")
	if runtime.GOOS == "darwin" {
		if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
			pairingDir = filepath.Join(home, "Library", "Application Support", "com.openai.chat", "app_pairing_extensions")
		}
	}
	return DesktopBridgeOptions{
		RuntimeDir: runtimeDir,
		PairingDir: pairingDir,
		AppName:    "Codex Telegram",
		BundleID:   "tech.mideco.codex-tg",
	}
}

func newDesktopBridge(service *Service, options DesktopBridgeOptions) *DesktopBridge {
	if strings.TrimSpace(options.ID) == "" {
		options.ID = "codex-tg-" + randomToken()
	}
	if strings.TrimSpace(options.AppName) == "" {
		options.AppName = "Codex Telegram"
	}
	if strings.TrimSpace(options.BundleID) == "" {
		options.BundleID = "tech.mideco.codex-tg"
	}
	return &DesktopBridge{
		service: service,
		options: options,
		clients: map[net.Conn]struct{}{},
	}
}

func (b *DesktopBridge) Start(ctx context.Context) error {
	if runtime.GOOS == "windows" {
		return errors.New("desktop bridge unix socket is not supported on windows")
	}
	if err := os.MkdirAll(b.options.RuntimeDir, 0o700); err != nil {
		return err
	}
	if err := os.MkdirAll(b.options.PairingDir, 0o700); err != nil {
		return err
	}
	socketPath := filepath.Join(b.options.RuntimeDir, b.options.ID+".sock")
	pairingPath := filepath.Join(b.options.PairingDir, b.options.ID+".json")
	_ = os.Remove(socketPath)
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return err
	}
	payload := b.pairingPayload(socketPath)
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		_ = listener.Close()
		return err
	}
	if err := os.WriteFile(pairingPath, append(encoded, '\n'), 0o600); err != nil {
		_ = listener.Close()
		return err
	}
	b.mu.Lock()
	b.listener = listener
	b.socketPath = socketPath
	b.pairingPath = pairingPath
	b.registered = true
	b.lastError = ""
	b.mu.Unlock()

	go b.acceptLoop(ctx)
	go func() {
		<-ctx.Done()
		_ = b.Close()
	}()
	return nil
}

func (b *DesktopBridge) Close() error {
	b.mu.Lock()
	if b.closed {
		b.mu.Unlock()
		return nil
	}
	b.closed = true
	listener := b.listener
	clients := make([]net.Conn, 0, len(b.clients))
	for conn := range b.clients {
		clients = append(clients, conn)
	}
	socketPath := b.socketPath
	pairingPath := b.pairingPath
	b.listener = nil
	b.clients = map[net.Conn]struct{}{}
	b.registered = false
	b.mu.Unlock()

	if listener != nil {
		_ = listener.Close()
	}
	for _, conn := range clients {
		_ = conn.Close()
	}
	if socketPath != "" {
		_ = os.Remove(socketPath)
	}
	if pairingPath != "" {
		_ = os.Remove(pairingPath)
	}
	return nil
}

func (b *DesktopBridge) Status() DesktopBridgeStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	return DesktopBridgeStatus{
		Enabled:          !b.closed,
		Registered:       b.registered,
		ConnectedClients: len(b.clients),
		PairingPath:      b.pairingPath,
		SocketPath:       b.socketPath,
		LastError:        b.lastError,
		Protocol:         desktopBridgeProtocol,
		ProtocolReady:    b.protocolOK,
	}
}

func (b *DesktopBridge) Broadcast(event appserver.Event) {
	threadID := threadIDFromEvent(event)
	payload := map[string]any{
		"event":    "thread-stream-state-changed",
		"protocol": desktopBridgeProtocol,
		"threadId": threadID,
		"method":   event.Method,
		"channel":  event.Channel,
		"params":   event.Params,
	}
	if threadID == "" {
		payload["event"] = "client-status-changed"
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		b.noteError(err)
		return
	}
	b.mu.Lock()
	clients := make([]net.Conn, 0, len(b.clients))
	for conn := range b.clients {
		clients = append(clients, conn)
	}
	b.mu.Unlock()
	for _, conn := range clients {
		_ = conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
		if _, err := conn.Write(append(encoded, '\n')); err != nil {
			b.removeClient(conn)
			_ = conn.Close()
			b.noteError(err)
		}
	}
}

func (b *DesktopBridge) PairingPath() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.pairingPath
}

func (b *DesktopBridge) SocketPath() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.socketPath
}

func (b *DesktopBridge) pairingPayload(socketPath string) map[string]any {
	return map[string]any{
		"appName":          b.options.AppName,
		"bundleID":         b.options.BundleID,
		"extensionVersion": version.Version,
		"marketplaceID":    "codex-tg",
		"extensionName":    "codex-tg",
		"workspaceName":    "Telegram",
		"id":               b.options.ID,
		"capabilities": map[string]any{
			"content":          false,
			"ping":             true,
			"selections":       false,
			"reload":           false,
			"markForReload":    false,
			"removeHighlights": false,
			"highlightLines":   false,
			"highlight":        false,
			"setContent":       false,
			"replaceSelection": false,
		},
		"needsReload": false,
		"socketPath":  socketPath,
		"timestamp":   time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (b *DesktopBridge) acceptLoop(ctx context.Context) {
	for {
		b.mu.Lock()
		listener := b.listener
		b.mu.Unlock()
		if listener == nil {
			return
		}
		conn, err := listener.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.noteError(err)
			return
		}
		b.mu.Lock()
		if b.closed {
			b.mu.Unlock()
			_ = conn.Close()
			return
		}
		b.clients[conn] = struct{}{}
		b.mu.Unlock()
		go b.handleClient(ctx, conn)
	}
}

func (b *DesktopBridge) handleClient(ctx context.Context, conn net.Conn) {
	defer func() {
		b.removeClient(conn)
		_ = conn.Close()
	}()
	_ = conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
	_, _ = conn.Write([]byte(`{"event":"client-status-changed","status":"connected","protocol":"experimental-jsonl"}` + "\n"))
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var message map[string]any
		if err := json.Unmarshal(line, &message); err != nil {
			b.writeAck(conn, nil, false, err.Error(), nil)
			continue
		}
		id := message["id"]
		result, err := b.handleMessage(ctx, message)
		if err != nil {
			b.writeAck(conn, id, false, err.Error(), nil)
			continue
		}
		b.markProtocolReady()
		b.writeAck(conn, id, true, "", result)
	}
	if err := scanner.Err(); err != nil && ctx.Err() == nil {
		b.noteError(err)
	}
}

func (b *DesktopBridge) handleMessage(ctx context.Context, message map[string]any) (map[string]any, error) {
	eventName := firstPayloadString(message, "event", "type", "method")
	switch eventName {
	case "ping", "client-ping":
		return map[string]any{"pong": true}, nil
	case "thread-follower-steer-turn":
		return b.service.handleDesktopBridgeSteer(ctx, message)
	case "thread-follower-start-turn":
		return b.service.handleDesktopBridgeStart(ctx, message)
	case "thread-follower-interrupt-turn":
		return b.service.handleDesktopBridgeInterrupt(ctx, message)
	case "thread-follower-set-model-and-reasoning":
		return b.service.handleDesktopBridgeModelSettings(ctx, message)
	case "thread-follower-submit-user-input":
		return b.service.handleDesktopBridgeUserInput(ctx, message)
	case "thread-read-state-changed", "thread-stream-state-changed", "client-status-changed":
		return map[string]any{"accepted": true}, nil
	default:
		if eventName == "" {
			return nil, errors.New("missing bridge event")
		}
		return nil, fmt.Errorf("unsupported desktop bridge event %q", eventName)
	}
}

func (b *DesktopBridge) writeAck(conn net.Conn, id any, ok bool, errText string, result map[string]any) {
	payload := map[string]any{
		"event": "codex-tg-ack",
		"ok":    ok,
	}
	if id != nil {
		payload["id"] = id
	}
	if errText != "" {
		payload["error"] = errText
	}
	if result != nil {
		payload["result"] = result
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		b.noteError(err)
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(1 * time.Second))
	if _, err := conn.Write(append(encoded, '\n')); err != nil {
		b.noteError(err)
	}
}

func (b *DesktopBridge) removeClient(conn net.Conn) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.clients, conn)
}

func (b *DesktopBridge) noteError(err error) {
	if err == nil {
		return
	}
	b.mu.Lock()
	b.lastError = err.Error()
	b.mu.Unlock()
}

func (b *DesktopBridge) markProtocolReady() {
	b.mu.Lock()
	b.protocolOK = true
	b.mu.Unlock()
}

func (s *Service) desktopBridgeLoop(ctx context.Context) {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		s.reconcileDesktopBridge(ctx)
		select {
		case <-ctx.Done():
			s.stopDesktopBridge()
			return
		case <-ticker.C:
		}
	}
}

func (s *Service) reconcileDesktopBridge(ctx context.Context) {
	_, _, enabled := s.appServerTransportSettings(ctx)
	s.mu.RLock()
	bridge := s.bridge
	s.mu.RUnlock()
	if !enabled {
		if bridge != nil {
			s.stopDesktopBridge()
		}
		return
	}
	if bridge != nil {
		return
	}
	created := newDesktopBridge(s, defaultDesktopBridgeOptions(s.cfg.Paths))
	if err := created.Start(ctx); err != nil {
		s.setError(ctx, fmt.Errorf("desktop_bridge: %w", err))
		return
	}
	s.mu.Lock()
	if s.bridge == nil {
		s.bridge = created
		created = nil
	}
	s.mu.Unlock()
	if created != nil {
		_ = created.Close()
	}
}

func (s *Service) stopDesktopBridge() {
	s.mu.Lock()
	bridge := s.bridge
	s.bridge = nil
	s.mu.Unlock()
	if bridge != nil {
		_ = bridge.Close()
	}
}

func (s *Service) desktopBridgeStatus() DesktopBridgeStatus {
	s.mu.RLock()
	bridge := s.bridge
	s.mu.RUnlock()
	if bridge == nil {
		return DesktopBridgeStatus{}
	}
	return bridge.Status()
}

func (s *Service) broadcastDesktopBridgeEvent(event appserver.Event) {
	s.mu.RLock()
	bridge := s.bridge
	s.mu.RUnlock()
	if bridge != nil {
		bridge.Broadcast(event)
	}
}

func (s *Service) handleDesktopBridgeSteer(ctx context.Context, message map[string]any) (map[string]any, error) {
	threadID := firstPayloadString(message, "threadId", "threadID", "thread")
	turnID := firstPayloadString(message, "turnId", "turnID", "expectedTurnId")
	text := firstPayloadString(message, "text", "message", "input")
	if threadID == "" || text == "" {
		return nil, errors.New("threadId and text are required")
	}
	if turnID == "" {
		if thread, _ := s.store.GetThread(ctx, threadID); thread != nil {
			turnID = strings.TrimSpace(thread.ActiveTurnID)
		}
	}
	live, err := s.desktopBridgeLiveSession()
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	result, err := live.TurnSteer(requestCtx, threadID, turnID, text)
	if err != nil {
		return nil, err
	}
	return map[string]any{"threadId": threadID, "turnId": appserverThreadTurnID(result)}, nil
}

func (s *Service) handleDesktopBridgeStart(ctx context.Context, message map[string]any) (map[string]any, error) {
	threadID := firstPayloadString(message, "threadId", "threadID", "thread")
	text := firstPayloadString(message, "text", "message", "input")
	cwd := firstPayloadString(message, "cwd", "workdir", "workspace")
	if text == "" {
		return nil, errors.New("text is required")
	}
	if cwd == "" {
		cwd = s.cfg.DefaultCWD
	}
	live, err := s.desktopBridgeLiveSession()
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	if threadID == "" {
		started, err := live.ThreadStart(requestCtx, cwd)
		if err != nil {
			return nil, err
		}
		threadID = threadIDFromPayload(started)
	}
	if threadID == "" {
		return nil, errors.New("app-server did not return a thread id")
	}
	var thread *model.Thread
	if loaded, _ := s.store.GetThread(ctx, threadID); loaded != nil {
		thread = loaded
		if cwd == "" {
			cwd = loaded.CWD
		}
	}
	result, err := live.TurnStart(requestCtx, threadID, text, cwd, s.turnStartOptions(ctx, "", thread))
	if err != nil {
		return nil, err
	}
	return map[string]any{"threadId": threadID, "turnId": appserverThreadTurnID(result)}, nil
}

func (s *Service) handleDesktopBridgeInterrupt(ctx context.Context, message map[string]any) (map[string]any, error) {
	threadID := firstPayloadString(message, "threadId", "threadID", "thread")
	turnID := firstPayloadString(message, "turnId", "turnID")
	if threadID == "" {
		return nil, errors.New("threadId is required")
	}
	if turnID == "" {
		if thread, _ := s.store.GetThread(ctx, threadID); thread != nil {
			turnID = strings.TrimSpace(thread.ActiveTurnID)
		}
	}
	if turnID == "" {
		return nil, errors.New("turnId is required")
	}
	live, err := s.desktopBridgeLiveSession()
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	if err := live.TurnInterrupt(requestCtx, threadID, turnID); err != nil {
		return nil, err
	}
	return map[string]any{"threadId": threadID, "turnId": turnID}, nil
}

func (s *Service) handleDesktopBridgeModelSettings(ctx context.Context, message map[string]any) (map[string]any, error) {
	modelValue := firstPayloadString(message, "model", "modelId")
	reasoningValue := normalizeReasoningEffort(firstPayloadString(message, "reasoningEffort", "reasoning_effort", "effort"))
	if modelValue != "" {
		if err := s.store.SetState(ctx, codexModelStateKey, modelValue); err != nil {
			return nil, err
		}
	}
	if reasoningValue != "" {
		if err := s.store.SetState(ctx, codexReasoningStateKey, reasoningValue); err != nil {
			return nil, err
		}
	}
	return map[string]any{"model": modelValue, "reasoningEffort": reasoningValue}, nil
}

func (s *Service) handleDesktopBridgeUserInput(ctx context.Context, message map[string]any) (map[string]any, error) {
	requestID := firstPayloadString(message, "requestId", "requestID")
	text := firstPayloadString(message, "text", "message", "input")
	if requestID == "" || text == "" {
		return nil, errors.New("requestId and text are required")
	}
	payloadJSON := firstPayloadString(message, "payloadJson", "payloadJSON")
	if payloadJSON == "" {
		if rawPayload, ok := message["payload"].(map[string]any); ok {
			encoded, _ := json.Marshal(rawPayload)
			payloadJSON = string(encoded)
		}
	}
	live, err := s.desktopBridgeLiveSession()
	if err != nil {
		return nil, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	if err := live.RespondServerRequest(requestCtx, requestID, userInputResponsePayload(payloadJSON, text)); err != nil {
		return nil, err
	}
	return map[string]any{"requestId": requestID}, nil
}

func (s *Service) desktopBridgeLiveSession() (Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.liveConnected || s.live == nil {
		return nil, errors.New("live app-server session is not ready")
	}
	return s.live, nil
}

func threadIDFromPayload(payload map[string]any) string {
	if value := firstPayloadString(payload, "threadId", "id"); value != "" {
		return value
	}
	if thread, ok := payload["thread"].(map[string]any); ok {
		return firstPayloadString(thread, "threadId", "id")
	}
	return ""
}

func guiVisibilityExpectation(status appserver.TransportStatus, bridgeEnabled bool, bridgeStatus DesktopBridgeStatus) string {
	if bridgeEnabled && bridgeStatus.Registered && bridgeStatus.ConnectedClients > 0 && bridgeStatus.ProtocolReady {
		return "expected via Desktop Bridge"
	}
	if bridgeEnabled && bridgeStatus.Registered && bridgeStatus.ConnectedClients > 0 {
		return "bridge connected; protocol unverified"
	}
	if bridgeEnabled && bridgeStatus.Registered {
		return "bridge registered; waiting for Desktop attach"
	}
	if status.ActiveMode == appserver.TransportModeUnix || status.ActiveMode == appserver.TransportModeWebSocket {
		return "expected via shared app-server transport"
	}
	return "Telegram-live only; Desktop may catch up after refresh/completion"
}
