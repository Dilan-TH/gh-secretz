package cli

import (
	"io"
	"os"

	"github.com/schollz/progressbar/v3"
	"golang.org/x/term"
)

// newProgressBar renders a live progress bar for a loop of total known
// steps, writing to w.
//
// It stays invisible when w is not a real terminal: piped stderr, a
// redirected file, or a test buffer. This matches the rest of the CLI's
// convention of keeping piped output free of decoration (see ui.Format's
// handling of width <= 0), and means it writes nothing a script or test
// would need to account for.
func newProgressBar(w io.Writer, total int, description string) *progressbar.ProgressBar {
	return progressbar.NewOptions(total,
		progressbar.OptionSetWriter(w),
		progressbar.OptionSetVisibility(isTerminalWriter(w)),
		progressbar.OptionSetDescription(description),
		progressbar.OptionShowCount(),
		progressbar.OptionClearOnFinish(),
	)
}

// isTerminalWriter reports whether w is a real terminal, the same check
// main.go uses to decide whether the interactive multi select can run.
func isTerminalWriter(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	return term.IsTerminal(int(f.Fd()))
}
