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

// WarnGlyph returns a marker for rows carrying warnings, blank otherwise.
func WarnGlyph(r model.Row) string {
	if len(r.Warnings) == 0 {
		return " "
	}
	return "!"
}

// Format renders one row as a single line. The identifier is always the alert
// number, because that is what every write path uses and what the web UI
// shows, so displaying the request number instead would invite mismatched
// cross referencing.
func Format(r model.Row) string {
	var (
		repo     string
		ident    int
		who      string
		reason   string
		secret   string
		validity string
		note     string
	)

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

	line := fmt.Sprintf("%s %-28s #%-5d %-14s %-16s %-34s %s",
		WarnGlyph(r), truncate(repo, 28), ident, truncate(who, 14),
		truncate(reason, 16), truncate(secret, 34), validity)

	if note != "" {
		line += "  " + truncate(note, 40)
	}
	if strings.Contains(strings.Join(r.Warnings, ","), enrich.WarnStaleClaim) {
		line += "  [claims revoked but secret is live]"
	}

	// Collapse any newline, since the pager assumes one row per line.
	return strings.ReplaceAll(strings.ReplaceAll(line, "\n", " "), "\r", " ")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 3 {
		return s[:n]
	}
	return s[:n-3] + "..."
}
