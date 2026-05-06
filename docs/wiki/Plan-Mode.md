# Plan Mode

Plan Mode waiting input is rendered as a separate `[Plan]` card.

Start Plan Mode from Telegram with `/plan <thread> <text>` or `/reply --plan <thread> <text>`. When replying to a routed thread card, `/plan <text>` and `/plan_mode <text>` use that reply route.

Leave Plan Mode by pressing `Turn off Plan` on a Plan Final Card, or by using
`/stop <thread>`. Both actions set a one-shot reset: the next ordinary turn for
that thread starts with App Server Default Mode and then the reset is cleared.

## Rules

- Telegram-started Plan turns pass Codex App Server `collaborationMode: plan` instead of relying on prompt text.
- `Turn off Plan` and `/stop` do not start a turn; they only arm the next ordinary `turn/start` with `collaborationMode: default`.
- The one-shot Default reset is cleared after a successful ordinary `turn/start`; if that start fails, the reset remains.
- `/model` and `/effort` expose button menus for the model settings used by Telegram-started collaboration-mode turns.
- The card is routeable to the exact thread and turn.
- Replying to the card sends the answer back to the same Codex run.
- Buttons appear only when Codex provides structured choices.
- Synthetic polling prompts use `turn/steer` first and fall back to `turn/start` only when the active turn is gone.

## Demo

See [Demo](Demo.md) for an English screenshot scenario.
