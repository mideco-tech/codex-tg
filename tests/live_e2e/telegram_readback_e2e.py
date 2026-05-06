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
DEFAULT_CASES = (
    "sequential_commands",
    "sleep20_timing",
    "tool_only_sleep_details",
    "multi_tool_current",
    "current_tool_priority",
    "details_binding",
    "complex_math",
)
AVAILABLE_CASES = DEFAULT_CASES + ("newchat_folder", "notification_contract", "plan_mode_reset")
NO_BASE_THREAD_CASES = {"newchat_folder", "plan_mode_reset"}


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


def require_env() -> list[str]:
    load_env_file(os.environ.get("CODEX_TG_LIVE_E2E_ENV", ""))
    if os.environ.get("CODEX_TG_LIVE_E2E") != "1":
        raise SystemExit("set CODEX_TG_LIVE_E2E=1 to run live Telegram E2E")
    cases = selected_cases()
    required = [
        "TG_SESSION",
        "TG_API_ID",
        "TG_API_HASH",
    ]
    if any(case not in NO_BASE_THREAD_CASES for case in cases):
        required.insert(0, "CODEX_TG_E2E_THREAD_ID")
    missing = [key for key in required if not os.environ.get(key, "").strip()]
    if not (os.environ.get("TG_BOT_USERNAME", "").strip() or os.environ.get("TG_BOT_PEER_ID", "").strip()):
        missing.append("TG_BOT_USERNAME or TG_BOT_PEER_ID")
    if missing:
        raise SystemExit("missing required env: " + ", ".join(missing))
    return cases


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
    unknown = sorted(set(requested) - set(AVAILABLE_CASES))
    if unknown:
        raise SystemExit(f"Unknown CODEX_TG_E2E_CASES values: {unknown}; valid={list(AVAILABLE_CASES)}")
    return requested


def is_bot_message(message, bot) -> bool:
    return getattr(message, "sender_id", None) == bot.id or getattr(message.sender, "id", None) == bot.id


def is_final_with_marker(text: str, marker: str) -> bool:
    return marker in text and "[Final]" in text


def has_button(message, text: str) -> bool:
    for row in message.buttons or []:
        for button in row:
            if getattr(button, "text", "") == text:
                return True
    return False


def parse_thread_id(text: str) -> str:
    match = re.search(r"(?m)^Thread ID:\s*(\S+)\s*$", text or "")
    if not match:
        return ""
    return match.group(1).strip()


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


async def wait_final_message(client, bot, after_id: int, marker: str, context: str, timeout: int = 240):
    deadline = time.time() + timeout
    seen = set()
    while time.time() < deadline:
        await asyncio.sleep(poll_seconds())
        async for message in bot_messages_after(client, bot, after_id):
            text = (message.raw_text or "").strip()
            key = (message.id, text)
            if key not in seen:
                seen.add(key)
                print(f"BOT case={context} message_id={message.id} preview={preview(text)}", flush=True)
            fail_if_bad_text(text, context)
            if is_final_with_marker(text, marker) and has_button(message, "Details"):
                return message
    raise SystemExit(f"FINAL_TIMEOUT case={context} marker={marker}")


async def wait_message_by_id(client, bot, message_id: int, context: str, predicate, timeout: int = 80):
    deadline = time.time() + timeout
    last = ""
    while time.time() < deadline:
        await asyncio.sleep(1)
        message = await client.get_messages(bot, ids=message_id)
        text = (message.raw_text or "").strip()
        if text != last:
            last = text
            print(f"EDIT case={context} message_id={message_id} preview={preview(text)}", flush=True)
        fail_if_bad_text(text, context)
        if predicate(message, text):
            return message
    raise SystemExit(f"MESSAGE_TIMEOUT case={context} message_id={message_id} preview={preview(last)}")


