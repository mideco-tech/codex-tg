package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mideco-tech/codex-tg/internal/appserver"
	"github.com/mideco-tech/codex-tg/internal/model"
	"github.com/mideco-tech/codex-tg/internal/storage"
)

const (
	newThreadStateTTL                  = 15 * time.Minute
	chatsProjectName                   = "Chats"
	defaultProjectsProjectPreviewLimit = 7
	defaultProjectsChatPreviewLimit    = 3
	defaultChatsPageSize               = 8
)

type projectWorkspace struct {
	Key           string `json:"key,omitempty"`
	ProjectName   string `json:"project_name"`
	DirectoryName string `json:"directory_name,omitempty"`
	CWD           string `json:"cwd"`
	LatestThread  string `json:"latest_thread_id,omitempty"`
	ThreadCount   int    `json:"thread_count,omitempty"`
	UpdatedAt     int64  `json:"updated_at,omitempty"`
}

type pendingNewThreadState struct {
	ProjectName   string `json:"project_name"`
	DirectoryName string `json:"directory_name,omitempty"`
	CWD           string `json:"cwd"`
	ExpiresAt     string `json:"expires_at"`
}

type projectCatalog struct {
	Workspaces []projectWorkspace
	Chats      []model.Thread
}

func (s *Service) projectCatalog(ctx context.Context) (projectCatalog, error) {
	threads, err := s.store.ListThreads(ctx, 500, "")
	if err != nil {
		return projectCatalog{}, err
	}
	grouped := map[string]*projectWorkspace{}
	chats := []model.Thread{}
	for _, thread := range threads {
		if isCodexChatThread(thread) {
			chats = append(chats, thread)
			continue
		}
		cwdKey := model.NormalizePath(thread.CWD)
		if cwdKey == "" {
			cwdKey = "thread:" + thread.ID
		}
		workspace := grouped[cwdKey]
		if workspace == nil {
			projectName := strings.TrimSpace(thread.ProjectName)
			directoryName := strings.TrimSpace(thread.DirectoryName)
			if projectName == "" || directoryName == "" {
				derivedProject, derivedDirectory := model.ProjectNameFromCWD(thread.CWD)
				projectName = firstNonEmpty(projectName, derivedProject)
				directoryName = firstNonEmpty(directoryName, derivedDirectory)
			}
			workspace = &projectWorkspace{
				ProjectName:   firstNonEmpty(projectName, "Shared/General"),
				DirectoryName: directoryName,
				CWD:           thread.CWD,
				LatestThread:  thread.ID,
				UpdatedAt:     thread.UpdatedAt,
			}
			grouped[cwdKey] = workspace
		}
		workspace.ThreadCount++
		if thread.UpdatedAt > workspace.UpdatedAt {
			workspace.UpdatedAt = thread.UpdatedAt
			workspace.LatestThread = thread.ID
		}
	}
	workspaces := make([]projectWorkspace, 0, len(grouped))
	for _, workspace := range grouped {
		workspaces = append(workspaces, *workspace)
	}
	sort.Slice(workspaces, func(i, j int) bool {
		if workspaces[i].UpdatedAt != workspaces[j].UpdatedAt {
			return workspaces[i].UpdatedAt > workspaces[j].UpdatedAt
		}
		leftName := strings.ToLower(workspaces[i].ProjectName)
		rightName := strings.ToLower(workspaces[j].ProjectName)
		if leftName != rightName {
			return leftName < rightName
		}
		leftCWD := strings.ToLower(model.NormalizePath(workspaces[i].CWD))
		rightCWD := strings.ToLower(model.NormalizePath(workspaces[j].CWD))
		return leftCWD < rightCWD
	})
	sort.Slice(chats, func(i, j int) bool {
		if chats[i].UpdatedAt != chats[j].UpdatedAt {
			return chats[i].UpdatedAt > chats[j].UpdatedAt
		}
		return strings.ToLower(chats[i].Label()) < strings.ToLower(chats[j].Label())
	})
	assignProjectWorkspaceKeys(workspaces)
	return projectCatalog{Workspaces: workspaces, Chats: chats}, nil
}

