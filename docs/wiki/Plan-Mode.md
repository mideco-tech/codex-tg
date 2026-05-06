# Plan Mode

Plan Mode waiting input is rendered as a separate `[Plan]` card.

Start Plan Mode from Telegram with `/plan <thread> <text>` or `/reply --plan <thread> <text>`. When replying to a routed thread card, `/plan <text>` and `/plan_mode <text>` use that reply route.

Leave Plan Mode by starting the next turn explicitly in Default Mode with `/default <thread> <text>`, `/default <text>` on the current route, or `/reply --default <thread> <text>`.

## Rules

- Telegram-started Plan turns pass Codex App Server `collaborationMode: plan` instead of relying on prompt text.
- Telegram-started Default turns can pass Codex App Server `collaborationMode: default` to reset a thread that remains in Plan Mode.
- `/model` and `/effort` expose button menus for the model settings used by Telegram-started collaboration-mode turns.
- The card is routeable to the exact thread and turn.
- Replying to the card sends the answer back to the same Codex run.
- Buttons appear only when Codex provides structured choices.
- Synthetic polling prompts use `turn/steer` first and fall back to `turn/start` only when the active turn is gone.

## Demo

See [Demo](Demo.md) for an English screenshot scenario.
