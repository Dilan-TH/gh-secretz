package cli

import (
	"bytes"
	"os"
	"testing"
)

func TestIsTerminalWriterFalseForBuffer(t *testing.T) {
	var buf bytes.Buffer
	if isTerminalWriter(&buf) {
		t.Error("a bytes.Buffer is never a terminal")
	}
}

func TestIsTerminalWriterFalseForRegularFile(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "not-a-terminal")
	if err != nil {
		t.Fatalf("CreateTemp() error = %v", err)
	}
	defer f.Close()

	if isTerminalWriter(f) {
		t.Error("a regular file is never a terminal")
	}
}

func TestNewProgressBarIsInvisibleForNonTerminalWriter(t *testing.T) {
	// A bar rendering against a test buffer would pollute assertions on
	// command output, so it must stay silent for anything that isn't a real
	// terminal.
	var buf bytes.Buffer
	bar := newProgressBar(&buf, 10, "working")
	_ = bar.Add(5)
	if buf.Len() != 0 {
		t.Errorf("expected no output for a non terminal writer, got %q", buf.String())
	}
}
