# Testing Blockers

These are current blockers for full test coverage of the Go runtime and the documented Telegram observer/UI v2 target without changing production code under `internal/` or `cmd/`.

## 1. Runtime/docs mismatch for observer/UI v2 remains intentional today

The docs now describe the target observer/UI v2 contract:

- global observer default-on
- `/observe all` moves one global target
- `/observe off` disables monitoring
- actionable summary panel per `(chat, project, thread)`
- passive tool/output messages
- final answer with `Получить полный лог`

The current runtime may still expose parts of the older additive observer-feed model or simpler message rendering.

Impact:

- doc acceptance and runtime acceptance are not yet identical
- operator validation must separate "runtime currently does X" from "target contract says it should do Y"

## 2. `internal/appserver` compile/runtime assumptions may still drift during the migration

Current build blockers observed during `go test ./...`:

- `previous.LastSeenThreadStatus` is referenced but `model.ThreadSnapshotState` does not expose that field
- `time` is used in `internal/appserver/normalize.go` without an import
- `model.MustJSON` is referenced, but the helper currently lives in `internal/storage`

Impact:

- default full-module `go test ./...` is not a valid signal yet
- direct tests for exported normalization helpers are checked in behind the build tag `appserver_contract`

## 3. Routing precedence is implemented in an unexported method

Product contract:

1. explicit thread id
2. reply-to message route
3. bound thread

Current code location:

- `internal/daemon/service.go`
- method: `resolveRoute`

Why this blocks direct unit coverage from the current write scope:

- the method is unexported
- package-level tests would need to live under `internal/daemon`
- this hardening task was restricted to `tests/`, `README.md`, `docs/`, and `AGENTS.md`

Result:

- routing precedence is codified in the contract docs and acceptance docs
- direct executable unit coverage for `resolveRoute` remains blocked until either:
  - package-level tests are allowed under `internal/daemon`, or
  - route resolution is exposed via a small public helper

## 4. Full storage-backed test coverage needs the module lockfile to be owned intentionally

The repo uses `modernc.org/sqlite` in `internal/storage`.

That means broad runnable test coverage normally expects a committed `go.sum`. This hardening pass does not change production code, so the default runnable tests in `./tests` stay focused on packages that do not require `internal/storage` or the broken `internal/appserver` package.

## What is covered right now

- model helper behavior that is part of the oracle contract
- config/env behavior used by the Go runtime
- deferred `appserver` contract tests stored under a build tag for later activation
- docs and coordination now describe the v2 target contract even where code parity is still pending
