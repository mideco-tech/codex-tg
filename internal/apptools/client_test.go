package apptools

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func TestSendMessageToThreadUsesDesktopPipe(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets are not available on Windows")
	}
	root := t.TempDir()
	socketPath := filepath.Join(root, "desktop.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer listener.Close()

	requests := make(chan map[string]any, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		request, readErr := readFrame(conn)
		if readErr != nil {
			return
		}
		requests <- request
		_ = writeFrame(conn, map[string]any{
			"jsonrpc": "2.0",
			"id":      request["id"],
			"result": map[string]any{
				"success": true,
				"contentItems": []any{
					map[string]any{"type": "inputText", "text": `{"turnId":"turn-from-desktop"}`},
				},
			},
		})
	}()

	client := NewClient(root, 2*time.Second)
	result, err := client.SendMessageToThread(
		context.Background(),
		"thread-1",
		"Continue the work",
		"deepseek/deepseek-v4.1-flash",
		"low",
	)
	if err != nil {
		t.Fatalf("SendMessageToThread failed: %v", err)
	}
	if result.TurnID != "turn-from-desktop" {
		t.Fatalf("TurnID = %q, want turn-from-desktop", result.TurnID)
	}
	request := <-requests
	params, _ := request["params"].(map[string]any)
	if got := params["tool"]; got != "send_message_to_thread" {
		t.Fatalf("tool = %v, want send_message_to_thread", got)
	}
	arguments, _ := params["arguments"].(map[string]any)
	if got := arguments["model"]; got != "deepseek/deepseek-v4.1-flash" {
		t.Fatalf("model = %v, want Flash", got)
	}
	if got := arguments["thinking"]; got != "low" {
		t.Fatalf("thinking = %v, want low", got)
	}
}

func TestReadThreadParsesStructuredDesktopPayload(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix sockets are not available on Windows")
	}
	root := t.TempDir()
	socketPath := filepath.Join(root, "desktop.sock")
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("Listen failed: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		request, readErr := readFrame(conn)
		if readErr != nil {
			return
		}
		payload, _ := json.Marshal(map[string]any{
			"thread": map[string]any{"id": "thread-1", "status": map[string]any{"type": "idle"}},
		})
		_ = writeFrame(conn, map[string]any{
			"jsonrpc": "2.0",
			"id":      request["id"],
			"result": map[string]any{
				"success": true,
				"contentItems": []any{
					map[string]any{"type": "inputText", "text": string(payload)},
				},
			},
		})
	}()

	client := NewClient(root, 2*time.Second)
	payload, err := client.ReadThread(context.Background(), "thread-1")
	if err != nil {
		t.Fatalf("ReadThread failed: %v", err)
	}
	thread, _ := payload["thread"].(map[string]any)
	if got := thread["id"]; got != "thread-1" {
		t.Fatalf("thread id = %v, want thread-1", got)
	}
}

func TestLiveDesktopReadThread(t *testing.T) {
	threadID := os.Getenv("CODEX_TG_LIVE_APPTOOLS_THREAD_ID")
	if threadID == "" {
		t.Skip("set CODEX_TG_LIVE_APPTOOLS_THREAD_ID to inspect a live desktop thread")
	}
	client := NewClient("", 5*time.Second)
	payload, err := client.ReadThread(context.Background(), threadID)
	if err != nil {
		t.Fatalf("ReadThread failed: %v", err)
	}
	thread, _ := payload["thread"].(map[string]any)
	if got := thread["id"]; got != threadID {
		t.Fatalf("thread id = %v, want %s", got, threadID)
	}
}
