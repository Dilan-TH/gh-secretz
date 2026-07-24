package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/Dilan-TH/gh-secretz/internal/model"
	"github.com/Dilan-TH/gh-secretz/internal/selection"
)

// Decision is what the operator asked for on exit. An empty Action means they
// quit without choosing, which must be indistinguishable from doing nothing.
type Decision struct {
	Action string
	// Resolution is set for close actions only, chosen by reason key.
	Resolution string
	// Comment is applied to every row in the decision.
	Comment string
	Rows    []model.Row
}

// reason maps a triage key to the API resolution and GitHub's own wording for
// it, so the tool asks the same question the web UI does.
type reason struct {
	resolution string
	label      string
	blurb      string
}

// triageReasons are keyed by the uppercase key that selects them. Uppercase
// only: a destructive action should take a deliberate keystroke.
var triageReasons = map[string]reason{
	"R": {"revoked", "revoked", "this secret has been revoked"},
	"T": {"used_in_tests", "used in tests", "this secret is not in production code"},
	"F": {"false_positive", "false positive", "this alert is not valid"},
	"W": {"wont_fix", "wont fix", "this alert is not relevant"},
}

// triageKeyOrder fixes the help line order, since map iteration is random.
var triageKeyOrder = []string{"R", "T", "F", "W"}

// Screen is the bubbletea model. Selection state is delegated entirely to
// internal/selection so it can be tested without a terminal.
type Screen struct {
	sel      *selection.Model
	mode     Mode
	header   string
	decision Decision
	height   int
	width    int
	offset   int
	// detail is true while the full detail pane for the cursor row is open.
	detail bool
	// pendingAction and pendingResolution hold the action awaiting a comment.
	// pendingAction is empty when not prompting.
	pendingAction     string
	pendingResolution string
	pendingLabel      string
	input             []rune
}

var (
	headerStyle = lipgloss.NewStyle().Bold(true)
	cursorStyle = lipgloss.NewStyle().Reverse(true)
	warnStyle   = lipgloss.NewStyle().Bold(true)
	footStyle   = lipgloss.NewStyle().Faint(true)
)

// defaultWidth is used until the terminal reports its real size. It is wide
// enough that the comment column is usable rather than a stub.
const defaultWidth = 160

func NewScreen(rows []model.Row, mode Mode, header string) *Screen {
	return &Screen{sel: selection.New(rows), mode: mode, header: header,
		height: 20, width: defaultWidth}
}

// Decision reports what the operator chose.
func (s *Screen) Decision() Decision { return s.decision }

// Prompting reports whether the comment prompt is open.
func (s *Screen) Prompting() bool { return s.pendingAction != "" }

func (s *Screen) Init() tea.Cmd { return nil }

func (s *Screen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.height = m.Height
		s.width = m.Width
		return s, nil

	case tea.KeyMsg:
		if m.Type == tea.KeyCtrlC {
			return s, tea.Quit
		}
		// The comment prompt captures all input, so typing a reason word
		// cannot trigger another action mid sentence.
		if s.pendingAction != "" {
			return s.updatePrompt(m)
		}
		// While the detail pane is open, any key closes it. Nothing else is
		// actionable from there, so trapping keys would only confuse.
		if s.detail {
			s.detail = false
			return s, nil
		}
		return s.updateList(m)
	}
	return s, nil
}

func (s *Screen) updateList(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.Type {
	case tea.KeyEnter:
		if s.sel.Len() > 0 {
			s.detail = true
		}
		return s, nil
	case tea.KeyUp:
		s.sel.MoveUp()
		s.follow()
		return s, nil
	case tea.KeyDown:
		s.sel.MoveDown()
		s.follow()
		return s, nil
	case tea.KeySpace:
		s.sel.Toggle()
		return s, nil
	}

	key := string(m.Runes)

	// Triage reasons double as the confirm key: choosing why is choosing to
	// act, which is how the web UI works too.
	if s.mode == ModeTriage {
		if r, ok := triageReasons[key]; ok {
			s.beginPrompt("close", r.resolution, r.label)
			return s, nil
		}
	}

	switch key {
	case " ":
		s.sel.Toggle()
	case "k":
		s.sel.MoveUp()
		s.follow()
	case "j":
		s.sel.MoveDown()
		s.follow()
	case "a":
		s.sel.CheckAll()
	case "n":
		s.sel.UncheckAll()
	case "q":
		return s, tea.Quit
	case "A":
		if s.mode == ModeReview {
			s.beginPrompt("approve", "", "approve")
		}
	case "D":
		if s.mode == ModeReview {
			s.beginPrompt("deny", "", "deny")
		}
	}
	return s, nil
}

// beginPrompt opens the comment prompt, but only when something is checked,
// so a stray key on an empty selection cannot act on the whole screen.
func (s *Screen) beginPrompt(action, resolution, label string) {
	if s.sel.CheckedCount() == 0 {
		return
	}
	s.pendingAction = action
	s.pendingResolution = resolution
	s.pendingLabel = label
	s.input = nil
}

func (s *Screen) cancelPrompt() {
	s.pendingAction = ""
	s.pendingResolution = ""
	s.pendingLabel = ""
	s.input = nil
}

