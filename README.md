# gh-secretz

A `gh` CLI extension for reviewing GitHub secret scanning work from the terminal.

Two jobs:

1. **Bulk approve or deny pending secret scanning dismissal requests** you are
   entitled to review, without clicking through the web UI one at a time.
2. **Triage open alerts that have no dismissal request yet**, closing them
   directly before a request is ever filed. This also resurfaces requests that
   silently expired.

> **Status: design complete, not yet implemented.** The design is in
> [`docs/superpowers/specs/`](docs/superpowers/specs/) and is worth reading even
> if you never use the tool, because it documents several GitHub API behaviours
> that are easy to get wrong.

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
