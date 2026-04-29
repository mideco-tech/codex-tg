package appserver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type fakeRPCTransport struct {
	sends  chan []byte
	recvs  chan []byte
	closed chan struct{}
	once   sync.Once
}

func newFakeRPCTransport() *fakeRPCTransport {
	return &fakeRPCTransport{
		sends:  make(chan []byte, 16),
		recvs:  make(chan []byte, 16),
		closed: make(chan struct{}),
	}
}

func (t *fakeRPCTransport) Name() string     { return "fake" }
func (t *fakeRPCTransport) Endpoint() string { return "fake://" }
func (t *fakeRPCTransport) Start(ctx context.Context) error {
	return nil
}
func (t *fakeRPCTransport) Send(ctx context.Context, payload []byte) error {
	select {
	case t.sends <- append([]byte(nil), payload...):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-t.closed:
		return io.EOF
	}
}
func (t *fakeRPCTransport) Recv(ctx context.Context) ([]byte, error) {
	select {
	case payload := <-t.recvs:
		return payload, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-t.closed:
		return nil, io.EOF
	}
}
func (t *fakeRPCTransport) Close() error {
	t.once.Do(func() { close(t.closed) })
	return nil
}
func (t *fakeRPCTransport) StderrTail() []string { return nil }

func newStartedFakeClient(t *testing.T, transport *fakeRPCTransport) *Client {
	t.Helper()
	client := NewClientWithOptions(ClientOptions{RequestTimeout: 3 * time.Second})
	client.transport = transport
	client.transportStatus = TransportStatus{RequestedMode: "fake", ActiveMode: "fake", Endpoint: "fake://"}
	client.started = true
	client.readerDone = make(chan struct{})
	go client.readLoop()
	t.Cleanup(func() {
		_ = client.Close()
	})
	return client
}

func TestJSONRPCClientUsesTransportForRequestsNotificationsAndServerRequests(t *testing.T) {
	t.Parallel()

	transport := newFakeRPCTransport()
	client := newStartedFakeClient(t, transport)
	events := client.Subscribe()

	go func() {
		sent := <-transport.sends
		var message map[string]any
		_ = json.Unmarshal(sent, &message)
		transport.recvs <- []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%.0f,"result":{"ok":true}}`, message["id"].(float64)))
	}()
	result, err := client.Request(context.Background(), "thread/read", map[string]any{"threadId": "thread-1"})
	if err != nil {
		t.Fatalf("Request failed: %v", err)
	}
	if got := asMap(result)["ok"]; got != true {
		t.Fatalf("request result = %#v, want ok true", result)
	}

	if err := client.Notify(context.Background(), "initialized", nil); err != nil {
		t.Fatalf("Notify failed: %v", err)
	}
	var notify map[string]any
	if err := json.Unmarshal(<-transport.sends, &notify); err != nil {
		t.Fatalf("notify payload unmarshal failed: %v", err)
	}
	if notify["method"] != "initialized" {
		t.Fatalf("notify method = %v, want initialized", notify["method"])
	}
	if _, ok := notify["id"]; ok {
		t.Fatalf("notify payload has id: %#v", notify)
	}

	transport.recvs <- []byte(`{"jsonrpc":"2.0","id":"srv-1","method":"serverRequest/userInput","params":{"threadId":"thread-1"}}`)
	event := <-events
	if event.Channel != "server_request" || event.Method != "serverRequest/userInput" || event.ID != "srv-1" {
		t.Fatalf("server request event = %#v", event)
	}
	if err := client.RespondServerRequest(context.Background(), "srv-1", map[string]any{"text": "ok"}); err != nil {
		t.Fatalf("RespondServerRequest failed: %v", err)
	}
	var response map[string]any
	if err := json.Unmarshal(<-transport.sends, &response); err != nil {
		t.Fatalf("server response unmarshal failed: %v", err)
	}
	if response["id"] != "srv-1" {
		t.Fatalf("server response id = %v, want srv-1", response["id"])
	}

	transport.recvs <- []byte(`{"jsonrpc":"2.0","method":"thread/status/changed","params":{"threadId":"thread-2"}}`)
	event = <-events
	if event.Channel != "notification" || event.Method != "thread/status/changed" || event.Params["threadId"] != "thread-2" {
		t.Fatalf("notification event = %#v", event)
	}
}

func TestStdioTransportBuildsCodexAppServerCommand(t *testing.T) {
	t.Parallel()

	transport := newStdioTransport("codex", "stdio://", "/tmp/project")
	cmd, err := transport.buildCommand()
	if err != nil {
		t.Fatalf("buildCommand failed: %v", err)
	}
	if len(cmd.Args) < 4 || cmd.Args[1] != "app-server" || cmd.Args[2] != "--listen" || cmd.Args[3] != "stdio://" {
		t.Fatalf("cmd args = %#v, want codex app-server --listen stdio://", cmd.Args)
	}
	if cmd.Dir != "/tmp/project" {
		t.Fatalf("cmd dir = %q, want /tmp/project", cmd.Dir)
	}
}

func TestWebSocketTransportUsesTextFrames(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{}
	received := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		messageType, payload, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("ReadMessage failed: %v", err)
			return
		}
		if messageType != websocket.TextMessage {
			t.Errorf("messageType = %d, want TextMessage", messageType)
		}
		received <- string(payload)
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","result":{"ok":true}}`))
	}))
	defer server.Close()

	transport, err := newWebSocketTransport("ws" + strings.TrimPrefix(server.URL, "http"))
	if err != nil {
		t.Fatalf("newWebSocketTransport failed: %v", err)
	}
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Close()

	if err := transport.Send(context.Background(), []byte(`{"jsonrpc":"2.0","method":"ping"}`)); err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if got := <-received; got != `{"jsonrpc":"2.0","method":"ping"}` {
		t.Fatalf("received frame = %q", got)
	}
	payload, err := transport.Recv(context.Background())
	if err != nil {
		t.Fatalf("Recv failed: %v", err)
	}
	if !strings.Contains(string(payload), `"ok":true`) {
		t.Fatalf("recv payload = %s, want ok true", payload)
	}
}

