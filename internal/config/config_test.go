package config

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func TestFromEnvReadsCodexChatsRoot(t *testing.T) {
	root := filepath.Join(t.TempDir(), "Codex")
	t.Setenv("CTR_GO_CODEX_CHATS_ROOT", root)

	cfg := FromEnv()

	if cfg.CodexChatsRoot != root {
		t.Fatalf("CodexChatsRoot = %q, want %q", cfg.CodexChatsRoot, root)
	}
}

func TestMarshalJSONIncludesNotifyNewRun(t *testing.T) {
	t.Parallel()

	data, err := json.Marshal(Config{NotifyNewRun: true})
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if got["notify_new_run"] != true {
		t.Fatalf("notify_new_run = %#v, want true", got["notify_new_run"])
	}
}