async def wait_details_document_after(client, bot, after_id: int, context: str, timeout: int = 80) -> str:
    deadline = time.time() + timeout
    while time.time() < deadline:
        await asyncio.sleep(poll_seconds())
        async for message in bot_messages_after(client, bot, after_id):
            if not message.file:
                continue
            caption = message.raw_text or ""
            if "[Details tools]" not in caption:
                continue
            data = await message.download_media(bytes)
            body = data.decode("utf-8", errors="replace") if data else ""
            print(f"DOCUMENT case={context} message_id={message.id} bytes={len(data or b'')}", flush=True)
            return body
    raise SystemExit(f"DOCUMENT_TIMEOUT case={context}")


async def wait_bot_text_after(client, bot, after_id: int, context: str, predicate, timeout: int = 80):
    deadline = time.time() + timeout
    seen = set()
    while time.time() < deadline:
        await asyncio.sleep(poll_seconds())
        async for message in bot_messages_after(client, bot, after_id):
            text = (message.raw_text or "").strip()
            key = (message.id, text)
            if key not in seen:
                seen.add(key)
                print(f"BOT case={context} message_id={message.id} preview={preview(text)}", flush=True)
            fail_if_bad_text(text, context)
            if predicate(text):
                return message
    raise SystemExit(f"BOT_TEXT_TIMEOUT case={context}")


async def wait_bot_message_after(client, bot, after_id: int, context: str, predicate, timeout: int = 120):
    deadline = time.time() + timeout
    seen = set()
    while time.time() < deadline:
        await asyncio.sleep(poll_seconds())
        async for message in bot_messages_after(client, bot, after_id):
            text = (message.raw_text or "").strip()
            key = (message.id, text)
            if key not in seen:
                seen.add(key)
                print(f"BOT case={context} message_id={message.id} preview={preview(text)}", flush=True)
            fail_if_bad_text(text, context)
            if predicate(message, text):
                return message
    raise SystemExit(f"BOT_MESSAGE_TIMEOUT case={context}")


async def current_context_thread_id(client, bot, context: str) -> str:
    sent = await client.send_message(bot, "/context")
    message = await wait_bot_text_after(
        client,
        bot,
        sent.id,
        context,
        lambda text: "Mode: Bound thread" in text and "Thread ID:" in text,
    )
    thread_id = parse_thread_id(message.raw_text or "")
    if not thread_id:
        raise SystemExit(f"CONTEXT_THREAD_ID_MISSING context={context} preview={preview(message.raw_text or '')}")
    return thread_id


async def wait_sleep_tool_and_final(client, bot, after_id: int, marker: str, context: str):
    deadline = time.time() + 180
    seen = set()
    tool_seen = False
    final = None
    while time.time() < deadline:
        await asyncio.sleep(poll_seconds())
        async for message in bot_messages_after(client, bot, after_id):
            text = (message.raw_text or "").strip()
            key = (message.id, text)
            if key not in seen:
                seen.add(key)
                print(f"BOT case={context} message_id={message.id} preview={preview(text)}", flush=True)
            fail_if_bad_text(text, context)
            if "[Tool]" in text and "sleep 5" in text and ("Current tool:" in text or "Last completed tool:" in text):
                tool_seen = True
            if is_final_with_marker(text, marker) and has_button(message, "Details"):
                final = message
        if final is not None and tool_seen:
            break
    if not tool_seen:
        raise SystemExit(f"PLAN_RESET_SLEEP_TOOL_NOT_SEEN context={context}")
    if final is None:
        raise SystemExit(f"FINAL_TIMEOUT case={context} marker={marker}")
    final_text = final.raw_text or ""
    if "Plan Mode" in final_text:
        raise SystemExit(f"PLAN_RESET_FINAL_STILL_PLAN_MODE context={context} preview={preview(final_text)}")
    return final


async def wait_completed_plan_commentary(client, bot, after_id: int, context: str):
    return await wait_bot_message_after(
        client,
        bot,
        after_id,
        context,
        lambda message, text: "[commentary]" in text and "Status: completed" in text and "[plan]" in text,
        timeout=240,
    )


async def wait_plan_final_with_turn_off(client, bot, after_id: int, context: str):
    return await wait_bot_message_after(
        client,
        bot,
        after_id,
        context,
        lambda message, text: "[Final]" in text and has_button(message, "Turn off Plan") and has_button(message, "Details"),
        timeout=240,
    )