func (s *Service) projectWorkspaces(ctx context.Context) ([]projectWorkspace, error) {
	catalog, err := s.projectCatalog(ctx)
	if err != nil {
		return nil, err
	}
	return catalog.Workspaces, nil
}

func isCodexChatThread(thread model.Thread) bool {
	if isCodexChatsCWD(thread.CWD) {
		return true
	}
	return strings.TrimSpace(thread.CWD) == "" && strings.EqualFold(strings.TrimSpace(thread.ProjectName), chatsProjectName)
}

func isCodexChatsCWD(cwd string) bool {
	normalized := strings.ToLower(model.NormalizePath(cwd))
	if normalized == "" {
		return false
	}
	parts := strings.Split(strings.Trim(normalized, "/"), "/")
	if len(parts) >= 4 && parts[0] == "users" && parts[2] == "documents" && parts[3] == "codex" {
		return true
	}
	return len(parts) >= 5 && strings.HasSuffix(parts[0], ":") && parts[1] == "users" && parts[3] == "documents" && parts[4] == "codex"
}

func (s *Service) projectsProjectPreviewLimit() int {
	return positiveOrDefault(s.cfg.ProjectsProjectPreviewLimit, defaultProjectsProjectPreviewLimit)
}

func (s *Service) projectsChatPreviewLimit() int {
	return positiveOrDefault(s.cfg.ProjectsChatPreviewLimit, defaultProjectsChatPreviewLimit)
}

func (s *Service) chatsPageSize() int {
	return positiveOrDefault(s.cfg.ChatsPageSize, defaultChatsPageSize)
}

func positiveOrDefault(value, fallback int) int {
	if value <= 0 {
		return fallback
	}
	return value
}

func assignProjectWorkspaceKeys(workspaces []projectWorkspace) {
	seen := map[string]int{}
	for i := range workspaces {
		base := projectWorkspaceKeyBase(workspaces[i])
		seen[base]++
		if seen[base] == 1 {
			workspaces[i].Key = base
			continue
		}
		workspaces[i].Key = fmt.Sprintf("%s-%d", base, seen[base])
	}
}

func projectWorkspaceKeyBase(workspace projectWorkspace) string {
	source := strings.TrimSpace(firstNonEmpty(workspace.ProjectName, workspace.DirectoryName, workspace.CWD, "project"))
	source = strings.ToLower(source)
	var builder strings.Builder
	lastDash := false
	for _, r := range source {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			builder.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				builder.WriteByte('-')
				lastDash = true
			}
		}
	}
	key := strings.Trim(builder.String(), "-")
	if key == "" {
		return "project"
	}
	return key
}

func (s *Service) projectsOverview(ctx context.Context) (*DirectResponse, error) {
	return s.projectsOverviewPage(ctx, 0)
}

