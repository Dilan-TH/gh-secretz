package enrich

import (
	"strings"
	"testing"

	"github.com/Dilan-TH/gh-secretz/internal/model"
)

func idx(as ...model.Alert) map[string]map[int]model.Alert {
	out := map[string]map[int]model.Alert{}
	for _, a := range as {
		k := RepoKey(a.Owner, a.Repo)
		if out[k] == nil {
			out[k] = map[int]model.Alert{}
		}
		out[k][a.Number] = a
	}
	return out
}

func TestJoinMatchesOnAlertNumberNotRequestNumber(t *testing.T) {
	// Request number 5 refers to alert 18. Joining on the request number
	// would attach the wrong alert, which is the failure this guards.
	req := model.Request{Number: 5, AlertNumber: 18, Owner: "acme", Repo: "r"}
	alert18 := model.Alert{Number: 18, Owner: "acme", Repo: "r", State: "open", SecretType: "password"}
	alert5 := model.Alert{Number: 5, Owner: "acme", Repo: "r", State: "resolved", SecretType: "jwt_header"}

	rows := Join([]model.Request{req}, idx(alert18, alert5), nil)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Alert == nil {
		t.Fatal("alert should have been joined")
	}
	if rows[0].Alert.Number != 18 {
		t.Errorf("joined alert number = %d, want 18", rows[0].Alert.Number)
	}
	if rows[0].Alert.SecretType != "password" {
		t.Errorf("joined the wrong alert, secret type = %q", rows[0].Alert.SecretType)
	}
}

func TestJoinWarnsWhenAlertUnreachable(t *testing.T) {
	req := model.Request{Number: 1, AlertNumber: 99, Owner: "acme", Repo: "r"}
	rows := Join([]model.Request{req}, idx(), nil)
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Alert != nil {
		t.Error("Alert should be nil when the alert was not enumerated")
	}
	if !hasWarning(rows[0], WarnNoAlert) {
		t.Errorf("warnings = %v, want %q", rows[0].Warnings, WarnNoAlert)
	}
}

func TestJoinFlagsStaleRevokedClaimOnLiveSecret(t *testing.T) {
	// A requester claiming "revoked" against a still open, still active
	// secret is the case worth making loud before an approval.
	req := model.Request{Number: 1, AlertNumber: 7, Owner: "acme", Repo: "r", Reason: "revoked"}
	live := model.Alert{Number: 7, Owner: "acme", Repo: "r", State: "open", Validity: "active"}

	rows := Join([]model.Request{req}, idx(live), nil)
	if !hasWarning(rows[0], WarnStaleClaim) {
		t.Errorf("warnings = %v, want %q", rows[0].Warnings, WarnStaleClaim)
	}
}

func TestJoinDoesNotFlagRevokedClaimWhenValidityUnknown(t *testing.T) {
	// Most alerts report validity unknown. Warning on those would make the
	// signal useless through sheer volume.
	req := model.Request{Number: 1, AlertNumber: 7, Owner: "acme", Repo: "r", Reason: "revoked"}
	unknown := model.Alert{Number: 7, Owner: "acme", Repo: "r", State: "open", Validity: "unknown"}

	rows := Join([]model.Request{req}, idx(unknown), nil)
	if hasWarning(rows[0], WarnStaleClaim) {
		t.Errorf("validity unknown should not raise a stale claim warning, got %v", rows[0].Warnings)
	}
}

func TestJoinDoesNotFlagUsedInTestsOnLiveSecret(t *testing.T) {
	// used_in_tests makes no claim about the secret being dead, so an
	// active secret is not a contradiction.
	req := model.Request{Number: 1, AlertNumber: 7, Owner: "acme", Repo: "r", Reason: "used_in_tests"}
	live := model.Alert{Number: 7, Owner: "acme", Repo: "r", State: "open", Validity: "active"}

	rows := Join([]model.Request{req}, idx(live), nil)
	if hasWarning(rows[0], WarnStaleClaim) {
		t.Errorf("used_in_tests should not raise a stale claim, got %v", rows[0].Warnings)
	}
}

func TestJoinFlagsPubliclyLeaked(t *testing.T) {
	req := model.Request{Number: 1, AlertNumber: 7, Owner: "acme", Repo: "r", Reason: "revoked"}
	leaked := model.Alert{Number: 7, Owner: "acme", Repo: "r", State: "open", PubliclyLeaked: true}

	rows := Join([]model.Request{req}, idx(leaked), nil)
	if !hasWarning(rows[0], WarnPubliclyLeaked) {
		t.Errorf("warnings = %v, want %q", rows[0].Warnings, WarnPubliclyLeaked)
	}
}

