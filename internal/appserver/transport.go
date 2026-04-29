package appserver

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	TransportModeAuto          = "auto"
	TransportModeStdio         = "stdio"
	TransportModeWebSocket     = "ws"
	TransportModeUnix          = "unix"
	TransportModeDesktopBridge = "desktop_bridge"
)

type ClientOptions struct {
	CodexBin       string
	ListenURL      string
	CWD            string
	RequestTimeout time.Duration
	TransportMode  string
	Endpoint       string
}

type TransportStatus struct {
	RequestedMode string
	ActiveMode    string
	Endpoint      string
	ProbeErrors   []string
}

type ProbeResult struct {
	Mode      string
	Endpoint  string
	Available bool
	Error     string
}

type rpcTransport interface {
	Name() string
	Endpoint() string
	Start(ctx context.Context) error
	Send(ctx context.Context, payload []byte) error
	Recv(ctx context.Context) ([]byte, error)
	Close() error
	StderrTail() []string
}

type stdioTransport struct {
	codexBin  string
	listenURL string
	cwd       string

	mu          sync.Mutex
	cmd         *exec.Cmd
	stdin       io.WriteCloser
	stdout      io.ReadCloser
	stderr      io.ReadCloser
	scanner     *bufio.Scanner
	stderrLines []string
}

func newStdioTransport(codexBin, listenURL, cwd string) *stdioTransport {
	listenURL = strings.TrimSpace(listenURL)
	if listenURL == "" {
		listenURL = "stdio://"
	}
	return &stdioTransport{codexBin: codexBin, listenURL: listenURL, cwd: cwd}
}

func (t *stdioTransport) Name() string     { return TransportModeStdio }
func (t *stdioTransport) Endpoint() string { return t.listenURL }

func (t *stdioTransport) Start(ctx context.Context) error {
	cmd, err := t.buildCommand()
	if err != nil {
		return err
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	t.mu.Lock()
	t.cmd = cmd
	t.stdin = stdin
	t.stdout = stdout
	t.stderr = stderr
	t.scanner = scanner
	t.mu.Unlock()
	go t.readStderr()
	return nil
}

func (t *stdioTransport) Send(ctx context.Context, payload []byte) error {
	t.mu.Lock()
	stdin := t.stdin
	t.mu.Unlock()
	if stdin == nil {
		return errors.New("stdio transport is not running")
	}
	_, err := io.WriteString(stdin, string(payload)+"\n")
	return err
}

func (t *stdioTransport) Recv(ctx context.Context) ([]byte, error) {
	t.mu.Lock()
	scanner := t.scanner
	t.mu.Unlock()
	if scanner == nil {
		return nil, errors.New("stdio transport is not running")
	}
	if scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		return line, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return nil, io.EOF
}

func (t *stdioTransport) Close() error {
	t.mu.Lock()
	cmd := t.cmd
	stdin := t.stdin
	t.cmd = nil
	t.stdin = nil
	t.stdout = nil
	t.stderr = nil
	t.scanner = nil
	t.mu.Unlock()
	if stdin != nil {
		_ = stdin.Close()
	}
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	}
	return nil
}

func (t *stdioTransport) StderrTail() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := make([]string, len(t.stderrLines))
	copy(out, t.stderrLines)
	return out
}

func (t *stdioTransport) buildCommand() (*exec.Cmd, error) {
	executable, err := exec.LookPath(t.codexBin)
	if err != nil {
		executable = t.codexBin
	}
	if runtime.GOOS == "windows" {
		ext := strings.ToLower(filepath.Ext(executable))
		if ext == ".cmd" || ext == ".bat" {
			command := fmt.Sprintf("%s app-server --listen %s", executable, t.listenURL)
			cmd := exec.Command(os.Getenv("ComSpec"), "/d", "/c", command)
			cmd.Dir = t.cwd
			return cmd, nil
		}
	}
	cmd := exec.Command(executable, "app-server", "--listen", t.listenURL)
	cmd.Dir = t.cwd
	return cmd, nil
}

