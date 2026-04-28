package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mideco-tech/codex-tg/internal/model"

	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &Store{db: db}
	if err := store.initialize(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) initialize(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `PRAGMA journal_mode=WAL`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA foreign_keys=ON`); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `PRAGMA busy_timeout=5000`); err != nil {
		return err
	}
	schema := `
	CREATE TABLE IF NOT EXISTS threads (
		thread_id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		cwd TEXT,
		project_name TEXT NOT NULL,
		directory_name TEXT,
		updated_at INTEGER NOT NULL,
		status TEXT,
		last_preview TEXT,
		active_turn_id TEXT,
		preferred_model TEXT,
		permissions_mode TEXT,
		archived INTEGER NOT NULL DEFAULT 0,
		raw_json TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS thread_snapshots (
		thread_id TEXT PRIMARY KEY,
		last_live_event_at TEXT,
		last_poll_at TEXT,
		next_poll_after TEXT,
		last_seen_thread_status TEXT,
		last_seen_turn_id TEXT,
		last_seen_turn_status TEXT,
		last_progress_fp TEXT,
		last_progress_sent_at TEXT,
		last_final_fp TEXT,
		last_completion_fp TEXT,
		last_approval_fp TEXT,
		snapshot_json TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS thread_bindings (
		chat_key TEXT PRIMARY KEY,
		chat_id INTEGER NOT NULL,
		topic_id INTEGER NOT NULL,
		thread_id TEXT NOT NULL,
		mode TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS observer_targets (
		chat_key TEXT PRIMARY KEY,
		chat_id INTEGER NOT NULL,
		topic_id INTEGER NOT NULL,
		enabled INTEGER NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS telegram_message_routes (
		chat_id INTEGER NOT NULL,
		topic_id INTEGER NOT NULL,
		message_id INTEGER NOT NULL,
		thread_id TEXT NOT NULL,
		turn_id TEXT,
		item_id TEXT,
		event_id TEXT,
		created_at TEXT NOT NULL,
		PRIMARY KEY(chat_id, topic_id, message_id)
	);

	CREATE TABLE IF NOT EXISTS callback_routes (
		route_token TEXT PRIMARY KEY,
		action TEXT NOT NULL,
		thread_id TEXT NOT NULL,
		turn_id TEXT,
		request_id TEXT,
		telegram_message_id INTEGER,
		status TEXT NOT NULL,
		expires_at TEXT,
		payload_json TEXT NOT NULL,
		created_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS pending_approvals (
		request_id TEXT PRIMARY KEY,
		thread_id TEXT NOT NULL,
		turn_id TEXT,
		item_id TEXT,
		prompt_kind TEXT NOT NULL,
		question TEXT,
		status TEXT NOT NULL,
		telegram_message_id INTEGER,
		payload_json TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS delivery_queue (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_id TEXT NOT NULL,
		chat_key TEXT NOT NULL,
		chat_id INTEGER NOT NULL,
		topic_id INTEGER NOT NULL,
		thread_id TEXT NOT NULL,
		kind TEXT NOT NULL,
		status TEXT NOT NULL,
		retry_count INTEGER NOT NULL DEFAULT 0,
		available_at TEXT NOT NULL,
		last_error TEXT,
		payload_json TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);

	CREATE UNIQUE INDEX IF NOT EXISTS idx_delivery_queue_event_target
		ON delivery_queue(event_id, chat_key);

	CREATE TABLE IF NOT EXISTS delivery_attempts (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		queue_id INTEGER NOT NULL,
		attempt_no INTEGER NOT NULL,
		status TEXT NOT NULL,
		error_text TEXT,
		created_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS daemon_state (
		key TEXT PRIMARY KEY,
		value TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS thread_panels (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		chat_id INTEGER NOT NULL,
		topic_id INTEGER NOT NULL,
		project_name TEXT NOT NULL,
		thread_id TEXT NOT NULL,
		source_mode TEXT NOT NULL DEFAULT 'explicit',
		summary_message_id INTEGER NOT NULL DEFAULT 0,
		tool_message_id INTEGER NOT NULL DEFAULT 0,
		output_message_id INTEGER NOT NULL DEFAULT 0,
		current_turn_id TEXT,
		status TEXT,
		archive_enabled INTEGER NOT NULL DEFAULT 1,
		last_summary_hash TEXT,
		last_tool_hash TEXT,
		last_output_hash TEXT,
		last_final_notice_fp TEXT,
		run_notice_message_id INTEGER NOT NULL DEFAULT 0,
		last_run_notice_fp TEXT,
		user_message_id INTEGER NOT NULL DEFAULT 0,
		last_user_notice_fp TEXT,
		plan_prompt_message_id INTEGER NOT NULL DEFAULT 0,
		last_plan_prompt_fp TEXT,
		details_view_json TEXT,
		last_final_card_hash TEXT,
		is_current INTEGER NOT NULL DEFAULT 1,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS chat_steer_state (
		chat_key TEXT PRIMARY KEY,
		chat_id INTEGER NOT NULL,
		topic_id INTEGER NOT NULL,
		thread_id TEXT NOT NULL,
		turn_id TEXT,
		panel_id INTEGER NOT NULL DEFAULT 0,
		expires_at TEXT NOT NULL,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_threads_updated_at ON threads(updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_threads_project_updated_at ON threads(project_name, updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_bindings_thread_id ON thread_bindings(thread_id);
	CREATE INDEX IF NOT EXISTS idx_observer_targets_enabled_updated_at ON observer_targets(enabled, updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_delivery_queue_status_available_at ON delivery_queue(status, available_at);
	CREATE INDEX IF NOT EXISTS idx_pending_approvals_status_updated_at ON pending_approvals(status, updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_thread_panels_thread_current ON thread_panels(chat_id, topic_id, thread_id, is_current, updated_at DESC);
	CREATE INDEX IF NOT EXISTS idx_chat_steer_expires_at ON chat_steer_state(expires_at);
	`
	if _, err := s.db.ExecContext(ctx, schema); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "thread_panels", "source_mode", `ALTER TABLE thread_panels ADD COLUMN source_mode TEXT NOT NULL DEFAULT 'explicit'`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "thread_panels", "last_final_notice_fp", `ALTER TABLE thread_panels ADD COLUMN last_final_notice_fp TEXT`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "thread_panels", "run_notice_message_id", `ALTER TABLE thread_panels ADD COLUMN run_notice_message_id INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "thread_panels", "last_run_notice_fp", `ALTER TABLE thread_panels ADD COLUMN last_run_notice_fp TEXT`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "thread_panels", "user_message_id", `ALTER TABLE thread_panels ADD COLUMN user_message_id INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "thread_panels", "last_user_notice_fp", `ALTER TABLE thread_panels ADD COLUMN last_user_notice_fp TEXT`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "thread_panels", "plan_prompt_message_id", `ALTER TABLE thread_panels ADD COLUMN plan_prompt_message_id INTEGER NOT NULL DEFAULT 0`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "thread_panels", "last_plan_prompt_fp", `ALTER TABLE thread_panels ADD COLUMN last_plan_prompt_fp TEXT`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "thread_panels", "details_view_json", `ALTER TABLE thread_panels ADD COLUMN details_view_json TEXT`); err != nil {
		return err
	}
	if err := s.ensureColumn(ctx, "thread_panels", "last_final_card_hash", `ALTER TABLE thread_panels ADD COLUMN last_final_card_hash TEXT`); err != nil {
		return err
	}
	return nil
}

func (s *Store) ensureColumn(ctx context.Context, tableName, columnName, alterSQL string) error {
	exists, err := s.hasColumn(ctx, tableName, columnName)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	if _, err := s.db.ExecContext(ctx, alterSQL); err != nil {
		return err
	}
	exists, err = s.hasColumn(ctx, tableName, columnName)
	if err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("column %s.%s was not created", tableName, columnName)
	}
	return nil
}

func (s *Store) hasColumn(ctx context.Context, tableName, columnName string) (bool, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf(`PRAGMA table_info(%s)`, tableName))
	if err != nil {
		return false, err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, columnType string
		var notNull, pk int
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &pk); err != nil {
			return false, err
		}
		if strings.EqualFold(name, columnName) {
			return true, nil
		}
	}
	return false, rows.Err()
}

func (s *Store) UpsertThread(ctx context.Context, thread model.Thread) error {
	raw := thread.Raw
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO threads(thread_id, title, cwd, project_name, directory_name, updated_at, status, last_preview, active_turn_id, preferred_model, permissions_mode, archived, raw_json)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(thread_id) DO UPDATE SET
		title = excluded.title,
		cwd = excluded.cwd,
		project_name = excluded.project_name,
		directory_name = excluded.directory_name,
		updated_at = excluded.updated_at,
		status = excluded.status,
		last_preview = excluded.last_preview,
		active_turn_id = excluded.active_turn_id,
		preferred_model = excluded.preferred_model,
		permissions_mode = excluded.permissions_mode,
		archived = excluded.archived,
		raw_json = excluded.raw_json`,
		thread.ID, thread.Title, nullable(thread.CWD), thread.ProjectName, nullable(thread.DirectoryName), thread.UpdatedAt,
		nullable(thread.Status), nullable(thread.LastPreview), nullable(thread.ActiveTurnID), nullable(thread.PreferredModel),
		nullable(thread.PermissionsMode), boolToInt(thread.Archived), string(raw),
	)
	return err
}

func (s *Store) GetThread(ctx context.Context, threadID string) (*model.Thread, error) {
	row := s.db.QueryRowContext(ctx, `
	SELECT thread_id, title, cwd, project_name, directory_name, updated_at, status, last_preview, active_turn_id, preferred_model, permissions_mode, archived, raw_json
	FROM threads WHERE thread_id = ?`, threadID)
	return scanThread(row)
}

func (s *Store) ListThreads(ctx context.Context, limit int, search string) ([]model.Thread, error) {
	if limit <= 0 {
		limit = 10
	}
	query := `
	SELECT thread_id, title, cwd, project_name, directory_name, updated_at, status, last_preview, active_turn_id, preferred_model, permissions_mode, archived, raw_json
	FROM threads`
	args := make([]any, 0, 2)
	if trimmed := strings.TrimSpace(search); trimmed != "" {
		query += ` WHERE lower(title) LIKE ? OR lower(project_name) LIKE ? OR lower(last_preview) LIKE ? OR lower(thread_id) LIKE ?`
		pattern := "%" + strings.ToLower(trimmed) + "%"
		args = append(args, pattern, pattern, pattern, pattern)
	}
	query += ` ORDER BY updated_at DESC LIMIT ?`
	args = append(args, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.Thread{}
	for rows.Next() {
		thread, err := scanThread(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *thread)
	}
	return out, rows.Err()
}

func (s *Store) CountThreads(ctx context.Context) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT count(*) FROM threads`)
	var count int
	return count, row.Scan(&count)
}

func (s *Store) ListProjectGroups(ctx context.Context) (map[string][]model.Thread, error) {
	rows, err := s.ListThreads(ctx, 500, "")
	if err != nil {
		return nil, err
	}
	grouped := map[string][]model.Thread{}
	for _, thread := range rows {
		grouped[thread.ProjectName] = append(grouped[thread.ProjectName], thread)
	}
	return grouped, nil
}

func (s *Store) UpsertSnapshot(ctx context.Context, threadID string, snapshot model.ThreadSnapshotState) error {
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return err
	}
	updatedAt := string(model.NowString())
	_, err = s.db.ExecContext(ctx, `
	INSERT INTO thread_snapshots(
		thread_id, last_live_event_at, last_poll_at, next_poll_after, last_seen_thread_status, last_seen_turn_id, last_seen_turn_status,
		last_progress_fp, last_progress_sent_at, last_final_fp, last_completion_fp, last_approval_fp, snapshot_json, updated_at
	)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(thread_id) DO UPDATE SET
		last_live_event_at = excluded.last_live_event_at,
		last_poll_at = excluded.last_poll_at,
		next_poll_after = excluded.next_poll_after,
		last_seen_thread_status = excluded.last_seen_thread_status,
		last_seen_turn_id = excluded.last_seen_turn_id,
		last_seen_turn_status = excluded.last_seen_turn_status,
		last_progress_fp = excluded.last_progress_fp,
		last_progress_sent_at = excluded.last_progress_sent_at,
		last_final_fp = excluded.last_final_fp,
		last_completion_fp = excluded.last_completion_fp,
		last_approval_fp = excluded.last_approval_fp,
		snapshot_json = excluded.snapshot_json,
		updated_at = excluded.updated_at`,
		threadID,
		nullable(string(snapshot.LastRichLiveEventAt)),
		nullable(string(snapshot.LastPollAt)),
		nullable(string(snapshot.NextPollAfter)),
		nullable(snapshot.LastSeenThreadStatus),
		nullable(snapshot.LastSeenTurnID),
		nullable(snapshot.LastSeenTurnStatus),
		nullable(snapshot.LastProgressFP),
		nullable(string(snapshot.LastProgressSentAt)),
		nullable(snapshot.LastFinalFP),
		nullable(snapshot.LastCompletionFP),
		nullable(snapshot.LastApprovalFP),
		string(payload),
		updatedAt,
	)
	return err
}

func (s *Store) GetSnapshot(ctx context.Context, threadID string) (*model.ThreadSnapshotState, error) {
	row := s.db.QueryRowContext(ctx, `SELECT snapshot_json FROM thread_snapshots WHERE thread_id = ?`, threadID)
	var payload string
	if err := row.Scan(&payload); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	var snapshot model.ThreadSnapshotState
	if err := json.Unmarshal([]byte(payload), &snapshot); err != nil {
		return nil, err
	}
	return &snapshot, nil
}

func (s *Store) MarkLiveEvent(ctx context.Context, threadID string, when model.TimeString) error {
	snapshot, err := s.GetSnapshot(ctx, threadID)
	if err != nil {
		return err
	}
	if snapshot == nil {
		snapshot = &model.ThreadSnapshotState{}
	}
	snapshot.LastRichLiveEventAt = when
	return s.UpsertSnapshot(ctx, threadID, *snapshot)
}

func (s *Store) SetBinding(ctx context.Context, chatID, topicID int64, threadID, mode string) error {
	now := string(model.NowString())
	chatKey := model.ChatKey(chatID, topicID)
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO thread_bindings(chat_key, chat_id, topic_id, thread_id, mode, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(chat_key) DO UPDATE SET thread_id = excluded.thread_id, mode = excluded.mode, updated_at = excluded.updated_at`,
		chatKey, chatID, topicID, threadID, mode, now, now,
	)
	return err
}

func (s *Store) ClearBinding(ctx context.Context, chatID, topicID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM thread_bindings WHERE chat_key = ?`, model.ChatKey(chatID, topicID))
	return err
}

func (s *Store) GetBinding(ctx context.Context, chatID, topicID int64) (*model.ThreadBinding, error) {
	row := s.db.QueryRowContext(ctx, `
	SELECT chat_key, chat_id, topic_id, thread_id, mode, created_at, updated_at
	FROM thread_bindings WHERE chat_key = ?`, model.ChatKey(chatID, topicID))
	var binding model.ThreadBinding
	err := row.Scan(&binding.ChatKey, &binding.ChatID, &binding.TopicID, &binding.ThreadID, &binding.Mode, &binding.CreatedAt, &binding.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &binding, nil
}

func (s *Store) ListBoundThreadIDs(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT thread_id FROM thread_bindings ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var threadID string
		if err := rows.Scan(&threadID); err != nil {
			return nil, err
		}
		out = append(out, threadID)
	}
	return out, rows.Err()
}

func (s *Store) SetObserverTarget(ctx context.Context, chatID, topicID int64, enabled bool) error {
	now := string(model.NowString())
	chatKey := model.ChatKey(chatID, topicID)
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO observer_targets(chat_key, chat_id, topic_id, enabled, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?)
	ON CONFLICT(chat_key) DO UPDATE SET enabled = excluded.enabled, updated_at = excluded.updated_at`,
		chatKey, chatID, topicID, boolToInt(enabled), now, now,
	)
	return err
}

func (s *Store) IsObserverTarget(ctx context.Context, chatID, topicID int64) (bool, error) {
	row := s.db.QueryRowContext(ctx, `SELECT enabled FROM observer_targets WHERE chat_key = ?`, model.ChatKey(chatID, topicID))
	var enabled int
	err := row.Scan(&enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return enabled == 1, err
}

func (s *Store) ListObserverTargets(ctx context.Context) ([]model.ObserverTarget, error) {
	rows, err := s.db.QueryContext(ctx, `
	SELECT chat_key, chat_id, topic_id, enabled, created_at, updated_at
	FROM observer_targets WHERE enabled = 1 ORDER BY updated_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.ObserverTarget{}
	for rows.Next() {
		var target model.ObserverTarget
		var enabled int
		if err := rows.Scan(&target.ChatKey, &target.ChatID, &target.TopicID, &enabled, &target.CreatedAt, &target.UpdatedAt); err != nil {
			return nil, err
		}
		target.Enabled = enabled == 1
		out = append(out, target)
	}
	return out, rows.Err()
}

func (s *Store) PutMessageRoute(ctx context.Context, route model.MessageRoute) error {
	_, err := s.db.ExecContext(ctx, `
	INSERT OR REPLACE INTO telegram_message_routes(chat_id, topic_id, message_id, thread_id, turn_id, item_id, event_id, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		route.ChatID, route.TopicID, route.MessageID, route.ThreadID, nullable(route.TurnID), nullable(route.ItemID), nullable(route.EventID), route.CreatedAt,
	)
	return err
}

func (s *Store) ResolveMessageRoute(ctx context.Context, chatID, topicID, messageID int64) (*model.MessageRoute, error) {
	row := s.db.QueryRowContext(ctx, `
	SELECT chat_id, topic_id, message_id, thread_id, coalesce(turn_id, ''), coalesce(item_id, ''), coalesce(event_id, ''), created_at
	FROM telegram_message_routes WHERE chat_id = ? AND topic_id = ? AND message_id = ?`,
		chatID, topicID, messageID,
	)
	var route model.MessageRoute
	err := row.Scan(&route.ChatID, &route.TopicID, &route.MessageID, &route.ThreadID, &route.TurnID, &route.ItemID, &route.EventID, &route.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &route, nil
}

func (s *Store) PutCallbackRoute(ctx context.Context, route model.CallbackRoute) error {
	_, err := s.db.ExecContext(ctx, `
	INSERT OR REPLACE INTO callback_routes(route_token, action, thread_id, turn_id, request_id, telegram_message_id, status, expires_at, payload_json, created_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		route.Token, route.Action, route.ThreadID, nullable(route.TurnID), nullable(route.RequestID), route.TelegramMessageID, route.Status, nullable(route.ExpiresAt), route.PayloadJSON, route.CreatedAt,
	)
	return err
}

func (s *Store) GetCallbackRoute(ctx context.Context, token string) (*model.CallbackRoute, error) {
	row := s.db.QueryRowContext(ctx, `
	SELECT route_token, action, thread_id, coalesce(turn_id,''), coalesce(request_id,''), coalesce(telegram_message_id,0), status, coalesce(expires_at,''), payload_json, created_at
	FROM callback_routes WHERE route_token = ?`, token)
	var route model.CallbackRoute
	err := row.Scan(&route.Token, &route.Action, &route.ThreadID, &route.TurnID, &route.RequestID, &route.TelegramMessageID, &route.Status, &route.ExpiresAt, &route.PayloadJSON, &route.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &route, nil
}

func (s *Store) ExpireCallbackRoute(ctx context.Context, token string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE callback_routes SET status = ? WHERE route_token = ?`, model.CallbackStatusExpired, token)
	return err
}

func (s *Store) SavePendingApproval(ctx context.Context, approval model.PendingApproval) error {
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO pending_approvals(request_id, thread_id, turn_id, item_id, prompt_kind, question, status, telegram_message_id, payload_json, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(request_id) DO UPDATE SET
		status = excluded.status,
		telegram_message_id = excluded.telegram_message_id,
		payload_json = excluded.payload_json,
		updated_at = excluded.updated_at`,
		approval.RequestID, approval.ThreadID, nullable(approval.TurnID), nullable(approval.ItemID), approval.PromptKind, nullable(approval.Question), approval.Status,
		approval.TelegramMessageID, approval.PayloadJSON, approval.UpdatedAt,
	)
	return err
}

func (s *Store) GetPendingApproval(ctx context.Context, requestID string) (*model.PendingApproval, error) {
	row := s.db.QueryRowContext(ctx, `
	SELECT request_id, thread_id, coalesce(turn_id,''), coalesce(item_id,''), prompt_kind, coalesce(question,''), status, coalesce(telegram_message_id,0), payload_json, updated_at
	FROM pending_approvals WHERE request_id = ?`, requestID)
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

func (s *Store) UpdatePendingApprovalStatus(ctx context.Context, requestID, status string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE pending_approvals SET status = ?, updated_at = ? WHERE request_id = ?`, status, string(model.NowString()), requestID)
	return err
}

func (s *Store) MarkAllPendingApprovals(ctx context.Context, status string) (int64, error) {
	result, err := s.db.ExecContext(ctx, `UPDATE pending_approvals SET status = ?, updated_at = ? WHERE status = 'pending'`, status, string(model.NowString()))
	if err != nil {
		return 0, err
	}
	return result.RowsAffected()
}

func (s *Store) EnqueueDelivery(ctx context.Context, item model.DeliveryQueueItem) error {
	now := string(model.NowString())
	if item.AvailableAt == "" {
		item.AvailableAt = model.TimeString(now)
	}
	if item.CreatedAt == "" {
		item.CreatedAt = model.TimeString(now)
	}
	if item.UpdatedAt == "" {
		item.UpdatedAt = model.TimeString(now)
	}
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO delivery_queue(event_id, chat_key, chat_id, topic_id, thread_id, kind, status, retry_count, available_at, last_error, payload_json, created_at, updated_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(event_id, chat_key) DO NOTHING`,
		item.EventID, item.ChatKey, item.ChatID, item.TopicID, item.ThreadID, item.Kind, item.Status, item.RetryCount, item.AvailableAt, nullable(item.LastError), item.PayloadJSON, item.CreatedAt, item.UpdatedAt,
	)
	return err
}

func (s *Store) ClaimDeliveryBatch(ctx context.Context, limit int) ([]model.DeliveryQueueItem, error) {
	if limit <= 0 {
		limit = 10
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer rollback(tx)
	rows, err := tx.QueryContext(ctx, `
	SELECT id, event_id, chat_key, chat_id, topic_id, thread_id, kind, status, retry_count, available_at, coalesce(last_error,''), payload_json, created_at, updated_at
	FROM delivery_queue
	WHERE status IN ('pending', 'retry') AND available_at <= ?
	ORDER BY id
	LIMIT ?`, string(model.NowString()), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.DeliveryQueueItem{}
	ids := []int64{}
	for rows.Next() {
		var item model.DeliveryQueueItem
		if err := rows.Scan(&item.ID, &item.EventID, &item.ChatKey, &item.ChatID, &item.TopicID, &item.ThreadID, &item.Kind, &item.Status, &item.RetryCount, &item.AvailableAt, &item.LastError, &item.PayloadJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, item)
		ids = append(ids, item.ID)
	}
	for _, id := range ids {
		if _, err := tx.ExecContext(ctx, `UPDATE delivery_queue SET status = ?, updated_at = ? WHERE id = ?`, model.DeliveryStatusProcessing, string(model.NowString()), id); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *Store) CompleteDelivery(ctx context.Context, queueID int64) error {
	_, err := s.db.ExecContext(ctx, `UPDATE delivery_queue SET status = ?, updated_at = ? WHERE id = ?`, model.DeliveryStatusDelivered, string(model.NowString()), queueID)
	return err
}

func (s *Store) FailDelivery(ctx context.Context, queueID int64, retryCount int, availableAt time.Time, errText string, dead bool) error {
	status := model.DeliveryStatusRetry
	if dead {
		status = model.DeliveryStatusDead
	}
	_, err := s.db.ExecContext(ctx, `
	UPDATE delivery_queue SET status = ?, retry_count = ?, available_at = ?, last_error = ?, updated_at = ? WHERE id = ?`,
		status, retryCount, availableAt.UTC().Format(time.RFC3339Nano), nullable(errText), string(model.NowString()), queueID,
	)
	return err
}

func (s *Store) RecordDeliveryAttempt(ctx context.Context, queueID int64, attemptNo int, status, errText string) error {
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO delivery_attempts(queue_id, attempt_no, status, error_text, created_at)
	VALUES (?, ?, ?, ?, ?)`,
		queueID, attemptNo, status, nullable(errText), string(model.NowString()),
	)
	return err
}

func (s *Store) DeliveryQueueBacklog(ctx context.Context) (int, error) {
	row := s.db.QueryRowContext(ctx, `SELECT count(*) FROM delivery_queue WHERE status IN ('pending', 'retry', 'processing')`)
	var count int
	return count, row.Scan(&count)
}

func (s *Store) SetState(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
	INSERT INTO daemon_state(key, value, updated_at) VALUES (?, ?, ?)
	ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, string(model.NowString()),
	)
	return err
}

func (s *Store) GetState(ctx context.Context, key string) (string, error) {
	row := s.db.QueryRowContext(ctx, `SELECT value FROM daemon_state WHERE key = ?`, key)
	var value string
	err := row.Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return value, err
}

func (s *Store) ListState(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM daemon_state ORDER BY key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, err
		}
		out[key] = value
	}
	return out, rows.Err()
}

func (s *Store) GetChatContext(ctx context.Context, chatID, topicID int64) (*model.ChatContext, error) {
	binding, err := s.GetBinding(ctx, chatID, topicID)
	if err != nil {
		return nil, err
	}
	globalTarget, _, err := s.GetGlobalObserverTarget(ctx)
	if err != nil {
		return nil, err
	}
	observerEnabled := globalTarget != nil && globalTarget.ChatID == chatID && globalTarget.TopicID == topicID
	contextState := &model.ChatContext{Mode: "unbound", Binding: binding, ObserverEnabled: observerEnabled, ObserverTarget: globalTarget}
	if observerEnabled {
		contextState.Mode = model.BindingModeObserver
	}
	if binding != nil {
		contextState.Mode = binding.Mode
		thread, err := s.GetThread(ctx, binding.ThreadID)
		if err != nil {
			return nil, err
		}
		contextState.Thread = thread
	}
	return contextState, nil
}

func scanThread(scanner interface{ Scan(...any) error }) (*model.Thread, error) {
	var thread model.Thread
	var cwd, directoryName, status, lastPreview, activeTurnID, preferredModel, permissionsMode sql.NullString
	var raw string
	var archived int
	if err := scanner.Scan(&thread.ID, &thread.Title, &cwd, &thread.ProjectName, &directoryName, &thread.UpdatedAt, &status, &lastPreview, &activeTurnID, &preferredModel, &permissionsMode, &archived, &raw); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	thread.CWD = cwd.String
	thread.DirectoryName = directoryName.String
	thread.Status = status.String
	thread.LastPreview = lastPreview.String
	thread.ActiveTurnID = activeTurnID.String
	thread.PreferredModel = preferredModel.String
	thread.PermissionsMode = permissionsMode.String
	thread.Archived = archived == 1
	thread.Raw = json.RawMessage(raw)
	return &thread, nil
}

func nullable(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

func MustJSON(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Errorf("marshal payload: %w", err))
	}
	return string(payload)
}
