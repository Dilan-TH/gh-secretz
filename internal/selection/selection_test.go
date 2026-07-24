package selection

import (
	"testing"

	"github.com/Dilan-TH/gh-secretz/internal/model"
)

func threeRows() []model.Row {
	return []model.Row{
		{Request: &model.Request{Number: 1, AlertNumber: 10, Owner: "acme", Repo: "r"}},
		{Request: &model.Request{Number: 2, AlertNumber: 11, Owner: "acme", Repo: "r"}},
		{Request: &model.Request{Number: 3, AlertNumber: 12, Owner: "acme", Repo: "r"}},
	}
}

func TestNewStartsAtTopWithNothingChecked(t *testing.T) {
	m := New(threeRows())
	if m.Cursor() != 0 {
		t.Errorf("Cursor() = %d, want 0", m.Cursor())
	}
	// Nothing is checked by default. A bulk tool must never arrive with a
	// destructive action pre-armed.
	if m.CheckedCount() != 0 {
		t.Errorf("CheckedCount() = %d, want 0", m.CheckedCount())
	}
	if m.Len() != 3 {
		t.Errorf("Len() = %d, want 3", m.Len())
	}
}

func TestCursorClampsAtBothEnds(t *testing.T) {
	m := New(threeRows())
	m.MoveUp()
	if m.Cursor() != 0 {
		t.Errorf("moving up from the top gave cursor %d, want 0", m.Cursor())
	}
	m.MoveDown()
	m.MoveDown()
	m.MoveDown()
	m.MoveDown()
	if m.Cursor() != 2 {
		t.Errorf("moving past the end gave cursor %d, want 2", m.Cursor())
	}
}

func TestToggleIsIdempotentPerRow(t *testing.T) {
	m := New(threeRows())
	m.Toggle()
	if !m.IsChecked(0) {
		t.Error("first toggle should check the row under the cursor")
	}
	if m.CheckedCount() != 1 {
		t.Errorf("CheckedCount() = %d, want 1", m.CheckedCount())
	}
	m.Toggle()
	if m.IsChecked(0) {
		t.Error("second toggle should uncheck the row")
	}
	if m.CheckedCount() != 0 {
		t.Errorf("CheckedCount() = %d, want 0", m.CheckedCount())
	}
}

func TestCheckedReturnsRowsInDisplayOrder(t *testing.T) {
	m := New(threeRows())
	m.MoveDown()
	m.MoveDown()
	m.Toggle() // index 2
	m.MoveUp()
	m.MoveUp()
	m.Toggle() // index 0

	got := m.Checked()
	if len(got) != 2 {
		t.Fatalf("got %d checked rows, want 2", len(got))
	}
	// Order must follow the display, not the order of clicking, so the
	// confirmation list reads the same as the screen.
	if got[0].Request.Number != 1 {
		t.Errorf("first checked row is request %d, want 1", got[0].Request.Number)
	}
	if got[1].Request.Number != 3 {
		t.Errorf("second checked row is request %d, want 3", got[1].Request.Number)
	}
}

func TestCheckAllAndUncheckAll(t *testing.T) {
	m := New(threeRows())
	m.CheckAll()
	if m.CheckedCount() != 3 {
		t.Errorf("CheckedCount() = %d, want 3", m.CheckedCount())
	}
	m.UncheckAll()
	if m.CheckedCount() != 0 {
		t.Errorf("CheckedCount() = %d, want 0", m.CheckedCount())
	}
}

func TestEmptyModelIsSafeToDrive(t *testing.T) {
	m := New(nil)
	m.MoveDown()
	m.MoveUp()
	m.Toggle()
	m.CheckAll()
	if m.CheckedCount() != 0 || m.Len() != 0 {
		t.Errorf("empty model reported len %d checked %d", m.Len(), m.CheckedCount())
	}
	if got := m.Checked(); len(got) != 0 {
		t.Errorf("Checked() = %v, want empty", got)
	}
}

func TestIsCheckedOutOfRangeIsFalse(t *testing.T) {
	m := New(threeRows())
	m.CheckAll()
	if m.IsChecked(-1) || m.IsChecked(99) {
		t.Error("out of range indices must report unchecked rather than panic")
	}
}
