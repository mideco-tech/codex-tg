# Codex Control Plane Research

This note records the evidence behind ADR-019. It is intentionally high-level
and public-safe: no local paths, tokens, private thread ids, or logs.

## Official Evidence

- [Codex App Server](https://developers.openai.com/codex/app-server) is the
  official surface for rich local integrations: conversation history, approvals,
  streamed events, and client-driven Codex sessions.
- [Codex SDK](https://developers.openai.com/codex/sdk) targets automation,
  CI/CD, internal tools, and agents that engage with Codex. It is useful for
  orchestration research, but it should not silently replace App Server as the
  interactive state authority.
- [Use Codex with Agents SDK](https://developers.openai.com/codex/guides/agents-sdk)
  shows Codex as an MCP server that can participate in agent workflows,
  handoffs, guardrails, and traces.
- [Codex MCP](https://developers.openai.com/codex/mcp) keeps existing MCP
  servers, tools, and resources inside the Codex setup. This supports the
  strategy of reusing the operator's Codex environment instead of migrating it
  into a separate assistant stack.
- [Skills](https://developers.openai.com/codex/skills) are the native reusable
  workflow format. Router work should discover and select skills rather than
  inventing a parallel format.
- [Hooks](https://developers.openai.com/codex/hooks) are lifecycle sidecars for
  audit, validation, memory capture, and policy checks. They are not the primary
  interactive router API.
- [Automations](https://developers.openai.com/codex/app/automations) are the
  native recurring Codex task surface. A router should prefer them for Codex
  schedules instead of creating a competing cron model.
- [Remote Connections](https://developers.openai.com/codex/remote-connections)
  cover the broad phone remote-control use case. `codex-tg` should position
  itself as a control layer and adapter system, not a replacement remote client.
- [Voice agents](https://developers.openai.com/api/docs/guides/voice-agents)
  support both speech-to-speech and chained voice pipelines. A chained pipeline
  is the safer first fit for a Codex router because it preserves routing,
  approval, audit, and notification control.

## Local Evidence

The local Codex CLI inspected during research was `codex-cli 0.130.0`.

`codex app-server --help` exposed:

- `stdio://`
- `unix://`
- `unix://PATH`
- `ws://IP:PORT`
- `off`
- `codex app-server proxy`
- `codex app-server generate-json-schema`

Generated schema capabilities included:

- thread lifecycle: `thread/start`, `thread/resume`, `thread/fork`,
  `thread/read`, `thread/list`, `thread/name/set`, `thread/archive`,
  `thread/unarchive`, `thread/compact/start`, `thread/rollback`,
  `thread/inject_items`;
- turn lifecycle: `turn/start`, `turn/steer`, `turn/interrupt`;
- live events: `turn/*`, `item/*`, `thread/status/changed`,
  `serverRequest/resolved`;
- user interaction: command/file/permission approvals, user input, MCP
  elicitation;
- ecosystem: `skills/list`, `hooks/list`, `mcpServerStatus/list`, `app/list`,
  plugin, config, filesystem, command, review, and realtime methods.

Known docs/schema drift: the public docs mention goal behavior, while the local
generated schema showed goal notifications but no client request method for
setting goals. Future implementation must verify capabilities from the
installed Codex version rather than assuming every documented surface is
available.

## Conclusion

The recommended architecture is hybrid:

- App Server is the primary interactive control plane.
- SDK/MCP are optional orchestration adapters.
- Native Skills, Hooks, and Automations are reused as Codex-native building
  blocks.
- Telegram remains valuable as a high-signal notification and reply adapter.
- Voice and router-agent work should be built on top of an adapter-independent
  control core.
