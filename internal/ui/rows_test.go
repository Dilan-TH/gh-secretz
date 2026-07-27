package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/Dilan-TH/gh-secretz/internal/enrich"
	"github.com/Dilan-TH/gh-secretz/internal/model"
)

func TestFormatShowsAlertNumberNotRequestNumber(t *testing.T) {
	// The operator acts on alert numbers, so that is what the line must
	// lead with. Showing the request number would invite mismatched
	// cross referencing against the web UI.
	row := model.Row{
		Request: &model.Request{Number: 5, AlertNumber: 18, Owner: "acme", Repo: "alpha",
			Requester: "alice", Reason: "revoked", SecretTypeDisplay: "Password"},
		Alert: &model.Alert{Number: 18, Validity: "unknown"},
	}
	got := Format(row, 160)
	if !strings.Contains(got, "#18") {
		t.Errorf("Format() = %q, want it to show alert #18", got)
	}
	if strings.Contains(got, "#5") {
		t.Errorf("Format() = %q, must not show the request number as the identifier", got)
	}
	for _, want := range []string{"alpha", "alice", "revoked"} {
		if !strings.Contains(got, want) {
			t.Errorf("Format() = %q, want it to contain %q", got, want)
		}
	}
}

func TestFormatSuppressesUnknownValidity(t *testing.T) {
	// Most alerts report unknown. Printing it on every row costs a column
	// and tells the operator nothing.
	row := model.Row{
		Request: &model.Request{Number: 1, AlertNumber: 7, Repo: "alpha", Requester: "alice"},
		Alert:   &model.Alert{Number: 7, Validity: "unknown"},
	}
	if strings.Contains(Format(row, 160), "unknown") {
		t.Errorf("Format() = %q, should not print unknown validity", Format(row, 160))
	}
}

func TestFormatShoutsAboutLiveSecrets(t *testing.T) {
	row := model.Row{
		Request: &model.Request{Number: 1, AlertNumber: 7, Repo: "alpha", Requester: "alice"},
		Alert:   &model.Alert{Number: 7, Validity: "active"},
	}
	if !strings.Contains(Format(row, 160), "LIVE") {
		t.Errorf("Format() = %q, want an active secret marked LIVE", Format(row, 160))
	}
}

func TestFormatGivesTheCommentRemainingWidth(t *testing.T) {
	// The comment carries the requester's reasoning, which is the field the
	// operator most needs, so a wider terminal must show more of it.
	long := strings.Repeat("reasoning ", 40)
	row := model.Row{Request: &model.Request{Number: 1, AlertNumber: 7, Owner: "acme", Repo: "alpha",
		Requester: "alice", Reason: "revoked", SecretTypeDisplay: "Password", RequesterComment: long}}

	narrow := Format(row, 120)
	wide := Format(row, 220)

	if len(wide) <= len(narrow) {
		t.Errorf("a wider terminal should show more comment: narrow=%d wide=%d", len(narrow), len(wide))
	}
	if len(narrow) > 121 {
		t.Errorf("narrow line is %d chars, should respect a 120 column terminal", len(narrow))
	}
}

func TestFormatWidthZeroDoesNotTruncateComment(t *testing.T) {
	// Piped output goes to a file the operator will grep. Truncating there
	// is pure loss.
	long := strings.Repeat("x", 300)
	row := model.Row{Request: &model.Request{Number: 1, AlertNumber: 7, Repo: "alpha",
		Requester: "alice", RequesterComment: long}}

	got := Format(row, 0)
	if !strings.Contains(got, long) {
		t.Error("width 0 should emit the full comment untruncated")
	}
}

func TestFormatStaleClaimIsCalledOutInTheComment(t *testing.T) {
	row := model.Row{
		Request:  &model.Request{Number: 1, AlertNumber: 7, Repo: "alpha", Reason: "revoked"},
		Alert:    &model.Alert{Number: 7, Validity: "active"},
		Warnings: []string{enrich.WarnStaleClaim},
	}
	got := Format(row, 200)
	if !strings.Contains(got, "CLAIMS REVOKED") {
		t.Errorf("Format() = %q, want the contradiction spelled out", got)
	}
}

