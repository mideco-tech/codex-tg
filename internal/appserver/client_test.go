package appserver

import "testing"

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
