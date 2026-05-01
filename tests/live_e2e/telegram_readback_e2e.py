#!/usr/bin/env python3
"""Live Telegram readback E2E for codex-tg.

This harness is public-safe: it has no default thread id, chat id, Telegram
session, or local log path. It requires explicit local environment variables and
uses Telegram readback rather than Bot API success as the acceptance surface.
"""

from __future__ import annotations

import asyncio
import json
import os
import re
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from urllib.parse import urlparse

import socks
from telethon import TelegramClient


REJECT_BITS = (
    "<nil>",
    "I did not start a parallel turn",
    "no active turn to steer",
    "expected active turn id",
    "already active",
    "Start-Sleep -Seconds 1800",
)

RANGE_LABELS = ("1 30000", "30001 60000", "60001 90000", "90001 120000")
EXPECTED_COMPLEX_COUNT = "2034"
EXPECTED_COMPLEX_SUM = "115514223"


def load_env_file(path: str) -> None:
    path = path.strip()
    if not path:
        return
    for raw in Path(path).read_text(encoding="utf-8").splitlines():
        raw = raw.strip()
        if not raw or raw.startswith("#") or "=" not in raw:
            continue
        key, value = raw.split("=", 1)
        os.environ.setdefault(key.strip(), value.strip())


def require_env() -> None:
    load_env_file(os.environ.get("CODEX_TG_LIVE_E2E_ENV", ""))
    if os.environ.get("CODEX_TG_LIVE_E2E") != "1":
        raise SystemExit("set CODEX_TG_LIVE_E2E=1 to run live Telegram E2E")
    required = [
        "CODEX_TG_E2E_THREAD_ID",
        "TG_SESSION",
        "TG_API_ID",
        "TG_API_HASH",
    ]
    missing = [key for key in required if not os.environ.get(key, "").strip()]
    if not (os.environ.get("TG_BOT_USERNAME", "").strip() or os.environ.get("TG_BOT_PEER_ID", "").strip()):
        missing.append("TG_BOT_USERNAME or TG_BOT_PEER_ID")
    if missing:
        raise SystemExit("missing required env: " + ", ".join(missing))


def parse_proxy(raw: str):
    raw = (raw or "").strip()
    if not raw:
        return None
    parsed = urlparse(raw)
    scheme_map = {
        "socks5": socks.SOCKS5,
        "socks4": socks.SOCKS4,
        "http": socks.HTTP,
        "https": socks.HTTP,
    }
    kind = scheme_map.get(parsed.scheme.lower())
    if kind is None or not parsed.hostname or not parsed.port:
        raise SystemExit("unsupported TG_PROXY value")
    return (kind, parsed.hostname, parsed.port, True, parsed.username, parsed.password)


def utc_now() -> datetime:
    return datetime.now(timezone.utc)


def parse_event_time(value: str):
    if not value:
        return None
    try:
        return datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return None


def load_log_events(since: datetime):
    path = os.environ.get("CODEX_TG_E2E_DAEMON_LOG", "").strip()
    if not path:
        return []
    log_path = Path(path)
    if not log_path.exists():
        return []
    events = []
    pattern = re.compile(r"daemon_event (\{.*\})")
    for line in log_path.read_text(encoding="utf-8", errors="replace").splitlines():
        match = pattern.search(line)
        if not match:
            continue
        try:
            event = json.loads(match.group(1))
        except json.JSONDecodeError:
            continue
        at = parse_event_time(str(event.get("at", "")))
        if at is None or at < since:
            continue
        event["_at"] = at
        events.append(event)
    return events


def preview(text: str) -> str:
    return text[:500].replace("\n", "\\n")


def poll_seconds() -> float:
    try:
        return max(0.5, float(os.environ.get("CODEX_TG_E2E_POLL_SECONDS", "2")))
    except ValueError:
        return 2.0


def readback_limit() -> int:
    try:
        return max(40, int(os.environ.get("CODEX_TG_E2E_READBACK_LIMIT", "100")))
    except ValueError:
        return 100