async def wait_message_deleted(client, bot, message_id: int, context: str, timeout: int = 40) -> None:
    deadline = time.time() + timeout
    while time.time() < deadline:
        await asyncio.sleep(1)
        message = await client.get_messages(bot, ids=message_id)
        if not message:
            print(f"DELETED case={context} message_id={message_id}", flush=True)
            return
    message = await client.get_messages(bot, ids=message_id)
    text = (message.raw_text or "").strip() if message else ""
    raise SystemExit(f"DELETE_TIMEOUT case={context} message_id={message_id} preview={preview(text)}")


def live_codex_chats_root() -> Path:
    raw = os.environ.get("CODEX_TG_E2E_CODEX_CHATS_ROOT", "").strip()
    if raw:
        return Path(raw).expanduser()
    return Path.home() / "Documents" / "Codex"


def recent_chat_dirs(root: Path, prefix: str, since_ts: float) -> list[Path]:
    date_dir = root / datetime.now().strftime("%Y-%m-%d")
    if not date_dir.exists():
        return []
    out = []
    for path in date_dir.glob(prefix + "*"):
        try:
            if path.is_dir() and path.stat().st_mtime >= since_ts - 2:
                out.append(path)
        except OSError:
            continue
    return sorted(out, key=lambda item: item.stat().st_mtime, reverse=True)


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


async def tool_only_sleep_details_case(client, bot, thread_id: str, stamp: str) -> None:
    marker = f"OK_TOOL_ONLY_SLEEP_DETAILS_{stamp}"
    prompt = (
        "Run exactly one shell command tool call: sleep 10. "
        "Do not use any other shell command. Do not answer before that command finishes. "
        f"Final answer exactly: {marker}"
    )
    since = utc_now()
    sent = await client.send_message(bot, f"/reply {thread_id} {prompt}")
    print(f"SENT case=tool_only_sleep_details message_id={sent.id} marker={marker}", flush=True)
    deadline = time.time() + 180
    seen = set()
    current_seen = False
    final = None
    while time.time() < deadline:
        await asyncio.sleep(poll_seconds())
        async for message in bot_messages_after(client, bot, sent.id):
            text = (message.raw_text or "").strip()
            key = (message.id, text)
            if key not in seen:
                seen.add(key)
                print(f"BOT case=tool_only_sleep_details message_id={message.id} preview={preview(text)}", flush=True)
            fail_if_bad_text(text, "tool_only_sleep_details")
            if "[Tool]" in text and "Current tool:" in text and "sleep 10" in text:
                current_seen = True
            if is_final_with_marker(text, marker) and has_button(message, "Details"):
                final = message
        if final is not None and current_seen:
            break
    if not current_seen:
        raise SystemExit("TOOL_ONLY_SLEEP_CURRENT_TOOL_NOT_SEEN")
    if final is None:
        raise SystemExit(f"FINAL_TIMEOUT case=tool_only_sleep_details marker={marker}")

    final = await client.get_messages(bot, ids=final.id)
    await final.click(text="Details")
    details = await wait_message_by_id(
        client,
        bot,
        final.id,
        "tool_only_sleep_details",
        lambda message, text: (
            "[Details]" in text
            and "Tool activity" in text
            and "sleep 10" in text
            and "Status: completed" in text
            and has_button(message, "Tool on")
        ),
    )
    await details.click(text="Tool on")
    tool_mode = await wait_message_by_id(
        client,
        bot,
        final.id,
        "tool_only_sleep_details",
        lambda message, text: (
            "[Details]" in text
            and "Tool activity" in text
            and "[Tool]" in text
            and "sleep 10" in text
            and has_button(message, "Tools file")
        ),
    )
    await tool_mode.click(text="Tools file")
    document_body = await wait_details_document_after(client, bot, final.id, "tool_only_sleep_details")
    if "Tool activity" not in document_body or "sleep 10" not in document_body or "Status: completed" not in document_body:
        raise SystemExit("TOOL_ONLY_SLEEP_DETAILS_DOCUMENT_MISSING_TOOL")

    fail_if_bad_logs(since, thread_id, "tool_only_sleep_details")
    print("RESULT case=tool_only_sleep_details ok", flush=True)


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