func (s *Service) projectsOverviewPage(ctx context.Context, page int) (*DirectResponse, error) {
	catalog, err := s.projectCatalog(ctx)
	if err != nil {
		return nil, err
	}
	if len(catalog.Workspaces) == 0 && len(catalog.Chats) == 0 {
		return &DirectResponse{Text: "No cached projects yet. Try /status or wait for sync."}, nil
	}
	projectLimit := s.projectsProjectPreviewLimit()
	chatLimit := s.projectsChatPreviewLimit()
	projectPages := maxInt(1, (len(catalog.Workspaces)+projectLimit-1)/projectLimit)
	page = clampInt(page, 0, projectPages-1)
	projectStart := page * projectLimit
	projectEnd := min(projectStart+projectLimit, len(catalog.Workspaces))
	chatEnd := min(chatLimit, len(catalog.Chats))

	lines := []string{
		"Projects",
		fmt.Sprintf("Project page %d/%d (showing %d of %d)", page+1, projectPages, maxInt(0, projectEnd-projectStart), len(catalog.Workspaces)),
		fmt.Sprintf("Latest Chats: showing %d of %d", chatEnd, len(catalog.Chats)),
		"Open Chats to view all Chat threads.",
	}
	buttons := [][]model.ButtonSpec{
		{
			s.callbackButton(ctx, "<", "projects_page", "", "", "", map[string]any{"page": page - 1}),
			s.callbackButton(ctx, "Close", "projects_close", "", "", "", nil),
			s.callbackButton(ctx, ">", "projects_page", "", "", "", map[string]any{"page": page + 1}),
		},
		{s.callbackButton(ctx, "Open Chats", "chats_open", "", "", "", map[string]any{"page": 0})},
	}
	if len(catalog.Workspaces) == 0 {
		lines = append(lines, "", "No cached projects outside Chats.")
	}
	for index, workspace := range catalog.Workspaces[projectStart:projectEnd] {
		displayIndex := projectStart + index + 1
		lines = append(lines,
			fmt.Sprintf("%d. %s (%d thread(s))", displayIndex, workspace.ProjectName, workspace.ThreadCount),
			fmt.Sprintf("   key: %s", workspace.Key),
			fmt.Sprintf("   cwd: %s", settingValueLabel(workspace.CWD, "unknown")),
		)
		buttons = append(buttons, []model.ButtonSpec{
			s.callbackButton(ctx, fmt.Sprintf("Project %d", displayIndex), "project_open", "", "", "", projectWorkspacePayload(workspace)),
		})
	}
	if chatEnd > 0 {
		lines = append(lines, "", "Latest Chats")
		for index, thread := range catalog.Chats[:chatEnd] {
			displayIndex := index + 1
			lines = append(lines, fmt.Sprintf("%d. %s | %s", displayIndex, firstNonEmpty(thread.Title, thread.ShortID()), thread.ShortID()))
			buttons = append(buttons, []model.ButtonSpec{
				s.callbackButton(ctx, fmt.Sprintf("Chat %d", displayIndex), "chat_open", thread.ID, "", "", nil),
			})
		}
	}
	return &DirectResponse{Text: strings.Join(lines, "\n"), Buttons: buttons}, nil
}

func (s *Service) chatsOverviewPage(ctx context.Context, page int) (*DirectResponse, error) {
	catalog, err := s.projectCatalog(ctx)
	if err != nil {
		return nil, err
	}
	pageSize := s.chatsPageSize()
	pageCount := maxInt(1, (len(catalog.Chats)+pageSize-1)/pageSize)
	page = clampInt(page, 0, pageCount-1)
	start := page * pageSize
	end := min(start+pageSize, len(catalog.Chats))
	lines := []string{
		"Chats",
		fmt.Sprintf("Page %d/%d (showing %d of %d)", page+1, pageCount, maxInt(0, end-start), len(catalog.Chats)),
		"Use /newchat <prompt> to create a new Chat.",
	}
	buttons := [][]model.ButtonSpec{
		{
			s.callbackButton(ctx, "<", "chats_page", "", "", "", map[string]any{"page": page - 1}),
			s.callbackButton(ctx, "Close", "projects_close", "", "", "", nil),
			s.callbackButton(ctx, ">", "chats_page", "", "", "", map[string]any{"page": page + 1}),
		},
	}
	if len(catalog.Chats) == 0 {
		lines = append(lines, "", "No cached Chats yet.")
		return &DirectResponse{Text: strings.Join(lines, "\n"), Buttons: buttons}, nil
	}
	for index, thread := range catalog.Chats[start:end] {
		displayIndex := start + index + 1
		lines = append(lines, fmt.Sprintf("%d. %s | %s", displayIndex, firstNonEmpty(thread.Title, thread.ShortID()), thread.ShortID()))
		buttons = append(buttons, []model.ButtonSpec{
			s.callbackButton(ctx, fmt.Sprintf("Chat %d", index+1), "chat_open", thread.ID, "", "", nil),
		})
	}
	return &DirectResponse{Text: strings.Join(lines, "\n"), Buttons: buttons}, nil
}

