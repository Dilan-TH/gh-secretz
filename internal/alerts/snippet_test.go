package alerts

import (
	"encoding/base64"
	"strings"
	"testing"

	"github.com/Dilan-TH/gh-secretz/internal/model"
	"github.com/Dilan-TH/gh-secretz/internal/transport"
)

// file is the fixture source. The secret sits on line 6, spanning columns
// 13 to 22, mirroring the real shape of a token embedded in a config value.
const file = `{
  "info": {
    "name": "collection"
  },
  "header": [
    "value": "SECRETVALU",
    "type": "text"
  ]
}`

func blobBody(content string) string {
	// The API wraps base64 at column 60, which a naive decoder rejects, so
	// the fixture wraps too.
	enc := base64.StdEncoding.EncodeToString([]byte(content))
	var wrapped strings.Builder
	for i := 0; i < len(enc); i += 60 {
		end := i + 60
		if end > len(enc) {
			end = len(enc)
		}
		wrapped.WriteString(enc[i:end] + "\n")
	}
	return `{"content":"` + strings.ReplaceAll(wrapped.String(), "\n", "\\n") +
		`","encoding":"base64","size":` + itoa(len(content)) + `}`
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func alert() model.Alert {
	return model.Alert{Number: 1, Owner: "acme", Repo: "r"}
}

func withLocation(f *transport.Fake, startLine, endLine, startCol, endCol int) {
	f.SetPage(LocationsPath("acme", "r", 1), `[{
		"type":"commit",
		"details":{
			"path":"postman/collection.json",
			"start_line":`+itoa(startLine)+`,
			"end_line":`+itoa(endLine)+`,
			"start_column":`+itoa(startCol)+`,
			"end_column":`+itoa(endCol)+`,
			"blob_sha":"abc123",
			"html_url":"https://example.test/blob#L6"
		}
	}]`)
	f.SetSingle(BlobPath("acme", "r", "abc123"), blobBody(file))
}

func TestFetchSnippetShowsSourceVerbatim(t *testing.T) {
	f := transport.NewFake()
	withLocation(f, 6, 6, 15, 24)

	got, err := FetchSnippet(f, alert())
	if err != nil {
		t.Fatalf("FetchSnippet() error = %v", err)
	}
	if got.Path != "postman/collection.json" {
		t.Errorf("Path = %q", got.Path)
	}
	if got.HTMLURL != "https://example.test/blob#L6" {
		t.Errorf("HTMLURL = %q", got.HTMLURL)
	}

	joined := strings.Join(texts(got), "\n")

	// The source is shown as it is. Seeing the value is sometimes what
	// distinguishes a real credential from a documented placeholder, and the
	// operator already has it through the web UI and the API.
	if !strings.Contains(joined, "SECRETVALU") {
		t.Errorf("the source should be verbatim, got:\n%s", joined)
	}
	// Surrounding context must survive, since that is what answers whether
	// the hit is a test fixture or production config.
	for _, want := range []string{"collection", "\"type\": \"text\"", "\"value\": "} {
		if !strings.Contains(joined, want) {
			t.Errorf("context line %q missing, got:\n%s", want, joined)
		}
	}
}

func TestFetchSnippetKeepsTheWholeLineIncludingPastTheReportedSpan(t *testing.T) {
	// GitHub's reported match span can be shorter than the credential it
	// found: an alert observed live reported a 773 character secret ending at
	// column 799 on a 1058 character line, where the rest of the line was the
	// remainder of the same token. The line is kept whole so nothing is lost.
	f := transport.NewFake()
	withLocation(f, 6, 6, 15, 18)

	got, _ := FetchSnippet(f, alert())
	joined := strings.Join(texts(got), "\n")
	if !strings.Contains(joined, "SECRETVALU") {
		t.Errorf("the full line should survive a short reported span, got:\n%s", joined)
	}
}

func TestFetchSnippetMarksTheHitLine(t *testing.T) {
	f := transport.NewFake()
	withLocation(f, 6, 6, 15, 24)

	got, _ := FetchSnippet(f, alert())
	var hits []int
	for _, l := range got.Lines {
		if l.Hit {
			hits = append(hits, l.Number)
		}
	}
	if len(hits) != 1 || hits[0] != 6 {
		t.Errorf("hit lines = %v, want just line 6", hits)
	}
}

func TestFetchSnippetIncludesContextAroundTheHit(t *testing.T) {
	f := transport.NewFake()
	withLocation(f, 6, 6, 15, 24)

	got, _ := FetchSnippet(f, alert())
	if len(got.Lines) == 0 {
		t.Fatal("no lines returned")
	}
	first := got.Lines[0].Number
	last := got.Lines[len(got.Lines)-1].Number
	if first != 2 {
		t.Errorf("first line = %d, want 2 (four lines of context before line 6)", first)
	}
	if last != 9 {
		t.Errorf("last line = %d, want the file to end at 9", last)
	}
}

func TestFetchSnippetClampsContextAtFileStart(t *testing.T) {
	// A hit on line 1 must not ask for line minus three.
	f := transport.NewFake()
	withLocation(f, 1, 1, 1, 1)

	got, err := FetchSnippet(f, alert())
	if err != nil {
		t.Fatalf("FetchSnippet() error = %v", err)
	}
	if got.Lines[0].Number != 1 {
		t.Errorf("first line = %d, want 1", got.Lines[0].Number)
	}
}

func TestFetchSnippetHandlesMultiLineSecret(t *testing.T) {
	// A private key spans lines, and every spanned line is marked as a hit so
	// the pane can highlight the whole credential.
	f := transport.NewFake()
	withLocation(f, 5, 7, 5, 8)

	got, _ := FetchSnippet(f, alert())
	var hits int
	for _, l := range got.Lines {
		if l.Hit {
			hits++
		}
	}
	if hits != 3 {
		t.Errorf("hit lines = %d, want 3", hits)
	}
}

func TestFetchSnippetReportsMultipleLocations(t *testing.T) {
	f := transport.NewFake()
	f.SetPage(LocationsPath("acme", "r", 1), `[
		{"type":"commit","details":{"path":"a.json","start_line":6,"end_line":6,
		 "start_column":15,"end_column":24,"blob_sha":"abc123","html_url":"u1"}},
		{"type":"commit","details":{"path":"b.json","start_line":1,"end_line":1,
		 "start_column":1,"end_column":2,"blob_sha":"def456","html_url":"u2"}}
	]`)
	f.SetSingle(BlobPath("acme", "r", "abc123"), blobBody(file))

	got, _ := FetchSnippet(f, alert())
	if got.Locations != 2 {
		t.Errorf("Locations = %d, want 2 so the pane can say one of several", got.Locations)
	}
}

func TestFetchSnippetNoLocationsIsANoteNotAnError(t *testing.T) {
	// A pane explaining why it cannot show code beats one showing nothing.
	f := transport.NewFake()
	f.SetPage(LocationsPath("acme", "r", 1), `[]`)

	got, err := FetchSnippet(f, alert())
	if err != nil {
		t.Fatalf("FetchSnippet() error = %v", err)
	}
	if got.Note == "" {
		t.Error("expected a Note explaining the absence")
	}
	if len(got.Lines) != 0 {
		t.Errorf("expected no lines, got %d", len(got.Lines))
	}
}

func TestFetchSnippetForbiddenLocationsIsANote(t *testing.T) {
	f := transport.NewFake()
	f.GetErr[LocationsPath("acme", "r", 1)] =
		&transport.HTTPError{StatusCode: 403, Message: "Forbidden"}

	got, err := FetchSnippet(f, alert())
	if err != nil {
		t.Fatalf("FetchSnippet() should not fail on a permission problem, got %v", err)
	}
	if !strings.Contains(got.Note, "permission") {
		t.Errorf("Note = %q, want it to mention permission", got.Note)
	}
}

func TestFetchSnippetMissingBlobIsANote(t *testing.T) {
	f := transport.NewFake()
	f.SetPage(LocationsPath("acme", "r", 1), `[{"type":"commit","details":{
		"path":"a.json","start_line":6,"end_line":6,"start_column":15,
		"end_column":24,"blob_sha":"gone","html_url":"u"}}]`)

	got, err := FetchSnippet(f, alert())
	if err != nil {
		t.Fatalf("FetchSnippet() error = %v", err)
	}
	if !strings.Contains(got.Note, "could not be read") {
		t.Errorf("Note = %q", got.Note)
	}
	if got.Path != "a.json" {
		t.Errorf("Path = %q, the location is still worth reporting", got.Path)
	}
}

func TestFetchSnippetSkipsOversizedBlobs(t *testing.T) {
	// Secret scanning flags vendored bundles. Fetching megabytes to show
	// eight lines is a poor trade.
	f := transport.NewFake()
	f.SetPage(LocationsPath("acme", "r", 1), `[{"type":"commit","details":{
		"path":"vendor/bundle.js","start_line":6,"end_line":6,"start_column":15,
		"end_column":24,"blob_sha":"big","html_url":"u"}}]`)
	f.SetSingle(BlobPath("acme", "r", "big"),
		`{"content":"","encoding":"base64","size":99999999}`)

	got, _ := FetchSnippet(f, alert())
	if !strings.Contains(got.Note, "too large") {
		t.Errorf("Note = %q, want it to explain the size skip", got.Note)
	}
}

func TestFetchLocationReturnsPathWithoutFetchingBlob(t *testing.T) {
	f := transport.NewFake()
	withLocation(f, 6, 6, 15, 24)

	got, err := FetchLocation(f, alert())
	if err != nil {
		t.Fatalf("FetchLocation() error = %v", err)
	}
	if got != "postman/collection.json" {
		t.Errorf("path = %q", got)
	}
	for _, p := range f.Gets {
		if p == BlobPath("acme", "r", "abc123") {
			t.Errorf("FetchLocation should not fetch the blob, but it did: %v", f.Gets)
		}
	}
}

func TestFetchLocationNoLocationsIsEmptyNotError(t *testing.T) {
	f := transport.NewFake()
	f.SetPage(LocationsPath("acme", "r", 1), `[]`)

	got, err := FetchLocation(f, alert())
	if err != nil {
		t.Fatalf("FetchLocation() error = %v", err)
	}
	if got != "" {
		t.Errorf("path = %q, want empty", got)
	}
}

func TestFetchLocationForbiddenIsEmptyNotError(t *testing.T) {
	f := transport.NewFake()
	f.GetErr[LocationsPath("acme", "r", 1)] =
		&transport.HTTPError{StatusCode: 403, Message: "Forbidden"}

	got, err := FetchLocation(f, alert())
	if err != nil {
		t.Fatalf("FetchLocation() should not fail on a permission problem, got %v", err)
	}
	if got != "" {
		t.Errorf("path = %q, want empty", got)
	}
}

func TestFetchLocationsPopulatesPathOnEveryAlert(t *testing.T) {
	f := transport.NewFake()
	als := []model.Alert{
		{Number: 1, Owner: "acme", Repo: "r"},
		{Number: 2, Owner: "acme", Repo: "r"},
		{Number: 3, Owner: "acme", Repo: "r"},
	}
	f.SetPage(LocationsPath("acme", "r", 1), `[{"type":"commit","details":{"path":"a/fixtures/one.json"}}]`)
	f.SetPage(LocationsPath("acme", "r", 2), `[{"type":"commit","details":{"path":"src/prod.go"}}]`)
	f.SetPage(LocationsPath("acme", "r", 3), `[]`)

	if err := FetchLocations(f, als, 2); err != nil {
		t.Fatalf("FetchLocations() error = %v", err)
	}
	want := map[int]string{1: "a/fixtures/one.json", 2: "src/prod.go", 3: ""}
	for _, a := range als {
		if a.Path != want[a.Number] {
			t.Errorf("alert %d Path = %q, want %q", a.Number, a.Path, want[a.Number])
		}
	}
}

func TestFetchLocationsToleratesOneAlertBeingForbidden(t *testing.T) {
	f := transport.NewFake()
	als := []model.Alert{
		{Number: 1, Owner: "acme", Repo: "r"},
		{Number: 2, Owner: "acme", Repo: "r"},
	}
	f.SetPage(LocationsPath("acme", "r", 1), `[{"type":"commit","details":{"path":"a.go"}}]`)
	f.GetErr[LocationsPath("acme", "r", 2)] =
		&transport.HTTPError{StatusCode: 403, Message: "Forbidden"}

	if err := FetchLocations(f, als, 2); err != nil {
		t.Fatalf("FetchLocations() error = %v", err)
	}
	if als[0].Path != "a.go" {
		t.Errorf("alert 1 Path = %q", als[0].Path)
	}
	if als[1].Path != "" {
		t.Errorf("alert 2 Path = %q, want empty", als[1].Path)
	}
}

func TestFetchLocationsReturnsFirstRealError(t *testing.T) {
	f := transport.NewFake()
	als := []model.Alert{{Number: 1, Owner: "acme", Repo: "r"}}
	f.GetErr[LocationsPath("acme", "r", 1)] =
		&transport.HTTPError{StatusCode: 500, Message: "Internal Server Error"}

	if err := FetchLocations(f, als, 1); err == nil {
		t.Fatal("FetchLocations() expected an error for a non-tolerated failure")
	}
}

func texts(s model.Snippet) []string {
	out := make([]string, 0, len(s.Lines))
	for _, l := range s.Lines {
		out = append(out, l.Text)
	}
	return out
}
