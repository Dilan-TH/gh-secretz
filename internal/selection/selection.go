// Package selection holds the TUI's cursor and checked set as a pure state
// machine, so key handling is tested without a terminal and the view layer
// stays free of logic.
package selection

import "github.com/Dilan-TH/gh-secretz/internal/model"

// Model tracks which rows the operator has selected.
type Model struct {
	rows    []model.Row
	cursor  int
	checked map[int]bool
}

// New builds a model with the cursor at the top and nothing checked. Nothing
// is ever checked by default, because a bulk tool must not arrive with a
// destructive action pre-armed.
func New(rows []model.Row) *Model {
	return &Model{rows: rows, checked: map[int]bool{}}
}

func (m *Model) Rows() []model.Row { return m.rows }
func (m *Model) Cursor() int       { return m.cursor }
func (m *Model) Len() int          { return len(m.rows) }

func (m *Model) MoveUp() {
	if m.cursor > 0 {
		m.cursor--
	}
}

func (m *Model) MoveDown() {
	if m.cursor < len(m.rows)-1 {
		m.cursor++
	}
}

// Toggle flips the checked state of the row under the cursor.
func (m *Model) Toggle() {
	if len(m.rows) == 0 {
		return
	}
	if m.checked[m.cursor] {
		delete(m.checked, m.cursor)
		return
	}
	m.checked[m.cursor] = true
}

func (m *Model) CheckAll() {
	for i := range m.rows {
		m.checked[i] = true
	}
}

func (m *Model) UncheckAll() {
	m.checked = map[int]bool{}
}

func (m *Model) IsChecked(i int) bool {
	if i < 0 || i >= len(m.rows) {
		return false
	}
	return m.checked[i]
}

func (m *Model) CheckedCount() int { return len(m.checked) }

// Checked returns the selected rows in display order, so a confirmation list
// reads the same way as the screen rather than in click order.
func (m *Model) Checked() []model.Row {
	out := make([]model.Row, 0, len(m.checked))
	for i := range m.rows {
		if m.checked[i] {
			out = append(out, m.rows[i])
		}
	}
	return out
}
