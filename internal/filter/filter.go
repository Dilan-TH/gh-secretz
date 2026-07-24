// Package filter composes the row predicates used by every command, and
// enforces that write commands always carry an explicit scope.
package filter

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Dilan-TH/gh-secretz/internal/model"
)

// ErrNoFilter is returned when a write command is invoked with no scope.
var ErrNoFilter = errors.New("no filter given")

// Spec is the set of filters supplied on the command line. Filters are
// conjunctive: a row must satisfy every non empty field.
type Spec struct {
	Repo       string
	Requester  string
	Reason     string
	SecretType string
	// OnlyWarned keeps rows carrying at least one warning.
	OnlyWarned bool
}

// IsEmpty reports whether no filter at all was supplied.
func (s Spec) IsEmpty() bool {
	return s.Repo == "" && s.Requester == "" && s.Reason == "" && s.SecretType == "" && !s.OnlyWarned
}

// Names lists the active filters, for printing in a run header.
func (s Spec) Names() []string {
	var out []string
	if s.Repo != "" {
		out = append(out, "repo="+s.Repo)
	}
	if s.Requester != "" {
		out = append(out, "requester="+s.Requester)
	}
	if s.Reason != "" {
		out = append(out, "reason="+s.Reason)
	}
	if s.SecretType != "" {
		out = append(out, "secret-type="+s.SecretType)
	}
	if s.OnlyWarned {
		out = append(out, "only-warned")
	}
	return out
}

// Validate enforces the scope requirement. requireFilter is true for write
// commands, so that a bulk approval always has a stated scope, and false for
// read only commands, where requiring a filter would be friction with no
// safety benefit.
func (s Spec) Validate(requireFilter bool) error {
	if !requireFilter || !s.IsEmpty() {
		return nil
	}
	return fmt.Errorf(
		"%w: this command changes state, so it needs an explicit scope. "+
			"Supply at least one of --repo, --requester, --reason, --secret-type, "+
			"--only-warned, or --all to cover the whole queue deliberately",
		ErrNoFilter)
}

// Apply returns the rows satisfying every active filter.
func (s Spec) Apply(rows []model.Row) []model.Row {
	out := make([]model.Row, 0, len(rows))
	for _, r := range rows {
		if s.matches(r) {
			out = append(out, r)
		}
	}
	return out
}

func (s Spec) matches(r model.Row) bool {
	if s.Repo != "" && !strings.EqualFold(repoOf(r), s.Repo) {
		return false
	}
	if s.Requester != "" && !strings.EqualFold(requesterOf(r), s.Requester) {
		return false
	}
	if s.Reason != "" && !strings.EqualFold(reasonOf(r), s.Reason) {
		return false
	}
	if s.SecretType != "" && !matchesSecretType(r, s.SecretType) {
		return false
	}
	if s.OnlyWarned && len(r.Warnings) == 0 {
		return false
	}
	return true
}

func repoOf(r model.Row) string {
	if r.Request != nil {
		return r.Request.Repo
	}
	if r.Alert != nil {
		return r.Alert.Repo
	}
	return ""
}

func requesterOf(r model.Row) string {
	if r.Request != nil {
		return r.Request.Requester
	}
	return ""
}

func reasonOf(r model.Row) string {
	if r.Request != nil {
		return r.Request.Reason
	}
	return ""
}

// matchesSecretType accepts either the API slug or the display name, because
// requests report display names ("Password") while alerts report slugs
// ("password") and an operator should not have to know which.
func matchesSecretType(r model.Row, want string) bool {
	candidates := []string{}
	if r.Alert != nil {
		candidates = append(candidates, r.Alert.SecretType, r.Alert.SecretTypeDisplay)
	}
	if r.Request != nil {
		candidates = append(candidates, r.Request.SecretTypeDisplay, slugify(r.Request.SecretTypeDisplay))
	}
	for _, c := range candidates {
		if c != "" && strings.EqualFold(c, want) {
			return true
		}
	}
	return false
}

// slugify converts a display name to the slug spelling the alerts API uses,
// so that "HTTP bearer authentication header" also matches
// "http_bearer_authentication_header".
func slugify(display string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(display)), " ", "_")
}