func projectWorkspacePayload(workspace projectWorkspace) map[string]any {
	return map[string]any{
		"key":            workspace.Key,
		"project_name":   workspace.ProjectName,
		"directory_name": workspace.DirectoryName,
		"cwd":            workspace.CWD,
		"latest_thread":  workspace.LatestThread,
		"thread_count":   workspace.ThreadCount,
	}
}

func projectWorkspaceFromPayload(payload map[string]any) projectWorkspace {
	return projectWorkspace{
		Key:           payloadMapString(payload, "key"),
		ProjectName:   payloadMapString(payload, "project_name"),
		DirectoryName: payloadMapString(payload, "directory_name"),
		CWD:           payloadMapString(payload, "cwd"),
		LatestThread:  payloadMapString(payload, "latest_thread"),
		ThreadCount:   payloadMapInt(payload, "thread_count"),
	}
}

func payloadMapInt(values map[string]any, key string) int {
	if values == nil {
		return 0
	}
	switch typed := values[key].(type) {
	case int:
		return typed
	case int64:
		return int(typed)
	case float64:
		return int(typed)
	case json.Number:
		value, _ := typed.Int64()
		return int(value)
	case string:
		value, _ := strconv.Atoi(strings.TrimSpace(typed))
		return value
	default:
		return 0
	}
}

func (s *Service) projectsPage(ctx context.Context, chatID, topicID, messageID int64, payload map[string]any) (*DirectResponse, error) {
	return s.editOrSendProjectsResponse(ctx, chatID, topicID, messageID, "Projects", func(ctx context.Context) (*DirectResponse, error) {
		return s.projectsOverviewPage(ctx, payloadMapInt(payload, "page"))
	})
}

func (s *Service) chatsPage(ctx context.Context, chatID, topicID, messageID int64, payload map[string]any) (*DirectResponse, error) {
	return s.editOrSendProjectsResponse(ctx, chatID, topicID, messageID, "Chats", func(ctx context.Context) (*DirectResponse, error) {
		return s.chatsOverviewPage(ctx, payloadMapInt(payload, "page"))
	})
}

func (s *Service) closeProjectsMenu(ctx context.Context, chatID, topicID, messageID int64) (*DirectResponse, error) {
	s.mu.RLock()
	sender := s.sender
	s.mu.RUnlock()
	if sender != nil && messageID != 0 {
		if err := sender.DeleteMessage(ctx, chatID, topicID, messageID); err == nil {
			return &DirectResponse{CallbackText: "Closed."}, nil
		}
		if err := sender.EditMessage(ctx, chatID, topicID, messageID, "Closed.", nil); err == nil {
			return &DirectResponse{CallbackText: "Closed."}, nil
		}
	}
	return &DirectResponse{Text: "Closed.", CallbackText: "Closed."}, nil
}

func (s *Service) editOrSendProjectsResponse(ctx context.Context, chatID, topicID, messageID int64, callbackText string, renderer func(context.Context) (*DirectResponse, error)) (*DirectResponse, error) {
	response, err := renderer(ctx)
	if err != nil {
		return nil, err
	}
	if response == nil {
		return &DirectResponse{CallbackText: callbackText}, nil
	}
	response.CallbackText = callbackText
	s.mu.RLock()
	sender := s.sender
	s.mu.RUnlock()
	if sender != nil && messageID != 0 && strings.TrimSpace(response.Text) != "" {
		if err := sender.EditMessage(ctx, chatID, topicID, messageID, response.Text, response.Buttons); err == nil {
			return &DirectResponse{CallbackText: callbackText}, nil
		}
	}
	return response, nil
}

func (s *Service) projectWorkspaceFromCallback(ctx context.Context, payload map[string]any) (projectWorkspace, bool) {
	requested := projectWorkspaceFromPayload(payload)
	requestedCWD := model.NormalizePath(requested.CWD)
	if requestedCWD == "" {
		return projectWorkspace{}, false
	}
	workspaces, err := s.projectWorkspaces(ctx)
	if err != nil {
		return projectWorkspace{}, false
	}
	for _, workspace := range workspaces {
		if model.NormalizePath(workspace.CWD) == requestedCWD {
			return workspace, true
		}
	}
	return projectWorkspace{}, false
}

