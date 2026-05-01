# Live Telegram Readback E2E

These tests are intentionally outside `go test ./...`. They drive a real Telegram
user session and verify the bot messages as the operator sees them, including
edited `[Tool]`, `[Output]`, and `[Final]` messages.

The harness is safe to keep in the public repository because it contains no
credentials, chat ids, thread ids, sessions, or local logs. All live state must
come from local environment variables.

## Required Environment

- `CODEX_TG_LIVE_E2E=1`
- `CODEX_TG_E2E_THREAD_ID`
- `TG_SESSION`
- `TG_API_ID`
- `TG_API_HASH`
- `TG_BOT_USERNAME` or `TG_BOT_PEER_ID`

Optional:

- `CODEX_TG_LIVE_E2E_ENV`: path to a local ignored env file.
- `CODEX_TG_E2E_DAEMON_LOG`: local daemon stdout log path for extra diagnostics.
- `TG_PROXY`: `socks5://`, `socks4://`, `http://`, or `https://` proxy URL.
- `CODEX_TG_E2E_POLL_SECONDS`
- `CODEX_TG_E2E_READBACK_LIMIT`

The env file and Telethon session files must stay local. `.gitignore` blocks
`.env*`, `telegram.env`, and `*.session*` as a belt-and-suspenders guard.

## Run

```bash
python3 -m venv .venv-live-e2e
. .venv-live-e2e/bin/activate
python -m pip install telethon pysocks

CODEX_TG_LIVE_E2E=1 \
CODEX_TG_LIVE_E2E_ENV=/path/to/local/telegram.env \
CODEX_TG_E2E_THREAD_ID=<private-test-thread-id> \
python tests/live_e2e/telegram_readback_e2e.py
```

## Cases

`sequential_commands` asks the agent to run `pwd`, `date`, `printf`, and a slow
`sleep 20; printf ...` command as separate tool calls. It passes only if the slow
command appears in `[Tool]` before its output appears in `[Output]`.

`complex_math` asks the agent, through `/reply`, to create a temporary Python
helper and run four separate range commands for a number-theory task. It passes
only if all four tool/output updates are observed and the final answer contains:

```text
COUNT=2034 SUM=115514223
```

Both cases fail on visible `Status: interrupted`, literal `"<nil>"`, stale known
commands from earlier regressions, or parallel-turn rejection text.
