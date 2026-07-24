package model

import (
	"testing"
	"time"
)

func TestAlertHasClosureRequest(t *testing.T) {
	tests := []struct {
		name    string
		comment string
		want    bool
	}{
		{"empty means no request", "", false},
		{"any comment means a request exists", "This password has been revoked", true},
		{"whitespace only still counts as absent", "   ", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := Alert{ClosureRequestComment: tt.comment}
			if got := a.HasClosureRequest(); got != tt.want {
				t.Errorf("HasClosureRequest() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestRequestExpired(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	past := Request{ExpiresAt: now.Add(-1 * time.Hour)}
	future := Request{ExpiresAt: now.Add(1 * time.Hour)}
	if !past.Expired(now) {
		t.Error("a request whose ExpiresAt has passed should be expired")
	}
	if future.Expired(now) {
		t.Error("a request whose ExpiresAt is in the future should not be expired")
	}
	zero := Request{}
	if zero.Expired(now) {
		t.Error("a zero ExpiresAt should not be treated as expired")
	}
}

func TestRowKeyUsesAlertNumberNotRequestNumber(t *testing.T) {
	// This guards the core safety property. Request number 5 and alert
	// number 18 are a real pairing observed in the API. The key must be
	// built from the alert number, because that is what write paths use.
	row := Row{Request: &Request{Number: 5, AlertNumber: 18, Owner: "o", Repo: "r"}}
	want := "o/r#18"
	if got := row.Key(); got != want {
		t.Errorf("Key() = %q, want %q", got, want)
	}
}

func TestRowKeyFallsBackToAlert(t *testing.T) {
	row := Row{Alert: &Alert{Number: 7, Owner: "o", Repo: "r"}}
	if got, want := row.Key(), "o/r#7"; got != want {
		t.Errorf("Key() = %q, want %q", got, want)
	}
}
