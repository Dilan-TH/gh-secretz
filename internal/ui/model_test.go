package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Dilan-TH/gh-secretz/internal/model"
)

func rows(n int) []model.Row {
	out := make([]model.Row, 0, n)
	for i := 1; i <= n; i++ {
		out = append(out, model.Row{Request: &model.Request{
			Number: i, AlertNumber: 100 + i, Owner: "acme", Repo: "alpha", Requester: "alice", Reason: "revoked",
		}})
	}
	return out
}

// press feeds a key to the model, the way bubbletea would.
func press(s *Screen, key string) *Screen {
	var msg tea.Msg
	switch key {
	case "up", "down", "enter", "esc", "backspace", "spacebar":
		msg = tea.KeyMsg{Type: keyType(key)}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, _ := s.Update(msg)
	return next.(*Screen)
}

func keyType(k string) tea.KeyType {
	switch k {
	case "up":
		return tea.KeyUp
	case "down":
		return tea.KeyDown
	case "esc":
		return tea.KeyEsc
	case "backspace":
		return tea.KeyBackspace
	case "spacebar":
		return tea.KeySpace
	default:
		return tea.KeyEnter
	}
}

// typeComment feeds each character of a comment, then confirms.
func typeComment(s *Screen, text string) *Screen {
	for _, r := range text {
		if r == ' ' {
			s = press(s, "spacebar")
			continue
		}
		s = press(s, string(r))
	}
	return press(s, "enter")
}

func TestApproveKeyOnlyActsOnCheckedRows(t *testing.T) {
	s := NewScreen(rows(3), ModeReview, "header")
	s = press(s, " ") // check row 0
	s = press(s, "down")
	s = press(s, " ") // check row 1
	s = press(s, "A")
	s = typeComment(s, "reviewed")

	d := s.Decision()
	if d.Action != "approve" {
		t.Fatalf("Action = %q, want approve", d.Action)
	}
	if len(d.Rows) != 2 {
		t.Fatalf("got %d rows, want 2", len(d.Rows))
	}
	if d.Rows[0].Request.AlertNumber != 101 || d.Rows[1].Request.AlertNumber != 102 {
		t.Errorf("wrong rows selected: %+v", d.Rows)
	}
}

func TestApproveWithNothingCheckedDoesNothing(t *testing.T) {
	// A capital A on an empty selection must not approve the whole screen.
	s := NewScreen(rows(3), ModeReview, "header")
	s = press(s, "A")
	if got := s.Decision().Action; got != "" {
		t.Errorf("Action = %q, want empty because nothing was checked", got)
	}
}

func TestDenyKeyIsDistinctFromApprove(t *testing.T) {
	s := NewScreen(rows(1), ModeReview, "header")
	s = press(s, " ")
	s = press(s, "D")
	s = typeComment(s, "not justified")
	if got := s.Decision().Action; got != "deny" {
		t.Errorf("Action = %q, want deny", got)
	}
}

func TestReasonKeysAreIgnoredInReviewMode(t *testing.T) {
	// Each mode exposes only its own destructive actions, so a muscle memory
	// keystroke cannot perform the wrong operation.
	for _, k := range []string{"R", "T", "F", "W"} {
		s := NewScreen(rows(1), ModeReview, "header")
		s = press(s, " ")
		s = press(s, k)
		if s.Prompting() {
			t.Errorf("key %q opened a close prompt in review mode", k)
		}
		if got := s.Decision().Action; got != "" {
			t.Errorf("key %q gave Action = %q, want empty in review mode", k, got)
		}
	}
}

func TestApproveKeyIsIgnoredInTriageMode(t *testing.T) {
	s := NewScreen(rows(1), ModeTriage, "header")
	s = press(s, " ")
	s = press(s, "A")
	if s.Prompting() {
		t.Error("approve is not available in triage mode")
	}
	if got := s.Decision().Action; got != "" {
		t.Errorf("Action = %q, want empty; approve is not available in triage mode", got)
	}
}

func TestEachReasonKeyMapsToItsResolution(t *testing.T) {
	want := map[string]string{
		"R": "revoked",
		"T": "used_in_tests",
		"F": "false_positive",
		"W": "wont_fix",
	}
	for key, resolution := range want {
		s := NewScreen(rows(2), ModeTriage, "header")
		s = press(s, " ")
		s = press(s, key)
		if !s.Prompting() {
			t.Fatalf("key %q should open the comment prompt", key)
		}
		s = typeComment(s, "rotated in vault")

		d := s.Decision()
		if d.Action != "close" {
			t.Errorf("key %q gave Action = %q, want close", key, d.Action)
		}
		if d.Resolution != resolution {
			t.Errorf("key %q gave Resolution = %q, want %q", key, d.Resolution, resolution)
		}
		if d.Comment != "rotated in vault" {
			t.Errorf("key %q gave Comment = %q", key, d.Comment)
		}
		if len(d.Rows) != 1 {
			t.Errorf("key %q selected %d rows, want 1", key, len(d.Rows))
		}
	}
}

func TestPromptRequiresANonEmptyComment(t *testing.T) {
	// The API rejects a blank message, so enter on an empty prompt must not
	// submit rather than failing after the batch is assembled.
	s := NewScreen(rows(1), ModeTriage, "header")
	s = press(s, " ")
	s = press(s, "R")
	s = press(s, "enter")
	if got := s.Decision().Action; got != "" {
		t.Errorf("Action = %q, want empty; a blank comment must not submit", got)
	}
	if !s.Prompting() {
		t.Error("the prompt should stay open until a comment is given")
	}
}

func TestPromptEscCancelsWithoutActing(t *testing.T) {
	s := NewScreen(rows(1), ModeTriage, "header")
	s = press(s, " ")
	s = press(s, "R")
	s = typeCommentNoEnter(s, "half typed")
	s = press(s, "esc")
	if s.Prompting() {
		t.Error("esc should close the prompt")
	}
	if got := s.Decision().Action; got != "" {
		t.Errorf("Action = %q, want empty after cancelling", got)
	}
}

func TestPromptBackspaceEdits(t *testing.T) {
	s := NewScreen(rows(1), ModeTriage, "header")
	s = press(s, " ")
	s = press(s, "R")
	s = typeCommentNoEnter(s, "abc")
	s = press(s, "backspace")
	s = press(s, "enter")
	if got := s.Decision().Comment; got != "ab" {
		t.Errorf("Comment = %q, want ab", got)
	}
}

func TestPromptCapturesReasonKeysAsText(t *testing.T) {
	// Typing a word containing R or W must not re-trigger an action.
	s := NewScreen(rows(1), ModeTriage, "header")
	s = press(s, " ")
	s = press(s, "R")
	s = typeComment(s, "Rotated With care")
	d := s.Decision()
	if d.Comment != "Rotated With care" {
		t.Errorf("Comment = %q, want the literal text", d.Comment)
	}
	if d.Resolution != "revoked" {
		t.Errorf("Resolution = %q, want revoked from the original key", d.Resolution)
	}
}

// typeCommentNoEnter types text without confirming.
func typeCommentNoEnter(s *Screen, text string) *Screen {
	for _, r := range text {
		if r == ' ' {
			s = press(s, "spacebar")
			continue
		}
		s = press(s, string(r))
	}
	return s
}

func TestQuitLeavesNoDecision(t *testing.T) {
	s := NewScreen(rows(2), ModeReview, "header")
	s = press(s, " ")
	s = press(s, "q")
	if got := s.Decision().Action; got != "" {
		t.Errorf("Action = %q, want empty after quitting", got)
	}
}

func TestCheckAllAndUncheckAllKeys(t *testing.T) {
	s := NewScreen(rows(4), ModeReview, "header")
	s = press(s, "a")
	s = press(s, "A")
	s = typeComment(s, "bulk")
	if got := len(s.Decision().Rows); got != 4 {
		t.Errorf("checked %d rows, want 4", got)
	}

	s = NewScreen(rows(4), ModeReview, "header")
	s = press(s, "a")
	s = press(s, "n")
	s = press(s, "A")
	if s.Prompting() {
		t.Error("A with nothing checked must not open the prompt")
	}
	if got := s.Decision().Action; got != "" {
		t.Errorf("Action = %q, want empty after unchecking all", got)
	}
}

func TestViewRendersWithoutPanicOnEmptyRows(t *testing.T) {
	s := NewScreen(nil, ModeReview, "header")
	if got := s.View(); got == "" {
		t.Error("View() should render an empty state rather than nothing")
	}
	s = press(s, "A")
	if got := s.Decision().Action; got != "" {
		t.Errorf("Action = %q, want empty", got)
	}
}

func TestEnterOpensDetailAndAnyKeyCloses(t *testing.T) {
	s := NewScreen(rows(3), ModeReview, "header")
	s = press(s, "enter")
	if !strings.Contains(s.View(), "detail for row 1 of 3") {
		t.Errorf("enter should open the detail pane, got:\n%s", s.View())
	}
	s = press(s, "x")
	if strings.Contains(s.View(), "detail for row") {
		t.Error("any key should close the detail pane")
	}
}

func TestDetailPaneDoesNotActOnDestructiveKeys(t *testing.T) {
	// While reading detail, a capital A must close the pane rather than
	// approve, otherwise reading about a row could approve it.
	s := NewScreen(rows(2), ModeReview, "header")
	s = press(s, " ")
	s = press(s, "enter")
	s = press(s, "A")
	if got := s.Decision().Action; got != "" {
		t.Errorf("Action = %q, want empty; A inside the detail pane must not approve", got)
	}
	if strings.Contains(s.View(), "detail for row") {
		t.Error("A should have closed the detail pane")
	}
}

func TestEnterOnEmptyListDoesNotOpenDetail(t *testing.T) {
	s := NewScreen(nil, ModeReview, "header")
	s = press(s, "enter")
	if strings.Contains(s.View(), "detail for row") {
		t.Error("there is no row to detail")
	}
}

func TestWindowSizeSetsWidthUsedForRows(t *testing.T) {
	long := strings.Repeat("reasoning ", 40)
	r := []model.Row{{Request: &model.Request{Number: 1, AlertNumber: 7, Owner: "acme",
		Repo: "alpha", Requester: "alice", Reason: "revoked", RequesterComment: long}}}

	s := NewScreen(r, ModeReview, "header")
	narrow, _ := s.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	narrowView := narrow.(*Screen).View()

	s2 := NewScreen(r, ModeReview, "header")
	wide, _ := s2.Update(tea.WindowSizeMsg{Width: 240, Height: 40})
	wideView := wide.(*Screen).View()

	if len(wideView) <= len(narrowView) {
		t.Errorf("a wider window should render more comment: narrow=%d wide=%d",
			len(narrowView), len(wideView))
	}
}

// stubSnippet returns a fixed snippet, standing in for the network.
func stubSnippet(snip model.Snippet, err error) SnippetFetcher {
	return func(model.Row) (model.Snippet, error) { return snip, err }
}

func TestDetailShowsSourceSnippet(t *testing.T) {
	snip := model.Snippet{
		Path:      "postman/collection.json",
		StartLine: 562,
		Lines: []model.SnippetLine{
			{Number: 561, Text: `  "key": "Authorization",`},
			{Number: 562, Text: `  "value": "Bearer TOKEN-PLACEHOLDER-NOT-A-REAL-SECRET"`, Hit: true},
			{Number: 563, Text: `  "type": "text"`},
		},
		HTMLURL: "https://example.test/blob#L562",
	}
	s := NewScreen(rows(1), ModeTriage, "header").WithSnippets(stubSnippet(snip, nil))

	// Opening the pane returns a command; running it yields the snippet.
	next, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	s = next.(*Screen)
	if cmd == nil {
		t.Fatal("opening the detail pane should issue a fetch command")
	}
	next, _ = s.Update(cmd())
	s = next.(*Screen)

	view := s.View()
	for _, want := range []string{
		"postman/collection.json", "562", "Authorization", "TOKEN-PLACEHOLDER",
		"https://example.test/blob#L562",
	} {
		if !strings.Contains(view, want) {
			t.Errorf("detail view missing %q, got:\n%s", want, view)
		}
	}
}

func TestDetailShowsLoadingThenResult(t *testing.T) {
	s := NewScreen(rows(1), ModeTriage, "header").
		WithSnippets(stubSnippet(model.Snippet{Path: "a.go"}, nil))

	next, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	s = next.(*Screen)
	if !strings.Contains(s.View(), "loading") {
		t.Errorf("pane should show a loading state while fetching, got:\n%s", s.View())
	}

	next, _ = s.Update(cmd())
	s = next.(*Screen)
	if strings.Contains(s.View(), "loading") {
		t.Error("loading state should clear once the fetch returns")
	}
}

func TestDetailReportsSnippetError(t *testing.T) {
	s := NewScreen(rows(1), ModeTriage, "header").
		WithSnippets(stubSnippet(model.Snippet{}, errors.New("rate limited")))

	next, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	s = next.(*Screen)
	next, _ = s.Update(cmd())
	s = next.(*Screen)

	if !strings.Contains(s.View(), "rate limited") {
		t.Errorf("pane should explain why source could not load, got:\n%s", s.View())
	}
}

func TestSnippetIsFetchedOnlyOncePerRow(t *testing.T) {
	// Reopening a pane must not refetch, since each fetch is two API calls.
	var calls int
	s := NewScreen(rows(2), ModeTriage, "header").
		WithSnippets(func(model.Row) (model.Snippet, error) {
			calls++
			return model.Snippet{Path: "a.go"}, nil
		})

	next, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	s = next.(*Screen)
	next, _ = s.Update(cmd())
	s = next.(*Screen)
	s = press(s, "x") // close

	_, cmd2 := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd2 != nil {
		t.Error("reopening the same row should not issue another fetch")
	}
	if calls != 1 {
		t.Errorf("fetcher called %d times, want 1", calls)
	}
}

func TestNoFetcherMeansNoSourceSection(t *testing.T) {
	s := NewScreen(rows(1), ModeReview, "header")
	next, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	s = next.(*Screen)
	if cmd != nil {
		t.Error("with no fetcher attached there is nothing to fetch")
	}
	if strings.Contains(s.View(), "loading") {
		t.Error("no source section should appear without a fetcher")
	}
}

func TestDetailWrapsTheHitLineSoLongSecretsStayReadable(t *testing.T) {
	// Truncating the line holding the secret would hide the thing being
	// inspected, so hit lines wrap across the pane instead.
	long := strings.Repeat("A", 400)
	snip := model.Snippet{
		Path:      "config.json",
		StartLine: 2,
		Lines: []model.SnippetLine{
			{Number: 1, Text: "before"},
			{Number: 2, Text: `"token": "` + long + `"`, Hit: true},
		},
	}
	s := NewScreen(rows(1), ModeTriage, "header").WithSnippets(stubSnippet(snip, nil))
	next, _ := s.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	s = next.(*Screen)
	next, cmd := s.Update(tea.KeyMsg{Type: tea.KeyEnter})
	s = next.(*Screen)
	next, _ = s.Update(cmd())
	s = next.(*Screen)

	view := s.View()
	// Every character of the value must be present somewhere in the pane.
	joined := strings.ReplaceAll(strings.ReplaceAll(view, "\n", ""), " ", "")
	if !strings.Contains(joined, long) {
		t.Error("the full hit line should be reachable across wrapped rows")
	}
}
