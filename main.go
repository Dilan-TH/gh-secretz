// Command gh-secretz reviews GitHub secret scanning dismissal requests and
// triages open alerts that have no request yet.
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/term"

	"github.com/Dilan-TH/gh-secretz/internal/cli"
	"github.com/Dilan-TH/gh-secretz/internal/config"
	"github.com/Dilan-TH/gh-secretz/internal/transport"
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
	case "list", "review", "show", "discover", "triage", "close":
		env, err := buildEnv(stdout, stderr)
		if err != nil {
			fmt.Fprintln(stderr, err)
			return 2
		}
		return cli.Dispatch(env, args)
	default:
		fmt.Fprintf(stderr, "unknown subcommand %q\n\n%s", args[0], usage)
		return 2
	}
}

// buildEnv assembles the real dependencies. It is separate from run so the
// dispatch table above stays testable without network or filesystem access.
func buildEnv(stdout, stderr io.Writer) (cli.Env, error) {
	t, err := transport.New()
	if err != nil {
		return cli.Env{}, err
	}
	dir, err := config.Dir()
	if err != nil {
		return cli.Env{}, err
	}
	cfg, err := config.Load(filepath.Join(dir, "config.toml"))
	if err != nil {
		return cli.Env{}, err
	}

	return cli.Env{
		T:           t,
		Cfg:         cfg,
		Dir:         dir,
		Actor:       currentActor(t),
		Stdout:      stdout,
		Stderr:      stderr,
		Interactive: term.IsTerminal(int(os.Stdout.Fd())),
	}, nil
}

// currentActor resolves the authenticated login for the audit log. A failure
// is not fatal, since the audit entry is still more useful with an unknown
// actor than not written at all.
func currentActor(t transport.Transport) string {
	var user struct {
		Login string `json:"login"`
	}
	if err := t.GetJSON("user", &user); err != nil || user.Login == "" {
		return "unknown"
	}
	return user.Login
}
