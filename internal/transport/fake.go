package transport

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// PatchCall records one write for assertion in tests.
type PatchCall struct {
	Path string
	Body any
}

// Fake is an in-memory Transport for tests. Paths are matched exactly, so
// tests must register the full path including query string.
//
// Fake is safe for concurrent use because the discovery sweep exercises it
// from multiple goroutines.
type Fake struct {
	mu sync.Mutex

	// Pages maps a path to the sequence of page bodies, each a JSON array.
	Pages map[string][][]byte
	// Singles maps a path to a single JSON document.
	Singles map[string][]byte
	// GetErr forces an error for a path on GetJSON and GetAllPages.
	GetErr map[string]error
	// PatchErr forces an error for a path on Patch.
	PatchErr map[string]error

	// Patches records every write in call order.
	Patches []PatchCall
	// Gets records every read path in call order.
	Gets []string
}

func NewFake() *Fake {
	return &Fake{
		Pages:    map[string][][]byte{},
		Singles:  map[string][]byte{},
		GetErr:   map[string]error{},
		PatchErr: map[string]error{},
	}
}

func (f *Fake) GetJSON(path string, out any) error {
	f.mu.Lock()
	f.Gets = append(f.Gets, path)
	if err, ok := f.GetErr[path]; ok {
		f.mu.Unlock()
		return err
	}
	doc, ok := f.Singles[path]
	f.mu.Unlock()

	if !ok {
		return &HTTPError{StatusCode: http.StatusNotFound, Path: path, Message: "Not Found"}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(doc, out)
}

func (f *Fake) GetAllPages(path string) ([]json.RawMessage, error) {
	f.mu.Lock()
	f.Gets = append(f.Gets, path)
	if err, ok := f.GetErr[path]; ok {
		f.mu.Unlock()
		return nil, err
	}
	pages, ok := f.Pages[path]
	f.mu.Unlock()

	if !ok {
		return nil, &HTTPError{StatusCode: http.StatusNotFound, Path: path, Message: "Not Found"}
	}
	var all []json.RawMessage
	for i, p := range pages {
		var page []json.RawMessage
		if err := json.Unmarshal(p, &page); err != nil {
			return nil, fmt.Errorf("fake page %d for %s is not a JSON array: %w", i, path, err)
		}
		all = append(all, page...)
	}
	return all, nil
}

func (f *Fake) Patch(path string, body any, out any) error {
	f.mu.Lock()
	f.Patches = append(f.Patches, PatchCall{Path: path, Body: body})
	err, forced := f.PatchErr[path]
	f.mu.Unlock()

	if forced {
		return err
	}
	if out == nil {
		return nil
	}
	f.mu.Lock()
	doc, ok := f.Singles[path]
	f.mu.Unlock()
	if !ok {
		return nil
	}
	return json.Unmarshal(doc, out)
}

// SetSingle is a convenience for tests that register one document.
func (f *Fake) SetSingle(path, jsonDoc string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Singles[path] = []byte(jsonDoc)
}

// SetPage is a convenience for tests that register one page of an array.
func (f *Fake) SetPage(path, jsonArray string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Pages[path] = [][]byte{[]byte(jsonArray)}
}