func TestUnixTransportUsesWebSocketOverUDS(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets are not available on windows")
	}

	root, err := os.MkdirTemp("/tmp", "ctg-uds-*")
	if err != nil {
		t.Fatalf("MkdirTemp failed: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	socketPath := filepath.Join(root, "app.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen(unix) failed: %v", err)
	}
	upgrader := websocket.Upgrader{}
	received := make(chan string, 1)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("Upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		_, payload, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("ReadMessage failed: %v", err)
			return
		}
		received <- string(payload)
		_ = conn.WriteMessage(websocket.TextMessage, []byte(`{"jsonrpc":"2.0","result":{"uds":true}}`))
	})}
	go func() {
		_ = server.Serve(listener)
	}()
	defer server.Close()

	transport, err := newUnixWebSocketTransport("unix://" + socketPath)
	if err != nil {
		t.Fatalf("newUnixWebSocketTransport failed: %v", err)
	}
	if err := transport.Start(context.Background()); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer transport.Close()
	if err := transport.Send(context.Background(), []byte(`{"jsonrpc":"2.0","method":"loaded"}`)); err != nil {
		t.Fatalf("Send failed: %v", err)
	}
	if got := <-received; got != `{"jsonrpc":"2.0","method":"loaded"}` {
		t.Fatalf("received frame = %q", got)
	}
	payload, err := transport.Recv(context.Background())
	if err != nil {
		t.Fatalf("Recv failed: %v", err)
	}
	if !strings.Contains(string(payload), `"uds":true`) {
		t.Fatalf("recv payload = %s, want uds true", payload)
	}
}

func TestAutoTransportFallsBackToStdioAfterProbeMiss(t *testing.T) {
	t.Parallel()
	if runtime.GOOS == "windows" {
		t.Skip("uses a unix shell helper")
	}
	root := t.TempDir()
	fakeCodex := filepath.Join(root, "codex")
	if err := os.WriteFile(fakeCodex, []byte("#!/bin/sh\ncat >/dev/null\n"), 0o755); err != nil {
		t.Fatalf("WriteFile(fake codex) failed: %v", err)
	}
	missingSocket := filepath.Join(root, "missing.sock")
	transport, probeErrors, err := connectAutoTransport(context.Background(), fakeCodex, "stdio://", root, "unix://"+missingSocket)
	if err != nil {
		t.Fatalf("connectAutoTransport failed: %v", err)
	}
	defer transport.Close()
	if transport.Name() != TransportModeStdio {
		t.Fatalf("transport = %s, want stdio fallback", transport.Name())
	}
	if len(probeErrors) == 0 {
		t.Fatalf("probeErrors is empty, want unix probe miss recorded")
	}
	for _, probeErr := range probeErrors {
		if strings.Contains(probeErr, missingSocket) {
			t.Fatalf("probe error leaked socket path: %s", probeErr)
		}
	}
}
