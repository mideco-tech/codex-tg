package apptools

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const maxFrameBytes = 8 * 1024 * 1024

type SendResult struct {
	Text   string
	TurnID string
}

type Client struct {
	directory string
	timeout   time.Duration
	nextID    atomic.Uint64
	nonce     string
}

func NewClient(directory string, timeout time.Duration) *Client {
	if strings.TrimSpace(directory) == "" {
		directory = filepath.Join(os.TempDir(), "codex-app-tools")
	}
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{directory: directory, timeout: timeout, nonce: randomNonce()}
}

func (c *Client) ReadThread(ctx context.Context, threadID string) (map[string]any, error) {
	result, err := c.callTool(ctx, threadID, "read_thread", map[string]any{
		"threadId":              threadID,
		"turnLimit":             1,
		"includeOutputs":        false,
		"maxOutputCharsPerItem": 4000,
	})
	if err != nil {
		return nil, err
	}
	_, payload, err := decodeToolContent(result)
	if err != nil {
		return nil, err
	}
	if payload == nil {
		return nil, errors.New("desktop read_thread returned no structured thread")
	}
	return payload, nil
}

func (c *Client) SendMessageToThread(ctx context.Context, threadID, prompt, model, thinking string) (SendResult, error) {
	arguments := map[string]any{
		"threadId": threadID,
		"prompt":   prompt,
	}
	if strings.TrimSpace(model) != "" {
		arguments["model"] = model
	}
	if strings.TrimSpace(thinking) != "" {
		arguments["thinking"] = thinking
	}
	result, err := c.callTool(ctx, threadID, "send_message_to_thread", arguments)
	if err != nil {
		return SendResult{}, err
	}
	text, payload, err := decodeToolContent(result)
	if err != nil {
		return SendResult{}, err
	}
	return SendResult{
		Text:   text,
		TurnID: findStringValue(payload, "turnId", "turn_id"),
	}, nil
}

func (c *Client) callTool(ctx context.Context, threadID, tool string, arguments map[string]any) (map[string]any, error) {
	paths, err := c.socketPaths()
	if err != nil {
		return nil, err
	}
	var failures []string
	for _, path := range paths {
		result, callErr := c.callToolOnSocket(ctx, path, threadID, tool, arguments)
		if callErr == nil {
			return result, nil
		}
		failures = append(failures, filepath.Base(path)+": "+callErr.Error())
	}
	return nil, fmt.Errorf("desktop app tools unavailable: %s", strings.Join(failures, "; "))
}

func (c *Client) callToolOnSocket(ctx context.Context, path, threadID, tool string, arguments map[string]any) (map[string]any, error) {
	dialer := net.Dialer{Timeout: c.timeout}
	conn, err := dialer.DialContext(ctx, "unix", path)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline := time.Now().Add(c.timeout)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}
	id := int64(c.nextID.Add(1))
	request := map[string]any{
		"jsonrpc": "2.0",
		"id":      id,
		"method":  "tools/call",
		"params": map[string]any{
			"arguments":    arguments,
			"callerSource": "codex",
			"callId":       fmt.Sprintf("ctr-go-%s-call-%d", c.nonce, id),
			"hostId":       "local",
			"namespace":    "codex_app",
			"threadId":     threadID,
			"tool":         tool,
			"turnId":       fmt.Sprintf("ctr-go-%s-turn-%d", c.nonce, id),
		},
	}
	if err := writeFrame(conn, request); err != nil {
		return nil, err
	}
	for {
		response, err := readFrame(conn)
		if err != nil {
			return nil, err
		}
		if !responseIDMatches(response["id"], id) {
			continue
		}
		if rpcError, ok := response["error"].(map[string]any); ok {
			return nil, fmt.Errorf("desktop app tool %s failed: %s", tool, stringValue(rpcError["message"]))
		}
		result, ok := response["result"].(map[string]any)
		if !ok {
			return nil, errors.New("desktop app tool returned an invalid result")
		}
		return result, nil
	}
}

func randomNonce() string {
	value := make([]byte, 12)
	if _, err := rand.Read(value); err != nil {
		return fmt.Sprint(time.Now().UnixNano())
	}
	return hex.EncodeToString(value)
}

func (c *Client) socketPaths() ([]string, error) {
	entries, err := os.ReadDir(c.directory)
	if err != nil {
		return nil, err
	}
	type socketEntry struct {
		path    string
		modTime time.Time
	}
	sockets := make([]socketEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sock") {
			continue
		}
		info, err := entry.Info()
		if err != nil || info.Mode()&os.ModeSocket == 0 {
			continue
		}
		sockets = append(sockets, socketEntry{
			path:    filepath.Join(c.directory, entry.Name()),
			modTime: info.ModTime(),
		})
	}
	if len(sockets) == 0 {
		return nil, errors.New("no Codex desktop app tool sockets found")
	}
	sort.Slice(sockets, func(i, j int) bool {
		return sockets[i].modTime.After(sockets[j].modTime)
	})
	paths := make([]string, 0, len(sockets))
	for _, socket := range sockets {
		paths = append(paths, socket.path)
	}
	return paths, nil
}

func decodeToolContent(result map[string]any) (string, map[string]any, error) {
	if success, ok := result["success"].(bool); ok && !success {
		return "", nil, errors.New("desktop app tool reported failure")
	}
	items, _ := result["contentItems"].([]any)
	var textParts []string
	var payload map[string]any
	for _, rawItem := range items {
		item, _ := rawItem.(map[string]any)
		if !strings.EqualFold(stringValue(item["type"]), "inputText") {
			continue
		}
		text := strings.TrimSpace(stringValue(item["text"]))
		if text == "" {
			continue
		}
		textParts = append(textParts, text)
		if payload == nil {
			var decoded map[string]any
			if json.Unmarshal([]byte(text), &decoded) == nil {
				payload = decoded
			}
		}
	}
	return strings.Join(textParts, "\n"), payload, nil
}

func writeFrame(writer io.Writer, value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	if len(payload) > maxFrameBytes {
		return errors.New("desktop app tool request is too large")
	}
	header := make([]byte, 4)
	binary.LittleEndian.PutUint32(header, uint32(len(payload)))
	if _, err := writer.Write(header); err != nil {
		return err
	}
	_, err = writer.Write(payload)
	return err
}

func readFrame(reader io.Reader) (map[string]any, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(reader, header); err != nil {
		return nil, err
	}
	length := int(binary.LittleEndian.Uint32(header))
	if length <= 0 || length > maxFrameBytes {
		return nil, errors.New("invalid desktop app tool frame size")
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(reader, payload); err != nil {
		return nil, err
	}
	var response map[string]any
	if err := json.Unmarshal(payload, &response); err != nil {
		return nil, err
	}
	return response, nil
}

func responseIDMatches(value any, expected int64) bool {
	switch typed := value.(type) {
	case float64:
		return int64(typed) == expected
	case int64:
		return typed == expected
	case json.Number:
		parsed, err := typed.Int64()
		return err == nil && parsed == expected
	case string:
		return typed == fmt.Sprint(expected)
	default:
		return false
	}
}

func findStringValue(value any, keys ...string) string {
	switch typed := value.(type) {
	case map[string]any:
		for _, key := range keys {
			if text := strings.TrimSpace(stringValue(typed[key])); text != "" {
				return text
			}
		}
		for _, child := range typed {
			if found := findStringValue(child, keys...); found != "" {
				return found
			}
		}
	case []any:
		for _, child := range typed {
			if found := findStringValue(child, keys...); found != "" {
				return found
			}
		}
	}
	return ""
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}