async def current_tool_priority_case(client, bot, thread_id: str, stamp: str) -> None:
    marker = f"OK_LIVE_CURRENT_PRIORITY_{stamp}"
    first_label = f"PRIORITY_HEAVY_A_{stamp}"
    second_label = f"PRIORITY_HEAVY_B_{stamp}"
    commands = [
        f"for i in 1 2 3 4 5; do echo {first_label} progress $i/5; sleep 2; done; echo {first_label} done",
        f"for i in 1 2 3 4 5; do echo {second_label} progress $i/5; sleep 2; done; echo {second_label} done",
    ]
    prompt = (
        "Run these two long-running shell command tool calls as two separate tool calls, strictly one after another, "
        "waiting for the first command to finish before starting the second. Do not combine them into a script. Commands:\n"
        + "\n".join(f"{index}. {command}" for index, command in enumerate(commands, start=1))
        + f"\nAfter the second command finishes, final answer exactly: {marker}"
    )
    since = utc_now()
    sent = await client.send_message(bot, f"/reply {thread_id} {prompt}")
    print(f"SENT case=current_tool_priority message_id={sent.id} marker={marker}", flush=True)
    deadline = time.time() + 240
    seen = set()
    active_wait_seen = False
    first_completed_seen = False
    first_output_seen = False
    second_current_seen = False
    second_completed_seen = False
    second_output_seen = False
    final_seen = False
    while time.time() < deadline:
        await asyncio.sleep(poll_seconds())
        async for message in bot_messages_after(client, bot, sent.id):
            text = (message.raw_text or "").strip()
            key = (message.id, text)
            if key not in seen:
                seen.add(key)
                print(f"BOT case=current_tool_priority message_id={message.id} preview={preview(text)}", flush=True)
            fail_if_bad_text(text, "current_tool_priority")
            if "[commentary]" in text and "Run active for:" in text:
                active_wait_seen = True
            if "[Tool]" in text:
                if "Current tool:" in text and second_label in text:
                    second_current_seen = True
                if second_current_seen and not second_completed_seen and "Last completed tool:" in text and first_label in text:
                    raise SystemExit(f"CURRENT_TOOL_PRIORITY_REVERTED preview={preview(text)}")
                if second_current_seen and not second_completed_seen and "Current tool:" in text and first_label in text:
                    raise SystemExit(f"CURRENT_TOOL_PRIORITY_STALE_CURRENT preview={preview(text)}")
                if "Last completed tool:" in text and first_label in text:
                    first_completed_seen = True
                if "Last completed tool:" in text and second_label in text:
                    second_completed_seen = True
            if "[Output]" in text and "Last completed output:" in text:
                if first_label in text:
                    first_output_seen = True
                if second_label in text:
                    second_output_seen = True
            if is_final_with_marker(text, marker):
                final_seen = True
                if "Run duration:" not in text:
                    raise SystemExit(f"CURRENT_PRIORITY_FINAL_DURATION_MISSING preview={preview(text)}")
        if final_seen and active_wait_seen and second_current_seen and second_completed_seen and second_output_seen:
            break
    if not final_seen:
        raise SystemExit(f"FINAL_TIMEOUT case=current_tool_priority marker={marker}")
    if not active_wait_seen:
        raise SystemExit("CURRENT_PRIORITY_RUN_TIMER_NOT_SEEN")
    if not second_current_seen:
        raise SystemExit("CURRENT_PRIORITY_SECOND_CURRENT_NOT_SEEN")
    if not first_output_seen:
        raise SystemExit(
            "CURRENT_PRIORITY_FIRST_OUTPUT_MISSING "
            f"first_tool_seen={first_completed_seen}"
        )
    if not second_completed_seen or not second_output_seen:
        raise SystemExit(
            "CURRENT_PRIORITY_SECOND_COMPLETED_MISSING "
            f"tool={second_completed_seen} output={second_output_seen}"
        )
    fail_if_bad_logs(since, thread_id, "current_tool_priority")
    print(
        "RESULT case=current_tool_priority ok "
        f"first={first_label} second={second_label}",
        flush=True,
    )