func TestFormatTriageRowShowsValidity(t *testing.T) {
	row := model.Row{Alert: &model.Alert{Number: 11, Owner: "acme", Repo: "alpha",
		SecretTypeDisplay: "Password", Validity: "active", State: "open"}}
	got := Format(row, 160)
	if !strings.Contains(got, "#11") {
		t.Errorf("Format() = %q, want #11", got)
	}
	// Closing a still active secret is the mistake worth making loud.
	if !strings.Contains(got, "LIVE") {
		t.Errorf("Format() = %q, want validity shown for triage rows", got)
	}
}

func TestWarnGlyphMarksStaleClaims(t *testing.T) {
	warned := model.Row{
		Request:  &model.Request{Number: 1, AlertNumber: 7, Repo: "alpha"},
		Warnings: []string{enrich.WarnStaleClaim},
	}
	clean := model.Row{Request: &model.Request{Number: 2, AlertNumber: 8, Repo: "alpha"}}

	if WarnGlyph(warned) == WarnGlyph(clean) {
		t.Error("a warned row must be visually distinct from a clean one")
	}
	if strings.TrimSpace(WarnGlyph(clean)) != "" {
		t.Errorf("WarnGlyph(clean) = %q, want blank", WarnGlyph(clean))
	}
}

func TestTestGlyphMarksTestPathRows(t *testing.T) {
	testRow := model.Row{Request: &model.Request{Number: 1, AlertNumber: 7, Repo: "alpha"}, TestPath: true}
	clean := model.Row{Request: &model.Request{Number: 2, AlertNumber: 8, Repo: "alpha"}}

	if TestGlyph(testRow) == TestGlyph(clean) {
		t.Error("a test-path row must be visually distinct from a clean one")
	}
	if strings.TrimSpace(TestGlyph(clean)) != "" {
		t.Errorf("TestGlyph(clean) = %q, want blank", TestGlyph(clean))
	}
}

func TestTestGlyphIsDistinctFromWarnGlyph(t *testing.T) {
	// A test-path match must never look like a real warning: the two glyphs
	// must never collide on the same character.
	row := model.Row{
		Request:  &model.Request{Number: 1, AlertNumber: 7, Repo: "alpha"},
		Warnings: []string{enrich.WarnPubliclyLeaked},
		TestPath: true,
	}
	if WarnGlyph(row) == TestGlyph(row) {
		t.Errorf("WarnGlyph and TestGlyph must not render identically, both were %q", WarnGlyph(row))
	}
}

func TestFormatShowsTestGlyphForTestPathRows(t *testing.T) {
	row := model.Row{
		Request:  &model.Request{Number: 1, AlertNumber: 7, Owner: "acme", Repo: "alpha"},
		Alert:    &model.Alert{Number: 7, Path: "test/fixtures/creds.yaml"},
		TestPath: true,
	}
	got := Format(row, 160)
	if !strings.Contains(got, "T") {
		t.Errorf("Format() = %q, want the test-path glyph present", got)
	}
}

func TestFormatIsSingleLine(t *testing.T) {
	// The pager assumes one row per line. A newline would corrupt the view.
	row := model.Row{Request: &model.Request{Number: 1, AlertNumber: 7, Owner: "acme", Repo: "alpha",
		Requester: "alice", Reason: "revoked", RequesterComment: "line one\nline two"}}
	if strings.Contains(Format(row, 160), "\n") {
		t.Error("Format() must not emit a newline")
	}
}

func TestFormatDetailShowsBothNumbersAndFullComment(t *testing.T) {
	// The detail pane exists to show what the list had to cut, so nothing
	// here may be abbreviated.
	long := strings.Repeat("full reasoning ", 30)
	row := model.Row{
		Request: &model.Request{Number: 5, AlertNumber: 18, Owner: "acme", Repo: "alpha",
			FullName: "acme/alpha", Requester: "alice", Reason: "revoked",
			SecretTypeDisplay: "Password", Status: "pending", RequesterComment: long,
			CreatedAt: time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC),
			ExpiresAt: time.Date(2026, 7, 31, 10, 0, 0, 0, time.UTC),
			HTMLURL:   "https://example.test/alert/18"},
		Alert: &model.Alert{Number: 18, State: "open", Validity: "active",
			SecretType: "password", SecretTypeDisplay: "Password"},
	}

	joined := strings.Join(FormatDetail(row), "\n")
	for _, want := range []string{
		"acme/alpha", "alert number", "18", "request number", "5",
		"alice", "revoked", "pending", long, "https://example.test/alert/18",
		"active", "password",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("FormatDetail() missing %q, got:\n%s", want, joined)
		}
	}
}

