# Contract Matrix

Python oracle: `..\codex-telegram-remote`

This file now serves two purposes:

- baseline behavior imported from the Python oracle
- target Telegram observer/UI v2 deltas that the Go runtime is expected to adopt

## Commands

- `/start`
- `/help`
- `/threads`
- `/projects`
- `/show <thread>`
- `/bind <thread>`
- `/reply [--plan] <thread> <text>`
- `/plan <thread> <text>`
- `/plan_mode <thread> <text>`
- `/settings`
- `/model`
- `/effort`
- `/codex_status`
- `/appserver_transport`
- `/new [project] <prompt>`
- `/context`
- `/whereami`
- `/observe all|off`
- `/status`
- `/repair`
- `/stop [thread]`
- `/approve <request_id>`
- `/deny <request_id>`

## Aliases and adjacent commands

- `/whereami` is an alias of `/context`
- `/models` is an alias of `/model`
- `/reasoning` and `/reasoning_effort` are aliases of `/effort`
- `/codex_settings` is an alias of `/settings`
- `/away` and `/back` exist in the Python product surface but are not part of the minimal Go cutover slice yet

## Target Telegram observer/UI v2 deltas

- Global observer monitoring is default-on when an operator target can be resolved automatically.
- `/observe all` moves the single global observer target to the current chat/topic.
- `/observe off` disables global background monitoring.
- The observer target model is no longer additive `main DM + extra feeds`.
- The observer surface is centered around a summary panel keyed by `(chat, project, thread)`.
- The summary panel owns `Stop` and `Steer`.
- Tool/output stream messages do not carry buttons.
- Final answers are delivered separately and expose `Получить полный лог`.
- Plan Mode / waiting-input states create a separate routeable `[Plan]` prompt-card.
- `[Plan]` buttons are structured-only: they come from Codex `choices/options/suggestions/responses`, never from bridge heuristics.
- Telegram-originated Plan Mode starts use App Server `turn/start` with `collaborationMode.mode = plan`; prompt wording alone is not Plan Mode.
- `/model` and `/effort` are button menus backed by SQLite daemon state for Telegram-started collaboration-mode model settings.
- `/settings` includes an App Server transport menu backed by SQLite daemon state.
- After a model, reasoning-effort, or transport selection, the edited settings message removes inline choice buttons.
- Synthetic polling prompts without `request_id` are answered with `turn/steer`, then `turn/start` if the turn is already unavailable.
- Replies to active turns steer the active turn. If steering is rejected while the thread still looks active, the bridge must not create a parallel `turn/start`.
- All observer/card messages carry a visual identity header: `emoji [Project] [Thread] [T:thread] [R:run] [Kind]`.
- Emoji markers are stable visual hints; route correctness remains based on DB message routes and callback tokens.
- Foreign GUI/CLI runs create separate `New run` and `[User]` cards before the live trio.
- If the prompt is not available when the run is discovered, `[User]` starts as a placeholder and is edited into the real prompt later.
- Telegram-originated runs create `New run` and the live trio, but do not duplicate the user request as `[User]`.

## Callback / button surface from the oracle

Navigation/edit-in-place callbacks:

- `nav_projects`
- `nav_all_chats`
- `nav_active`
- `nav_threads_page`
- `nav_projects_page`
- `nav_project_threads_page`
- `pick_project`
- `show_thread`
- `show_context`

State-changing callbacks:

- `bind_here`
- `follow_here`
- `observe_all`
- `reply_hint`
- `stop_turn`
- `approve`
- `approve_session`
- `deny`
- `cancel`

Target v2 callback surface:

- `show_thread`
- `bind_here`
- `stop_turn`
- `steer_turn`
- `get_full_log`
- `answer_choice`
- `observe_all`
- `observe_off`
- `settings_overview`
- `settings_model_menu`
- `settings_reasoning_menu`
- `settings_model_set`
- `settings_reasoning_set`

## Routing precedence

From Python tests and router behavior:

1. explicit thread id from command
2. reply-to Telegram message route
3. current thread binding

Additional route rules:

- `/show` and `/bind` without an explicit thread id must resolve reply-route first
- route precedence stays unchanged even after the observer/UI v2 changes
- target v2 no longer assumes a dedicated read-only observer-only chat
- free-text routing still needs an unambiguous target even if the current chat also receives global observer panels
- reply-to `[Plan]` routes before binding and carries `thread_id`, `turn_id`, and `request_id` when available
- real `request_id` Plan answers use App Server server-request response; synthetic Plan answers use `turn/steer`
- `/reply --plan`, `/plan`, and `/plan_mode` carry an explicit Plan Mode start intent when they create a new turn

## Observer targets

- Oracle baseline:
  - implicit main DM when exactly one allowed user exists
  - explicit observer targets from `/observe all`
  - explicit observer targets do not replace the implicit main DM
- Target v2:
  - one global observer target
  - default-on when the target can be resolved automatically
  - `/observe all` moves the target
  - `/observe off` disables monitoring

## Minimal observer event kinds

- `turn_started`
- `tool_activity`
- `thread_updated`
- `final_answer`
- `turn_completed`
- `turn_failed`

Observer/UI v2 presentation contract:

- run notice:
  - appears before `[User]` and summary/tool/output for new runs
  - carries source markers, source mode, and route metadata, but not run status
  - is deleted best-effort after finalization
- user notice:
  - appears after `New run` for GUI/CLI runs and before summary/tool/output
  - remains after finalization as the historical request marker
  - may start as a placeholder and edit into the actual prompt
- summary-panel update:
  - carries project/thread source markers
  - owns live run status while active
  - carries action buttons such as `Stop` and `Steer`
- tool/output message:
  - carries source markers
  - carries no buttons
  - is deleted best-effort after finalization
- final-answer message:
  - carries source markers
  - carries on-demand `Получить полный лог`
  - contains final answer/status without replaying completed commentary/tool/output transcript

Minimal event payload expected by the Telegram layer:

- `event_id`
- `kind`
- `thread_id`
- `project_name`
- `thread_title`
- `text`

Optional event payload fields:

- `status`
- `turn_id`
- `item_id`
- `request_id`
- `needs_reply`
- `needs_approval`

Plan prompt payload fields:

- `prompt_id`
- `source`
- `thread_id`
- `turn_id`
- `item_id`
- `request_id`
- `question`
- `options`
- `fingerprint`

## Acceptance scenarios

- global observer is active by default when one operator target exists
- `/observe all` moves the observer target to the current chat/topic
- `/observe off` disables global monitoring
- `/status` must show readiness, transport, queue, tracked thread count, and current routing
- `/context` must describe the active tuple of chat/project/thread or the lack of one
- polling fallback must emit progress/final/completion for foreign threads
- stale live-only assumptions must not suppress polling fallback
- repair must recreate app-server sessions and resume tracked threads without manual route surgery
- observer delivery must remain durable across daemon restart
- summary panels must be stable per `(chat, project, thread)` instead of spamming a new actionable message for every event
- waiting Plan prompts must be visible as `[Plan]` messages and answerable by Telegram Reply
- late foreign `[User]` prompts must edit the existing placeholder, not append below live trio messages
- duplicate live+poll sync must not create multiple `[Plan]` cards for the same prompt fingerprint
