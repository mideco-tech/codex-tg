package tests

import (
	"testing"

	"github.com/mideco-tech/codex-tg/internal/model"
)

func TestProjectNameFromCWDUsesSharedGeneralFallbacks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name        string
		cwd         string
		wantProject string
		wantDirName string
	}{
		{
			name:        "empty path falls back to shared general",
			cwd:         "",
			wantProject: "Shared/General",
			wantDirName: "",
		},
		{
			name:        "special documents codex path is shared general",
			cwd:         `C:\Users\you\Documents\Codex`,
			wantProject: "Shared/General",
			wantDirName: "General",
		},
		{
			name:        "regular repo path uses basename",
			cwd:         `C:\Users\you\Projects\Codex\chrome-ai-assistant-extension`,
			wantProject: "chrome-ai-assistant-extension",
			wantDirName: "chrome-ai-assistant-extension",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			project, dirName := model.ProjectNameFromCWD(tc.cwd)
			if project != tc.wantProject {
				t.Fatalf("project = %q, want %q", project, tc.wantProject)
			}
			if dirName != tc.wantDirName {
				t.Fatalf("directory = %q, want %q", dirName, tc.wantDirName)
			}
		})
	}
}

func TestThreadShortIDAndLabelFollowOracleShape(t *testing.T) {
	t.Parallel()

	thread := model.Thread{
		ID:          "019db243-0fc6-7fe3-a0ca-7a54a2ca9c40",
		Title:       "Проверь версию Node.js",
		ProjectName: "2026-04-22-node",
	}

	if got, want := thread.ShortID(), "019db243"; got != want {
		t.Fatalf("ShortID() = %q, want %q", got, want)
	}

	if got, want := thread.Label(), "[2026-04-22-node] Проверь версию Node.js"; got != want {
		t.Fatalf("Label() = %q, want %q", got, want)
	}
}

func TestThreadLabelFallsBackToShortIDWhenTitleMissing(t *testing.T) {
	t.Parallel()

	thread := model.Thread{
		ID:          "019db243-0fc6-7fe3-a0ca-7a54a2ca9c40",
		ProjectName: "Shared/General",
	}

	if got, want := thread.Label(), "[Shared/General] 019db243"; got != want {
		t.Fatalf("Label() = %q, want %q", got, want)
	}
}

func TestChatKeyIsStableForRoutingState(t *testing.T) {
	t.Parallel()

	if got, want := model.ChatKey(123456789, 0), "123456789:0"; got != want {
		t.Fatalf("ChatKey() = %q, want %q", got, want)
	}
}
