# Progress bars for API-call waits

## Context

Every command that loops over multiple repos or rows making blocking GitHub
API calls gives the operator no feedback while it runs. `discover` is the
partial exception: it prints a plain, carriage-return-overwritten counter
("probing 12/40, 3 with alerts"). `list`, `review`, and `triage` load alerts
and requests per repo with nothing printed between repos, and the bulk
write+verify loop in `executor.Run` (used by `review`'s approve/deny,
`triage`'s close, and `gh secretz close`) reports nothing until the entire
batch finishes. On a queue or org sweep of any size this reads as a hang.

## Approach

Add `github.com/schollz/progressbar/v3` and a single shared helper, reused
everywhere a loop of known length blocks on API calls.

## Scope

- `discover` (migrate its existing counter)
- `list` / `review`'s shared `loadRows` loading loop
- `triage`'s per-repo loading loop
- `executor.Run`'s write+verify loop (used by `review`, `triage`, not
  `close`, which acts on a single row)

## Design

### `internal/cli/progress.go` (new)

```go
func newProgressBar(w io.Writer, total int, description string) *progressbar.ProgressBar
func isTerminalWriter(w io.Writer) bool
```

`newProgressBar` sets `OptionSetVisibility(isTerminalWriter(w))`, so it
renders nothing at all when `w` is not a real terminal — piped stderr, a
redirected file, or a test's `bytes.Buffer`. This matches the project's
existing convention of piped output staying undecorated (`ui.Format`'s
`width <= 0` handling), and means no existing stderr-based test assertion
needs to change. `isTerminalWriter` checks `w.(*os.File)` then
`golang.org/x/term.IsTerminal`, the same check `main.go` already uses to set
`Env.Interactive`.

### `internal/cli/discover.go`

Replace the inline `fmt.Fprintf(env.Stderr, "\rprobing %d/%d...")` callback
with one that lazily creates a bar on first invocation (total is only known
inside the callback `Sweep.Run` already passes) and calls `bar.Set(done)` /
`bar.Describe(...)` each time. No change to `internal/discover` itself.

### `internal/cli/list.go` (`loadRows`, shared by `list` and `review`)

The repo set is built before the per-repo `alerts.List` loop, so total is
known upfront. Wrap the loop with `newProgressBar(env.Stderr, len(repos),
"fetching alerts")`, advancing by one per repo, `Finish()` after the loop
(including on early error return, via `defer`).

### `internal/cli/triage.go`

Same shape: `newProgressBar(env.Stderr, len(targets), "processing repos")`
around the per-target loop (`alerts.List` + `queue.List` per repo).

### `internal/executor/executor.go`

Add a `Progress func(done, total int)` field to `Executor`, matching
`discover.Sweep`'s existing progress-callback pattern (nil-safe, optional).
`Run` calls it once per row after `e.one(...)` completes, before appending to
`results`. This keeps `executor` a pure library with no rendering
dependency — `cli` owns bar construction and passes the callback in.

### `internal/cli/review.go` / `internal/cli/triage.go`

Construct a bar sized to `len(dec.Rows)` and wire
`executor.Executor{..., Progress: func(done, total int) { bar.Set(done) }}`,
`Finish()` after `ex.Run` returns. `close.go` is left unchanged; a bar over a
single row has nothing meaningful to show.

## Testing

- `internal/cli`: unit tests for `isTerminalWriter` (a `bytes.Buffer` and a
  regular temp file both report `false`; the true/terminal branch is a thin
  wrapper over `term.IsTerminal` and isn't independently retested, matching
  how `main.go`'s existing use of the same check isn't tested either).
- `internal/executor`: a test asserting `Progress` fires once per row with
  the correct `(done, total)` sequence, using a plain recording function,
  not the real progress-bar library.
- Existing `internal/cli` tests continue to pass unchanged: bars are
  invisible against the `bytes.Buffer` stderr those tests use, so no
  existing stdout/stderr assertion is affected.
- `make lint && make test` must pass, matching CI.

## Verification

Manually run `gh secretz discover`, `gh secretz list`, and `gh secretz
review --all` (or `triage --all`) against a real terminal with a config
pointed at a repo set large enough to see multiple bar updates, confirming
the bar renders, advances, and clears on completion, and that piping any of
these commands' stderr to a file produces no bar noise in the file.
