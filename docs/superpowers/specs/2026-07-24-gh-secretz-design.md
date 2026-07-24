# gh-secretz Design

Date: 2026-07-24
Status: approved for planning

## Purpose

A terminal tool for reviewing GitHub secret scanning work in an organization
without the web UI. Two jobs:

1. **Approve or deny pending dismissal requests** that the operator is entitled
   to review, in bulk.
2. **Triage open alerts that have no dismissal request**, closing them directly
   before a request is ever filed.

## Verified API behaviour

Every item here was confirmed empirically against a live organization on
2026-07-24 using `gh api`. These findings drive the design, and several are
counterintuitive enough that the implementation must not deviate from them.

Repository names, alert numbers, and user handles from that investigation are
deliberately omitted here. Observed counts are given only as ratios where the
ratio is the point.

### Dismissal requests

* List endpoint: `GET /orgs/{org}/dismissal-requests/secret-scanning`.
* The status filter is `request_status`, **not** `status`. A `status` parameter
  is silently ignored and returns unfiltered results. Valid values: `completed`,
  `cancelled`, `approved`, `expired`, `denied`, `open`, `all`. Default `all`.
* `time_period` defaults to `day` and caps at `month`. **Requests older than a
  month cannot be listed at all.** The tool always sends `time_period=month`.
* `request_status=approved` also returns `expired` records, so status filtering
  is repeated client side rather than trusted.
* `reviewer` filters on who already responded, not on who is expected to
  respond. It is not an assignment filter.
* `requester` works as expected.
* Review endpoint: `PATCH /repos/{owner}/{repo}/dismissal-requests/secret-scanning/{alert_number}`
  with body `{"status": "approve"|"deny", "message": string}`. `message` is
  required, max 2048 characters.

### The alert number versus request number trap

A dismissal request carries both a request `number` and an alert number, and
they differ. A request whose `number` is 5 may refer to `alert_number` 18. The
PATCH path segment is the **alert** number. Confirmed in both directions:
fetching `.../dismissal-requests/secret-scanning/18` returns the record whose
`number` is 5.

Consequences for the implementation:

* Records are keyed on `(repo, alert_number)`.
* On ingest, assert `resource_identifier == data[0].alert_number`.
* If `data` holds more than one entry, or the assertion fails, **skip the record
  with a warning**. Do not guess. Acting on the wrong number dismisses an
  unrelated live secret.

### There is no assignee

No reviewer or assignee field exists on a dismissal request. Reviewers are org
owners, security managers, or holders of a custom role granting *"Review and
manage secret scanning alert dismissal requests"*. Custom role holders see only
requests for repos where they can access secret scanning alerts.

An operator who is a plain org `member` can still list the org queue if they hold
that custom role. Therefore `GET /orgs/{org}/dismissal-requests/secret-scanning`
already returns exactly their reviewable queue. **That endpoint is the definition
of "assigned to me."**

The alert object exposes an `assigned_to` field, but it was null across every
alert inspected, is absent from the changelog, and is not filterable. It is not a
usable assignment mechanism.

### Alert enumeration is incomplete by default

The single most dangerous behaviour found. The default alert list silently omits
generic detection types, notably `password` and
`http_bearer_authentication_header`. Measured across three repositories, default
enumeration returned:

| Repo | Default list | Explicit `secret_type` union |
|---|---|---|
| A | 4 | 18 |
| B | 2 | 37 |
| C | 0 | 5 |

In repo C the default list found **nothing at all** while five open alerts
existed. `exclude_secret_types` with an unmatched value does not unlock them.
Only naming types explicitly does.

So alerts are enumerated as the **union** of the default list and an explicit
comma separated `secret_type` query over the generic family, deduplicated by
alert number.

The union depends on a maintained slug list, which is the design's one
irreducible completeness risk: a generic type GitHub adds and this list omits
stays invisible. Mitigations:

* The slug list is overridable with `--secret-types`.
* Every run prints `enumerated N alerts across M secret types`, making the
  assumption visible rather than implied.

