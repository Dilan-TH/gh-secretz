package ui

import (
	"strings"
	"testing"

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
	got := Format(row)
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

func TestFormatTriageRowHasNoRequester(t *testing.T) {
	row := model.Row{Alert: &model.Alert{Number: 11, Owner: "acme", Repo: "alpha",
		SecretTypeDisplay: "Password", Validity: "active", State: "open"}}
	got := Format(row)
	if !strings.Contains(got, "#11") {
		t.Errorf("Format() = %q, want #11", got)
	}
	// Validity is surfaced because closing a still active secret is the
	// mistake worth making loud.
	if !strings.Contains(got, "active") {
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

func TestFormatIsSingleLine(t *testing.T) {
	// The pager assumes one row per line. A newline would corrupt the view.
	row := model.Row{Request: &model.Request{Number: 1, AlertNumber: 7, Owner: "acme", Repo: "alpha",
		Requester: "alice", Reason: "revoked", RequesterComment: "line one\nline two"}}
	if strings.Contains(Format(row), "\n") {
		t.Error("Format() must not emit a newline")
	}
}
