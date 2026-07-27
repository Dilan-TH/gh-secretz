# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`gh-secretz` is a `gh` CLI extension (Go) for bulk-reviewing GitHub secret
scanning dismissal requests and triaging open alerts that have none, from a
terminal TUI (Bubble Tea) instead of the GitHub web UI one request at a time.

Read the README before making behavioural changes — it documents the product
rationale, the keybindings, and several GitHub API quirks (see below) that are
easy to get wrong. The full design rationale lives in
`docs/superpowers/specs/2026-07-24-gh-secretz-design.md` and
`docs/internal/2026-07-24-gh-secretz-design.INTERNAL.md`.

## Commands

```
make build     # go build -ldflags with version stamped from git describe
make test      # go test ./... -race
make lint      # go vet ./... && gofmt -l .
make install   # build, then `gh extension install .`
```

Run a single package's tests: `go test ./internal/executor/... -race`
Run a single test: `go test ./internal/executor/... -run TestName -race -v`

CI (`.github/workflows/ci.yml`) runs `go vet`, a `gofmt -l .` check, and
`go test ./... -race` on every push/PR to `main`. Match that locally before
declaring work done — `make lint && make test`.

## Architecture

Strict one-way dependency chain, each package owning one concern. Nothing
depends on `internal/ui` or `internal/cli`; everything else is a pure library
tested without a terminal or network:

```
transport → queue, alerts → enrich → filter/selection → executor
                                          ↓
                                         ui ← cli ← main
```

- **`internal/transport`** — the only package that talks HTTP. Wraps go-gh's
  REST client behind a `Transport` interface (`GetJSON`, `GetAllPages`,
  `Patch`), owns pagination via `Link: rel="next"`, and classifies non-2xx
  responses into `*HTTPError` (`IsNotFound`, `IsForbidden`). Every other
  package is tested against `transport.Fake` (`internal/transport/fake.go`),
  never the network.
- **`internal/model`** — shared types only (`Request`, `Alert`, `Snippet`,
  `Row`). No behavior beyond small derived predicates, no dependencies outside
  stdlib. `Request.Number` (dismissal request) and `Request.AlertNumber`
  (alert) are different values on the same record — every write path uses
  `AlertNumber`, never `Number`.
- **`internal/queue`** — fetches and normalizes dismissal requests. Refuses to
  emit a record whose alert number is ambiguous, since that's the field every
  write depends on.
- **`internal/alerts`** — enumerates secret scanning alerts. Queries the
  default listing *and* an explicit generic-types listing and unions them,
  because the default listing silently omits generic detection types
  (`password`, `http_bearer_authentication_header`, etc.) — see
  `GenericSecretTypes`.
- **`internal/discover`** — sweeps an org's repos for open alerts and caches
  the result to disk, needed because the org-wide alert endpoint 404s for
  custom-role reviewers (org owner / security manager only).
- **`internal/enrich`** — joins alerts onto requests, deriving warnings
  (`WarnStaleClaim`, `WarnNoAlert`, `WarnPubliclyLeaked`) that surface a bad
  approval before it happens (e.g. requester claims "revoked" but the alert is
  still open and valid).
- **`internal/filter`** — composes row predicates (`Spec`: repo, requester,
  reason, secret type, warned-only) and enforces that write commands
  (`review`, `triage`) always carry an explicit scope; `ErrNoFilter` otherwise.
  `--all` is a distinct, deliberate scope, not the default.
- **`internal/selection`** — the TUI's cursor/checked-set as a pure state
  machine, independent of Bubble Tea, so key handling is unit tested.
- **`internal/executor`** — performs every state change (`ActionApprove`,
  `ActionDeny`, `ActionClose`). Central rule: a write is never reported
  successful because the HTTP call returned 200 — delegated dismissal can
  silently convert a `close` into a new dismissal request, which also returns
  200. Every write is followed by a verification read, and the outcome
  (`OutcomeDone`, `OutcomeRequestCreated`, ...) comes from that observed state.
  Also appends every write to the audit log at `~/.gh-secretz/audit.jsonl`;
  an audit-write failure must be surfaced in the result, never swallowed.
- **`internal/ui`** — Bubble Tea model/view. Produces a `Decision` (action +
  resolution + comment + rows) on exit; quitting without choosing must be
  indistinguishable from doing nothing (empty `Action`).
- **`internal/cli`** — wires flags to the domain layers via an injectable
  `Env` (transport, config, dir, actor, stdout/stderr), holding no domain
  logic beyond argument handling and output formatting. One file per
  subcommand (`list.go`, `review.go`, `show.go`, `discover.go`, `triage.go`,
  `close.go`).
- **`internal/config`** — loads optional TOML from `~/.gh-secretz/config.toml`
  (`org`, `repo_prefixes`). Deliberately no built-in defaults: an empty
  `repo_prefixes` matches nothing, not everything, since a wildcard sweep of a
  large org is thousands of API calls.

## GitHub API behaviors that drive this design

These are empirically confirmed and are the reason several packages above
exist; don't "simplify" them away without re-reading
`docs/superpowers/specs/2026-07-24-gh-secretz-design.md`:

- The status filter on dismissal requests is `request_status`, not `status`
  — a `status` param is silently ignored, returning unfiltered results.
- `time_period` defaults to `day` and caps at `month`; requests older than a
  month cannot be listed at all.
- A dismissal request's request number and alert number differ; the review
  `PATCH` path takes the *alert* number.
- The default alert list silently omits generic detection types — must name
  secret types explicitly (`internal/alerts.GenericSecretTypes`).
- There is no assignee concept; `reviewer` filters on who already responded,
  not who is expected to.
- `GET /orgs/{org}/secret-scanning/alerts` requires org owner or security
  manager; a 404 there often means insufficient permission, not "not found".

## Testing conventions

Every package above `transport` is tested against `transport.Fake`, which
matches paths exactly (including query strings) and records `Gets`/`Patches`
in call order for assertion — register the full path when adding fixtures.
`selection` and `ui` are tested without a terminal since their models are
pure state machines. When adding a subcommand, wire it through `internal/cli`
with an injectable `Env` so tests can drive it with a fake transport and
buffers, matching the existing `cli_test.go` pattern.