func (s *Service) openChatThread(ctx context.Context, chatID, topicID int64, threadID string) (*DirectResponse, error) {
	threadID = strings.TrimSpace(threadID)
	if threadID == "" {
		return &DirectResponse{Text: "This Chat button is stale. Use Open Chats to refresh."}, nil
	}
	thread, err := s.store.GetThread(ctx, threadID)
	if err != nil {
		return nil, err
	}
	if thread == nil || !isCodexChatThread(*thread) {
		return &DirectResponse{Text: "This Chat button is stale. Use Open Chats to refresh."}, nil
	}
	if err := s.store.SetBinding(ctx, chatID, topicID, threadID, model.BindingModeBound); err != nil {
		return nil, err
	}
	s.kickBootstrap()
	response, err := s.showThread(ctx, chatID, topicID, threadID, true)
	if err != nil {
		return nil, err
	}
	if response == nil {
		response = &DirectResponse{}
	}
	response.CallbackText = "Opened Chat."
	return response, nil
}

func (s *Service) projectMenu(ctx context.Context, payload map[string]any) (*DirectResponse, error) {
	workspace, ok := s.projectWorkspaceFromCallback(ctx, payload)
	if !ok {
		return &DirectResponse{Text: "This project button is stale. Use /projects to refresh."}, nil
	}
	lines := []string{
		"Project",
		fmt.Sprintf("Name: %s", workspace.ProjectName),
		fmt.Sprintf("Directory: %s", settingValueLabel(workspace.DirectoryName, "unknown")),
		fmt.Sprintf("CWD: %s", settingValueLabel(workspace.CWD, "unknown")),
		fmt.Sprintf("Cached threads: %d", workspace.ThreadCount),
	}
	buttons := [][]model.ButtonSpec{
		{s.callbackButton(ctx, "New thread", "project_new_thread", "", "", "", projectWorkspacePayload(workspace))},
		{
			s.callbackButton(ctx, "Threads", "project_threads", "", "", "", projectWorkspacePayload(workspace)),
			s.callbackButton(ctx, "Bind latest", "project_bind_latest", workspace.LatestThread, "", "", projectWorkspacePayload(workspace)),
		},
	}
	return &DirectResponse{Text: strings.Join(lines, "\n"), Buttons: buttons}, nil
}

func (s *Service) projectThreads(ctx context.Context, payload map[string]any) (*DirectResponse, error) {
	workspace, ok := s.projectWorkspaceFromCallback(ctx, payload)
	if !ok {
		return &DirectResponse{Text: "This project button is stale. Use /projects to refresh."}, nil
	}
	threads, err := s.store.ListThreads(ctx, 500, "")
	if err != nil {
		return nil, err
	}
	lines := []string{fmt.Sprintf("Threads for %s", workspace.ProjectName)}
	buttons := [][]model.ButtonSpec{}
	for _, thread := range threads {
		if model.NormalizePath(thread.CWD) != model.NormalizePath(workspace.CWD) {
			continue
		}
		lines = append(lines, fmt.Sprintf("- %s | %s", firstNonEmpty(thread.Title, thread.ShortID()), thread.ShortID()))
		buttons = append(buttons, []model.ButtonSpec{
			s.callbackButton(ctx, shortButtonLabel(firstNonEmpty(thread.Title, thread.ShortID())), "show_thread", thread.ID, "", "", nil),
		})
	}
	if len(buttons) == 0 {
		lines = append(lines, "No cached threads for this project.")
	}
	return &DirectResponse{Text: strings.Join(lines, "\n"), Buttons: buttons}, nil
}

