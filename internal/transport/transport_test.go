package transport

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestHTTPErrorClassification(t *testing.T) {
	nf := &HTTPError{StatusCode: 404, Path: "orgs/x/y", Message: "Not Found"}
	fb := &HTTPError{StatusCode: 403, Path: "orgs/x/y", Message: "Forbidden"}

	if !IsNotFound(nf) {
		t.Error("IsNotFound should recognise a 404")
	}
	if IsNotFound(fb) {
		t.Error("IsNotFound should not match a 403")
	}
	if !IsForbidden(fb) {
		t.Error("IsForbidden should recognise a 403")
	}
	if IsNotFound(errors.New("plain")) {
		t.Error("IsNotFound should be false for a non HTTPError")
	}
	if got := nf.Error(); got == "" {
		t.Error("Error() should not be empty")
	}
}

func TestFakeGetJSON(t *testing.T) {
	f := NewFake()
	f.Singles["repos/o/r/secret-scanning/alerts/1"] = []byte(`{"number":1,"state":"open"}`)

	var got struct {
		Number int    `json:"number"`
		State  string `json:"state"`
	}
	if err := f.GetJSON("repos/o/r/secret-scanning/alerts/1", &got); err != nil {
		t.Fatalf("GetJSON() error = %v", err)
	}
	if got.Number != 1 || got.State != "open" {
		t.Errorf("decoded = %+v, want number 1 state open", got)
	}
}

func TestFakeGetJSONUnknownPathIsNotFound(t *testing.T) {
	f := NewFake()
	var out map[string]any
	err := f.GetJSON("missing/path", &out)
	if !IsNotFound(err) {
		t.Errorf("err = %v, want a 404 HTTPError", err)
	}
}

func TestFakeGetAllPagesConcatenates(t *testing.T) {
	f := NewFake()
	f.Pages["orgs/o/dismissal-requests/secret-scanning"] = [][]byte{
		[]byte(`[{"number":1},{"number":2}]`),
		[]byte(`[{"number":3}]`),
	}

	raws, err := f.GetAllPages("orgs/o/dismissal-requests/secret-scanning")
	if err != nil {
		t.Fatalf("GetAllPages() error = %v", err)
	}
	if len(raws) != 3 {
		t.Fatalf("len = %d, want 3 elements flattened across pages", len(raws))
	}
	var third struct{ Number int }
	if err := json.Unmarshal(raws[2], &third); err != nil {
		t.Fatalf("unmarshal element: %v", err)
	}
	if third.Number != 3 {
		t.Errorf("third element number = %d, want 3", third.Number)
	}
}

func TestFakeRecordsPatches(t *testing.T) {
	f := NewFake()
	f.Singles["repos/o/r/dismissal-requests/secret-scanning/18"] = []byte(`{}`)

	body := map[string]string{"status": "approve", "message": "ok"}
	if err := f.Patch("repos/o/r/dismissal-requests/secret-scanning/18", body, nil); err != nil {
		t.Fatalf("Patch() error = %v", err)
	}
	if len(f.Patches) != 1 {
		t.Fatalf("recorded %d patches, want 1", len(f.Patches))
	}
	if f.Patches[0].Path != "repos/o/r/dismissal-requests/secret-scanning/18" {
		t.Errorf("recorded path = %q", f.Patches[0].Path)
	}
}

func TestFakePatchErrorInjection(t *testing.T) {
	f := NewFake()
	want := &HTTPError{StatusCode: 403, Message: "Forbidden"}
	f.PatchErr["repos/o/r/x/1"] = want

	err := f.Patch("repos/o/r/x/1", nil, nil)
	if !IsForbidden(err) {
		t.Errorf("err = %v, want forbidden", err)
	}
}
