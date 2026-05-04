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
- `CODEX_TG_E2E_CASES`: comma-separated subset of
  `sequential_commands,sleep20_timing,multi_tool_current,complex_math`.

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

`sequential_commands` sends three `/reply` prompts one after another for `pwd`,
`date`, and `printf`. Each command also reads a runtime token from a temp file
created by the harness, so the agent has to read tool output before answering.
Each command sleeps long enough for edited run-state messages to stay
observable. The harness waits for each run to finish
before sending the next one, and validates whole-run timing in `[commentary]`,
last completed tool state in `[Tool]`, last completed output in `[Output]`, and
run duration in `[Final]`.

`sleep20_timing` asks the agent to run one `sleep 20; printf ...` command and
validates that `[commentary]` keeps showing active run elapsed time while
Telegram-origin `[Tool]` shows the live `Current tool` before completion.

`multi_tool_current` asks the agent to run three separate slow shell commands
and validates that Telegram-origin `[Tool]` moves through live `Current tool`
states before completed tool/output cards settle.

`complex_math` asks the agent, through `/reply`, to create a temporary Python
helper and run four separate range commands for a number-theory task. It passes
only if last completed tool/output updates are observed, active run timing is
visible, and the final answer contains:

```text
COUNT=2034 SUM=115514223
```

All cases fail on visible `Status: interrupted`, literal `"<nil>"`, stale known
commands from earlier regressions, parallel-turn rejection text, or `[Tool]`
putting run timing in the tool card. Running/in-progress tool status is allowed
only under the explicit Telegram-origin `Current tool:` heading.