func (s *Service) bindLatestProjectThread(ctx context.Context, chatID, topicID int64, payload map[string]any) (*DirectResponse, error) {
	workspace, ok := s.projectWorkspaceFromCallback(ctx, payload)
	if !ok || strings.TrimSpace(workspace.LatestThread) == "" {
		return &DirectResponse{Text: "This project button is stale. Use /projects to refresh."}, nil
	}
	if err := s.store.SetBinding(ctx, chatID, topicID, workspace.LatestThread, model.BindingModeBound); err != nil {
		return nil, err
	}
	s.kickBootstrap()
	return &DirectResponse{CallbackText: fmt.Sprintf("Bound this chat to %s.", workspace.LatestThread)}, nil
}

func (s *Service) armProjectNewThread(ctx context.Context, chatID, topicID int64, payload map[string]any) (*DirectResponse, error) {
	workspace, ok := s.projectWorkspaceFromCallback(ctx, payload)
	if !ok {
		return &DirectResponse{Text: "This project button is stale. Use /projects to refresh."}, nil
	}
	if strings.TrimSpace(workspace.CWD) == "" {
		return &DirectResponse{Text: "Project cwd is not available. Use /projects after Codex has seen this workspace."}, nil
	}
	state := pendingNewThreadState{
		ProjectName:   workspace.ProjectName,
		DirectoryName: workspace.DirectoryName,
		CWD:           workspace.CWD,
		ExpiresAt:     time.Now().UTC().Add(newThreadStateTTL).Format(time.RFC3339Nano),
	}
	payloadBytes, _ := json.Marshal(state)
	if err := s.store.SetState(ctx, newThreadStateKey(chatID, topicID), string(payloadBytes)); err != nil {
		return nil, err
	}
	return &DirectResponse{Text: fmt.Sprintf("New thread for %s.\nSend the first prompt as your next message.", workspace.ProjectName)}, nil
}

func (s *Service) newThreadCommand(ctx context.Context, chatID, topicID int64, rest string) (*DirectResponse, error) {
	selector, prompt := splitCommandHead(rest)
	if selector == "" || strings.TrimSpace(prompt) == "" {
		return s.newThreadUsage(ctx)
	}
	workspace, ok := s.resolveProjectWorkspaceSelector(ctx, selector)
	if !ok {
		return s.newThreadUsage(ctx)
	}
	return s.createThreadFromProjectPrompt(ctx, chatID, topicID, pendingNewThreadState{
		ProjectName:   workspace.ProjectName,
		DirectoryName: workspace.DirectoryName,
		CWD:           workspace.CWD,
	}, strings.TrimSpace(prompt))
}

func (s *Service) newChatCommand(ctx context.Context, chatID, topicID int64, rest string) (*DirectResponse, error) {
	prompt := strings.TrimSpace(rest)
	if prompt == "" {
		return &DirectResponse{Text: "Usage: /newchat <prompt>"}, nil
	}
	return s.createThreadFromProjectPrompt(ctx, chatID, topicID, pendingNewThreadState{
		ProjectName: chatsProjectName,
	}, prompt)
}

func (s *Service) newThreadUsage(ctx context.Context) (*DirectResponse, error) {
	workspaces, err := s.projectWorkspaces(ctx)
	if err != nil {
		return nil, err
	}
	lines := []string{"Usage: /new <project-key-or-number> <prompt>"}
	if len(workspaces) == 0 {
		lines = append(lines, "No cached projects yet. Try /projects after Codex has seen a workspace.")
		return &DirectResponse{Text: strings.Join(lines, "\n")}, nil
	}
	lines = append(lines, "", "Projects:")
	for index, workspace := range workspaces {
		lines = append(lines, fmt.Sprintf("%d. %s (%s)", index+1, workspace.Key, workspace.ProjectName))
	}
	return &DirectResponse{Text: strings.Join(lines, "\n")}, nil
}