def is_bot_message(message, bot) -> bool:
    return getattr(message, "sender_id", None) == bot.id or getattr(message.sender, "id", None) == bot.id


def is_final_with_marker(text: str, marker: str) -> bool:
    return marker in text and "[Final]" in text


def fail_if_bad_text(text: str, context: str) -> None:
    for bit in REJECT_BITS:
        if bit in text:
            raise SystemExit(f"BAD_TEXT context={context} bit={bit!r} preview={preview(text)}")
    if "Status: interrupted" in text:
        raise SystemExit(f"VISIBLE_INTERRUPTED context={context} preview={preview(text)}")


def fail_if_bad_logs(since: datetime, thread_id: str, context: str) -> None:
    events = load_log_events(since)
    if not events:
        return
    for event in events:
        if event.get("event") == "telegram_render_contains_nil":
            raise SystemExit(f"LOG_RENDER_NIL context={context}")
        if event.get("event") == "telegram_turn_input_rejected":
            raise SystemExit(f"LOG_INPUT_REJECTED context={context}")
        if event.get("thread_id") != thread_id:
            continue
        if (
            event.get("event") == "telegram_origin_turn_terminal"
            and event.get("latest_turn_status") == "interrupted"
            and not event.get("has_final")
        ):
            raise SystemExit(f"LOG_INTERRUPTED_WITHOUT_FINAL context={context}")


async def bot_messages_after(client, bot, sent_id: int):
    messages = await client.get_messages(bot, limit=readback_limit())
    for message in reversed(messages):
        if message.id > sent_id and is_bot_message(message, bot):
            yield message


async def sequential_commands_case(client, bot, thread_id: str, stamp: str) -> None:
    marker = f"OK_LIVE_COMMANDS_{stamp}"
    prompt = (
        "Run these four shell commands as four separate tool calls, strictly one after another, "
        "waiting for each to finish before starting the next: "
        "1) pwd 2) date 3) printf 'alpha\\nbeta\\n' 4) sleep 20; printf 'slow-command-done\\n'. "
        f"Do not final-answer before the last command completes. Then final answer exactly: {marker}"
    )
    since = utc_now()
    sent = await client.send_message(bot, f"/reply {thread_id} {prompt}")
    print(f"SENT case=sequential_commands message_id={sent.id} marker={marker}", flush=True)
    deadline = time.time() + 240
    seen = set()
    sleep_tool_at = None
    output_at = None
    final = None
    while time.time() < deadline:
        await asyncio.sleep(poll_seconds())
        async for message in bot_messages_after(client, bot, sent.id):
            text = (message.raw_text or "").strip()
            key = (message.id, text)
            if key not in seen:
                seen.add(key)
                print(f"BOT case=sequential_commands message_id={message.id} preview={preview(text)}", flush=True)
            fail_if_bad_text(text, "sequential_commands")
            now = time.time()
            if "[Tool]" in text and "sleep 20" in text and sleep_tool_at is None:
                sleep_tool_at = now
            if "[Output]" in text and "slow-command-done" in text and output_at is None:
                output_at = now
            if is_final_with_marker(text, marker):
                final = message
        if final is not None:
            break
    if final is None:
        raise SystemExit(f"FINAL_TIMEOUT case=sequential_commands marker={marker}")
    if sleep_tool_at is None or output_at is None:
        raise SystemExit("SLOW_COMMAND_VISIBILITY_MISSING")
    if output_at - sleep_tool_at < 10:
        raise SystemExit(f"SLOW_COMMAND_TOO_LATE delta={output_at - sleep_tool_at:.1f}")
    fail_if_bad_logs(since, thread_id, "sequential_commands")
    print(f"RESULT case=sequential_commands ok delta={output_at - sleep_tool_at:.1f}", flush=True)


