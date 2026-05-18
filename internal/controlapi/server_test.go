package controlapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestHandlerExposesHealthAndStatus(t *testing.T) {
	t.Parallel()

	backend := &fakeBackend{doctor: map[string]any{"ready": true}}
	handler := NewHandler("test-version", backend)

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.Code)
	}
	var healthPayload map[string]any
	if err := json.Unmarshal(health.Body.Bytes(), &healthPayload); err != nil {
		t.Fatalf("health JSON failed: %v", err)
	}
	if healthPayload["version"] != "test-version" || healthPayload["ok"] != true {
		t.Fatalf("health payload = %#v", healthPayload)
	}

	status := httptest.NewRecorder()
	handler.ServeHTTP(status, httptest.NewRequest(http.MethodGet, "/v1/status", nil))
	if status.Code != http.StatusOK || !strings.Contains(status.Body.String(), `"ready":true`) {
		t.Fatalf("status = %d body=%s, want ready JSON", status.Code, status.Body.String())
	}
}

func TestHandlerListsAndReadsThreads(t *testing.T) {
	t.Parallel()

	backend := &fakeBackend{
		threadList: map[string]any{"threads": []any{map[string]any{"id": "thread-1"}}},
		threadRead: map[string]any{"thread": map[string]any{
			"id": "thread-1",
		}},
	}
	handler := NewHandler("test-version", backend)

	list := httptest.NewRecorder()
	handler.ServeHTTP(list, httptest.NewRequest(http.MethodGet, "/v1/threads?limit=250&cursor=next", nil))
	if list.Code != http.StatusOK {
		t.Fatalf("thread list status = %d body=%s", list.Code, list.Body.String())
	}
	if backend.listLimit != 100 || backend.listCursor != "next" {
		t.Fatalf("list args = limit %d cursor %q, want clamped limit and cursor", backend.listLimit, backend.listCursor)
	}

	read := httptest.NewRecorder()
	handler.ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/v1/threads/thread-1?include_turns=true", nil))
	if read.Code != http.StatusOK {
		t.Fatalf("thread read status = %d body=%s", read.Code, read.Body.String())
	}
	if backend.readThreadID != "thread-1" || !backend.readIncludeTurns {
		t.Fatalf("read args = thread %q include %t", backend.readThreadID, backend.readIncludeTurns)
	}
}

func TestHandlerRejectsStateChangingThreadMethods(t *testing.T) {
	t.Parallel()

	handler := NewHandler("test-version", &fakeBackend{})
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/threads", strings.NewReader(`{}`)))
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /v1/threads status = %d, want 405", response.Code)
	}
}

func TestListenAllowsOnlyLoopbackTCP(t *testing.T) {
	t.Parallel()

	for _, address := range []string{"127.0.0.1:0", "localhost:0", "[::1]:0"} {
		listener, err := Listen(address)
		if err != nil {
			t.Fatalf("Listen(%q) failed: %v", address, err)
		}
		if listener != nil {
			_ = listener.Close()
		}
	}
	for _, address := range []string{":8080", "0.0.0.0:8080", "192.168.1.10:8080", "example.com:8080"} {
		if listener, err := Listen(address); err == nil {
			if listener != nil {
				_ = listener.Close()
			}
			t.Fatalf("Listen(%q) succeeded, want loopback-only error", address)
		}
	}
}

type fakeBackend struct {
	doctor           map[string]any
	threadList       map[string]any
	threadRead       map[string]any
	listLimit        int
	listCursor       string
	readThreadID     string
	readIncludeTurns bool
}

func (b *fakeBackend) Doctor(ctx context.Context) (map[string]any, error) {
	return b.doctor, nil
}

func (b *fakeBackend) ControlThreadList(ctx context.Context, limit int, cursor string) (map[string]any, error) {
	b.listLimit = limit
	b.listCursor = cursor
	return b.threadList, nil
}

func (b *fakeBackend) ControlThreadRead(ctx context.Context, threadID string, includeTurns bool) (map[string]any, error) {
	b.readThreadID = threadID
	b.readIncludeTurns = includeTurns
	return b.threadRead, nil
}
