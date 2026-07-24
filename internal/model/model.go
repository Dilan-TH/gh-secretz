// Package model holds the types shared across every layer of gh-secretz.
// It has no behaviour beyond small derived predicates and no dependencies
// outside the standard library.
package model

import (
	"fmt"
	"strings"
	"time"
)

// Request is a normalised secret scanning dismissal request.
//
// Number and AlertNumber are different values on the same record and must
// never be conflated. A request with Number 5 can refer to AlertNumber 18.
// Every write path uses AlertNumber.
type Request struct {
	ID                int64
	Number            int
	AlertNumber       int
	Owner             string
	Repo              string
	FullName          string
	Requester         string
	Reason            string
	SecretTypeDisplay string
	Status            string
	RequesterComment  string
	CreatedAt         time.Time
	ExpiresAt         time.Time
	HTMLURL           string
}

// Expired reports whether the request's expiry has passed. A zero ExpiresAt
// is treated as "no expiry known" rather than as long expired.
func (r Request) Expired(now time.Time) bool {
	if r.ExpiresAt.IsZero() {
		return false
	}
	return r.ExpiresAt.Before(now)
}

// Alert is a normalised secret scanning alert.
type Alert struct {
	Number                int
	Owner                 string
	Repo                  string
	State                 string
	SecretType            string
	SecretTypeDisplay     string
	Validity              string
	Resolution            string
	ClosureRequestComment string
	HTMLURL               string
	MultiRepo             bool
	PubliclyLeaked        bool
}

// HasClosureRequest reports whether a dismissal request exists for this
// alert. This is the primary signal for triage, and it is preferred over a
// set difference against the request list because the request list is capped
// at one month while this field has no time window.
func (a Alert) HasClosureRequest() bool {
	return strings.TrimSpace(a.ClosureRequestComment) != ""
}

// Row pairs a request with its alert. Request is nil for triage rows, which
// are alerts with no request. Alert is nil when enrichment could not reach
// the alert.
type Row struct {
	Request  *Request
	Alert    *Alert
	Warnings []string
}

// Key identifies a row as owner/repo#alertNumber. It is built from the alert
// number because that is the identifier every write path uses.
func (r Row) Key() string {
	switch {
	case r.Request != nil:
		return fmt.Sprintf("%s/%s#%d", r.Request.Owner, r.Request.Repo, r.Request.AlertNumber)
	case r.Alert != nil:
		return fmt.Sprintf("%s/%s#%d", r.Alert.Owner, r.Alert.Repo, r.Alert.Number)
	default:
		return ""
	}
}
