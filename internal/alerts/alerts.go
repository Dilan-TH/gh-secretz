// Package alerts enumerates secret scanning alerts.
//
// Enumeration is deliberately done twice and unioned. The default alert
// listing silently omits generic detection types such as password and
// http_bearer_authentication_header. Measured against real repositories, the
// default listing returned 0 alerts where 5 were open, and 2 where 37 were
// open. Naming secret types explicitly is the only way to see them, and
// exclude_secret_types does not unlock them.
package alerts

import (
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/Dilan-TH/gh-secretz/internal/model"
	"github.com/Dilan-TH/gh-secretz/internal/transport"
)

// GenericSecretTypes are the slugs the default listing hides. This list is
// the tool's one irreducible completeness risk: a generic type GitHub adds
// and this list omits stays invisible. Override with --secret-types.
var GenericSecretTypes = []string{
	"password",
	"http_bearer_authentication_header",
	"http_basic_authentication_header",
	"secret_value",
	"token_value",
	"jwt_header",
	"connection_string",
}

// Options selects which alerts to enumerate.
type Options struct {
	Owner string
	Repo  string
	// State is open or resolved. Empty means no state filter.
	State string
	// SecretTypes overrides GenericSecretTypes for the explicit query.
	SecretTypes []string
}

// Result carries the alerts plus the completeness metadata that gets printed
// so the operator can see the assumption being made.
type Result struct {
	Alerts             []model.Alert
	SecretTypesQueried int
}

func (o Options) types() []string {
	if len(o.SecretTypes) > 0 {
		return o.SecretTypes
	}
	return GenericSecretTypes
}

func (o Options) base() string {
	return fmt.Sprintf("repos/%s/%s/secret-scanning/alerts", o.Owner, o.Repo)
}

// DefaultPath is the plain listing, which omits generic types.
func DefaultPath(opts Options) string {
	q := url.Values{}
	if opts.State != "" {
		q.Set("state", opts.State)
	}
	q.Set("per_page", "100")
	return opts.base() + "?" + q.Encode()
}

// UnionPath names secret types explicitly, which is the only way the generic
// family is returned.
func UnionPath(opts Options) string {
	q := url.Values{}
	if opts.State != "" {
		q.Set("state", opts.State)
	}
	q.Set("secret_type", strings.Join(opts.types(), ","))
	q.Set("per_page", "100")
	return opts.base() + "?" + q.Encode()
}

// List returns the union of the default listing and the explicit secret type
// listing, deduplicated by alert number.
//
// A 404 from either query is treated as "no alerts visible here" rather than
// an error, because repos with scanning disabled or without access 404 and
// that is normal during a sweep.
func List(t transport.Transport, opts Options) (Result, error) {
	seen := map[int]model.Alert{}

	for _, path := range []string{DefaultPath(opts), UnionPath(opts)} {
		raws, err := t.GetAllPages(path)
		if err != nil {
			if transport.IsNotFound(err) || transport.IsForbidden(err) {
				continue
			}
			return Result{}, err
		}
		for _, raw := range raws {
			a, err := decode(raw, opts.Owner, opts.Repo)
			if err != nil {
				return Result{}, fmt.Errorf("decoding alert from %s: %w", path, err)
			}
			seen[a.Number] = a
		}
	}

	out := make([]model.Alert, 0, len(seen))
	for _, a := range seen {
		out = append(out, a)
	}
	// Descending by number matches the API's own ordering and keeps output
	// stable across runs.
	sort.Slice(out, func(i, j int) bool { return out[i].Number > out[j].Number })

	return Result{Alerts: out, SecretTypesQueried: len(opts.types())}, nil
}

// Index keys alerts by number for joining onto requests.
func Index(as []model.Alert) map[int]model.Alert {
	m := make(map[int]model.Alert, len(as))
	for _, a := range as {
		m[a.Number] = a
	}
	return m
}

type wire struct {
	Number                int    `json:"number"`
	State                 string `json:"state"`
	SecretType            string `json:"secret_type"`
	SecretTypeDisplayName string `json:"secret_type_display_name"`
	Validity              string `json:"validity"`
	Resolution            string `json:"resolution"`
	ClosureRequestComment string `json:"closure_request_comment"`
	HTMLURL               string `json:"html_url"`
	MultiRepo             bool   `json:"multi_repo"`
	PubliclyLeaked        bool   `json:"publicly_leaked"`
}

func decode(raw json.RawMessage, owner, repo string) (model.Alert, error) {
	var w wire
	if err := json.Unmarshal(raw, &w); err != nil {
		return model.Alert{}, err
	}
	return model.Alert{
		Number:                w.Number,
		Owner:                 owner,
		Repo:                  repo,
		State:                 w.State,
		SecretType:            w.SecretType,
		SecretTypeDisplay:     w.SecretTypeDisplayName,
		Validity:              w.Validity,
		Resolution:            w.Resolution,
		ClosureRequestComment: w.ClosureRequestComment,
		HTMLURL:               w.HTMLURL,
		MultiRepo:             w.MultiRepo,
		PubliclyLeaked:        w.PubliclyLeaked,
	}, nil
}
