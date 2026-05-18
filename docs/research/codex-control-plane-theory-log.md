# Codex Control Plane Theory Log

Use this file to keep architecture theories explicit. Each theory should be
confirmed by official docs, local schema, source inspection, or an experiment
before becoming an implementation requirement.

| Theory | Evidence | Decision |
| --- | --- | --- |
| App Server remains the primary control plane. | Official App Server docs and local schema expose thread, turn, approval, event, history, and ecosystem operations. | Accepted for v0.5. |
| `unix://` and `app-server proxy` may decouple daemon lifecycle from active turns. | Local Codex CLI help exposes both transports. | Research candidate; do not change runtime transport in docs-only slice. |
| Skills power router skill discovery. | Official Skills docs and local `skills/list` schema exist. | Accepted as future router input. |
| Hooks support audit, memory, validation, and policy. | Official Hooks docs and local `hooks/list`, `hook/started`, `hook/completed` schema exist. | Accepted as sidecar, not primary router API. |
| Automations should own recurring Codex jobs. | Official Automations docs cover recurring app tasks. | Accepted as preferred Codex scheduling surface. |
| MCP server supports multi-agent orchestration, not full thread UI. | Agents SDK guide describes Codex MCP workflow integration; MCP help starts a stdio server. | Accepted as orchestration adapter. |
| Voice should start as a chained pipeline. | Voice Agents docs describe speech-to-speech and chained architectures; Codex routing needs audit and approval control. | Accepted for first voice prototype. |
| Telegram remains a high-signal adapter. | Existing implementation has routing, Plan prompts, Details, notifications, and live E2E contracts. | Accepted; no deprecation. |
| Remote Connections invalidate old positioning. | Official Remote Connections cover broad mobile remote-control workflows. | Accepted; README should reposition. |
| Generated schema drift needs regression checks. | Local schema differed from some public goal-surface expectations. | Accepted; future capability map should be schema-backed. |