func (s *Service) resolveProjectWorkspaceSelector(ctx context.Context, selector string) (projectWorkspace, bool) {
	workspaces, err := s.projectWorkspaces(ctx)
	if err != nil {
		return projectWorkspace{}, false
	}
	selector = strings.TrimSpace(strings.ToLower(selector))
	if selector == "" {
		return projectWorkspace{}, false
	}
	if index, err := strconv.Atoi(selector); err == nil && index >= 1 && index <= len(workspaces) {
		return workspaces[index-1], true
	}
	for _, workspace := range workspaces {
		if strings.EqualFold(workspace.Key, selector) ||
			strings.EqualFold(workspace.ProjectName, selector) ||
			strings.EqualFold(workspace.DirectoryName, selector) {
			return workspace, true
		}
	}
	return projectWorkspace{}, false
}

func (s *Service) maybeConsumeNewThreadPrompt(ctx context.Context, chatID, topicID int64, text string) (*DirectResponse, bool, error) {
	state, ok, expired, err := s.pendingNewThreadState(ctx, chatID, topicID)
	if err != nil {
		return nil, true, err
	}
	if !ok {
		return nil, false, nil
	}
	if expired {
		_ = s.store.DeleteState(ctx, newThreadStateKey(chatID, topicID))
		return &DirectResponse{Text: "New thread request expired. Use /projects and New thread again."}, true, nil
	}
	_ = s.store.DeleteState(ctx, newThreadStateKey(chatID, topicID))
	response, err := s.createThreadFromProjectPrompt(ctx, chatID, topicID, state, strings.TrimSpace(text))
	return response, true, err
}

func (s *Service) pendingNewThreadState(ctx context.Context, chatID, topicID int64) (pendingNewThreadState, bool, bool, error) {
	raw, err := s.store.GetState(ctx, newThreadStateKey(chatID, topicID))
	if err != nil {
		return pendingNewThreadState{}, false, false, err
	}
	if strings.TrimSpace(raw) == "" {
		return pendingNewThreadState{}, false, false, nil
	}
	var state pendingNewThreadState
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return pendingNewThreadState{}, true, true, nil
	}
	expiresAt, err := time.Parse(time.RFC3339Nano, state.ExpiresAt)
	if err != nil || time.Now().UTC().After(expiresAt) {
		return state, true, true, nil
	}
	return state, true, false, nil
}

func (s *Service) createThreadFromProjectPrompt(ctx context.Context, chatID, topicID int64, state pendingNewThreadState, prompt string) (*DirectResponse, error) {
	if strings.TrimSpace(prompt) == "" {
		return &DirectResponse{Text: "First prompt is empty. Use New thread again and send a non-empty prompt."}, nil
	}
	s.mu.RLock()
	live := s.live
	connected := s.liveConnected
	s.mu.RUnlock()
	if !connected || live == nil {
		return &DirectResponse{Text: "Live app-server session is not ready yet. Try /status or /repair."}, nil
	}
	requestCtx, cancel := context.WithTimeout(ctx, s.cfg.RequestTimeout)
	defer cancel()
	started := time.Now()
	threadPayload, err := live.ThreadStart(requestCtx, state.CWD)
	s.logAppServerCall("ThreadStart", started, err, live, lifecycleFields{
		"cwd":          state.CWD,
		"project_name": state.ProjectName,
	})
	if err != nil {
		return nil, err
	}
	thread := threadFromStartPayload(threadPayload, state)
	if strings.TrimSpace(thread.ID) == "" {
		return &DirectResponse{Text: "App Server could not create thread: response did not include thread id."}, nil
	}
	if err := s.store.UpsertThread(ctx, thread); err != nil {
		return nil, err
	}

	options := s.turnStartOptions(ctx, "", &thread)
	started = time.Now()
	turnPayload, err := live.TurnStart(requestCtx, thread.ID, prompt, thread.CWD, options)
	s.logAppServerCall("TurnStart", started, err, live, lifecycleFields{
		"thread_id":        thread.ID,
		"returned_turn_id": appserverThreadTurnID(turnPayload),
		"model":            options.Model,
		"reasoning_effort": options.ReasoningEffort,
	})
	if err != nil {
		_ = s.store.SetBinding(ctx, chatID, topicID, thread.ID, model.BindingModeBound)
		return &DirectResponse{
			Text:     fmt.Sprintf("Created thread %s, but could not start first turn: %v\nUse /reply %s <text> to retry.", thread.ID, err, thread.ID),
			ThreadID: thread.ID,
		}, nil
	}
	turnID := appserverThreadTurnID(turnPayload)
	if strings.TrimSpace(turnID) != "" {
		thread.ActiveTurnID = turnID
		thread.Status = "inProgress"
		thread.LastPreview = prompt
		_ = s.store.UpsertThread(ctx, thread)
		_ = s.markTelegramOriginTurnFromTelegram(ctx, thread.ID, turnID, chatID, topicID)
		s.ensureStartedTurnSnapshot(ctx, &thread, turnID)
	}
	if _, refreshErr := s.refreshThreadForOperation(ctx, live, thread.ID, "refresh_new_thread_after_start"); refreshErr != nil {
		s.logLifecycle("thread_refresh_failed", lifecycleFields{
			"operation": "refresh_new_thread_after_start",
			"thread_id": thread.ID,
			"turn_id":   turnID,
			"error":     refreshErr,
		})
	}
	s.ensureNewChatThreadFallback(ctx, thread.ID, state)
	if err := s.store.SetBinding(ctx, chatID, topicID, thread.ID, model.BindingModeBound); err != nil {
		return nil, err
	}
	target := model.ObserverTarget{ChatKey: model.ChatKey(chatID, topicID), ChatID: chatID, TopicID: topicID, Enabled: true}
	s.syncThreadPanelToTarget(ctx, target, thread.ID, true, model.PanelSourceTelegramInput)
	if strings.TrimSpace(turnID) != "" {
		s.startTelegramOriginHotPoll(ctx, thread.ID, turnID)
	}
	return &DirectResponse{ThreadID: thread.ID, TurnID: turnID}, nil
}