func TestFormatDetailNotesWhenAlertUnreadable(t *testing.T) {
	row := model.Row{
		Request:  &model.Request{Number: 1, AlertNumber: 99, Repo: "alpha", FullName: "acme/alpha"},
		Warnings: []string{enrich.WarnNoAlert},
	}
	joined := strings.Join(FormatDetail(row), "\n")
	if !strings.Contains(joined, "could not be read") {
		t.Errorf("FormatDetail() should say the alert was unreachable, got:\n%s", joined)
	}
	if !strings.Contains(joined, enrich.WarnNoAlert) {
		t.Errorf("FormatDetail() should list warnings, got:\n%s", joined)
	}
}

func TestFormatDetailShowsTestPathSeparatelyFromWarnings(t *testing.T) {
	row := model.Row{
		Request:  &model.Request{Number: 1, AlertNumber: 7, Repo: "alpha", FullName: "acme/alpha"},
		Alert:    &model.Alert{Number: 7, State: "open", PubliclyLeaked: true, Path: "e2e/token.go"},
		Warnings: []string{enrich.WarnPubliclyLeaked},
		TestPath: true,
	}
	joined := strings.Join(FormatDetail(row), "\n")
	if !strings.Contains(joined, "test path") {
		t.Errorf("FormatDetail() missing a test path line, got:\n%s", joined)
	}
	// The test-path indicator must not be folded into the warnings line.
	warningsLine := ""
	for _, l := range FormatDetail(row) {
		if strings.Contains(l, "warnings:") {
			warningsLine = l
		}
	}
	if strings.Contains(warningsLine, "test") {
		t.Errorf("warnings line = %q, must not mention the test-path signal", warningsLine)
	}
}

func TestFormatDetailHandlesTriageRow(t *testing.T) {
	row := model.Row{Alert: &model.Alert{Number: 11, Owner: "acme", Repo: "alpha",
		State: "open", Validity: "unknown", SecretType: "password",
		SecretTypeDisplay: "Password", HTMLURL: "https://example.test/alert/11"}}

	joined := strings.Join(FormatDetail(row), "\n")
	for _, want := range []string{"acme/alpha", "11", "Password", "https://example.test/alert/11"} {
		if !strings.Contains(joined, want) {
			t.Errorf("FormatDetail() missing %q, got:\n%s", want, joined)
		}
	}
}

func TestFormatNeverExceedsTerminalWidth(t *testing.T) {
	// The invariant that a hardcoded minimum comment width previously broke:
	// at 120 columns the line came out 133 wide and wrapped, corrupting the
	// pager's one-row-per-line assumption.
	row := model.Row{
		Request: &model.Request{Number: 5, AlertNumber: 123456, Owner: "acme",
			Repo: "a-very-long-repository-name-indeed", Requester: "a-long-requester-login",
			Reason: "used_in_tests", SecretTypeDisplay: "Amazon AWS Temporary Access Key ID",
			RequesterComment: strings.Repeat("reasoning ", 60)},
		Alert: &model.Alert{Number: 123456, Validity: "active"},
	}

	for width := 60; width <= 400; width += 7 {
		got := Format(row, width)
		// The view prepends a 4 character checkbox, which the budget reserves.
		if total := len(got) + checkboxWidth; total > width {
			t.Errorf("width %d produced a %d char line (with checkbox), which would wrap:\n%q",
				width, total, got)
		}
	}
}

func TestFormatKeepsCommentVisibleAtNarrowWidths(t *testing.T) {
	// Narrow terminals must sacrifice the less informative columns rather
	// than the comment, which is the field being read.
	row := model.Row{Request: &model.Request{Number: 1, AlertNumber: 7, Owner: "acme",
		Repo: "alpha", Requester: "alice", Reason: "revoked",
		SecretTypeDisplay: "Amazon AWS Secret Access Key",
		RequesterComment:  "rotated in vault on tuesday"}}

	for _, width := range []int{100, 120, 140} {
		got := Format(row, width)
		if !strings.Contains(got, "rotated in vault") {
			t.Errorf("width %d dropped the comment entirely: %q", width, got)
		}
	}
}