func (t *stdioTransport) readStderr() {
	t.mu.Lock()
	stderr := t.stderr
	t.mu.Unlock()
	if stderr == nil {
		return
	}
	scanner := bufio.NewScanner(stderr)
	scanner.Buffer(make([]byte, 0, 512), 64*1024)
	for scanner.Scan() {
		line := scanner.Text()
		t.mu.Lock()
		t.stderrLines = append(t.stderrLines, line)
		if len(t.stderrLines) > 100 {
			t.stderrLines = t.stderrLines[len(t.stderrLines)-100:]
		}
		t.mu.Unlock()
	}
}

type websocketTransport struct {
	mode      string
	endpoint  string
	unixPath  string
	connURLs  []string
	header    http.Header
	dialer    websocket.Dialer
	mu        sync.Mutex
	conn      *websocket.Conn
	connected string
}

func newWebSocketTransport(endpoint string) (*websocketTransport, error) {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return nil, errors.New("ws endpoint is required")
	}
	if !isLoopbackWebSocket(endpoint) {
		return nil, fmt.Errorf("ws endpoint must be loopback: %s", endpoint)
	}
	return &websocketTransport{
		mode:     TransportModeWebSocket,
		endpoint: endpoint,
		connURLs: websocketCandidates(endpoint),
		dialer:   websocket.Dialer{},
	}, nil
}

func newUnixWebSocketTransport(endpoint string) (*websocketTransport, error) {
	path := unixSocketPath(endpoint)
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("unix socket path is required")
	}
	dialer := websocket.Dialer{
		NetDialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", path)
		},
	}
	return &websocketTransport{
		mode:     TransportModeUnix,
		endpoint: "unix://" + path,
		unixPath: path,
		connURLs: []string{"ws://localhost/rpc", "ws://localhost/"},
		dialer:   dialer,
	}, nil
}

func (t *websocketTransport) Name() string     { return t.mode }
func (t *websocketTransport) Endpoint() string { return t.endpoint }

func (t *websocketTransport) Start(ctx context.Context) error {
	var lastErr error
	for _, target := range t.connURLs {
		conn, _, err := t.dialer.DialContext(ctx, target, t.header)
		if err == nil {
			t.mu.Lock()
			t.conn = conn
			t.connected = target
			t.mu.Unlock()
			return nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = errors.New("no websocket dial targets")
	}
	return lastErr
}

func (t *websocketTransport) Send(ctx context.Context, payload []byte) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.conn == nil {
		return errors.New("websocket transport is not running")
	}
	return t.conn.WriteMessage(websocket.TextMessage, payload)
}

func (t *websocketTransport) Recv(ctx context.Context) ([]byte, error) {
	t.mu.Lock()
	conn := t.conn
	t.mu.Unlock()
	if conn == nil {
		return nil, errors.New("websocket transport is not running")
	}
	messageType, payload, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	if messageType != websocket.TextMessage && messageType != websocket.BinaryMessage {
		return nil, fmt.Errorf("unexpected websocket message type %d", messageType)
	}
	return payload, nil
}

func (t *websocketTransport) Close() error {
	t.mu.Lock()
	conn := t.conn
	t.conn = nil
	t.connected = ""
	t.mu.Unlock()
	if conn != nil {
		return conn.Close()
	}
	return nil
}

func (t *websocketTransport) StderrTail() []string { return nil }

func NormalizeTransportMode(value string) string {
	switch strings.TrimSpace(strings.ToLower(value)) {
	case "", TransportModeAuto:
		return TransportModeAuto
	case "stdio", "stdio://":
		return TransportModeStdio
	case "ws", "websocket":
		return TransportModeWebSocket
	case "unix", "uds":
		return TransportModeUnix
	case "desktop", "bridge", "desktop_bridge", "desktop-bridge":
		return TransportModeDesktopBridge
	default:
		return ""
	}
}

