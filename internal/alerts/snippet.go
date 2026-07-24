package alerts

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/Dilan-TH/gh-secretz/internal/model"
	"github.com/Dilan-TH/gh-secretz/internal/transport"
)

// ContextLines is how many lines either side of the hit to show.
const ContextLines = 4

// maxBlobBytes caps the file we will pull back. Secret scanning happily flags
// vendored bundles and lock files, and fetching a very large blob to show
// eight lines of it is a poor trade.
const maxBlobBytes = 2 << 20

// LocationsPath is the endpoint listing where a secret was detected.
func LocationsPath(owner, repo string, alertNumber int) string {
	return fmt.Sprintf("repos/%s/%s/secret-scanning/alerts/%d/locations?per_page=100",
		owner, repo, alertNumber)
}

// BlobPath is the endpoint returning a file's contents by blob sha.
func BlobPath(owner, repo, sha string) string {
	return fmt.Sprintf("repos/%s/%s/git/blobs/%s", owner, repo, sha)
}

type locationWire struct {
	Type    string `json:"type"`
	Details struct {
		Path        string `json:"path"`
		StartLine   int    `json:"start_line"`
		EndLine     int    `json:"end_line"`
		StartColumn int    `json:"start_column"`
		EndColumn   int    `json:"end_column"`
		BlobSHA     string `json:"blob_sha"`
		HTMLURL     string `json:"html_url"`
	} `json:"details"`
}

type blobWire struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
	Size     int    `json:"size"`
}

// FetchSnippet returns the source context for an alert, with the secret
// masked.
//
// A missing location or an unreadable blob yields a Snippet carrying a Note
// rather than an error, because a detail pane that explains why it cannot show
// code is more useful than one that shows nothing.
func FetchSnippet(t transport.Transport, a model.Alert) (model.Snippet, error) {
	raws, err := t.GetAllPages(LocationsPath(a.Owner, a.Repo, a.Number))
	if err != nil {
		if transport.IsNotFound(err) || transport.IsForbidden(err) {
			return model.Snippet{Note: "no permission to read this alert's locations"}, nil
		}
		return model.Snippet{}, err
	}
	if len(raws) == 0 {
		return model.Snippet{Note: "the API reported no locations for this alert"}, nil
	}

	var loc locationWire
	if err := decodeLocation(raws[0], &loc); err != nil {
		return model.Snippet{}, err
	}
	d := loc.Details

	snip := model.Snippet{
		Path:      d.Path,
		StartLine: d.StartLine,
		EndLine:   d.EndLine,
		HTMLURL:   d.HTMLURL,
		Locations: len(raws),
	}

	if d.BlobSHA == "" {
		snip.Note = fmt.Sprintf("location type %q carries no blob to read", loc.Type)
		return snip, nil
	}

	var blob blobWire
	if err := t.GetJSON(BlobPath(a.Owner, a.Repo, d.BlobSHA), &blob); err != nil {
		if transport.IsNotFound(err) || transport.IsForbidden(err) {
			snip.Note = "the file could not be read, it may have been removed"
			return snip, nil
		}
		return model.Snippet{}, err
	}
	if blob.Size > maxBlobBytes {
		snip.Note = fmt.Sprintf("file is %d bytes, too large to fetch for a preview", blob.Size)
		return snip, nil
	}
	if blob.Encoding != "base64" {
		snip.Note = fmt.Sprintf("unexpected blob encoding %q", blob.Encoding)
		return snip, nil
	}

	// The API wraps base64 at column 60, which the decoder rejects.
	decoded, err := base64.StdEncoding.DecodeString(
		strings.NewReplacer("\n", "", "\r", "").Replace(blob.Content))
	if err != nil {
		snip.Note = "the file contents could not be decoded"
		return snip, nil
	}

	snip.Lines = extract(string(decoded), d.StartLine, d.EndLine)
	return snip, nil
}

// extract pulls the context window around the hit.
//
// Line numbers from the API are one based. The source is shown verbatim: the
// operator already has these values through the web UI, the API, and the
// repository, and seeing a value is sometimes what distinguishes a real
// credential from a documented placeholder.
func extract(content string, startLine, endLine int) []model.SnippetLine {
	lines := strings.Split(strings.ReplaceAll(content, "\r\n", "\n"), "\n")

	from := startLine - ContextLines
	if from < 1 {
		from = 1
	}
	to := endLine + ContextLines
	if to > len(lines) {
		to = len(lines)
	}
	if startLine > len(lines) {
		return nil
	}

	out := make([]model.SnippetLine, 0, to-from+1)
	for n := from; n <= to; n++ {
		text := lines[n-1]
		hit := n >= startLine && n <= endLine
		out = append(out, model.SnippetLine{Number: n, Text: text, Hit: hit})
	}
	return out
}

func decodeLocation(raw json.RawMessage, out *locationWire) error {
	if err := json.Unmarshal(raw, out); err != nil {
		return fmt.Errorf("decoding alert location: %w", err)
	}
	return nil
}
