# gh-secretz

A `gh` CLI extension for reviewing GitHub secret scanning work from the terminal.

Two jobs:

1. **Bulk approve or deny pending secret scanning dismissal requests** you are
   entitled to review, without clicking through the web UI one at a time.
2. **Triage open alerts that have no dismissal request yet**, closing them
   directly before a request is ever filed. This also resurfaces requests that
   silently expired.

The design notes in [`docs/superpowers/specs/`](docs/superpowers/specs/) are
worth reading even if you never use the tool, because they document several
GitHub API behaviours that are easy to get wrong.

## Usage

```
gh secretz list     [--org O] [--repo R] [--requester U] [--reason R] [--secret-type T] [--json]
gh secretz review    --org O  <at least one filter>
gh secretz show      --org O  <repo> <alert-number>
gh secretz discover  --org O  [--workers N]
gh secretz triage    --org O  <--repo R | --all>
gh secretz close     --org O  <repo> <alert-number> --resolution R --comment C
```

### In the multi select

`space` toggles a row, `a` checks all, `n` clears, `enter` opens the full detail
for the row under the cursor, and `q` aborts without sending anything.

The detail pane shows the untruncated requester comment, both the alert and
request numbers, and the **source context around the detected secret**: the file
path, line number, and the surrounding lines, fetched on demand and cached per
row. The line holding the secret is marked and wrapped rather than truncated, so
a long token is fully readable. Seeing the value is often what settles the
question: an expired dev token in a fixture and a live production credential
look identical when all you have is the secret type.

The destructive keys are capitals, and each mode exposes only its own, so a
muscle memory keystroke cannot perform the wrong operation:

| Mode | Key | Action |
|---|---|---|
| `review` | `A` | approve the checked rows |
| `review` | `D` | deny the checked rows |
| `triage` | `R` | close as revoked, the secret has been revoked |
| `triage` | `T` | close as used in tests, not in production code |
| `triage` | `F` | close as false positive, the alert is not valid |
| `triage` | `W` | close as wont fix, the alert is not relevant |

Any of those opens a prompt for the comment applied to every checked row, with
the affected rows still on screen. A comment is required, `enter` confirms, and
`esc` cancels without sending anything. Choosing the reason and writing the
comment happen alongside choosing the rows, so a selection can never be lost to
a missing flag.

Configuration is optional, at `~/.gh-secretz/config.toml`:

```toml
org = "my-org"
repo_prefixes = ["svc-", "lib-"]
```

`repo_prefixes` scopes the `discover` sweep. An empty list matches nothing
rather than everything, because a wildcard sweep of a large org is thousands of
API calls.

## Why this exists

GitHub's delegated alert dismissal puts a review step in front of closing a
secret scanning alert. That is good policy and a poor experience once a queue
reaches the hundreds: the web UI reviews one request at a time, and requests
expire after 7 days whether or not anyone looked at them.

## API behaviours worth knowing

These were confirmed empirically and are documented in full in the design. If you
are writing your own tooling against these endpoints, this is the part to read:

- The status filter on dismissal requests is `request_status`, **not** `status`.
  A `status` parameter is silently ignored and you get unfiltered results back.
- `time_period` defaults to `day` and **caps at `month`**. Requests older than a
  month cannot be listed at all.
- A dismissal request has both a request number and an alert number, and **they
  differ**. The review `PATCH` path takes the *alert* number. Getting this
  backwards dismisses an unrelated live secret.
- **The default alert list silently omits generic detection types** such as
  `password` and `http_bearer_authentication_header`. In one repository tested,
  the default list returned 0 alerts while 5 were open. You must name secret
  types explicitly to see them.
- There is no assignee concept. `reviewer` filters on who already responded, not
  who is expected to.
- `GET /orgs/{org}/secret-scanning/alerts` is gated on org owner or security
  manager. The org security overview page in the web UI showing you this data is
  not evidence the API will.

## Requirements

- [`gh`](https://cli.github.com) installed and authenticated. The tool reuses
  gh's auth and holds no credential of its own.
- Permission to review dismissal requests in your org: org owner, security
  manager, or a custom role granting *"Review and manage secret scanning alert
  dismissal requests"*.

## Install

```
gh extension install Dilan-TH/gh-secretz
```

## Safety

Approving a dismissal and closing an alert are both irreversible. The design
commits to:

- Write commands refuse to run without an explicit filter, so a bulk approval
  always has a stated scope.
- Every write is verified by re-reading the resource afterwards. Outcomes are
  reported from observed state, never from an HTTP 200.
- Every write is appended to a local audit log at `~/.gh-secretz/audit.jsonl`.
- Enrichment flags stale claims, such as a "revoked" justification on a secret
  that is still live.

## License

MIT
