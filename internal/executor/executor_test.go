package executor

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Dilan-TH/gh-secretz/internal/model"
	"github.com/Dilan-TH/gh-secretz/internal/transport"
)

func fixedNow() time.Time {
	return time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
}

func newExec(t *testing.T, f *transport.Fake) (Executor, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "audit.jsonl")
	return Executor{T: f, Actor: "tester", AuditPath: p, Now: fixedNow}, p
}

func requestRow(alertNum int) model.Row {
	return model.Row{Request: &model.Request{
		Number: 1, AlertNumber: alertNum, Owner: "acme", Repo: "r", Reason: "revoked",
	}}
}

func alertRow(alertNum int) model.Row {
	return model.Row{Alert: &model.Alert{
		Number: alertNum, Owner: "acme", Repo: "r", State: "open",
	}}
}

func TestValidateMessage(t *testing.T) {
	if err := ValidateMessage(""); err == nil {
		t.Error("an empty message must be rejected, the API requires one")
	}
	if err := ValidateMessage("   "); err == nil {
		t.Error("a whitespace only message must be rejected")
	}
	if err := ValidateMessage(strings.Repeat("x", 2049)); err == nil {
		t.Error("a message over the 2048 character API limit must be rejected")
	}
	if err := ValidateMessage("looks fine"); err != nil {
		t.Errorf("ValidateMessage() error = %v", err)
	}
}

func TestValidateResolution(t *testing.T) {
	for _, ok := range ValidResolutions {
		if err := ValidateResolution(ok); err != nil {
			t.Errorf("ValidateResolution(%q) error = %v", ok, err)
		}
	}
	if err := ValidateResolution(""); err == nil {
		t.Error("an empty resolution must be rejected; there is no sensible default when closing")
	}
	if err := ValidateResolution("probably_fine"); err == nil {
		t.Error("an unknown resolution must be rejected")
	}
}

func TestApproveUsesAlertNumberInThePatchPath(t *testing.T) {
	// The single most important assertion in the suite. Request number 1
	// refers to alert 18, and the PATCH path must carry 18.
	f := transport.NewFake()
	e, _ := newExec(t, f)
	f.SetSingle("repos/acme/r/dismissal-requests/secret-scanning/18",
		`{"number":1,"status":"approved","resource_identifier":"18",
		  "data":[{"reason":"revoked","secret_type":"Password","alert_number":"18"}]}`)

	res, err := e.Run([]model.Row{requestRow(18)}, ActionApprove, "reviewed", "")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(f.Patches) != 1 {
		t.Fatalf("sent %d patches, want 1", len(f.Patches))
	}
	want := "repos/acme/r/dismissal-requests/secret-scanning/18"
	if f.Patches[0].Path != want {
		t.Errorf("patch path = %q, want %q", f.Patches[0].Path, want)
	}
	body, _ := json.Marshal(f.Patches[0].Body)
	if !strings.Contains(string(body), `"status":"approve"`) {
		t.Errorf("body = %s, want status approve", body)
	}
	if !strings.Contains(string(body), `"message":"reviewed"`) {
		t.Errorf("body = %s, want the message included", body)
	}
	if res[0].Outcome != OutcomeDone {
		t.Errorf("Outcome = %q, want %q", res[0].Outcome, OutcomeDone)
	}
}

func TestApproveVerifiesByRereading(t *testing.T) {
	// The API returned success but the record is still pending. That is
	// reported as unverified, never as done.
	f := transport.NewFake()
	e, _ := newExec(t, f)
	f.SetSingle("repos/acme/r/dismissal-requests/secret-scanning/18",
		`{"number":1,"status":"pending","resource_identifier":"18",
		  "data":[{"reason":"revoked","secret_type":"Password","alert_number":"18"}]}`)

	res, err := e.Run([]model.Row{requestRow(18)}, ActionApprove, "reviewed", "")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res[0].Outcome != OutcomeUnverified {
		t.Errorf("Outcome = %q, want %q when the re-read still shows pending",
			res[0].Outcome, OutcomeUnverified)
	}
}

