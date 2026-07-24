// Package queue fetches secret scanning dismissal requests and normalises
// them into model.Request.
//
// It owns the tool's most important safety property. A dismissal request
// carries a request number and an alert number, and they differ on the same
// record. Every write path uses the alert number, so this package refuses to
// emit a record whose alert number is ambiguous.
package queue

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Dilan-TH/gh-secretz/internal/model"
	"github.com/Dilan-TH/gh-secretz/internal/transport"
)

// Options selects which requests to list.
type Options struct {
	Org string
	// Repo, when set, targets the repo endpoint instead of the org endpoint.
	Repo string
	// RequestStatus is one of completed, cancelled, approved, expired,
	// denied, open, all. Empty means all.
	RequestStatus string
	// TimePeriod is hour, day, week, or month. Empty defaults to month,
	// which is the API maximum.
	TimePeriod string
	Requester  string
}

// Skip records a request that was deliberately not returned.
type Skip struct {
	RequestNumber int
	FullName      string
	Reason        string
}

// Path builds the request listing path including its query string.
//
// The status parameter is named request_status. A parameter called status is
// accepted and silently ignored by the API, returning unfiltered results, so
// using it would produce a bulk tool that acts on records the operator did
// not select.
func Path(opts Options) string {
	q := url.Values{}
	if opts.RequestStatus != "" {
		q.Set("request_status", opts.RequestStatus)
	}
	tp := opts.TimePeriod
	if tp == "" {
		tp = "month"
	}
	q.Set("time_period", tp)
	if opts.Requester != "" {
		q.Set("requester", opts.Requester)
	}
	q.Set("per_page", "100")

	if opts.Repo != "" {
		return fmt.Sprintf("repos/%s/%s/dismissal-requests/secret-scanning?%s", opts.Org, opts.Repo, q.Encode())
	}
	return fmt.Sprintf("orgs/%s/dismissal-requests/secret-scanning?%s", opts.Org, q.Encode())
}

// wire mirrors the API payload. alert_number arrives as a JSON string.
type wire struct {
	ID         int64 `json:"id"`
	Number     int   `json:"number"`
	Repository struct {
		Name     string `json:"name"`
		FullName string `json:"full_name"`
	} `json:"repository"`
	Organization struct {
		Name string `json:"name"`
	} `json:"organization"`
	Requester struct {
		ActorName string `json:"actor_name"`
	} `json:"requester"`
	Data []struct {
		Reason      string `json:"reason"`
		SecretType  string `json:"secret_type"`
		AlertNumber string `json:"alert_number"`
	} `json:"data"`
	ResourceIdentifier string `json:"resource_identifier"`
	Status             string `json:"status"`
	RequesterComment   string `json:"requester_comment"`
	ExpiresAt          string `json:"expires_at"`
	CreatedAt          string `json:"created_at"`
	HTMLURL            string `json:"html_url"`
}

// List fetches and normalises requests. Records that cannot be safely
// interpreted are returned as skips rather than dropped silently or guessed
// at, so the caller can report them.
func List(t transport.Transport, opts Options) ([]model.Request, []Skip, error) {
	raws, err := t.GetAllPages(Path(opts))
	if err != nil {
		return nil, nil, err
	}

	var out []model.Request
	var skips []Skip

	for _, raw := range raws {
		var w wire
		if err := json.Unmarshal(raw, &w); err != nil {
			skips = append(skips, Skip{Reason: fmt.Sprintf("undecodable record: %v", err)})
			continue
		}

		if len(w.Data) != 1 {
			skips = append(skips, Skip{
				RequestNumber: w.Number,
				FullName:      w.Repository.FullName,
				Reason:        fmt.Sprintf("data holds %d entries, expected exactly 1, so the target alert is ambiguous", len(w.Data)),
			})
			continue
		}

		alertNum, err := strconv.Atoi(w.Data[0].AlertNumber)
		if err != nil {
			skips = append(skips, Skip{
				RequestNumber: w.Number,
				FullName:      w.Repository.FullName,
				Reason:        fmt.Sprintf("alert_number %q is not an integer", w.Data[0].AlertNumber),
			})
			continue
		}

		// resource_identifier duplicates the alert number. If the two
		// disagree we cannot tell which alert is meant, and acting on the
		// wrong one dismisses an unrelated live secret.
		if w.ResourceIdentifier != w.Data[0].AlertNumber {
			skips = append(skips, Skip{
				RequestNumber: w.Number,
				FullName:      w.Repository.FullName,
				Reason: fmt.Sprintf("resource_identifier %q disagrees with data.alert_number %q",
					w.ResourceIdentifier, w.Data[0].AlertNumber),
			})
			continue
		}

		if !statusMatches(opts.RequestStatus, w.Status) {
			continue
		}

		out = append(out, model.Request{
			ID:                w.ID,
			Number:            w.Number,
			AlertNumber:       alertNum,
			Owner:             owner(w.Organization.Name, w.Repository.FullName),
			Repo:              w.Repository.Name,
			FullName:          w.Repository.FullName,
			Requester:         w.Requester.ActorName,
			Reason:            w.Data[0].Reason,
			SecretTypeDisplay: w.Data[0].SecretType,
			Status:            w.Status,
			RequesterComment:  w.RequesterComment,
			CreatedAt:         parseTime(w.CreatedAt),
			ExpiresAt:         parseTime(w.ExpiresAt),
			HTMLURL:           w.HTMLURL,
		})
	}

	return out, skips, nil
}

// statusMatches repeats the server side filter locally, because
// request_status=approved also returns expired records.
func statusMatches(want, got string) bool {
	if want == "" || want == "all" {
		return true
	}
	// The API accepts "open" but reports those records as "pending".
	if want == "open" {
		return got == "pending" || got == "open"
	}
	return want == got
}

func owner(orgName, fullName string) string {
	if orgName != "" {
		return orgName
	}
	if i := strings.Index(fullName, "/"); i >= 0 {
		return fullName[:i]
	}
	return fullName
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	// The API mixes Z and numeric offsets across fields.
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts
		}
	}
	return time.Time{}
}