Requests report secret types as display names (`"Password"`) while alerts use
slugs (`"password"`). A display-name to slug mapping is required; the alert
object carries both `secret_type` and `secret_type_display_name`.

### No org-wide alert endpoint for a custom role

`GET /orgs/{org}/secret-scanning/alerts` returns 404 for a custom-role reviewer.
Confirmed not to be a scope problem: the org and repo endpoints advertise
identical `X-Accepted-Oauth-Scopes` (`public_repo, repo, security_events`), the
token held `repo`, and the repo endpoint returned 200. Two different tokens both
404, and the enterprise equivalent 404s too. The endpoint is gated on org owner
or security manager.

The web UI at `/orgs/{org}/security/alerts/secret-scanning` does show this data,
via internal endpoints with different gating. **The page working is not evidence
the API will.** Repo scope must therefore come from elsewhere.

### Detecting an unrequested alert

An alert carries `closure_request_comment`, `closure_request_reviewer`, and
`closure_request_reviewer_comment`, populated exactly when a closure request
exists. `closure_request_comment == null` identifies an alert with no dismissal
request.

This is preferred over a set difference against the request list, because the
request list is capped at one month. A request filed five weeks ago would make
the set difference wrongly classify its alert as unrequested and invite closing
something already in flight. The alert field has no time window.

The set difference still runs as a secondary cross-check, and disagreement
between the two signals is surfaced, not silently resolved.

Validated against a repository whose expired requests referenced an alert set
entirely disjoint from its open unrequested alerts.

### Expired requests are the highest-value finding

A dismissal request expires 7 days after creation. Expiry leaves the alert open
and does **not** set the closure marker, so triage naturally resurfaces it. One
repository examined had a full batch of requests expire untouched. Recovering
silently dropped review work is likely this tool's most useful behaviour.

### Direct closing works

`PATCH /repos/{owner}/{repo}/secret-scanning/alerts/{alert_number}` with
`{"state": "resolved", "resolution": ..., "resolution_comment": ...}`.
Resolutions: `revoked`, `false_positive`, `used_in_tests`, `wont_fix`.

GitHub documents that reviewers "can dismiss alerts directly themselves."
Corroborated by observation: a batch of generic `password` alerts was found
`resolved` with `resolution: revoked`, closed directly by a reviewer, despite
their dismissal requests having expired.

That is evidence the mechanism exists, not proof that any given role bypasses the
delegated flow. The failure mode is quiet: the PATCH may create a dismissal
request instead of closing. Therefore **the executor always re-GETs each alert
after writing and reports outcome from observed state, never from a 200
response.** A write reported as `closed` means `state == resolved` was read back.

### Repo discovery

`gh repo list <org> --limit 3000 --json ...` returned roughly 2,100 repos in 18s
for the organization tested, about 85% of them unarchived. Filtering to the
relevant repo-name prefixes cut that to a few hundred.

Per-repo probe latency was ~0.38s via `gh api` subprocesses, and most repos 404
quickly because scanning is disabled or inaccessible. Several hundred sequential
probes is minutes, so the sweep runs **concurrently with an 8 worker pool**. Well
inside the 5,000/hour core limit.

## Scope decisions

* Filters are mandatory for write commands. No org-wide blanket approve.
* Repo scope for triage is a cached sweep over configured repo-name prefixes,
  set in `~/.gh-secretz/config.toml` under `repo_prefixes`, so widening or
  narrowing the sweep needs no code change. There are no built-in defaults; the
  tool is not specific to any organization.
* Alerts are enriched during listing so stale claims are visible before acting.
* Bulk close relies on post-write verification rather than a staged manual test.

## Out of scope

Code scanning dismissals, push protection bypass requests, enterprise level
listing, and any write beyond approve, deny, and close.

## Language and distribution

Go, shipped as a precompiled `gh` CLI extension. The requirement that drove this
is shareability: the tool must compile to a single binary a colleague can run.

