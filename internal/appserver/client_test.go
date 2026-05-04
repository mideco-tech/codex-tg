package appserver

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRPCStringSkipsNilLikeValues(t *testing.T) {
	t.Parallel()

	for _, value := range []any{nil, "", " ", "<nil>"} {
		if got := rpcString(value); got != "" {
			t.Fatalf("rpcString(%#v) = %q, want empty", value, got)
		}
	}
	if got := rpcString(float64(42)); got != "42" {
		t.Fatalf("rpcString(42) = %q, want 42", got)
	}
}

func TestHandlePayloadIgnoresStaleGeneration(t *testing.T) {
	t.Parallel()

	client := NewClient("codex", "stdio", t.TempDir(), time.Second)
	events := client.Subscribe()
	reply := make(chan rpcResponse, 1)
	client.mu.Lock()
	client.started = true
	client.generation = 2
	client.pending[1] = reply
	client.mu.Unlock()

	client.handlePayload(map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"result":  map[string]any{"ok": true},
	}, 1)
	select {
	case response := <-reply:
		t.Fatalf("stale generation resolved pending response: %#v", response)
	default:
	}
	client.mu.Lock()
	if _, ok := client.pending[1]; !ok {
		t.Fatal("stale generation deleted pending response")
	}
	client.mu.Unlock()

	client.handlePayload(map[string]any{
		"jsonrpc": "2.0",
		"id":      "req-stale",
		"method":  "serverRequest/approval",
		"params":  map[string]any{"requestId": "req-stale"},
	}, 1)
	client.mu.Lock()
	_, stored := client.serverRequests["req-stale"]
	client.mu.Unlock()
	if stored {
		t.Fatal("stale generation stored server request")
	}
	client.handlePayload(map[string]any{
		"jsonrpc": "2.0",
		"method":  "thread/status/changed",
		"params":  map[string]any{"threadId": "thread-stale"},
	}, 1)
	select {
	case event := <-events:
		t.Fatalf("stale generation broadcast event: %#v", event)
	default:
	}

	client.handlePayload(map[string]any{
		"jsonrpc": "2.0",
		"id":      float64(1),
		"result":  map[string]any{"ok": true},
	}, 2)
	select {
	case response := <-reply:
		if response.Error != nil {
			t.Fatalf("current generation response error: %v", response.Error)
		}
	default:
		t.Fatal("current generation did not resolve pending response")
	}
}

func TestTurnStartParamsIncludesCollaborationMode(t *testing.T) {
	params, err := turnStartParams("thread-1", "Draft a plan", "/tmp/project", TurnStartOptions{
		CollaborationMode: "plan",
		Model:             "gpt-test",
		ReasoningEffort:   "x-high",
	})
	if err != nil {
		t.Fatalf("turnStartParams failed: %v", err)
	}
	if got, want := params["threadId"], "thread-1"; got != want {
		t.Fatalf("threadId = %v, want %q", got, want)
	}
	collaborationMode, ok := params["collaborationMode"].(map[string]any)
	if !ok {
		t.Fatalf("collaborationMode = %#v, want object", params["collaborationMode"])
	}
	if got, want := collaborationMode["mode"], "plan"; got != want {
		t.Fatalf("mode = %v, want %q", got, want)
	}
	settings, ok := collaborationMode["settings"].(map[string]any)
	if !ok {
		t.Fatalf("settings = %#v, want object", collaborationMode["settings"])
	}
	if got, want := settings["model"], "gpt-test"; got != want {
		t.Fatalf("model = %v, want %q", got, want)
	}
	if got, want := settings["reasoning_effort"], "xhigh"; got != want {
		t.Fatalf("reasoning_effort = %v, want %q", got, want)
	}
	if _, ok := settings["developer_instructions"]; !ok {
		t.Fatal("developer_instructions key is missing")
	}
}

func TestTurnStartParamsRejectsModeWithoutModel(t *testing.T) {
	_, err := turnStartParams("thread-1", "Draft a plan", "", TurnStartOptions{CollaborationMode: "plan"})
	if err == nil {
		t.Fatal("turnStartParams succeeded, want missing model error")
	}
}

func TestStartConcurrentCallsShareInitializedProcess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake app-server shell script is Unix-only")
	}
	root := t.TempDir()
	logPath := filepath.Join(root, "rpc.log")
	t.Setenv("CODEX_TG_FAKE_APPSERVER_LOG", logPath)
	script := writeFakeAppServer(t, root, `#!/bin/sh
set -eu
log="${CODEX_TG_FAKE_APPSERVER_LOG:-}"
if IFS= read -r line; then
  if [ -n "$log" ]; then printf '%s\n' "$line" >> "$log"; fi
  sleep 0.2
  printf '{"jsonrpc":"2.0","id":1,"result":{}}\n'
fi
if IFS= read -r line; then
  if [ -n "$log" ]; then printf '%s\n' "$line" >> "$log"; fi
fi
sleep 5
`)
	client := NewClient(script, "stdio", root, 5*time.Second)
	defer client.Close()

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			errs[index] = client.Start(ctx)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			t.Fatalf("Start[%d] failed: %v", i, err)
		}
	}
	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("ReadFile(%s) failed: %v", logPath, err)
	}
	if got := strings.Count(string(data), `"method":"initialize"`); got != 1 {
		t.Fatalf("initialize requests = %d, want 1; log:\n%s", got, data)
	}
}

func TestStartCleansUpAfterInitializeFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake app-server shell script is Unix-only")
	}
	root := t.TempDir()
	script := writeFakeAppServer(t, root, `#!/bin/sh
set -eu
if IFS= read -r line; then
  printf '{"jsonrpc":"2.0","id":1,"error":{"message":"init failed"}}\n'
fi
sleep 5
`)
	client := NewClient(script, "stdio", root, 5*time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := client.Start(ctx)
	if err == nil {
		t.Fatal("Start succeeded, want initialize failure")
	}

	client.mu.Lock()
	started := client.started
	cmd := client.cmd
	stdin := client.stdin
	pending := len(client.pending)
	client.mu.Unlock()
	if started || cmd != nil || stdin != nil || pending != 0 {
		t.Fatalf("client state after failed Start: started=%t cmd_nil=%t stdin_nil=%t pending=%d", started, cmd == nil, stdin == nil, pending)
	}
	if _, requestErr := client.Request(context.Background(), "thread/list", nil); requestErr == nil || !strings.Contains(requestErr.Error(), "not running") {
		t.Fatalf("Request after failed Start error = %v, want not running", requestErr)
	}
}

func writeFakeAppServer(t *testing.T, root, body string) string {
	t.Helper()
	path := filepath.Join(root, "fake-codex")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatalf("WriteFile(fake app-server) failed: %v", err)
	}
	return path
}
