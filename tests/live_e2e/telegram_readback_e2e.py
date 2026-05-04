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
import secrets
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
DEFAULT_CASES = ("sequential_commands", "sleep20_timing", "multi_tool_current", "complex_math")


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


def selected_cases() -> list[str]:
    raw = os.environ.get("CODEX_TG_E2E_CASES", "").strip()
    if not raw:
        return list(DEFAULT_CASES)
    requested = [part.strip() for part in raw.split(",") if part.strip()]
    unknown = sorted(set(requested) - set(DEFAULT_CASES))
    if unknown:
        raise SystemExit(f"Unknown CODEX_TG_E2E_CASES values: {unknown}; valid={list(DEFAULT_CASES)}")
    return requested


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
    if "[Tool]" in text and "Current tool:" not in text and "Status: inProgress" in text:
        raise SystemExit(f"CURRENT_TOOL_RENDERED context={context} preview={preview(text)}")
    if "[Tool]" in text and "Running for:" in text:
        raise SystemExit(f"TOOL_TIMER_RENDERED context={context} preview={preview(text)}")


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
    temp_specs = []
    for label in ("pwd", "date", "printf"):
        token = f"SEQ_TOKEN_{stamp}_{label}_{secrets.token_hex(8)}"
        path = Path(f"/tmp/codex_tg_live_seq_{stamp}_{label}.txt")
        path.write_text(token + "\n", encoding="utf-8")
        temp_specs.append((label, path, token))

    commands = [
        ("pwd", f"sleep 12; pwd; cat {temp_specs[0][1]}", temp_specs[0][2]),
        ("date", f"sleep 12; date; cat {temp_specs[1][1]}", temp_specs[1][2]),
        (
            "printf",
            f"sleep 12; printf 'alpha\\nbeta\\n'; cat {temp_specs[2][1]}",
            temp_specs[2][2],
        ),
    ]
    since = utc_now()
    observed_tools = set()
    alpha_output_seen = False

    try:
        for index, (label, command, required_output) in enumerate(commands, start=1):
            marker = f"OK_LIVE_COMMANDS_{stamp}_{label}"
            prompt = (
                "Run exactly one shell command tool call, verbatim, and wait for it to finish before answering: "
                f"{command}. "
                "The command prints a token from a local temp file; the token is not in this prompt. "
                f"Final answer in one line containing {marker} and the exact SEQ_TOKEN_... line from the tool output."
            )
            sent = await client.send_message(bot, f"/reply {thread_id} {prompt}")
            print(
                f"SENT case=sequential_commands step={index} label={label} "
                f"message_id={sent.id} marker={marker}",
                flush=True,
            )
            tool_seen = False
            active_wait_seen = False
            output_seen = False
            final = None
            final_seen_at = None
            seen = set()
            deadline = time.time() + 180
            while time.time() < deadline:
                await asyncio.sleep(poll_seconds())
                now = time.time()
                async for message in bot_messages_after(client, bot, sent.id):
                    text = (message.raw_text or "").strip()
                    key = (message.id, text)
                    if key not in seen:
                        seen.add(key)
                        print(
                            f"BOT case=sequential_commands step={index} "
                            f"message_id={message.id} preview={preview(text)}",
                            flush=True,
                        )
                    fail_if_bad_text(text, "sequential_commands")
                    for other_label, _, other_token in temp_specs:
                        if other_label != label and other_token in text:
                            raise SystemExit(
                                f"SEQUENTIAL_STALE_TOKEN label={label} stale_label={other_label}"
                            )
                    if "[commentary]" in text and "Run active for:" in text:
                        active_wait_seen = True
                    if "[Tool]" in text and "Last completed tool:" in text and label in text:
                        tool_seen = True
                        observed_tools.add(label)
                    if "[Output]" in text and "Last completed output:" in text and required_output in text:
                        output_seen = True
                        if label == "printf" and "alpha" in text and "beta" in text:
                            alpha_output_seen = True
                    if is_final_with_marker(text, marker):
                        final = message
                        if final_seen_at is None:
                            final_seen_at = now
                if final is not None and tool_seen and output_seen:
                    break
                if final is not None and final_seen_at is not None and now-final_seen_at > 20:
                    break
            if final is None:
                raise SystemExit(f"FINAL_TIMEOUT case=sequential_commands marker={marker}")
            final_text = (final.raw_text or "").strip()
            if required_output not in final_text:
                raise SystemExit(f"SEQUENTIAL_FINAL_TOKEN_MISSING label={label}")
            if "Run duration:" not in final_text:
                raise SystemExit(f"SEQUENTIAL_FINAL_DURATION_MISSING label={label}")
            if not active_wait_seen:
                raise SystemExit(f"SEQUENTIAL_RUN_TIMER_MISSING label={label}")
            if tool_seen:
                observed_tools.add(label)
            if output_seen and label == "printf":
                alpha_output_seen = True
    finally:
        for _, path, _ in temp_specs:
            try:
                path.unlink()
            except FileNotFoundError:
                pass

    fail_if_bad_logs(since, thread_id, "sequential_commands")
    print(f"RESULT case=sequential_commands ok observed_completed_tools={sorted(observed_tools)}", flush=True)


