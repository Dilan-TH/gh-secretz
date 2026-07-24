package filter

import (
	"errors"
	"strings"
	"testing"

	"github.com/Dilan-TH/gh-secretz/internal/enrich"
	"github.com/Dilan-TH/gh-secretz/internal/model"
)

func rows() []model.Row {
	return []model.Row{
		{
			Request: &model.Request{Number: 1, AlertNumber: 10, Owner: "acme", Repo: "alpha",
				Requester: "alice", Reason: "revoked", SecretTypeDisplay: "Password"},
			Alert: &model.Alert{Number: 10, SecretType: "password"},
		},
		{
			Request: &model.Request{Number: 2, AlertNumber: 11, Owner: "acme", Repo: "beta",
				Requester: "bob", Reason: "used_in_tests", SecretTypeDisplay: "JWT Header"},
			Alert:    &model.Alert{Number: 11, SecretType: "jwt_header"},
			Warnings: []string{enrich.WarnStaleClaim},
		},
		{
			Request: &model.Request{Number: 3, AlertNumber: 12, Owner: "acme", Repo: "alpha",
				Requester: "bob", Reason: "revoked", SecretTypeDisplay: "Password"},
			Alert: &model.Alert{Number: 12, SecretType: "password"},
		},
	}
}

func TestValidateRefusesEmptyFilterForWrites(t *testing.T) {
	err := Spec{}.Validate(true)
	if !errors.Is(err, ErrNoFilter) {
		t.Fatalf("err = %v, want ErrNoFilter", err)
	}
	// The message must tell the operator what they can filter on, since the
	// refusal is otherwise a dead end.
	if !strings.Contains(err.Error(), "--repo") {
		t.Errorf("error %q should list the available filters", err.Error())
	}
}

func TestValidateAllowsEmptyFilterForReads(t *testing.T) {
	if err := (Spec{}).Validate(false); err != nil {
		t.Errorf("read only commands may run bare, got %v", err)
	}
}

func TestValidateAcceptsAnySingleFilter(t *testing.T) {
	for _, s := range []Spec{
		{Repo: "alpha"},
		{Requester: "alice"},
		{Reason: "revoked"},
		{SecretType: "password"},
		{OnlyWarned: true},
	} {
		if err := s.Validate(true); err != nil {
			t.Errorf("Spec %+v should satisfy the filter requirement, got %v", s, err)
		}
	}
}

func TestApplyRepo(t *testing.T) {
	got := Spec{Repo: "alpha"}.Apply(rows())
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2", len(got))
	}
	for _, r := range got {
		if r.Request.Repo != "alpha" {
			t.Errorf("row repo = %q, want alpha", r.Request.Repo)
		}
	}
}

func TestApplyRequesterAndReasonCombine(t *testing.T) {
	// Filters are conjunctive. bob has two rows but only one is revoked.
	got := Spec{Requester: "bob", Reason: "revoked"}.Apply(rows())
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	if got[0].Request.Number != 3 {
		t.Errorf("matched request %d, want 3", got[0].Request.Number)
	}
}

func TestApplySecretTypeMatchesSlugOrDisplayName(t *testing.T) {
	// Requests carry the display name "Password" while alerts carry the slug
	// "password". Either spelling must work from the command line.
	bySlug := Spec{SecretType: "password"}.Apply(rows())
	byDisplay := Spec{SecretType: "Password"}.Apply(rows())
	if len(bySlug) != 2 {
		t.Errorf("slug match got %d rows, want 2", len(bySlug))
	}
	if len(byDisplay) != 2 {
		t.Errorf("display name match got %d rows, want 2", len(byDisplay))
	}
}

func TestApplySecretTypeIsCaseInsensitive(t *testing.T) {
	if got := (Spec{SecretType: "PASSWORD"}).Apply(rows()); len(got) != 2 {
		t.Errorf("got %d rows, want 2", len(got))
	}
}

func TestApplyOnlyWarned(t *testing.T) {
	got := Spec{OnlyWarned: true}.Apply(rows())
	if len(got) != 1 {
		t.Fatalf("got %d rows, want 1", len(got))
	}
	if got[0].Request.Number != 2 {
		t.Errorf("matched request %d, want 2", got[0].Request.Number)
	}
}

func TestApplyHandlesTriageRowsWithNoRequest(t *testing.T) {
	// Triage rows carry an alert and no request. Filtering must not panic.
	triage := []model.Row{{Alert: &model.Alert{Number: 5, Repo: "alpha", Owner: "acme", SecretType: "password"}}}
	if got := (Spec{Repo: "alpha"}).Apply(triage); len(got) != 1 {
		t.Errorf("got %d rows, want 1", len(got))
	}
	if got := (Spec{Requester: "alice"}).Apply(triage); len(got) != 0 {
		t.Errorf("got %d rows, want 0; a triage row has no requester", len(got))
	}
}

func TestNamesListsActiveFilters(t *testing.T) {
	got := Spec{Repo: "alpha", Reason: "revoked"}.Names()
	if len(got) != 2 {
		t.Fatalf("Names() = %v, want 2 entries", got)
	}
}