async def details_binding_case(client, bot, thread_id: str, stamp: str) -> None:
    old_commentary = f"DETAILS_BINDING_OLD_COMMENTARY_{stamp}"
    new_commentary = f"DETAILS_BINDING_NEW_COMMENTARY_{stamp}"
    old_tool = f"DETAILS_BINDING_OLD_TOOL_{stamp}"
    new_tool = f"DETAILS_BINDING_NEW_TOOL_{stamp}"
    old_final_marker = f"DETAILS_BINDING_OLD_FINAL_{stamp}"
    new_final_marker = f"DETAILS_BINDING_NEW_FINAL_{stamp}"
    since = utc_now()

    old_prompt = (
        f"First write a brief commentary sentence containing exactly {old_commentary}. "
        "Then run exactly one shell command tool call, verbatim: "
        f"printf '{old_tool}\\n'. "
        f"Wait for it to finish. Final answer exactly: {old_final_marker}"
    )
    sent_old = await client.send_message(bot, f"/reply {thread_id} {old_prompt}")
    print(f"SENT case=details_binding phase=old message_id={sent_old.id}", flush=True)
    old_final = await wait_final_message(client, bot, sent_old.id, old_final_marker, "details_binding")

    new_prompt = (
        f"First write a brief commentary sentence containing exactly {new_commentary}. "
        "Then run exactly one shell command tool call, verbatim: "
        f"printf '{new_tool}\\n'. "
        f"Wait for it to finish. Final answer exactly: {new_final_marker}"
    )
    sent_new = await client.send_message(bot, f"/reply {thread_id} {new_prompt}")
    print(f"SENT case=details_binding phase=new message_id={sent_new.id}", flush=True)
    new_final = await wait_final_message(client, bot, sent_new.id, new_final_marker, "details_binding")

    old_final = await client.get_messages(bot, ids=old_final.id)
    await old_final.click(text="Details")
    old_details = await wait_message_by_id(
        client,
        bot,
        old_final.id,
        "details_binding",
        lambda message, text: (
            "[Details]" in text
            and old_commentary in text
            and new_commentary not in text
            and new_final_marker not in text
            and has_button(message, "Tool on")
        ),
    )

    await old_details.click(text="Tool on")
    old_tool_details = await wait_message_by_id(
        client,
        bot,
        old_final.id,
        "details_binding",
        lambda message, text: (
            "[Details]" in text
            and "[Tool]" in text
            and old_tool in text
            and new_tool not in text
            and has_button(message, "Tools file")
        ),
    )

    await old_tool_details.click(text="Tools file")
    document_body = await wait_details_document_after(client, bot, new_final.id, "details_binding")
    if old_commentary not in document_body or old_tool not in document_body:
        raise SystemExit("DETAILS_BINDING_DOCUMENT_OLD_CONTENT_MISSING")
    if new_commentary in document_body or new_tool in document_body:
        raise SystemExit("DETAILS_BINDING_DOCUMENT_LEAKED_NEW_RUN")

    old_tool_details = await client.get_messages(bot, ids=old_final.id)
    await old_tool_details.click(text="Back")
    old_back = await wait_message_by_id(
        client,
        bot,
        old_final.id,
        "details_binding",
        lambda message, text: (
            "[Final]" in text
            and old_final_marker in text
            and new_final_marker not in text
            and has_button(message, "Details")
        ),
    )
    if new_final_marker in (old_back.raw_text or ""):
        raise SystemExit("DETAILS_BINDING_BACK_RENDERED_NEW_RUN")

    new_after = await client.get_messages(bot, ids=new_final.id)
    new_text = (new_after.raw_text or "").strip()
    if "[Final]" not in new_text or new_final_marker not in new_text or old_final_marker in new_text:
        raise SystemExit(f"DETAILS_BINDING_NEW_FINAL_CHANGED preview={preview(new_text)}")

    fail_if_bad_logs(since, thread_id, "details_binding")
    print("RESULT case=details_binding ok", flush=True)


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


