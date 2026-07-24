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
	Rows   []model.Row
}

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

func (s *Screen) Init() tea.Cmd { return nil }

func (s *Screen) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		s.height = m.Height
		s.width = m.Width
		return s, nil

	case tea.KeyMsg:
		// While the detail pane is open, any key closes it. Nothing else is
		// actionable from there, so trapping keys would only confuse.
		if s.detail {
			if m.Type == tea.KeyCtrlC {
				return s, tea.Quit
			}
			s.detail = false
			return s, nil
		}

		switch m.Type {
		case tea.KeyCtrlC:
			return s, tea.Quit
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

		switch string(m.Runes) {
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
			// Approve, deny, and close are distinct capital keys rather than
			// a shared confirm, so the destructive action is always named
			// explicitly and each mode exposes only its own.
			if s.mode == ModeReview {
				return s.commit("approve")
			}
		case "D":
			if s.mode == ModeReview {
				return s.commit("deny")
			}
		case "C":
			if s.mode == ModeTriage {
				return s.commit("close")
			}
		}
		return s, nil
	}
	return s, nil
}

// commit records the decision only when something is actually checked, so a
// stray capital key on an empty selection cannot act on the whole screen.
func (s *Screen) commit(action string) (tea.Model, tea.Cmd) {
	if s.sel.CheckedCount() == 0 {
		return s, nil
	}
	s.decision = Decision{Action: action, Rows: s.sel.Checked()}
	return s, tea.Quit
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
	act := "A approve  D deny"
	if s.mode == ModeTriage {
		act = "C close"
	}
	return fmt.Sprintf("%d/%d selected   space toggle  a all  n none  enter detail  %s  q abort",
		s.sel.CheckedCount(), total, act)
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