async def sleep20_timing_case(client, bot, thread_id: str, stamp: str) -> None:
    marker = f"OK_LIVE_SLEEP20_{stamp}"
    done = f"SLEEP20_DONE_{stamp}"
    prompt = (
        "This is a live UI timing test. Run exactly one shell command tool call: "
        f"sleep 20; printf '{done}\\n'. "
        "Do not use any other shell command. Do not answer before that command finishes. "
        f"Final answer exactly: {marker}"
    )
    since = utc_now()
    sent = await client.send_message(bot, f"/reply {thread_id} {prompt}")
    print(f"SENT case=sleep20_timing message_id={sent.id} marker={marker}", flush=True)
    deadline = time.time() + 180
    seen = set()
    tool_seen = False
    current_seen = False
    active_wait_seen = False
    output_seen = False
    final_seen = False
    tool_first_at = None
    final_at = None
    while time.time() < deadline:
        await asyncio.sleep(poll_seconds())
        async for message in bot_messages_after(client, bot, sent.id):
            text = (message.raw_text or "").strip()
            key = (message.id, text)
            if key not in seen:
                seen.add(key)
                print(f"BOT case=sleep20_timing message_id={message.id} preview={preview(text)}", flush=True)
            fail_if_bad_text(text, "sleep20_timing")
            if "[commentary]" in text and "Run active for:" in text:
                active_wait_seen = True
            if "[Tool]" in text:
                if "Current tool:" in text and "sleep 20" in text and done in text and not final_seen:
                    current_seen = True
                if tool_seen and not output_seen and "No completed tool yet." in text:
                    raise SystemExit(f"SLEEP20_TOOL_DISAPPEARED preview={preview(text)}")
                if "Last completed tool:" in text and "sleep 20" in text and done in text:
                    if not tool_seen:
                        tool_seen = True
                        tool_first_at = time.time()
            if "[Output]" in text and "Last completed output:" in text and done in text:
                output_seen = True
            if is_final_with_marker(text, marker):
                final_seen = True
                final_at = time.time()
                if "Run duration:" not in text:
                    raise SystemExit(f"SLEEP20_FINAL_DURATION_MISSING preview={preview(text)}")
        if final_seen and active_wait_seen:
            break
    if not active_wait_seen:
        raise SystemExit("SLEEP20_RUN_TIMER_NOT_SEEN")
    if not current_seen:
        raise SystemExit("SLEEP20_CURRENT_TOOL_NOT_SEEN")
    if not final_seen:
        raise SystemExit(f"FINAL_TIMEOUT case=sleep20_timing marker={marker}")
    fail_if_bad_logs(since, thread_id, "sleep20_timing")
    visible_for = 0.0
    if tool_first_at is not None and final_at is not None:
        visible_for = final_at - tool_first_at
    print(f"RESULT case=sleep20_timing ok visible_for={visible_for:.1f}", flush=True)