async def newchat_folder_case(client, bot, thread_id: str, stamp: str) -> None:
    del thread_id
    root = live_codex_chats_root()
    newchat_marker = f"OK_NEWCHAT_FOLDER_{stamp}"
    newthread_marker = f"OK_NEWTHREAD_NOCWD_{stamp}"
    before_newchat = time.time()
    newchat_prompt = (
        "/newchat Проверь tool call по погоде. "
        "This is a folder-routing smoke test; do not call external tools. "
        f"Final answer must contain exactly {newchat_marker}."
    )
    sent_newchat = await client.send_message(bot, newchat_prompt)
    print(f"SENT case=newchat_folder phase=newchat message_id={sent_newchat.id}", flush=True)
    final_newchat = await wait_final_message(client, bot, sent_newchat.id, newchat_marker, "newchat_folder")
    dirs = recent_chat_dirs(root, "tool-call", before_newchat)
    if not dirs:
        raise SystemExit(f"NEWCHAT_FOLDER_MISSING root={root}")
    chat_dir = dirs[0]
    context_sent = await client.send_message(bot, "/context")
    await wait_bot_text_after(
        client,
        bot,
        context_sent.id,
        "newchat_folder",
        lambda text: "Mode: Bound thread" in text and str(chat_dir) in text,
    )

    before_newthread = time.time()
    newthread_prompt = (
        f"/newthread newthread nocwd {stamp}. "
        f"Final answer must contain exactly {newthread_marker}."
    )
    sent_newthread = await client.send_message(bot, newthread_prompt)
    print(f"SENT case=newchat_folder phase=newthread message_id={sent_newthread.id}", flush=True)
    await wait_final_message(client, bot, sent_newthread.id, newthread_marker, "newchat_folder")
    unexpected_dirs = recent_chat_dirs(root, "newthread-nocwd", before_newthread)
    if unexpected_dirs:
        raise SystemExit(f"NEWTHREAD_CREATED_CHAT_FOLDER dirs={[str(path) for path in unexpected_dirs]}")
    context_sent = await client.send_message(bot, "/context")
    await wait_bot_text_after(
        client,
        bot,
        context_sent.id,
        "newchat_folder",
        lambda text: "Mode: Bound thread" in text and str(root) not in text,
    )
    fail_if_bad_text(final_newchat.raw_text or "", "newchat_folder")
    print(f"RESULT case=newchat_folder ok chat_dir={chat_dir}", flush=True)