async def complex_math_case(client, bot, thread_id: str, stamp: str) -> None:
    marker = f"OK_LIVE_COMPLEX_MATH_{stamp}"
    script_path = f"/tmp/codex_tg_live_math_{stamp}.py"
    prompt = f"""
Solve this number-theory task using multiple shell commands, because this E2E checks that a /reply-started run does not self-interrupt while several tools execute.

Task:
For every integer n from 1 to 120000 inclusive, keep n only if:
- n % 7 == 3 or n % 11 == 4
- n is not divisible by 5
- n*n + 3*n + 7 is prime

Run at least five separate shell tool calls:
1. create a temporary Python helper at {script_path}
2. run it for range 1..30000
3. run it for range 30001..60000
4. run it for range 60001..90000
5. run it for range 90001..120000

The helper must sleep for 4 seconds before printing each range result, so Telegram has time to show each tool as in progress. Each range run should print:
RANGE <lo> <hi> COUNT <count> SUM <sum>

After the four range outputs, add the counts and sums yourself. Final answer must contain exactly:
{marker} COUNT=<count> SUM=<sum>
""".strip()
    since = utc_now()
    sent = await client.send_message(bot, f"/reply {thread_id} {prompt}")
    print(f"SENT case=complex_math message_id={sent.id} marker={marker}", flush=True)
    deadline = time.time() + 420
    seen = set()
    observed_tools = set()
    observed_outputs = set()
    final = None
    while time.time() < deadline:
        await asyncio.sleep(poll_seconds())
        async for message in bot_messages_after(client, bot, sent.id):
            text = (message.raw_text or "").strip()
            key = (message.id, text)
            if key not in seen:
                seen.add(key)
                print(f"BOT case=complex_math message_id={message.id} preview={preview(text)}", flush=True)
            fail_if_bad_text(text, "complex_math")
            if "[Tool]" in text and ("python" in text or script_path in text):
                for label in RANGE_LABELS:
                    if label in text:
                        observed_tools.add(label)
            if "[Output]" in text and "RANGE " in text:
                for label in RANGE_LABELS:
                    if label in text:
                        observed_outputs.add(label)
            if is_final_with_marker(text, marker):
                final = message
        if final is not None:
            break
    if final is None:
        raise SystemExit(f"FINAL_TIMEOUT case=complex_math marker={marker}")
    text = (final.raw_text or "").strip()
    if f"COUNT={EXPECTED_COMPLEX_COUNT}" not in text or f"SUM={EXPECTED_COMPLEX_SUM}" not in text:
        raise SystemExit(f"COMPLEX_MATH_ANSWER_MISSING preview={preview(text)}")
    if len(observed_tools) < 3 or len(observed_outputs) < 3:
        raise SystemExit(
            "COMPLEX_MATH_TOO_FEW_UPDATES "
            f"tools={sorted(observed_tools)} outputs={sorted(observed_outputs)}"
        )
    fail_if_bad_logs(since, thread_id, "complex_math")
    print(f"RESULT case=complex_math ok tools={sorted(observed_tools)} outputs={sorted(observed_outputs)}", flush=True)


async def main() -> None:
    require_env()
    thread_id = os.environ["CODEX_TG_E2E_THREAD_ID"].strip()
    stamp = str(int(time.time()))
    client = TelegramClient(
        os.environ["TG_SESSION"],
        int(os.environ["TG_API_ID"]),
        os.environ["TG_API_HASH"],
        proxy=parse_proxy(os.environ.get("TG_PROXY", "")),
    )
    await client.connect()
    try:
        if not await client.is_user_authorized():
            raise SystemExit("Telegram user session is not authorized")
        bot_peer_id = os.environ.get("TG_BOT_PEER_ID", "").strip()
        bot = await client.get_entity(int(bot_peer_id) if bot_peer_id else os.environ["TG_BOT_USERNAME"])
        print("LIVE_E2E start cases=sequential_commands,complex_math", flush=True)
        await sequential_commands_case(client, bot, thread_id, stamp)
        await complex_math_case(client, bot, thread_id, stamp)
        print("ALL_OK", flush=True)
    finally:
        await client.disconnect()


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        sys.exit(130)
