package queue

import (
	"os"
	"strings"
	"testing"

	"github.com/Dilan-TH/gh-secretz/internal/transport"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("reading fixture %s: %v", name, err)
	}
	return string(b)
}

func TestPathUsesRequestStatusAndMonth(t *testing.T) {
	got := Path(Options{Org: "acme", RequestStatus: "open", TimePeriod: "month"})

	// request_status is the real parameter. A "status" parameter is silently
	// ignored by the API and returns unfiltered results, so its presence
	// would be a correctness bug rather than a harmless extra.
	if !strings.Contains(got, "request_status=open") {
		t.Errorf("path %q must contain request_status=open", got)
	}
	if !strings.Contains(got, "time_period=month") {
		t.Errorf("path %q must pin time_period=month, the API maximum", got)
	}
	if !strings.Contains(got, "per_page=100") {
		t.Errorf("path %q should request full pages", got)
	}
	if !strings.HasPrefix(got, "orgs/acme/dismissal-requests/secret-scanning?") {
		t.Errorf("path %q has the wrong prefix", got)
	}
}

func TestPathRepoScoped(t *testing.T) {
	got := Path(Options{Org: "acme", Repo: "repo-a", RequestStatus: "open", TimePeriod: "month"})
	if !strings.HasPrefix(got, "repos/acme/repo-a/dismissal-requests/secret-scanning?") {
		t.Errorf("path %q should target the repo endpoint when Repo is set", got)
	}
}

func TestPathDefaultsTimePeriodToMonth(t *testing.T) {
	got := Path(Options{Org: "acme"})
	if !strings.Contains(got, "time_period=month") {
		t.Errorf("path %q must default time_period to month, not leave it at the API default of day", got)
	}
}

func TestListNormalisesAlertNumberFromString(t *testing.T) {
	f := transport.NewFake()
	opts := Options{Org: "acme", RequestStatus: "open", TimePeriod: "month"}
	f.SetPage(Path(opts), fixture(t, "requests.json"))

	reqs, skips, err := List(f, opts)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(skips) != 0 {
		t.Fatalf("unexpected skips: %+v", skips)
	}
	if len(reqs) != 2 {
		t.Fatalf("got %d requests, want 2", len(reqs))
	}

	first := reqs[0]
	if first.Number != 5 {
		t.Errorf("Number = %d, want 5", first.Number)
	}
	// The critical assertion. alert_number arrives as the string "18" and
	// must become the int 18, distinct from the request number 5.
	if first.AlertNumber != 18 {
		t.Errorf("AlertNumber = %d, want 18 (parsed from the JSON string)", first.AlertNumber)
	}
	if first.Owner != "acme" || first.Repo != "repo-a" {
		t.Errorf("owner/repo = %s/%s, want acme/repo-a", first.Owner, first.Repo)
	}
	if first.Requester != "alice" {
		t.Errorf("Requester = %q, want alice", first.Requester)
	}
	if first.Reason != "revoked" {
		t.Errorf("Reason = %q, want revoked", first.Reason)
	}
	if first.SecretTypeDisplay != "HTTP bearer authentication header" {
		t.Errorf("SecretTypeDisplay = %q", first.SecretTypeDisplay)
	}
	if first.CreatedAt.IsZero() || first.ExpiresAt.IsZero() {
		t.Error("timestamps should parse, including the -06:00 offset")
	}
}

func TestListSkipsWhenResourceIdentifierDisagreesWithAlertNumber(t *testing.T) {
	// A disagreement means we cannot tell which alert the request refers to.
	// Guessing could dismiss an unrelated live secret, so the record is
	// skipped and reported.
	const bad = `[{
		"id": 1, "number": 5,
		"repository": {"name":"repo-a","full_name":"acme/repo-a"},
		"organization": {"name":"acme"},
		"requester": {"actor_name":"alice"},
		"data": [{"reason":"revoked","secret_type":"Password","alert_number":"18"}],
		"resource_identifier": "99",
		"status": "pending",
		"created_at": "2026-07-24T14:16:23-06:00",
		"expires_at": "2026-07-31T14:16:23-06:00"
	}]`

	f := transport.NewFake()
	opts := Options{Org: "acme", RequestStatus: "open", TimePeriod: "month"}
	f.SetPage(Path(opts), bad)

	reqs, skips, err := List(f, opts)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(reqs) != 0 {
		t.Errorf("got %d requests, want 0 because the record is ambiguous", len(reqs))
	}
	if len(skips) != 1 {
		t.Fatalf("got %d skips, want 1", len(skips))
	}
	if !strings.Contains(skips[0].Reason, "resource_identifier") {
		t.Errorf("skip reason %q should name the mismatched field", skips[0].Reason)
	}
	if skips[0].RequestNumber != 5 {
		t.Errorf("skip should identify request 5, got %d", skips[0].RequestNumber)
	}
}