async def multi_tool_current_case(client, bot, thread_id: str, stamp: str) -> None:
    marker = f"OK_LIVE_CURRENT_TOOLS_{stamp}"
    labels = [
        f"CURRENT_STEP_A_{stamp}",
        f"CURRENT_STEP_B_{stamp}",
        f"CURRENT_STEP_C_{stamp}",
    ]
    commands = [
        f"sleep 7; printf '{labels[0]}\\n'",
        f"sleep 7; printf '{labels[1]}\\n'",
        f"sleep 7; printf '{labels[2]}\\n'",
    ]
    prompt = (
        "Run these three shell command tool calls as three separate tool calls, strictly one after another, "
        "waiting for each to finish before starting the next. Do not combine them into a script. Commands:\n"
        + "\n".join(f"{index}. {command}" for index, command in enumerate(commands, start=1))
        + f"\nAfter the third command finishes, final answer exactly: {marker}"
    )
    since = utc_now()
    sent = await client.send_message(bot, f"/reply {thread_id} {prompt}")
    print(f"SENT case=multi_tool_current message_id={sent.id} marker={marker}", flush=True)
    deadline = time.time() + 240
    seen = set()
    current_seen = set()
    completed_seen = set()
    output_seen = set()
    active_wait_seen = False
    final_seen = False
    while time.time() < deadline:
        await asyncio.sleep(poll_seconds())
        async for message in bot_messages_after(client, bot, sent.id):
            text = (message.raw_text or "").strip()
            key = (message.id, text)
            if key not in seen:
                seen.add(key)
                print(f"BOT case=multi_tool_current message_id={message.id} preview={preview(text)}", flush=True)
            fail_if_bad_text(text, "multi_tool_current")
            if "[commentary]" in text and "Run active for:" in text:
                active_wait_seen = True
            if "[Tool]" in text and "Current tool:" in text:
                for label in labels:
                    if label in text:
                        current_seen.add(label)
            if "[Tool]" in text and "Last completed tool:" in text:
                for label in labels:
                    if label in text:
                        completed_seen.add(label)
            if "[Output]" in text and "Last completed output:" in text:
                for label in labels:
                    if label in text:
                        output_seen.add(label)
            if is_final_with_marker(text, marker):
                final_seen = True
        if final_seen and active_wait_seen and len(current_seen) >= 2 and len(completed_seen) >= 2 and len(output_seen) >= 2:
            break
    if not final_seen:
        raise SystemExit(f"FINAL_TIMEOUT case=multi_tool_current marker={marker}")
    if not active_wait_seen:
        raise SystemExit("MULTI_TOOL_RUN_TIMER_NOT_SEEN")
    if len(current_seen) < 2:
        raise SystemExit(f"MULTI_TOOL_CURRENT_TOO_FEW current={sorted(current_seen)}")
    if len(completed_seen) < 2 or len(output_seen) < 2:
        raise SystemExit(
            "MULTI_TOOL_COMPLETED_TOO_FEW "
            f"completed={sorted(completed_seen)} outputs={sorted(output_seen)}"
        )
    fail_if_bad_logs(since, thread_id, "multi_tool_current")
    print(
        "RESULT case=multi_tool_current ok "
        f"current={sorted(current_seen)} completed={sorted(completed_seen)} outputs={sorted(output_seen)}",
        flush=True,
    )


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
    active_wait_seen = False
    final = None
    final_seen_at = None
    while time.time() < deadline:
        await asyncio.sleep(poll_seconds())
        now = time.time()
        async for message in bot_messages_after(client, bot, sent.id):
            text = (message.raw_text or "").strip()
            key = (message.id, text)
            if key not in seen:
                seen.add(key)
                print(f"BOT case=complex_math message_id={message.id} preview={preview(text)}", flush=True)
            fail_if_bad_text(text, "complex_math")
            if "[commentary]" in text and "Run active for:" in text:
                active_wait_seen = True
            if "[Tool]" in text and "Last completed tool:" in text and ("python" in text or script_path in text):
                for label in RANGE_LABELS:
                    if label in text:
                        observed_tools.add(label)
            if "[Output]" in text and "Last completed output:" in text and "RANGE " in text:
                for label in RANGE_LABELS:
                    if label in text:
                        observed_outputs.add(label)
            if is_final_with_marker(text, marker):
                final = message
                if final_seen_at is None:
                    final_seen_at = now
        if final is not None and active_wait_seen and len(observed_tools) >= 3 and len(observed_outputs) >= 3:
            break
        if final is not None and final_seen_at is not None and now-final_seen_at > 20:
            break
    if final is None:
        raise SystemExit(f"FINAL_TIMEOUT case=complex_math marker={marker}")
    text = (final.raw_text or "").strip()
    if f"COUNT={EXPECTED_COMPLEX_COUNT}" not in text or f"SUM={EXPECTED_COMPLEX_SUM}" not in text:
        raise SystemExit(f"COMPLEX_MATH_ANSWER_MISSING preview={preview(text)}")
    if "Run duration:" not in text:
        raise SystemExit(f"COMPLEX_MATH_DURATION_MISSING preview={preview(text)}")
    if not active_wait_seen:
        raise SystemExit("COMPLEX_MATH_RUN_TIMER_MISSING")
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
    cases = selected_cases()
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
        print(f"LIVE_E2E start cases={','.join(cases)}", flush=True)
        for case in cases:
            if case == "sequential_commands":
                await sequential_commands_case(client, bot, thread_id, stamp)
            elif case == "sleep20_timing":
                await sleep20_timing_case(client, bot, thread_id, stamp)
            elif case == "multi_tool_current":
                await multi_tool_current_case(client, bot, thread_id, stamp)
            elif case == "complex_math":
                await complex_math_case(client, bot, thread_id, stamp)
        print("ALL_OK", flush=True)
    finally:
        await client.disconnect()


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        sys.exit(130)