func ProbeTransports(ctx context.Context, codexHome, endpoint string) []ProbeResult {
	out := []ProbeResult{}
	unixEndpoint := "unix://" + defaultUnixSocketPath(codexHome)
	out = append(out, probeTransport(ctx, TransportModeUnix, unixEndpoint))
	endpoint = strings.TrimSpace(endpoint)
	if strings.HasPrefix(endpoint, "ws://") {
		out = append(out, probeTransport(ctx, TransportModeWebSocket, endpoint))
	}
	if strings.HasPrefix(endpoint, "unix://") && endpoint != unixEndpoint {
		out = append(out, probeTransport(ctx, TransportModeUnix, endpoint))
	}
	return out
}

func probeTransport(ctx context.Context, mode, endpoint string) ProbeResult {
	result := ProbeResult{Mode: mode, Endpoint: endpoint}
	transport, err := transportForMode(mode, "codex", "stdio://", "", endpoint)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if mode == TransportModeUnix {
		path := unixSocketPath(endpoint)
		if st, err := os.Stat(path); err != nil {
			result.Error = redactEndpointDiagnostic(endpoint, err.Error())
			return result
		} else if st.Mode()&os.ModeSocket == 0 {
			result.Error = "path is not a unix socket"
			return result
		}
	}
	if err := transport.Start(ctx); err != nil {
		result.Error = err.Error()
		return result
	}
	_ = transport.Close()
	result.Available = true
	return result
}

func transportForMode(mode, codexBin, listenURL, cwd, endpoint string) (rpcTransport, error) {
	switch NormalizeTransportMode(mode) {
	case TransportModeStdio:
		return newStdioTransport(codexBin, "stdio://", cwd), nil
	case TransportModeWebSocket:
		target := strings.TrimSpace(endpoint)
		if target == "" && strings.HasPrefix(strings.TrimSpace(listenURL), "ws://") {
			target = strings.TrimSpace(listenURL)
		}
		return newWebSocketTransport(target)
	case TransportModeUnix:
		target := strings.TrimSpace(endpoint)
		if target == "" && strings.HasPrefix(strings.TrimSpace(listenURL), "unix://") {
			target = strings.TrimSpace(listenURL)
		}
		if target == "" {
			target = "unix://" + defaultUnixSocketPath("")
		}
		return newUnixWebSocketTransport(target)
	default:
		return nil, fmt.Errorf("unsupported app-server transport mode %q", mode)
	}
}

func connectAutoTransport(ctx context.Context, codexBin, listenURL, cwd, endpoint string) (rpcTransport, []string, error) {
	errorsOut := []string{}
	candidates := []struct {
		mode     string
		endpoint string
	}{
		{mode: TransportModeUnix, endpoint: "unix://" + defaultUnixSocketPath("")},
	}
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		listenURL = strings.TrimSpace(listenURL)
		if strings.HasPrefix(listenURL, "unix://") || strings.HasPrefix(listenURL, "ws://") {
			endpoint = listenURL
		}
	}
	if strings.HasPrefix(endpoint, "unix://") || strings.HasPrefix(endpoint, "ws://") {
		candidates = append(candidates, struct {
			mode     string
			endpoint string
		}{mode: modeFromEndpoint(endpoint), endpoint: endpoint})
	}
	for _, candidate := range candidates {
		transport, err := transportForMode(candidate.mode, codexBin, listenURL, cwd, candidate.endpoint)
		if err != nil {
			errorsOut = append(errorsOut, fmt.Sprintf("%s %s: %s", candidate.mode, redactedEndpoint(candidate.endpoint), redactEndpointDiagnostic(candidate.endpoint, err.Error())))
			continue
		}
		if candidate.mode == TransportModeUnix {
			path := unixSocketPath(candidate.endpoint)
			if st, err := os.Stat(path); err != nil {
				errorsOut = append(errorsOut, fmt.Sprintf("%s %s: %s", candidate.mode, redactedEndpoint(candidate.endpoint), redactEndpointDiagnostic(candidate.endpoint, err.Error())))
				continue
			} else if st.Mode()&os.ModeSocket == 0 {
				errorsOut = append(errorsOut, fmt.Sprintf("%s %s: path is not a unix socket", candidate.mode, redactedEndpoint(candidate.endpoint)))
				continue
			}
		}
		if err := transport.Start(ctx); err != nil {
			errorsOut = append(errorsOut, fmt.Sprintf("%s %s: %s", candidate.mode, redactedEndpoint(candidate.endpoint), redactEndpointDiagnostic(candidate.endpoint, err.Error())))
			_ = transport.Close()
			continue
		}
		return transport, errorsOut, nil
	}
	transport := newStdioTransport(codexBin, "stdio://", cwd)
	if err := transport.Start(ctx); err != nil {
		return nil, errorsOut, err
	}
	return transport, errorsOut, nil
}

