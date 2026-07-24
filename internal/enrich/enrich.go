// Package enrich joins alerts onto requests and derives the warnings that
// let an operator spot a bad approval before making it.
package enrich

import (
	"fmt"

	"github.com/Dilan-TH/gh-secretz/internal/model"
)

// Warning identifiers attached to model.Row.Warnings.
const (
	// WarnStaleClaim means the requester claimed the secret is dead while
	// the alert reports it open and still valid.
	WarnStaleClaim = "stale-claim"
	// WarnNoAlert means enumeration did not reach the alert, so the request
	// cannot be cross checked.
	WarnNoAlert = "alert-unreachable"
	// WarnPubliclyLeaked means the secret is known to be public.
	WarnPubliclyLeaked = "publicly-leaked"
)

// Disagreement records a conflict between the alert's closure marker and the
// dismissal request list.
type Disagreement struct {
	Key    string
	Detail string
}

// RepoKey builds the map key used to index alerts per repository.
func RepoKey(owner, repo string) string {
	return owner + "/" + repo
}

// Join attaches each request's alert, matched on the alert number. Matching
// on the request number would attach an unrelated alert, because the two
// numbers differ on the same record.
func Join(reqs []model.Request, byRepo map[string]map[int]model.Alert) []model.Row {
	rows := make([]model.Row, 0, len(reqs))

	for i := range reqs {
		req := reqs[i]
		row := model.Row{Request: &req}

		if alertsForRepo, ok := byRepo[RepoKey(req.Owner, req.Repo)]; ok {
			if a, ok := alertsForRepo[req.AlertNumber]; ok {
				alert := a
				row.Alert = &alert
			}
		}

		if row.Alert == nil {
			row.Warnings = append(row.Warnings, WarnNoAlert)
		} else {
			if staleClaim(req, *row.Alert) {
				row.Warnings = append(row.Warnings, WarnStaleClaim)
			}
			if row.Alert.PubliclyLeaked {
				row.Warnings = append(row.Warnings, WarnPubliclyLeaked)
			}
		}

		rows = append(rows, row)
	}

	return rows
}

// staleClaim reports a requester asserting the secret is dead while the alert
// says otherwise.
//
// Only reasons that actually claim the secret is dead qualify. used_in_tests
// and false_positive make no such claim, so an active secret does not
// contradict them. Validity must be explicitly active, because most alerts
// report unknown and warning on those would drown the signal.
func staleClaim(req model.Request, a model.Alert) bool {
	if req.Reason != "revoked" {
		return false
	}
	return a.State == "open" && a.Validity == "active"
}

// Unrequested returns alerts that have no dismissal request, using the
// alert's own closure marker as the primary signal.
//
// The marker is preferred over a set difference against the request list
// because the request list is capped at one month by the API. A request filed
// five weeks ago would make a set difference wrongly classify its alert as
// unrequested and invite closing something already in flight. The marker has
// no time window.
//
// The set difference still runs as a cross check. Where the two disagree the
// alert is withheld and the conflict reported, rather than silently resolved
// in either direction.
func Unrequested(as []model.Alert, reqs []model.Request) ([]model.Row, []Disagreement) {
	// Index only requests that are actually in flight. An expired or denied
	// request leaves the alert open and is exactly the work triage exists to
	// recover, so it is not a conflict.
	inFlight := map[string]model.Request{}
	for _, r := range reqs {
		if r.Status == "pending" || r.Status == "open" {
			inFlight[fmt.Sprintf("%s/%s#%d", r.Owner, r.Repo, r.AlertNumber)] = r
		}
	}

	var rows []model.Row
	var dis []Disagreement

	for i := range as {
		a := as[i]
		if a.State != "open" {
			continue
		}
		if a.HasClosureRequest() {
			continue
		}

		key := fmt.Sprintf("%s/%s#%d", a.Owner, a.Repo, a.Number)
		if r, clash := inFlight[key]; clash {
			dis = append(dis, Disagreement{
				Key: key,
				Detail: fmt.Sprintf(
					"alert reports no closure request but request %d is %s; withheld from triage",
					r.Number, r.Status),
			})
			continue
		}

		alert := a
		row := model.Row{Alert: &alert}
		if alert.PubliclyLeaked {
			row.Warnings = append(row.Warnings, WarnPubliclyLeaked)
		}
		rows = append(rows, row)
	}

	return rows, dis
}
