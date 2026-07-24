// Package ui renders the interactive multi select. All state lives in
// internal/selection, so this package holds formatting and key mapping only.
package ui

import (
	"fmt"
	"strings"

	"github.com/Dilan-TH/gh-secretz/internal/enrich"
	"github.com/Dilan-TH/gh-secretz/internal/model"
)

// Mode selects which destructive action the screen exposes.
type Mode int

const (
	ModeReview Mode = iota
	ModeTriage
)

// Preferred column widths. The comment has no fixed width: it carries the
// requester's actual reasoning, which is the field an operator needs most, so
// it receives whatever space is left over.
const (
	colAlert = 7
	// checkboxWidth accounts for the "[x] " the view prepends.
	checkboxWidth = 4
	// wantComment is how much room the comment should get before the layout
	// stops sacrificing other columns for it.
	wantComment = 48
)

// cols holds the widths that flex when the terminal is narrow.
type cols struct {
	repo, requester, reason, secret, validity int
}

// used is the width consumed before the comment begins, including the
// checkbox the view prepends and the two space gap in front of the comment.
func (c cols) used() int {
	return checkboxWidth + 2 + c.repo + 1 + colAlert + 1 +
		c.requester + 1 + c.reason + 1 + c.secret + 1 + c.validity + 2
}

// layoutFor budgets the columns for a terminal of the given width and returns
// the room left for the comment. A width of zero or less means unlimited, and
// yields a comment room of minus one.
//
// When space is tight, columns shrink in order of how little they tell the
// operator, so the comment keeps its space rather than the line overflowing.
func layoutFor(width int) (cols, int) {
	c := cols{repo: 24, requester: 18, reason: 14, secret: 30, validity: 7}
	if width <= 0 {
		return c, -1
	}

	order := []struct {
		p   *int
		min int
	}{
		{&c.validity, 0},
		{&c.secret, 16},
		{&c.requester, 10},
		{&c.repo, 14},
		{&c.reason, 8},
	}

	for {
		room := width - c.used()
		if room >= wantComment {
			return c, room
		}
		shrunk := false
		for _, o := range order {
			if *o.p > o.min {
				*o.p--
				shrunk = true
				break
			}
		}
		if !shrunk {
			if room < 0 {
				room = 0
			}
			return c, room
		}
	}
}

// WarnGlyph returns a marker for rows carrying warnings, blank otherwise.
func WarnGlyph(r model.Row) string {
	if len(r.Warnings) == 0 {
		return " "
	}
	return "!"
}

// fields pulls the display values out of a row, tolerating either half being
// absent: triage rows have no request, and enrichment may not have reached
// the alert.
func fields(r model.Row) (repo string, ident int, who, reason, secret, validity, note string) {
	if r.Request != nil {
		repo = r.Request.Repo
		ident = r.Request.AlertNumber
		who = r.Request.Requester
		reason = r.Request.Reason
		secret = r.Request.SecretTypeDisplay
		note = r.Request.RequesterComment
	}
	if r.Alert != nil {
		if repo == "" {
			repo = r.Alert.Repo
			ident = r.Alert.Number
		}
		if secret == "" {
			secret = r.Alert.SecretTypeDisplay
		}
		validity = r.Alert.Validity
	}
	return repo, ident, who, reason, secret, validity, note
}

// displayValidity suppresses the "unknown" that most alerts report. Printing
// it on every row costs a column and tells the operator nothing, whereas an
// actually live secret is worth shouting about.
func displayValidity(v string) string {
	switch v {
	case "", "unknown":
		return ""
	case "active":
		return "LIVE"
	default:
		return v
	}
}

// Format renders one row as a single line, sized to width.
//
// width is the full terminal width. A width of zero or less means unlimited,
// which is what piped output wants: truncating a comment in a file the
// operator is going to grep is pure loss.
func Format(r model.Row, width int) string {
	repo, ident, who, reason, secret, validity, note := fields(r)
	c, room := layoutFor(width)

	head := fmt.Sprintf("%s %-*s #%-*d %-*s %-*s %-*s",
		WarnGlyph(r),
		c.repo, truncate(repo, c.repo),
		colAlert-1, ident,
		c.requester, truncate(who, c.requester),
		c.reason, truncate(reason, c.reason),
		c.secret, truncate(secret, c.secret))

	// A zero width validity column means the terminal was too narrow to
	// afford it, so it is dropped rather than squeezing the comment.
	if c.validity > 0 {
		head += fmt.Sprintf(" %-*s", c.validity, displayValidity(validity))
	}

	if strings.Contains(strings.Join(r.Warnings, ","), enrich.WarnStaleClaim) {
		note = "CLAIMS REVOKED BUT SECRET IS LIVE. " + note
	}

	if note != "" && room != 0 {
		if room > 0 {
			note = truncate(note, room)
		}
		head += "  " + note
	}

	// Collapse any newline, since the pager assumes one row per line.
	head = strings.ReplaceAll(strings.ReplaceAll(head, "\n", " "), "\r", " ")

	// Final clamp. Below roughly 70 columns even the minimum column widths
	// do not fit, so the line is cut outright. This guarantees the pager's
	// one-row-per-line assumption holds without depending on the column
	// arithmetic above being exhaustive.
	if width > 0 {
		if limit := width - checkboxWidth; limit >= 0 && len(head) > limit {
			head = truncate(head, limit)
		}
	}
	return head
}

// FormatDetail returns the full, untruncated view of one row for the detail
// pane. Nothing here is abbreviated, because the whole point of the pane is
// reading what the list had to cut.
func FormatDetail(r model.Row) []string {
	var out []string
	add := func(label, value string) {
		if value != "" {
			out = append(out, fmt.Sprintf("  %-18s %s", label, value))
		}
	}

	if r.Request != nil {
		q := r.Request
		add("repository", q.FullName)
		out = append(out, fmt.Sprintf("  %-18s %d", "alert number", q.AlertNumber))
		out = append(out, fmt.Sprintf("  %-18s %d", "request number", q.Number))
		add("requester", q.Requester)
		add("stated reason", q.Reason)
		add("secret type", q.SecretTypeDisplay)
		add("request status", q.Status)
		if !q.CreatedAt.IsZero() {
			add("requested", q.CreatedAt.Format("2006-01-02 15:04 MST"))
		}
		if !q.ExpiresAt.IsZero() {
			add("expires", q.ExpiresAt.Format("2006-01-02 15:04 MST"))
		}
		add("link", q.HTMLURL)
		if q.RequesterComment != "" {
			out = append(out, "", "  requester comment:", "    "+q.RequesterComment)
		}
	}

	if r.Alert != nil {
		a := r.Alert
		if r.Request == nil {
			add("repository", a.Owner+"/"+a.Repo)
			out = append(out, fmt.Sprintf("  %-18s %d", "alert number", a.Number))
			add("secret type", a.SecretTypeDisplay)
			add("link", a.HTMLURL)
		}
		out = append(out, "")
		add("alert state", a.State)
		add("secret validity", a.Validity)
		add("secret type slug", a.SecretType)
		if a.Resolution != "" {
			add("resolution", a.Resolution)
		}
		if a.PubliclyLeaked {
			add("publicly leaked", "yes")
		}
		if a.MultiRepo {
			add("multi repo", "yes")
		}
	} else if r.Request != nil {
		out = append(out, "", "  the alert itself could not be read, so nothing here is cross checked")
	}

	if len(r.Warnings) > 0 {
		out = append(out, "", "  warnings: "+strings.Join(r.Warnings, ", "))
	}

	return out
}

func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