async def notification_contract_case(client, bot, thread_id: str, stamp: str) -> None:
    final_marker = f"NOTIFICATION_FINAL_{stamp}"
    since = utc_now()
    prompt = (
        "This is a notification contract smoke test. "
        "Do not use tools and do not write a plan. "
        f"Final answer exactly: {final_marker}"
    )
    sent = await client.send_message(bot, f"/reply {thread_id} {prompt}")
    print(f"SENT case=notification_contract phase=run message_id={sent.id}", flush=True)

    deadline = time.time() + 180
    seen = set()
    new_run_id = None
    commentary_id = None
    final = None
    while time.time() < deadline and final is None:
        await asyncio.sleep(poll_seconds())
        async for message in bot_messages_after(client, bot, sent.id):
            text = (message.raw_text or "").strip()
            key = (message.id, text)
            if key not in seen:
                seen.add(key)
                print(f"BOT case=notification_contract phase=run message_id={message.id} preview={preview(text)}", flush=True)
            fail_if_bad_text(text, "notification_contract")
            if "New run:" in text:
                new_run_id = message.id
            if "[commentary]" in text:
                commentary_id = message.id
            if is_final_with_marker(text, final_marker) and has_button(message, "Details"):
                final = message
                break
    if final is None:
        raise SystemExit(f"FINAL_TIMEOUT case=notification_contract marker={final_marker}")
    if new_run_id is None:
        raise SystemExit("NOTIFICATION_CONTRACT_NEW_RUN_MISSING")
    if commentary_id is None:
        raise SystemExit("NOTIFICATION_CONTRACT_COMMENTARY_MISSING")
    if final.id == commentary_id:
        raise SystemExit("NOTIFICATION_CONTRACT_FINAL_REUSED_COMMENTARY_MESSAGE")
    await wait_message_deleted(client, bot, commentary_id, "notification_contract")

    final = await client.get_messages(bot, ids=final.id)
    await final.click(text="Details")
    details = await wait_message_by_id(
        client,
        bot,
        final.id,
        "notification_contract",
        lambda message, text: "[Details]" in text and has_button(message, "Back"),
    )
    await details.click(text="Back")
    await wait_message_by_id(
        client,
        bot,
        final.id,
        "notification_contract",
        lambda message, text: "[Final]" in text and final_marker in text and has_button(message, "Details"),
    )
    fail_if_bad_logs(since, thread_id, "notification_contract")

    plan_marker = f"NOTIFICATION_PLAN_FINAL_{stamp}"
    plan_prompt = (
        "/plan "
        f"{thread_id} "
        "Immediately ask me one structured multiple-choice question using the available user-input tool. "
        "Use question text exactly: Pick one notification branch. "
        "Use exactly two options labeled Alpha and Beta. "
        "Do not put those choice buttons under commentary. "
        f"After I pick Alpha, final answer exactly: {plan_marker}"
    )
    sent_plan = await client.send_message(bot, plan_prompt)
    print(f"SENT case=notification_contract phase=plan message_id={sent_plan.id}", flush=True)
    plan_card = await wait_bot_text_after(
        client,
        bot,
        sent_plan.id,
        "notification_contract",
        lambda text: "[Plan]" in text.split("\n", 1)[0],
        timeout=160,
    )
    if has_button(plan_card, "Alpha") and has_button(plan_card, "Beta"):
        await plan_card.click(text="Alpha")
        await wait_final_message(client, bot, sent_plan.id, plan_marker, "notification_contract", timeout=180)
        fail_if_bad_logs(since, thread_id, "notification_contract")
    else:
        print(
            "PLAN_FALLBACK case=notification_contract reason=no_structured_buttons",
            flush=True,
        )
        await client.send_message(bot, f"/stop {thread_id}")
    print("RESULT case=notification_contract ok", flush=True)


