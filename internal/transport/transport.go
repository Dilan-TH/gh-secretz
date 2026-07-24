// Package transport wraps the go-gh REST client behind an interface so that
// every layer above it is unit tested against a fake and never touches the
// network. It also owns pagination and HTTP error classification.
package transport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
)

// Transport is the surface the rest of gh-secretz uses to reach GitHub.
type Transport interface {
	// GetJSON fetches a single JSON document and decodes it into out.
	GetJSON(path string, out any) error
	// GetAllPages follows Link rel=next and returns every element of every
	// page as raw JSON, flattened in order.
	GetAllPages(path string) ([]json.RawMessage, error)
	// Patch sends a PATCH with a JSON body. out may be nil to discard the
	// response body.
	Patch(path string, body any, out any) error
}

// HTTPError is a non 2xx response. Callers classify with IsNotFound and
// IsForbidden rather than comparing status codes inline.
type HTTPError struct {
	StatusCode int
	Path       string
	Message    string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("GitHub API %d for %s: %s", e.StatusCode, e.Path, e.Message)
}

// IsNotFound reports whether err is a 404. On GitHub security endpoints a 404
// frequently means "insufficient permission" rather than "does not exist", so
// callers should treat it as a capability signal, not proof of absence.
func IsNotFound(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.StatusCode == http.StatusNotFound
}

// IsForbidden reports whether err is a 403.
func IsForbidden(err error) bool {
	var he *HTTPError
	return errors.As(err, &he) && he.StatusCode == http.StatusForbidden
}

type ghClient struct {
	rest *api.RESTClient
}

// New builds a Transport from the active gh authentication. It holds no
// credential of its own; go-gh reads gh's configuration and keyring.
func New() (Transport, error) {
	c, err := api.DefaultRESTClient()
	if err != nil {
		return nil, fmt.Errorf("could not build a GitHub client from gh auth, try running gh auth login: %w", err)
	}
	return &ghClient{rest: c}, nil
}

func (g *ghClient) GetJSON(path string, out any) error {
	resp, err := g.rest.Request(http.MethodGet, path, nil)
	if err != nil {
		return classify(path, err)
	}
	defer resp.Body.Close()
	return decodeInto(resp.Body, out)
}

var linkNextRe = regexp.MustCompile(`<([^>]+)>;\s*rel="next"`)

func (g *ghClient) GetAllPages(path string) ([]json.RawMessage, error) {
	var all []json.RawMessage
	next := path
	for next != "" {
		resp, err := g.rest.Request(http.MethodGet, next, nil)
		if err != nil {
			return nil, classify(next, err)
		}
		var page []json.RawMessage
		if err := decodeInto(resp.Body, &page); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decoding page %s: %w", next, err)
		}
		resp.Body.Close()
		all = append(all, page...)

		next = ""
		if m := linkNextRe.FindStringSubmatch(resp.Header.Get("Link")); len(m) == 2 {
			next = strings.TrimPrefix(m[1], "https://api.github.com/")
		}
	}
	return all, nil
}

func (g *ghClient) Patch(path string, body any, out any) error {
	var r io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encoding request body for %s: %w", path, err)
		}
		r = bytes.NewReader(b)
	}
	resp, err := g.rest.Request(http.MethodPatch, path, r)
	if err != nil {
		return classify(path, err)
	}
	defer resp.Body.Close()
	if out == nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	return decodeInto(resp.Body, out)
}

func decodeInto(r io.Reader, out any) error {
	if out == nil {
		_, err := io.Copy(io.Discard, r)
		return err
	}
	return json.NewDecoder(r).Decode(out)
}

// classify converts a go-gh HTTPError into our own so callers do not depend
// on go-gh types.
func classify(path string, err error) error {
	var ghErr *api.HTTPError
	if errors.As(err, &ghErr) {
		return &HTTPError{StatusCode: ghErr.StatusCode, Path: path, Message: ghErr.Message}
	}
	return fmt.Errorf("requesting %s: %w", path, err)
}
