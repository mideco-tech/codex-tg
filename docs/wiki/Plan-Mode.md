# Plan Mode

Plan Mode waiting input is rendered as a separate `[Plan]` card.

## Rules

- The card is routeable to the exact thread and turn.
- Replying to the card sends the answer back to the same Codex run.
- Buttons appear only when Codex provides structured choices.
- Synthetic polling prompts use `turn/steer` first and fall back to `turn/start` only when the active turn is gone.

## Demo

See [Demo](Demo.md) for an English screenshot scenario.