func (s *Screen) updatePrompt(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.Type {
	case tea.KeyEsc:
		s.cancelPrompt()
		return s, nil
	case tea.KeyBackspace:
		if len(s.input) > 0 {
			s.input = s.input[:len(s.input)-1]
		}
		return s, nil
	case tea.KeySpace:
		s.input = append(s.input, ' ')
		return s, nil
	case tea.KeyEnter:
		// The API requires a non empty message, so an empty prompt simply
		// does not submit rather than failing after the fact.
		if strings.TrimSpace(string(s.input)) == "" {
			return s, nil
		}
		s.decision = Decision{
			Action:     s.pendingAction,
			Resolution: s.pendingResolution,
			Comment:    strings.TrimSpace(string(s.input)),
			Rows:       s.sel.Checked(),
		}
		return s, tea.Quit
	case tea.KeyRunes:
		s.input = append(s.input, m.Runes...)
		return s, nil
	}
	return s, nil
}

// follow keeps the cursor inside the visible window.
func (s *Screen) follow() {
	visible := s.visibleCount()
	if s.sel.Cursor() < s.offset {
		s.offset = s.sel.Cursor()
	}
	if s.sel.Cursor() >= s.offset+visible {
		s.offset = s.sel.Cursor() - visible + 1
	}
	if s.offset < 0 {
		s.offset = 0
	}
}

func (s *Screen) visibleCount() int {
	// Reserve lines for the header and footer.
	n := s.height - 4
	if n < 1 {
		return 1
	}
	return n
}

func (s *Screen) View() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render(s.header))
	b.WriteString("\n\n")

	rows := s.sel.Rows()
	if len(rows) == 0 {
		b.WriteString("  nothing to review\n\n")
		b.WriteString(footStyle.Render("q to quit"))
		return b.String()
	}

	if s.pendingAction != "" {
		return s.promptView()
	}
	if s.detail {
		return s.detailView()
	}

	end := s.offset + s.visibleCount()
	if end > len(rows) {
		end = len(rows)
	}

	for i := s.offset; i < end; i++ {
		box := "[ ]"
		if s.sel.IsChecked(i) {
			box = "[x]"
		}
		line := box + " " + Format(rows[i], s.width)
		if len(rows[i].Warnings) > 0 {
			line = warnStyle.Render(line)
		}
		if i == s.sel.Cursor() {
			line = cursorStyle.Render(line)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(footStyle.Render(s.footer(len(rows))))
	return b.String()
}

// promptView asks for the comment that will be applied to every checked row.
func (s *Screen) promptView() string {
	var b strings.Builder

	b.WriteString(headerStyle.Render(s.header))
	b.WriteString("\n\n")

	verb := s.pendingAction
	if s.pendingResolution != "" {
		verb = fmt.Sprintf("close as %s", s.pendingLabel)
	}
	b.WriteString(headerStyle.Render(fmt.Sprintf("  %s %d selected %s",
		verb, s.sel.CheckedCount(), plural("alert", s.sel.CheckedCount()))))
	b.WriteString("\n\n")

	// Showing the affected rows means the comment is written with the actual
	// batch in view, not from memory.
	shown := 0
	for _, r := range s.sel.Checked() {
		if shown >= 8 {
			b.WriteString(fmt.Sprintf("    ... and %d more\n", s.sel.CheckedCount()-shown))
			break
		}
		b.WriteString("    " + Format(r, s.width-4) + "\n")
		shown++
	}

	b.WriteString("\n  comment applied to every one of them:\n\n")
	b.WriteString("    " + string(s.input) + cursorStyle.Render(" "))
	b.WriteString("\n\n")

	hint := "enter confirms, esc cancels, a comment is required"
	if strings.TrimSpace(string(s.input)) == "" {
		hint = "type a comment, then enter. esc cancels"
	}
	b.WriteString(footStyle.Render("  " + hint))
	return b.String()
}

// detailView renders the untruncated detail pane for the cursor row.
func (s *Screen) detailView() string {
	var b strings.Builder
	rows := s.sel.Rows()
	cur := s.sel.Cursor()

	b.WriteString(headerStyle.Render(s.header))
	b.WriteString("\n\n")
	b.WriteString(headerStyle.Render(fmt.Sprintf("  detail for row %d of %d", cur+1, len(rows))))
	b.WriteString("\n\n")

	for _, line := range FormatDetail(rows[cur]) {
		b.WriteString(line)
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(footStyle.Render("any key returns to the list"))
	return b.String()
}

func (s *Screen) footer(total int) string {
	nav := fmt.Sprintf("%d/%d selected   space toggle  a all  n none  enter detail",
		s.sel.CheckedCount(), total)

	if s.mode == ModeReview {
		return nav + "  A approve  D deny  q abort"
	}

	var keys []string
	for _, k := range triageKeyOrder {
		keys = append(keys, k+" "+triageReasons[k].label)
	}
	return nav + "\n  close as:  " + strings.Join(keys, "   ") + "   q abort"
}

func plural(word string, n int) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

// Run drives the screen to completion and returns the operator's decision.
func Run(rows []model.Row, mode Mode, header string) (Decision, error) {
	s := NewScreen(rows, mode, header)
	p := tea.NewProgram(s)
	out, err := p.Run()
	if err != nil {
		return Decision{}, err
	}
	final, ok := out.(*Screen)
	if !ok {
		return Decision{}, fmt.Errorf("unexpected final model type %T", out)
	}
	return final.Decision(), nil
}