Python was the original choice and was rejected on evidence. Stock macOS
`/usr/bin/python3` is **3.9.6**, while a stdlib-only design of this shape needs
3.11 for `tomllib`. A shared script would therefore work on a developer's
homebrew 3.11 and fail on a default Mac, which is the worst available failure
mode for a tool meant to be handed around. The escape hatches do not help:
PyInstaller needs a per-platform build anyway, produces roughly 15 MB, and yields
binaries that trip Gatekeeper when downloaded; `zipapp` produces one file but
still requires 3.11+ on the target.

A Go prototype was built and run against a live org to validate the approach
before committing to it:

* `api.DefaultRESTClient()` from `github.com/cli/go-gh/v2` picked up the existing
  `gh` auth with **zero configuration** and successfully listed dismissal
  requests.
* Stripped binary: **6.4 MB** for `darwin/arm64`, ad-hoc code signed.
* Cross-compiles in seconds: `darwin/amd64` 6.9 MB, `linux/amd64` 6.8 MB.

This preserves the most valuable property of the original design. `go-gh` reads
gh's own auth, so **the tool holds no credential of its own.**

Distribution is via the extension mechanism. Extensions must live in a repo named
`gh-<name>` containing a matching executable, which `gh-secretz` already
satisfies, and this tool is a `gh` wrapper by nature:

```
gh extension install <owner>/gh-secretz
```

`gh` checks for new versions every 24 hours unprompted, `gh extension upgrade`
applies them, and the official `gh-extension-precompile` action builds the
per-platform release assets. Because `gh` downloads the binary itself rather than
a browser, no quarantine attribute is set and there is no Gatekeeper prompt.

Replacing `gh api` subprocesses with a real HTTP client also removes the per-call
process spawn that dominated the measured 0.38s probe latency. Enumeration is the
heaviest thing this tool does, so the sweep should get markedly faster, but no
figure is claimed here until it is measured.

## Architecture

```
gh-secretz/
  main.go
  internal/
    transport/   # go-gh REST client, pagination, error classification
    queue/       # dismissal request fetch and normalisation
    alerts/      # union enumeration, indexing
    discover/    # concurrent repo sweep and cache
    filter/      # predicate composition
    selection/   # cursor and checked set, pure logic
    ui/          # bubbletea models and lipgloss styling
    executor/    # writes, post-write verification, audit log
  .github/workflows/release.yml
  README.md
```

Dependencies, pinned in `go.sum`: `github.com/cli/go-gh/v2`,
`github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`, and
`github.com/BurntSushi/toml` for config, since Go has no standard TOML parser.

Layers, each independently testable behind an interface so tests never touch the
network:

1. **Transport** wraps the go-gh REST client: pagination, JSON decoding, HTTP
   error classification. Defined as an interface so every layer above it is
   tested against a fake.
2. **Queue** fetches and normalises dismissal requests into flat records,
   enforcing the alert-number assertion.
3. **Alerts** enumerates via the union strategy and indexes by alert number.
4. **Enrichment** joins alerts onto requests, one alert fetch per repo rather
   than per request.
5. **Discovery** sweeps the configured repo prefixes concurrently and caches
   hits.
6. **Filter** composes repo, requester, reason, and secret-type predicates, and
   refuses an empty filter set.
7. **Selection model** holds cursor and checked set as pure logic.
8. **UI** holds the bubbletea models and lipgloss styling. The view rendering is
   the only untested layer.
9. **Executor** performs sequential writes with post-write verification, per item
   outcomes, and audit logging.

## Commands

| Command | Behaviour |
|---|---|
| `list [filters]` | Table or `--json`. Scriptable, no TUI. Filters optional. |
| `review <filters>` | TUI multi-select over pending requests, then approve or deny. At least one filter required. |
| `show <repo> <alert>` | Full detail for one alert plus its dismissal request, if any. Keyed on alert number. |
| `discover [--refresh]` | Concurrent sweep of the configured repo prefixes; caches repos with open alerts. |
| `triage <--repo r \| --all>` | TUI over alerts with no dismissal request, then close. `--all` reads the cache. |
| `close <repo> <alert>` | Single non-interactive close. Escape hatch. |

