# Fresh-Context Review Checklist

Use a new session or reviewer context for meaningful changes.

## Inputs

- Issue, feature brief, or bug report
- Diff
- Tests changed or added
- Relevant ADRs and `AGENTS.md`
- Validation output and live Telegram evidence when applicable

## Review Order

1. Check whether the tests prove the requested behavior.
2. Check whether the diff solves only the requested scope.
3. Check module boundaries and duplicated rules.
4. Check hidden behavior changes, migrations, and compatibility risks.
5. Check private data, secrets, local paths, logs, and screenshots.
6. Check validation: targeted tests, full checks, CI, and live E2E when required.

## Output

- Blocking issues
- Non-blocking suggestions
- Weak or missing tests
- Merge recommendation
