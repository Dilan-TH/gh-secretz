// Package executor performs every state change gh-secretz makes.
//
// The central rule is that a write is never reported as successful because
// the API returned 200. Delegated alert dismissal can convert a close into a
// dismissal request, and that conversion also returns 200. Only re-reading
// the resource distinguishes the two, so every write is followed by a
// verification read and the outcome comes from observed state.
package executor

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Dilan-TH/gh-secretz/internal/model"
	"github.com/Dilan-TH/gh-secretz/internal/transport"
)

// Action is the operation being performed.
type Action string

const (
	ActionApprove Action = "approve"
	ActionDeny    Action = "deny"
	ActionClose   Action = "close"
)

// Outcome is the verified result of one write.
type Outcome string

const (
	// OutcomeDone means the verification read confirmed the change.
	OutcomeDone Outcome = "done"
	// OutcomeRequestCreated means a close became a dismissal request instead
	// of closing, which the delegated flow does silently.
	OutcomeRequestCreated Outcome = "request-created"
	// OutcomeForbidden means the operator lacks permission on that repo.
	OutcomeForbidden Outcome = "forbidden"
	// OutcomeGone means the target no longer exists, usually because it was
	// already resolved by someone else.
	OutcomeGone Outcome = "gone"
	// OutcomeAlreadyReviewed means someone, possibly this operator in an
	// earlier run, already reviewed the request. The desired end state holds,
	// so this is benign rather than a failure.
	OutcomeAlreadyReviewed Outcome = "already-reviewed"
	// OutcomeUnverified means the write returned success but the re-read did
	// not show the expected state.
	OutcomeUnverified Outcome = "unverified"
	// OutcomeError is any other failure.
	OutcomeError Outcome = "error"
)

// ValidResolutions are the resolutions the alerts API accepts.
var ValidResolutions = []string{"revoked", "false_positive", "used_in_tests", "wont_fix"}

// maxMessage is the API's documented limit.
const maxMessage = 2048

// Result is the verified outcome for one row.
type Result struct {
	Key         string
	Repo        string
	AlertNumber int
	Action      Action
	Outcome     Outcome
	Detail      string
}

// Executor performs writes sequentially and records them.
type Executor struct {
	T         transport.Transport
	Actor     string
	AuditPath string
	// Now is injectable so audit timestamps are deterministic in tests.
	Now func() time.Time
	// Progress, when set, is called once per row after it has been written
	// and verified, with the count completed so far and the batch size.
	// Optional, so callers with nothing to render can leave it nil.
	Progress func(done, total int)
}

// ValidateMessage enforces the API's required, bounded message.
func ValidateMessage(m string) error {
	if strings.TrimSpace(m) == "" {
		return fmt.Errorf("a message is required and cannot be blank")
	}
	if len(m) > maxMessage {
		return fmt.Errorf("message is %d characters, over the API limit of %d", len(m), maxMessage)
	}
	return nil
}

// ValidateResolution requires an explicit resolution. There is deliberately
// no default: unlike an approval, a close has no requester stated reason to
// inherit, so guessing would attribute a justification nobody gave.
func ValidateResolution(r string) error {
	for _, v := range ValidResolutions {
		if r == v {
			return nil
		}
	}
	return fmt.Errorf("resolution %q is not one of %s", r, strings.Join(ValidResolutions, ", "))
}

// Run performs act against every row, sequentially.
//
// Validation happens before any write, so a bad message or resolution cannot
// leave a batch half applied. Per item failures do not abort the batch; they
// are collected so the caller can report them and exit non zero.
func (e Executor) Run(rows []model.Row, act Action, message, resolution string) ([]Result, error) {
	if err := ValidateMessage(message); err != nil {
		return nil, err
	}
	if act == ActionClose {
		if err := ValidateResolution(resolution); err != nil {
			return nil, err
		}
	}

	now := e.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}

	results := make([]Result, 0, len(rows))
	for i, row := range rows {
		res := e.one(row, act, message, resolution)
		if err := e.appendAudit(now(), res, message); err != nil {
			// An audit failure must be visible but must not silently discard
			// the record of a write that already happened.
			res.Detail = strings.TrimSpace(res.Detail + " (audit write failed: " + err.Error() + ")")
		}
		results = append(results, res)
		if e.Progress != nil {
			e.Progress(i+1, len(rows))
		}
	}
	return results, nil
}

func (e Executor) one(row model.Row, act Action, message, resolution string) Result {
	owner, repo, alertNum, ok := target(row)
	if !ok {
		return Result{Action: act, Outcome: OutcomeError,
			Detail: "row carries neither a request nor an alert, nothing to act on"}
	}

	res := Result{
		Key:         fmt.Sprintf("%s/%s#%d", owner, repo, alertNum),
		Repo:        fmt.Sprintf("%s/%s", owner, repo),
		AlertNumber: alertNum,
		Action:      act,
	}

	var path string
	var body any
	switch act {
	case ActionApprove, ActionDeny:
		// The path segment is the alert number, not the request number.
		path = fmt.Sprintf("repos/%s/%s/dismissal-requests/secret-scanning/%d", owner, repo, alertNum)
		status := "approve"
		if act == ActionDeny {
			status = "deny"
		}
		body = map[string]string{"status": status, "message": message}
	case ActionClose:
		path = fmt.Sprintf("repos/%s/%s/secret-scanning/alerts/%d", owner, repo, alertNum)
		body = map[string]string{
			"state":              "resolved",
			"resolution":         resolution,
			"resolution_comment": message,
		}
	default:
		res.Outcome = OutcomeError
		res.Detail = fmt.Sprintf("unknown action %q", act)
		return res
	}

	if err := e.T.Patch(path, body, nil); err != nil {
		switch {
		case transport.IsForbidden(err):
			res.Outcome = OutcomeForbidden
		case transport.IsNotFound(err):
			res.Outcome = OutcomeGone
		case isAlreadyReviewed(err):
			// The API rejects a second review with 422. Overlapping runs and
			// a co-reviewer working the same queue both cause this, and in
			// both cases the request is already in its intended state, so
			// reporting it as an error would make a healthy run look failed.
			res.Outcome = OutcomeAlreadyReviewed
		default:
			res.Outcome = OutcomeError
		}
		res.Detail = err.Error()
		return res
	}

	return e.verify(res, owner, repo, alertNum, act)
}

