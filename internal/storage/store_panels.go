package storage

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"codex-telegram-remote-go/internal/model"
)

func (s *Store) SetGlobalObserverTarget(ctx context.Context, chatID, topicID int64, enabled bool) error {
	if err := s.SetState(ctx, "observer.global_enabled", strconv.FormatBool(enabled)); err != nil {
		return err
	}
	if enabled {
		if err := s.SetState(ctx, "observer.global_chat_id", strconv.FormatInt(chatID, 10)); err != nil {
			return err
		}
		if err := s.SetState(ctx, "observer.global_topic_id", strconv.FormatInt(topicID, 10)); err != nil {
			return err
		}
		if err := s.SetState(ctx, "observer.global_since_unix", strconv.FormatInt(time.Now().UTC().Unix(), 10)); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) GetGlobalObserverTarget(ctx context.Context) (*model.ObserverTarget, bool, error) {
	enabledRaw, err := s.GetState(ctx, "observer.global_enabled")
	if err != nil {
		return nil, false, err
	}
	if strings.TrimSpace(enabledRaw) == "" {
		return nil, false, nil
	}
	enabled := strings.EqualFold(strings.TrimSpace(enabledRaw), "true")
	if !enabled {
		return nil, true, nil
	}
	chatIDRaw, err := s.GetState(ctx, "observer.global_chat_id")
	if err != nil {
		return nil, false, err
	}
	topicIDRaw, err := s.GetState(ctx, "observer.global_topic_id")
	if err != nil {
		return nil, false, err
	}
	chatID, _ := strconv.ParseInt(strings.TrimSpace(chatIDRaw), 10, 64)
	topicID, _ := strconv.ParseInt(strings.TrimSpace(topicIDRaw), 10, 64)
	if chatID == 0 {
		return nil, true, nil
	}
	return &model.ObserverTarget{
		ChatKey:   model.ChatKey(chatID, topicID),
		ChatID:    chatID,
		TopicID:   topicID,
		Enabled:   true,
		CreatedAt: model.NowString(),
		UpdatedAt: model.NowString(),
	}, true, nil
}

func (s *Store) GetGlobalObserverSinceUnix(ctx context.Context) (int64, bool, error) {
	raw, err := s.GetState(ctx, "observer.global_since_unix")
	if err != nil {
		return 0, false, err
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, false, nil
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false, nil
	}
	return value, true, nil
}

func (s *Store) CreateThreadPanel(ctx context.Context, panel model.ThreadPanel) (*model.ThreadPanel, error) {
	now := string(model.NowString())
	if strings.TrimSpace(panel.SourceMode) == "" {
		panel.SourceMode = model.PanelSourceExplicit
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(ctx, `
	UPDATE thread_panels
	SET is_current = 0, updated_at = ?
	WHERE chat_id = ? AND topic_id = ? AND thread_id = ? AND is_current = 1`,
		now, panel.ChatID, panel.TopicID, panel.ThreadID,
	); err != nil {
		return nil, err
	}
	result, err := tx.ExecContext(ctx, `
	INSERT INTO thread_panels(
		chat_id, topic_id, project_name, thread_id, source_mode,
		summary_message_id, tool_message_id, output_message_id,
		current_turn_id, status, archive_enabled,
		last_summary_hash, last_tool_hash, last_output_hash, last_final_notice_fp,
		run_notice_message_id, last_run_notice_fp,
		user_message_id, last_user_notice_fp, plan_prompt_message_id, last_plan_prompt_fp,
		details_view_json, last_final_card_hash, is_current, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?)`,
		panel.ChatID, panel.TopicID, panel.ProjectName, panel.ThreadID, nullable(panel.SourceMode),
		panel.SummaryMessageID, panel.ToolMessageID, panel.OutputMessageID,
		nullable(panel.CurrentTurnID), nullable(panel.Status), boolToInt(panel.ArchiveEnabled),
		nullable(panel.LastSummaryHash), nullable(panel.LastToolHash), nullable(panel.LastOutputHash), nullable(panel.LastFinalNoticeFP),
		panel.RunNoticeMessageID, nullable(panel.LastRunNoticeFP),
		panel.UserMessageID, nullable(panel.LastUserNoticeFP),
		panel.PlanPromptMessageID, nullable(panel.LastPlanPromptFP),
		nullable(panel.DetailsViewJSON), nullable(panel.LastFinalCardHash),
		now, now,
	)
	if err != nil {
		return nil, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	panel.ID = id
	panel.IsCurrent = true
	panel.CreatedAt = model.TimeString(now)
	panel.UpdatedAt = model.TimeString(now)
	return &panel, nil
}

func (s *Store) GetCurrentThreadPanel(ctx context.Context, chatID, topicID int64, threadID string) (*model.ThreadPanel, error) {
	row := s.db.QueryRowContext(ctx, `
	SELECT id, chat_id, topic_id, project_name, thread_id, coalesce(source_mode,'explicit'),
		summary_message_id, tool_message_id, output_message_id,
		coalesce(current_turn_id,''), coalesce(status,''), archive_enabled,
		coalesce(last_summary_hash,''), coalesce(last_tool_hash,''), coalesce(last_output_hash,''), coalesce(last_final_notice_fp,''),
		coalesce(run_notice_message_id,0), coalesce(last_run_notice_fp,''),
		coalesce(user_message_id,0), coalesce(last_user_notice_fp,''),
		coalesce(plan_prompt_message_id,0), coalesce(last_plan_prompt_fp,''),
		coalesce(details_view_json,''), coalesce(last_final_card_hash,''),
		is_current, created_at, updated_at
	FROM thread_panels
	WHERE chat_id = ? AND topic_id = ? AND thread_id = ? AND is_current = 1
	ORDER BY id DESC
	LIMIT 1`,
		chatID, topicID, threadID,
	)
	return scanThreadPanel(row)
}

func (s *Store) ListCurrentPanelsForThread(ctx context.Context, threadID string) ([]model.ThreadPanel, error) {
	rows, err := s.db.QueryContext(ctx, `
	SELECT id, chat_id, topic_id, project_name, thread_id, coalesce(source_mode,'explicit'),
		summary_message_id, tool_message_id, output_message_id,
		coalesce(current_turn_id,''), coalesce(status,''), archive_enabled,
		coalesce(last_summary_hash,''), coalesce(last_tool_hash,''), coalesce(last_output_hash,''), coalesce(last_final_notice_fp,''),
		coalesce(run_notice_message_id,0), coalesce(last_run_notice_fp,''),
		coalesce(user_message_id,0), coalesce(last_user_notice_fp,''),
		coalesce(plan_prompt_message_id,0), coalesce(last_plan_prompt_fp,''),
		coalesce(details_view_json,''), coalesce(last_final_card_hash,''),
		is_current, created_at, updated_at
	FROM thread_panels
	WHERE thread_id = ? AND is_current = 1
	ORDER BY id ASC`,
		threadID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var panels []model.ThreadPanel
	for rows.Next() {
		panel, err := scanThreadPanel(rows)
		if err != nil {
			return nil, err
		}
		if panel != nil {
			panels = append(panels, *panel)
		}
	}
	return panels, rows.Err()
}

func (s *Store) UpdateThreadPanelMessages(ctx context.Context, panelID, summaryMessageID, toolMessageID, outputMessageID int64) error {
	_, err := s.db.ExecContext(ctx, `
	UPDATE thread_panels
	SET summary_message_id = ?, tool_message_id = ?, output_message_id = ?, updated_at = ?
	WHERE id = ?`,
		summaryMessageID, toolMessageID, outputMessageID, string(model.NowString()), panelID,
	)
	return err
}

func (s *Store) UpdateThreadPanelSourceMode(ctx context.Context, panelID int64, sourceMode string) error {
	_, err := s.db.ExecContext(ctx, `
	UPDATE thread_panels
	SET source_mode = ?, updated_at = ?
	WHERE id = ?`,
		nullable(sourceMode), string(model.NowString()), panelID,
	)
	return err
}

func (s *Store) UpdateThreadPanelState(ctx context.Context, panelID int64, currentTurnID, status, lastSummaryHash, lastToolHash, lastOutputHash, lastFinalNoticeFP string) error {
	_, err := s.db.ExecContext(ctx, `
	UPDATE thread_panels
	SET current_turn_id = ?, status = ?, last_summary_hash = ?, last_tool_hash = ?, last_output_hash = ?, last_final_notice_fp = ?, updated_at = ?
	WHERE id = ?`,
		nullable(currentTurnID), nullable(status), nullable(lastSummaryHash), nullable(lastToolHash), nullable(lastOutputHash), nullable(lastFinalNoticeFP), string(model.NowString()), panelID,
	)
	return err
}

func (s *Store) UpdateThreadPanelUserNotice(ctx context.Context, panelID, userMessageID int64, lastUserNoticeFP string) error {
	_, err := s.db.ExecContext(ctx, `
	UPDATE thread_panels
	SET user_message_id = ?, last_user_notice_fp = ?, updated_at = ?
	WHERE id = ?`,
		userMessageID, nullable(lastUserNoticeFP), string(model.NowString()), panelID,
	)
	return err
}

func (s *Store) UpdateThreadPanelRunNotice(ctx context.Context, panelID, runNoticeMessageID int64, lastRunNoticeFP string) error {
	_, err := s.db.ExecContext(ctx, `
	UPDATE thread_panels
	SET run_notice_message_id = ?, last_run_notice_fp = ?, updated_at = ?
	WHERE id = ?`,
		runNoticeMessageID, nullable(lastRunNoticeFP), string(model.NowString()), panelID,
	)
	return err
}

func (s *Store) UpdateThreadPanelPlanPrompt(ctx context.Context, panelID, planPromptMessageID int64, lastPlanPromptFP string) error {
	_, err := s.db.ExecContext(ctx, `
	UPDATE thread_panels
	SET plan_prompt_message_id = ?, last_plan_prompt_fp = ?, updated_at = ?
	WHERE id = ?`,
		planPromptMessageID, nullable(lastPlanPromptFP), string(model.NowString()), panelID,
	)
	return err
}

func (s *Store) UpdateThreadPanelDetails(ctx context.Context, panelID int64, detailsViewJSON, lastFinalCardHash string) error {
	_, err := s.db.ExecContext(ctx, `
	UPDATE thread_panels
	SET details_view_json = ?, last_final_card_hash = ?, updated_at = ?
	WHERE id = ?`,
		nullable(detailsViewJSON), nullable(lastFinalCardHash), string(model.NowString()), panelID,
	)
	return err
}

func (s *Store) GetThreadPanelByID(ctx context.Context, panelID int64) (*model.ThreadPanel, error) {
	row := s.db.QueryRowContext(ctx, `
	SELECT id, chat_id, topic_id, project_name, thread_id, coalesce(source_mode,'explicit'),
		summary_message_id, tool_message_id, output_message_id,
		coalesce(current_turn_id,''), coalesce(status,''), archive_enabled,
		coalesce(last_summary_hash,''), coalesce(last_tool_hash,''), coalesce(last_output_hash,''), coalesce(last_final_notice_fp,''),
		coalesce(run_notice_message_id,0), coalesce(last_run_notice_fp,''),
		coalesce(user_message_id,0), coalesce(last_user_notice_fp,''),
		coalesce(plan_prompt_message_id,0), coalesce(last_plan_prompt_fp,''),
		coalesce(details_view_json,''), coalesce(last_final_card_hash,''),
		is_current, created_at, updated_at
	FROM thread_panels
	WHERE id = ?`,
		panelID,
	)
	return scanThreadPanel(row)
}

func (s *Store) ArmSteerState(ctx context.Context, state model.SteerState) error {
	now := string(model.NowString())
	if state.CreatedAt == "" {
		state.CreatedAt = model.TimeString(now)
	}
	if state.UpdatedAt == "" {
		state.UpdatedAt = model.TimeString(now)
	}
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO chat_steer_state(chat_key, chat_id, topic_id, thread_id, turn_id, panel_id, expires_at, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(chat_key) DO UPDATE SET
		thread_id = excluded.thread_id,
		turn_id = excluded.turn_id,
		panel_id = excluded.panel_id,
		expires_at = excluded.expires_at,
		updated_at = excluded.updated_at`,
		state.ChatKey, state.ChatID, state.TopicID, state.ThreadID, nullable(state.TurnID), state.PanelID, state.ExpiresAt, state.CreatedAt, state.UpdatedAt,
	)
	return err
}

func (s *Store) GetSteerState(ctx context.Context, chatID, topicID int64) (*model.SteerState, error) {
	row := s.db.QueryRowContext(ctx, `
	SELECT chat_key, chat_id, topic_id, thread_id, coalesce(turn_id,''), panel_id, expires_at, created_at, updated_at
	FROM chat_steer_state
	WHERE chat_key = ?`,
		model.ChatKey(chatID, topicID),
	)
	var state model.SteerState
	err := row.Scan(&state.ChatKey, &state.ChatID, &state.TopicID, &state.ThreadID, &state.TurnID, &state.PanelID, &state.ExpiresAt, &state.CreatedAt, &state.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &state, nil
}

func (s *Store) ClearSteerState(ctx context.Context, chatID, topicID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM chat_steer_state WHERE chat_key = ?`, model.ChatKey(chatID, topicID))
	return err
}

func (s *Store) GetLatestPendingApprovalForThread(ctx context.Context, threadID string) (*model.PendingApproval, error) {
	row := s.db.QueryRowContext(ctx, `
	SELECT request_id, thread_id, coalesce(turn_id,''), coalesce(item_id,''), prompt_kind, coalesce(question,''), status, coalesce(telegram_message_id,0), payload_json, updated_at
	FROM pending_approvals
	WHERE thread_id = ? AND status = 'pending'
	ORDER BY updated_at DESC
	LIMIT 1`,
		threadID,
	)
	var approval model.PendingApproval
	err := row.Scan(&approval.RequestID, &approval.ThreadID, &approval.TurnID, &approval.ItemID, &approval.PromptKind, &approval.Question, &approval.Status, &approval.TelegramMessageID, &approval.PayloadJSON, &approval.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &approval, nil
}

func scanThreadPanel(scanner interface{ Scan(...any) error }) (*model.ThreadPanel, error) {
	var panel model.ThreadPanel
	var currentTurnID, status, lastSummaryHash, lastToolHash, lastOutputHash, lastFinalNoticeFP, lastRunNoticeFP, lastUserNoticeFP, lastPlanPromptFP, detailsViewJSON, lastFinalCardHash sql.NullString
	var archiveEnabled, isCurrent int
	if err := scanner.Scan(
		&panel.ID,
		&panel.ChatID,
		&panel.TopicID,
		&panel.ProjectName,
		&panel.ThreadID,
		&panel.SourceMode,
		&panel.SummaryMessageID,
		&panel.ToolMessageID,
		&panel.OutputMessageID,
		&currentTurnID,
		&status,
		&archiveEnabled,
		&lastSummaryHash,
		&lastToolHash,
		&lastOutputHash,
		&lastFinalNoticeFP,
		&panel.RunNoticeMessageID,
		&lastRunNoticeFP,
		&panel.UserMessageID,
		&lastUserNoticeFP,
		&panel.PlanPromptMessageID,
		&lastPlanPromptFP,
		&detailsViewJSON,
		&lastFinalCardHash,
		&isCurrent,
		&panel.CreatedAt,
		&panel.UpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	panel.CurrentTurnID = currentTurnID.String
	panel.Status = status.String
	if strings.TrimSpace(panel.SourceMode) == "" {
		panel.SourceMode = model.PanelSourceExplicit
	}
	panel.ArchiveEnabled = archiveEnabled == 1
	panel.LastSummaryHash = lastSummaryHash.String
	panel.LastToolHash = lastToolHash.String
	panel.LastOutputHash = lastOutputHash.String
	panel.LastFinalNoticeFP = lastFinalNoticeFP.String
	panel.LastRunNoticeFP = lastRunNoticeFP.String
	panel.LastUserNoticeFP = lastUserNoticeFP.String
	panel.LastPlanPromptFP = lastPlanPromptFP.String
	panel.DetailsViewJSON = detailsViewJSON.String
	panel.LastFinalCardHash = lastFinalCardHash.String
	panel.IsCurrent = isCurrent == 1
	return &panel, nil
}