func modeFromEndpoint(endpoint string) string {
	switch {
	case strings.HasPrefix(endpoint, "ws://"):
		return TransportModeWebSocket
	case strings.HasPrefix(endpoint, "unix://"):
		return TransportModeUnix
	default:
		return TransportModeStdio
	}
}

func websocketCandidates(raw string) []string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return []string{raw}
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return []string{raw}
	}
	withRPC := *parsed
	withRPC.Path = "/rpc"
	withRoot := *parsed
	withRoot.Path = "/"
	return []string{withRPC.String(), withRoot.String()}
}

func isLoopbackWebSocket(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "ws" {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "localhost" || host == "127.0.0.1" || host == "::1" || host == "[::1]"
}

func unixSocketPath(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if strings.HasPrefix(endpoint, "unix://") {
		path := strings.TrimPrefix(endpoint, "unix://")
		if strings.TrimSpace(path) != "" {
			return path
		}
	}
	return defaultUnixSocketPath("")
}

func defaultUnixSocketPath(codexHome string) string {
	codexHome = strings.TrimSpace(codexHome)
	if codexHome == "" {
		codexHome = strings.TrimSpace(os.Getenv("CODEX_HOME"))
	}
	if codexHome == "" {
		home, _ := os.UserHomeDir()
		codexHome = filepath.Join(home, ".codex")
	}
	return filepath.Join(codexHome, "app-server-control", "app-server-control.sock")
}

func redactedEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	if endpoint == "stdio://" || endpoint == "stdio:" {
		return "stdio://"
	}
	if strings.HasPrefix(endpoint, "unix://") {
		return "unix://<local-socket>"
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return endpoint
	}
	if parsed.User != nil {
		parsed.User = url.User("<redacted>")
	}
	return parsed.String()
}

func RedactEndpoint(endpoint string) string {
	return redactedEndpoint(endpoint)
}

func RedactTransportDiagnostic(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	value = redactDefaultSocketPath(value)
	if strings.Contains(value, "unix://") {
		value = replaceUnixEndpointPaths(value)
	}
	value = replaceLocalPathTokens(value)
	return value
}

func redactEndpointDiagnostic(endpoint, value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if strings.HasPrefix(endpoint, "unix://") {
		path := unixSocketPath(endpoint)
		if path != "" {
			value = strings.ReplaceAll(value, path, "<local-socket>")
		}
	}
	return RedactTransportDiagnostic(value)
}

func redactDefaultSocketPath(value string) string {
	defaultPath := defaultUnixSocketPath("")
	if defaultPath == "" {
		return value
	}
	return strings.ReplaceAll(value, defaultPath, "<local-socket>")
}

func replaceUnixEndpointPaths(value string) string {
	parts := strings.Fields(value)
	for _, part := range parts {
		trimmed := strings.Trim(part, ".,;:()[]{}\"'")
		if strings.HasPrefix(trimmed, "unix://") && trimmed != "unix://<local-socket>" {
			value = strings.ReplaceAll(value, trimmed, "unix://<local-socket>")
		}
	}
	return value
}

func replaceLocalPathTokens(value string) string {
	parts := strings.Fields(value)
	for _, part := range parts {
		trimmed := strings.Trim(part, ".,;:()[]{}\"'")
		switch {
		case strings.Contains(trimmed, "/") && strings.Contains(trimmed, ".sock"):
			value = strings.ReplaceAll(value, trimmed, "<local-socket>")
		case strings.Contains(trimmed, "app_pairing_extensions"):
			value = strings.ReplaceAll(value, trimmed, "<pairing-payload>")
		}
	}
	return value
}