func TestCloseReportsRequestCreatedWhenAlertStaysOpen(t *testing.T) {
	// The delegated dismissal flow may convert a close into a request. The
	// PATCH returns 200 either way, so only the re-read distinguishes them.
	f := transport.NewFake()
	e, _ := newExec(t, f)
	f.SetSingle("repos/acme/r/secret-scanning/alerts/11",
		`{"number":11,"state":"open","closure_request_comment":"cleanup"}`)

	res, err := e.Run([]model.Row{alertRow(11)}, ActionClose, "cleanup", "revoked")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res[0].Outcome != OutcomeRequestCreated {
		t.Errorf("Outcome = %q, want %q; the alert is still open with a closure marker",
			res[0].Outcome, OutcomeRequestCreated)
	}
}

func TestCloseSucceedsWhenAlertBecomesResolved(t *testing.T) {
	f := transport.NewFake()
	e, _ := newExec(t, f)
	f.SetSingle("repos/acme/r/secret-scanning/alerts/11",
		`{"number":11,"state":"resolved","resolution":"revoked"}`)

	res, err := e.Run([]model.Row{alertRow(11)}, ActionClose, "cleanup", "revoked")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res[0].Outcome != OutcomeDone {
		t.Errorf("Outcome = %q, want %q", res[0].Outcome, OutcomeDone)
	}
	body, _ := json.Marshal(f.Patches[0].Body)
	for _, want := range []string{`"state":"resolved"`, `"resolution":"revoked"`, `"resolution_comment":"cleanup"`} {
		if !strings.Contains(string(body), want) {
			t.Errorf("body = %s, want it to contain %s", body, want)
		}
	}
}

func TestPerItemFailureDoesNotAbortTheBatch(t *testing.T) {
	f := transport.NewFake()
	e, _ := newExec(t, f)
	f.PatchErr["repos/acme/r/dismissal-requests/secret-scanning/10"] =
		&transport.HTTPError{StatusCode: 403, Message: "Forbidden"}
	f.SetSingle("repos/acme/r/dismissal-requests/secret-scanning/11",
		`{"number":2,"status":"approved","resource_identifier":"11",
		  "data":[{"reason":"revoked","secret_type":"Password","alert_number":"11"}]}`)

	res, err := e.Run([]model.Row{requestRow(10), requestRow(11)}, ActionApprove, "reviewed", "")
	if err != nil {
		t.Fatalf("Run() should not fail the whole batch, got %v", err)
	}
	if len(res) != 2 {
		t.Fatalf("got %d results, want 2", len(res))
	}
	if res[0].Outcome != OutcomeForbidden {
		t.Errorf("first outcome = %q, want %q", res[0].Outcome, OutcomeForbidden)
	}
	if res[1].Outcome != OutcomeDone {
		t.Errorf("second outcome = %q, want %q; a 403 on one item must not stop the next",
			res[1].Outcome, OutcomeDone)
	}

	done, benign, failed := Summarise(res)
	if done != 1 || benign != 0 || failed != 1 {
		t.Errorf("Summarise() = (%d, %d, %d), want (1, 0, 1)", done, benign, failed)
	}
}

func TestGoneIsReportedSeparately(t *testing.T) {
	f := transport.NewFake()
	e, _ := newExec(t, f)
	f.PatchErr["repos/acme/r/dismissal-requests/secret-scanning/10"] =
		&transport.HTTPError{StatusCode: 404, Message: "Not Found"}

	res, err := e.Run([]model.Row{requestRow(10)}, ActionApprove, "reviewed", "")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res[0].Outcome != OutcomeGone {
		t.Errorf("Outcome = %q, want %q; the request was likely already resolved",
			res[0].Outcome, OutcomeGone)
	}
}

func TestAuditLogRecordsEveryWriteWithVerifiedState(t *testing.T) {
	f := transport.NewFake()
	e, auditPath := newExec(t, f)
	f.SetSingle("repos/acme/r/dismissal-requests/secret-scanning/18",
		`{"number":1,"status":"approved","resource_identifier":"18",
		  "data":[{"reason":"revoked","secret_type":"Password","alert_number":"18"}]}`)

	if _, err := e.Run([]model.Row{requestRow(18)}, ActionApprove, "reviewed", ""); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	b, err := os.ReadFile(auditPath)
	if err != nil {
		t.Fatalf("reading audit log: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 1 {
		t.Fatalf("audit log has %d lines, want 1", len(lines))
	}

	var entry map[string]any
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("audit line is not JSON: %v", err)
	}
	for _, k := range []string{"timestamp", "actor", "repo", "alert_number", "action", "message", "outcome"} {
		if _, ok := entry[k]; !ok {
			t.Errorf("audit entry missing key %q: %v", k, entry)
		}
	}
	if entry["actor"] != "tester" {
		t.Errorf("actor = %v, want tester", entry["actor"])
	}
	if entry["outcome"] != string(OutcomeDone) {
		t.Errorf("outcome = %v, want %q", entry["outcome"], OutcomeDone)
	}
	if entry["alert_number"].(float64) != 18 {
		t.Errorf("alert_number = %v, want 18", entry["alert_number"])
	}
}