Globals: `--org` (required, or set in config), `--time-period` (default `month`),
`--secret-types`, `--json`.

The mandatory-filter rule applies to the **write** commands only. `list` is
read-only and may be run bare to see the whole queue; `review` refuses an empty
filter set so that a bulk approval always has a stated scope. `triage --all` is
an explicit scope, so it satisfies the rule.

### TUI keys

Shared by `review` and `triage`:

| Key | Action |
|---|---|
| up / down, `k` / `j` | Move cursor |
| space | Toggle checked |
| `a` | Check all in view |
| `n` | Uncheck all |
| `A` | Approve checked (`review` only) |
| `D` | Deny checked (`review` only) |
| `C` | Close checked (`triage` only) |
| enter | Show detail for the row under the cursor |
| `q`, Ctrl-C | Abort, sending nothing |

Approve, deny, and close are distinct capital keys rather than a shared confirm,
so that the destructive action is always named explicitly. Each prompts for the
required message before anything is sent, and shows the affected count.

## Behaviour and safety

* Enrichment flags stale claims: a stated reason of `revoked` against an alert
  still `state: open` with `validity: active` gets a warning glyph. Triage
  surfaces `validity` prominently for the same reason.
* Approving or denying requires a non-empty `message`, prompted once and applied
  to the batch.
* Closing requires an explicit `--resolution` and comment. No default, because
  unlike approval there is no requester reason to inherit.
* Zero filters on a write command is an error listing the available filters.
* Non-TTY refuses the TUI and points at `list`.
* Ctrl-C in the TUI aborts with nothing sent.
* Per-item failures (403 without review permission, 404 already resolved) do not
  abort the batch. They collect into a closing summary and force a non-zero exit.
* Every write appends to `~/.gh-secretz/audit.jsonl`: timestamp, actor, repo,
  alert number, request number, action, message, HTTP status, and verified
  resulting state.
* Cache lives at `~/.gh-secretz/cache.json` with an age shown on use.

## Error handling

| Condition | Response |
|---|---|
| `gh` missing or unauthenticated | Clear message naming `gh auth login`, exit 2 |
| Org endpoint 404 | Expected for a custom role; fall back to cached repo scope |
| Alert-number assertion fails | Skip record, warn, continue |
| Write returns 200 but state not `resolved` | Report as "request created instead of close" |
| Rate limit exhaustion | Stop, report remaining work, suggest resume |

A capability probe runs once at startup: if the org-wide alert endpoint is
available to the operator, org-wide triage activates with no code change.

## Testing

`go test ./...`, no network. The transport interface is faked with recorded
fixtures captured from the real API during design, so the counterintuitive
behaviours are pinned by tests rather than by comments. Covered:

* Filter composition, including the empty-filter refusal.
* The alert-number assertion and its skip path, in both directions.
* Multi-entry `data` rejection.
* Enrichment join and display-name to slug mapping.
* Union enumeration deduplication.
* Stale-claim detection.
* Unrequested-alert detection, including the expired-request case.
* Message and resolution validation.
* Selection model transitions.
* Post-write verification treating a 200 with a non-resolved read-back as
  failure.

The bubbletea view layer is not unit tested; selection state is deliberately
separated from rendering so the logic is covered without a terminal. Bubbletea's
`Update` function is pure, so key handling is tested by feeding it messages
directly.

## Known limitations

Stated plainly because they are GitHub's, not the tool's:

* Requests older than one month cannot be listed.
* "Assigned to me" means "what the operator's role permits reviewing." If the
  role changes, the queue changes silently.
* Enumeration completeness depends on the maintained generic slug list.
* Triage covers only the configured repo prefixes, and only repos the sweep can
  see. A repo outside those prefixes is invisible.
* Security manager or org owner access would remove the org-wide alert
  limitation. The capability probe means no code change would be needed.
* `gh` must be installed and authenticated on any machine running this. That is
  inherent to a `gh` extension and to reusing gh's auth rather than handling a
  token.
* Extension installs are trusted by the installer, not verified by GitHub. Anyone
  told to install this should be pointed at the source.
