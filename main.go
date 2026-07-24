// Command gh-secretz reviews GitHub secret scanning dismissal requests and
// triages open alerts that have no request yet.
package main

import (
	"fmt"
	"io"
	"os"
)

// version is overridden at build time with -ldflags "-X main.version=v1.2.3".
var version = "dev"

const usage = `usage: gh secretz <command> [flags]

commands:
  list       list dismissal requests
  review     bulk approve or deny dismissal requests
  show       show one alert and its dismissal request
  discover   sweep repos for open alerts and cache the result
  triage     close open alerts that have no dismissal request
  close      close a single alert

run "gh secretz <command> --help" for command flags
`

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// run is the testable entry point. It returns the process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return 2
	}

	switch args[0] {
	case "--version", "-v", "version":
		fmt.Fprintf(stdout, "gh-secretz %s\n", version)
		return 0
	case "--help", "-h", "help":
		fmt.Fprint(stdout, usage)
		return 0
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n\n%s", args[0], usage)
		return 2
	}
}