func TestUnrequestedUsesClosureMarker(t *testing.T) {
	withReq := model.Alert{Number: 1, Owner: "acme", Repo: "r", State: "open", ClosureRequestComment: "please close"}
	without := model.Alert{Number: 2, Owner: "acme", Repo: "r", State: "open"}

	rows, dis := Unrequested([]model.Alert{withReq, without}, nil, nil)
	if len(dis) != 0 {
		t.Fatalf("unexpected disagreements: %+v", dis)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if rows[0].Alert.Number != 2 {
		t.Errorf("returned alert %d, want 2, the one without a closure marker", rows[0].Alert.Number)
	}
	if rows[0].Request != nil {
		t.Error("a triage row has no request")
	}
}

func TestUnrequestedExcludesResolvedAlerts(t *testing.T) {
	resolved := model.Alert{Number: 3, Owner: "acme", Repo: "r", State: "resolved"}
	rows, _ := Unrequested([]model.Alert{resolved}, nil, nil)
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0; a resolved alert needs no triage", len(rows))
	}
}

func TestUnrequestedReportsDisagreementWithRequestList(t *testing.T) {
	// The closure marker says no request, but a request exists in the list.
	// That contradiction is surfaced rather than silently resolved.
	alert := model.Alert{Number: 5, Owner: "acme", Repo: "r", State: "open"}
	req := model.Request{Number: 9, AlertNumber: 5, Owner: "acme", Repo: "r", Status: "pending"}

	rows, dis := Unrequested([]model.Alert{alert}, []model.Request{req}, nil)
	if len(dis) != 1 {
		t.Fatalf("got %d disagreements, want 1", len(dis))
	}
	if !strings.Contains(dis[0].Detail, "pending") {
		t.Errorf("detail %q should describe the conflicting request", dis[0].Detail)
	}
	// The alert is withheld from triage, because acting on something already
	// in flight is the risk being avoided.
	if len(rows) != 0 {
		t.Errorf("got %d rows, want 0 while the contradiction is unresolved", len(rows))
	}
}

func TestUnrequestedIgnoresExpiredRequestsInDisagreementCheck(t *testing.T) {
	// An expired request leaves the alert open and clears the marker. That
	// is exactly the work triage exists to recover, not a contradiction.
	alert := model.Alert{Number: 5, Owner: "acme", Repo: "r", State: "open"}
	expired := model.Request{Number: 9, AlertNumber: 5, Owner: "acme", Repo: "r", Status: "expired"}

	rows, dis := Unrequested([]model.Alert{alert}, []model.Request{expired}, nil)
	if len(dis) != 0 {
		t.Errorf("an expired request is not a contradiction, got %+v", dis)
	}
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1; expired requests should resurface for triage", len(rows))
	}
}

func TestIsTestPathMatchesCaseInsensitively(t *testing.T) {
	if !IsTestPath("Spec/Fixtures/CREDS.YAML", []string{"fixtures/"}) {
		t.Error("expected a case insensitive match")
	}
}

func TestIsTestPathNoMatch(t *testing.T) {
	if IsTestPath("src/prod/config.go", []string{"fixtures/", "e2e/"}) {
		t.Error("production path should not match")
	}
}

func TestIsTestPathEmptyPathOrPatternsNeverMatches(t *testing.T) {
	if IsTestPath("", []string{"fixtures/"}) {
		t.Error("empty path must not match")
	}
	if IsTestPath("fixtures/creds.yaml", nil) {
		t.Error("nil pattern list must not match")
	}
}

func TestJoinSetsTestPathWithoutTouchingWarnings(t *testing.T) {
	req := model.Request{Number: 1, AlertNumber: 7, Owner: "acme", Repo: "r", Reason: "revoked"}
	leaked := model.Alert{
		Number: 7, Owner: "acme", Repo: "r", State: "open",
		PubliclyLeaked: true, Path: "test/fixtures/creds.yaml",
	}

	rows := Join([]model.Request{req}, idx(leaked), []string{"fixtures/"})
	if !rows[0].TestPath {
		t.Error("expected TestPath to be set for a fixtures/ path")
	}
	// A test-path match must never suppress a real warning.
	if !hasWarning(rows[0], WarnPubliclyLeaked) {
		t.Errorf("warnings = %v, TestPath must not suppress WarnPubliclyLeaked", rows[0].Warnings)
	}
}

func TestUnrequestedSetsTestPathWithoutTouchingWarnings(t *testing.T) {
	alert := model.Alert{
		Number: 5, Owner: "acme", Repo: "r", State: "open",
		PubliclyLeaked: true, Path: "e2e/token.go",
	}

	rows, _ := Unrequested([]model.Alert{alert}, nil, []string{"e2e/"})
	if len(rows) != 1 {
		t.Fatalf("got %d rows, want 1", len(rows))
	}
	if !rows[0].TestPath {
		t.Error("expected TestPath to be set for an e2e/ path")
	}
	if !hasWarning(rows[0], WarnPubliclyLeaked) {
		t.Errorf("warnings = %v, TestPath must not suppress WarnPubliclyLeaked", rows[0].Warnings)
	}
}

func hasWarning(r model.Row, want string) bool {
	for _, w := range r.Warnings {
		if w == want {
			return true
		}
	}
	return false
}