async def plan_mode_reset_case(client, bot, thread_id: str, stamp: str) -> None:
    del thread_id
    since = utc_now()
    bootstrap_marker = f"PLAN_RESET_BOOTSTRAP_{stamp}"
    click_marker = f"PLAN_RESET_CLICK_SLEEP_{stamp}"
    stop_sleep_marker = f"PLAN_RESET_STOP_SLEEP_{stamp}"

    bootstrap_prompt = (
        f"/newthread disposable plan reset {stamp}. "
        "Do not use tools. "
        f"Final answer exactly: {bootstrap_marker}"
    )
    sent_bootstrap = await client.send_message(bot, bootstrap_prompt)
    print(f"SENT case=plan_mode_reset phase=bootstrap message_id={sent_bootstrap.id}", flush=True)
    await wait_final_message(client, bot, sent_bootstrap.id, bootstrap_marker, "plan_mode_reset")
    dedicated_thread_id = await current_context_thread_id(client, bot, "plan_mode_reset")

    plan_prompt = (
        f"/plan {dedicated_thread_id} "
        "Составь короткий план выполнения команды sleep 5. Не выполняй команду."
    )
    sent_plan = await client.send_message(bot, plan_prompt)
    print(f"SENT case=plan_mode_reset phase=turn_off_plan_plan message_id={sent_plan.id}", flush=True)
    await wait_completed_plan_commentary(client, bot, sent_plan.id, "plan_mode_reset")

    stuck_prompt = "Выполни sleep 5. Это проверка выхода из Plan Mode."
    sent_stuck = await client.send_message(bot, f"/reply {dedicated_thread_id} {stuck_prompt}")
    print(f"SENT case=plan_mode_reset phase=turn_off_plan_stuck message_id={sent_stuck.id}", flush=True)
    plan_final = await wait_plan_final_with_turn_off(client, bot, sent_stuck.id, "plan_mode_reset")
    await plan_final.click(text="Turn off Plan")
    await wait_message_by_id(
        client,
        bot,
        plan_final.id,
        "plan_mode_reset",
        lambda message, text: "[Final]" in text and not has_button(message, "Turn off Plan"),
    )

    click_sleep_prompt = (
        "Run exactly one shell command tool call: sleep 5. "
        "Do not use any other shell command. Do not answer before that command finishes. "
        f"Final answer exactly: {click_marker}"
    )
    sent_click_sleep = await client.send_message(bot, f"/reply {dedicated_thread_id} {click_sleep_prompt}")
    print(f"SENT case=plan_mode_reset phase=turn_off_plan_sleep message_id={sent_click_sleep.id}", flush=True)
    await wait_sleep_tool_and_final(client, bot, sent_click_sleep.id, click_marker, "plan_mode_reset")

    stop_plan_prompt = (
        f"/plan {dedicated_thread_id} "
        "Составь короткий план выполнения команды sleep 5. Не выполняй команду."
    )
    sent_stop_plan = await client.send_message(bot, stop_plan_prompt)
    print(f"SENT case=plan_mode_reset phase=stop_plan message_id={sent_stop_plan.id}", flush=True)
    await wait_completed_plan_commentary(client, bot, sent_stop_plan.id, "plan_mode_reset")

    sent_stop = await client.send_message(bot, f"/stop {dedicated_thread_id}")
    print(f"SENT case=plan_mode_reset phase=stop message_id={sent_stop.id}", flush=True)
    await wait_bot_text_after(
        client,
        bot,
        sent_stop.id,
        "plan_mode_reset",
        lambda text: "Thread is already idle" in text or "Interrupt requested" in text,
    )

    stop_sleep_prompt = (
        "Run exactly one shell command tool call: sleep 5. "
        "Do not use any other shell command. Do not answer before that command finishes. "
        f"Final answer exactly: {stop_sleep_marker}"
    )
    sent_stop_sleep = await client.send_message(bot, f"/reply {dedicated_thread_id} {stop_sleep_prompt}")
    print(f"SENT case=plan_mode_reset phase=stop_sleep message_id={sent_stop_sleep.id}", flush=True)
    await wait_sleep_tool_and_final(client, bot, sent_stop_sleep.id, stop_sleep_marker, "plan_mode_reset")

    fail_if_bad_logs(since, dedicated_thread_id, "plan_mode_reset")
    print("RESULT case=plan_mode_reset ok", flush=True)


async def main() -> None:
    cases = require_env()
    thread_id = os.environ.get("CODEX_TG_E2E_THREAD_ID", "").strip()
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
        print(f"LIVE_E2E start cases={','.join(cases)}", flush=True)
        for case in cases:
            if case == "sequential_commands":
                await sequential_commands_case(client, bot, thread_id, stamp)
            elif case == "sleep20_timing":
                await sleep20_timing_case(client, bot, thread_id, stamp)
            elif case == "tool_only_sleep_details":
                await tool_only_sleep_details_case(client, bot, thread_id, stamp)
            elif case == "multi_tool_current":
                await multi_tool_current_case(client, bot, thread_id, stamp)
            elif case == "current_tool_priority":
                await current_tool_priority_case(client, bot, thread_id, stamp)
            elif case == "details_binding":
                await details_binding_case(client, bot, thread_id, stamp)
            elif case == "complex_math":
                await complex_math_case(client, bot, thread_id, stamp)
            elif case == "newchat_folder":
                await newchat_folder_case(client, bot, thread_id, stamp)
            elif case == "notification_contract":
                await notification_contract_case(client, bot, thread_id, stamp)
            elif case == "plan_mode_reset":
                await plan_mode_reset_case(client, bot, thread_id, stamp)
        print("ALL_OK", flush=True)
    finally:
        await client.disconnect()


if __name__ == "__main__":
    try:
        asyncio.run(main())
    except KeyboardInterrupt:
        sys.exit(130)