func TestListSkipsMultiEntryData(t *testing.T) {
	const multi = `[{
		"id": 1, "number": 7,
		"repository": {"name":"repo-a","full_name":"acme/repo-a"},
		"organization": {"name":"acme"},
		"requester": {"actor_name":"alice"},
		"data": [
			{"reason":"revoked","secret_type":"Password","alert_number":"18"},
			{"reason":"revoked","secret_type":"Password","alert_number":"19"}
		],
		"resource_identifier": "18",
		"status": "pending",
		"created_at": "2026-07-24T14:16:23-06:00",
		"expires_at": "2026-07-31T14:16:23-06:00"
	}]`

	f := transport.NewFake()
	opts := Options{Org: "acme", RequestStatus: "open", TimePeriod: "month"}
	f.SetPage(Path(opts), multi)

	reqs, skips, err := List(f, opts)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(reqs) != 0 || len(skips) != 1 {
		t.Fatalf("got %d requests and %d skips, want 0 and 1", len(reqs), len(skips))
	}
	if !strings.Contains(skips[0].Reason, "2 entries") {
		t.Errorf("skip reason %q should state how many entries were found", skips[0].Reason)
	}
}

func TestListSkipsEmptyData(t *testing.T) {
	const empty = `[{
		"id": 1, "number": 8,
		"repository": {"name":"repo-a","full_name":"acme/repo-a"},
		"organization": {"name":"acme"},
		"requester": {"actor_name":"alice"},
		"data": [],
		"resource_identifier": "18",
		"status": "pending",
		"created_at": "2026-07-24T14:16:23-06:00",
		"expires_at": "2026-07-31T14:16:23-06:00"
	}]`

	f := transport.NewFake()
	opts := Options{Org: "acme", RequestStatus: "open", TimePeriod: "month"}
	f.SetPage(Path(opts), empty)

	reqs, skips, err := List(f, opts)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(reqs) != 0 || len(skips) != 1 {
		t.Fatalf("got %d requests and %d skips, want 0 and 1", len(reqs), len(skips))
	}
}

func TestListFiltersStatusClientSide(t *testing.T) {
	// request_status=approved also returns expired records, so the server
	// side filter is not trusted on its own.
	const mixed = `[
		{"id":1,"number":1,"repository":{"name":"r","full_name":"acme/r"},"organization":{"name":"acme"},
		 "requester":{"actor_name":"a"},"data":[{"reason":"revoked","secret_type":"Password","alert_number":"1"}],
		 "resource_identifier":"1","status":"approved","created_at":"2026-07-24T00:00:00Z","expires_at":"2026-07-31T00:00:00Z"},
		{"id":2,"number":2,"repository":{"name":"r","full_name":"acme/r"},"organization":{"name":"acme"},
		 "requester":{"actor_name":"a"},"data":[{"reason":"revoked","secret_type":"Password","alert_number":"2"}],
		 "resource_identifier":"2","status":"expired","created_at":"2026-07-24T00:00:00Z","expires_at":"2026-07-31T00:00:00Z"}
	]`

	f := transport.NewFake()
	opts := Options{Org: "acme", RequestStatus: "approved", TimePeriod: "month"}
	f.SetPage(Path(opts), mixed)

	reqs, _, err := List(f, opts)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("got %d requests, want 1 after client side status filtering", len(reqs))
	}
	if reqs[0].Status != "approved" {
		t.Errorf("Status = %q, want approved", reqs[0].Status)
	}
}

func TestListMapsOpenToPending(t *testing.T) {
	// The API accepts request_status=open but reports the resulting records
	// with status "pending". Client side filtering must not drop them.
	f := transport.NewFake()
	opts := Options{Org: "acme", RequestStatus: "open", TimePeriod: "month"}
	f.SetPage(Path(opts), fixture(t, "requests.json"))

	reqs, _, err := List(f, opts)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(reqs) != 2 {
		t.Fatalf("got %d requests, want 2; open must match the pending status", len(reqs))
	}
}