func (s *Service) ensureNewChatThreadFallback(ctx context.Context, threadID string, state pendingNewThreadState) {
	if !strings.EqualFold(strings.TrimSpace(state.ProjectName), chatsProjectName) {
		return
	}
	thread, err := s.store.GetThread(ctx, threadID)
	if err != nil || thread == nil || strings.TrimSpace(thread.CWD) != "" {
		return
	}
	if strings.EqualFold(strings.TrimSpace(thread.ProjectName), chatsProjectName) {
		return
	}
	thread.ProjectName = chatsProjectName
	thread.DirectoryName = firstNonEmpty(state.DirectoryName, thread.DirectoryName)
	_ = s.store.UpsertThread(ctx, *thread)
}

func threadFromStartPayload(payload map[string]any, state pendingNewThreadState) model.Thread {
	thread := appserver.ThreadFromPayload(payload)
	if strings.TrimSpace(thread.CWD) == "" {
		thread.CWD = state.CWD
	}
	if strings.EqualFold(strings.TrimSpace(state.ProjectName), chatsProjectName) && strings.TrimSpace(thread.CWD) == "" {
		thread.ProjectName = chatsProjectName
		thread.DirectoryName = firstNonEmpty(state.DirectoryName, thread.DirectoryName)
	}
	if strings.TrimSpace(thread.ProjectName) == "" {
		project, directory := model.ProjectNameFromCWD(thread.CWD)
		thread.ProjectName = firstNonEmpty(state.ProjectName, project)
		thread.DirectoryName = firstNonEmpty(state.DirectoryName, directory)
	}
	if strings.TrimSpace(thread.DirectoryName) == "" {
		_, directory := model.ProjectNameFromCWD(thread.CWD)
		thread.DirectoryName = firstNonEmpty(state.DirectoryName, directory)
	}
	if strings.TrimSpace(thread.Title) == "" {
		thread.Title = "New thread"
	}
	if thread.UpdatedAt == 0 {
		thread.UpdatedAt = time.Now().UTC().Unix()
	}
	if len(thread.Raw) == 0 {
		thread.Raw = json.RawMessage(storage.MustJSON(payload))
	}
	return thread
}

func newThreadStateKey(chatID, topicID int64) string {
	return "chat.new_thread." + model.ChatKey(chatID, topicID)
}