// verify re-reads the resource. This is the reason the executor exists as its
// own layer: the HTTP status of the write tells us nothing about whether the
// intended change occurred.
func (e Executor) verify(res Result, owner, repo string, alertNum int, act Action) Result {
	switch act {
	case ActionApprove, ActionDeny:
		var got struct {
			Status string `json:"status"`
		}
		path := fmt.Sprintf("repos/%s/%s/dismissal-requests/secret-scanning/%d", owner, repo, alertNum)
		if err := e.T.GetJSON(path, &got); err != nil {
			res.Outcome = OutcomeUnverified
			res.Detail = "write returned success but the verification read failed: " + err.Error()
			return res
		}
		want := "approved"
		if act == ActionDeny {
			want = "denied"
		}
		if got.Status == want {
			res.Outcome = OutcomeDone
			return res
		}
		res.Outcome = OutcomeUnverified
		res.Detail = fmt.Sprintf("write returned success but the request still reports status %q, expected %q",
			got.Status, want)
		return res

	case ActionClose:
		var got struct {
			State                 string `json:"state"`
			Resolution            string `json:"resolution"`
			ClosureRequestComment string `json:"closure_request_comment"`
		}
		path := fmt.Sprintf("repos/%s/%s/secret-scanning/alerts/%d", owner, repo, alertNum)
		if err := e.T.GetJSON(path, &got); err != nil {
			res.Outcome = OutcomeUnverified
			res.Detail = "write returned success but the verification read failed: " + err.Error()
			return res
		}
		if got.State == "resolved" {
			res.Outcome = OutcomeDone
			res.Detail = "resolution " + got.Resolution
			return res
		}
		// Still open with a closure marker means the delegated flow turned
		// the close into a request from us.
		if strings.TrimSpace(got.ClosureRequestComment) != "" {
			res.Outcome = OutcomeRequestCreated
			res.Detail = "the alert is still open and now carries a closure request, so this role does not close directly"
			return res
		}
		res.Outcome = OutcomeUnverified
		res.Detail = fmt.Sprintf("write returned success but the alert still reports state %q", got.State)
		return res
	}

	res.Outcome = OutcomeError
	return res
}

func target(row model.Row) (owner, repo string, alertNum int, ok bool) {
	switch {
	case row.Request != nil:
		return row.Request.Owner, row.Request.Repo, row.Request.AlertNumber, true
	case row.Alert != nil:
		return row.Alert.Owner, row.Alert.Repo, row.Alert.Number, true
	default:
		return "", "", 0, false
	}
}

type auditEntry struct {
	Timestamp   string `json:"timestamp"`
	Actor       string `json:"actor"`
	Repo        string `json:"repo"`
	AlertNumber int    `json:"alert_number"`
	Action      string `json:"action"`
	Message     string `json:"message"`
	Outcome     string `json:"outcome"`
	Detail      string `json:"detail,omitempty"`
}

// appendAudit writes one JSONL record per attempted write, recording the
// verified outcome rather than the HTTP status.
func (e Executor) appendAudit(ts time.Time, res Result, message string) error {
	if e.AuditPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(e.AuditPath), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(e.AuditPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()

	b, err := json.Marshal(auditEntry{
		Timestamp:   ts.Format(time.RFC3339),
		Actor:       e.Actor,
		Repo:        res.Repo,
		AlertNumber: res.AlertNumber,
		Action:      string(res.Action),
		Message:     message,
		Outcome:     string(res.Outcome),
		Detail:      res.Detail,
	})
	if err != nil {
		return err
	}
	_, err = f.Write(append(b, '\n'))
	return err
}

// isAlreadyReviewed reports whether the API refused because the request had
// already been reviewed. The status is 422 and the reason is only in the
// message, so this matches on the text.
func isAlreadyReviewed(err error) bool {
	var he *transport.HTTPError
	if !errors.As(err, &he) || he.StatusCode != http.StatusUnprocessableEntity {
		return false
	}
	return strings.Contains(strings.ToLower(he.Message), "already been reviewed")
}

// Summarise buckets results into verified successes, benign outcomes that
// need no action, and genuine failures.
//
// Already reviewed is benign: the request holds its intended state, so a run
// that hits it should not report failure or exit non zero.
func Summarise(rs []Result) (done, benign, failed int) {
	for _, r := range rs {
		switch r.Outcome {
		case OutcomeDone:
			done++
		case OutcomeAlreadyReviewed:
			benign++
		default:
			failed++
		}
	}
	return done, benign, failed
}