func TestAuditLogAppendsAcrossRuns(t *testing.T) {
	f := transport.NewFake()
	e, auditPath := newExec(t, f)
	f.SetSingle("repos/acme/r/dismissal-requests/secret-scanning/18",
		`{"number":1,"status":"approved","resource_identifier":"18",
		  "data":[{"reason":"revoked","secret_type":"Password","alert_number":"18"}]}`)

	for i := 0; i < 2; i++ {
		if _, err := e.Run([]model.Row{requestRow(18)}, ActionApprove, "reviewed", ""); err != nil {
			t.Fatalf("Run() error = %v", err)
		}
	}
	b, _ := os.ReadFile(auditPath)
	if got := len(strings.Split(strings.TrimSpace(string(b)), "\n")); got != 2 {
		t.Errorf("audit log has %d lines, want 2; it must append not truncate", got)
	}
}

func TestRunRejectsBadMessageBeforeSendingAnything(t *testing.T) {
	f := transport.NewFake()
	e, _ := newExec(t, f)

	if _, err := e.Run([]model.Row{requestRow(18)}, ActionApprove, "", ""); err == nil {
		t.Fatal("Run() should reject an empty message")
	}
	if len(f.Patches) != 0 {
		t.Errorf("sent %d patches, want 0; validation must happen before any write", len(f.Patches))
	}
}

func TestRunRejectsBadResolutionBeforeSendingAnything(t *testing.T) {
	f := transport.NewFake()
	e, _ := newExec(t, f)

	if _, err := e.Run([]model.Row{alertRow(11)}, ActionClose, "cleanup", "nonsense"); err == nil {
		t.Fatal("Run() should reject an unknown resolution")
	}
	if len(f.Patches) != 0 {
		t.Errorf("sent %d patches, want 0", len(f.Patches))
	}
}

func TestRunSkipsRowsWithNoUsableTarget(t *testing.T) {
	f := transport.NewFake()
	e, _ := newExec(t, f)

	res, err := e.Run([]model.Row{{}}, ActionApprove, "reviewed", "")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(res) != 1 || res[0].Outcome != OutcomeError {
		t.Errorf("results = %+v, want one error outcome", res)
	}
	if len(f.Patches) != 0 {
		t.Errorf("sent %d patches for an empty row, want 0", len(f.Patches))
	}
}

func TestAlreadyReviewedIsBenignNotAFailure(t *testing.T) {
	// The API rejects a second review with 422. Overlapping runs and a
	// co-reviewer working the same queue both cause this, and the request is
	// already in its intended state, so a healthy run must not look failed.
	// Observed live: one session logged 249 successes and 100 of these.
	f := transport.NewFake()
	e, _ := newExec(t, f)
	f.PatchErr["repos/acme/r/dismissal-requests/secret-scanning/163"] =
		&transport.HTTPError{StatusCode: 422, Message: "Dismissal request has already been reviewed"}

	res, err := e.Run([]model.Row{requestRow(163)}, ActionApprove, "reviewed", "")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res[0].Outcome != OutcomeAlreadyReviewed {
		t.Errorf("Outcome = %q, want %q", res[0].Outcome, OutcomeAlreadyReviewed)
	}

	done, benign, failed := Summarise(res)
	if done != 0 || benign != 1 || failed != 0 {
		t.Errorf("Summarise() = (%d, %d, %d), want (0, 1, 0)", done, benign, failed)
	}
}

func TestOther422IsStillAnError(t *testing.T) {
	// Only the already reviewed case is benign. Any other 422 is a real
	// problem and must not be swallowed.
	f := transport.NewFake()
	e, _ := newExec(t, f)
	f.PatchErr["repos/acme/r/dismissal-requests/secret-scanning/10"] =
		&transport.HTTPError{StatusCode: 422, Message: "Validation failed: message is too long"}

	res, err := e.Run([]model.Row{requestRow(10)}, ActionApprove, "reviewed", "")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if res[0].Outcome != OutcomeError {
		t.Errorf("Outcome = %q, want %q", res[0].Outcome, OutcomeError)
	}
}
