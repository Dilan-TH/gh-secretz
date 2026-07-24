// Package cli wires flags to the domain layers. It holds no domain logic of
// its own beyond argument handling and output formatting.
package cli

import (
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"strings"

	"github.com/Dilan-TH/gh-secretz/internal/config"
	"github.com/Dilan-TH/gh-secretz/internal/executor"
	"github.com/Dilan-TH/gh-secretz/internal/filter"
	"github.com/Dilan-TH/gh-secretz/internal/transport"
)

// Env is everything a subcommand needs, injected so tests drive the CLI with
// a fake transport and buffers.
type Env struct {
	T           transport.Transport
	Cfg         config.Config
	Dir         string
	Actor       string
	Stdout      io.Writer
	Stderr      io.Writer
	Interactive bool
	// Width is the terminal width for table output. Zero means unlimited,
	// which is what piped output wants: truncating a comment in a file the
	// operator is going to grep is pure loss.
	Width int
}

func (e Env) cachePath() string { return filepath.Join(e.Dir, "cache.json") }
func (e Env) auditPath() string { return filepath.Join(e.Dir, "audit.jsonl") }

// Globals are the flags every subcommand accepts.
type Globals struct {
	Org         string
	TimePeriod  string
	SecretTypes string
	JSON        bool
}

// GlobalFlags registers the shared flags on fs.
func GlobalFlags(fs *flag.FlagSet) *Globals {
	g := &Globals{}
	fs.StringVar(&g.Org, "org", "", "GitHub organization (required unless set in config)")
	fs.StringVar(&g.TimePeriod, "time-period", "month", "hour, day, week, or month (month is the API maximum)")
	fs.StringVar(&g.SecretTypes, "secret-types", "", "comma separated secret type slugs to enumerate")
	fs.BoolVar(&g.JSON, "json", false, "emit JSON instead of a table")
	return g
}

// FilterFlags registers the row filters on fs.
func FilterFlags(fs *flag.FlagSet) *filter.Spec {
	s := &filter.Spec{}
	fs.StringVar(&s.Repo, "repo", "", "limit to one repository name")
	fs.StringVar(&s.Requester, "requester", "", "limit to one requester login")
	fs.StringVar(&s.Reason, "reason", "", "limit to one reason, such as revoked")
	fs.StringVar(&s.SecretType, "secret-type", "", "limit to one secret type, slug or display name")
	fs.BoolVar(&s.OnlyWarned, "only-warned", false, "limit to rows carrying a warning")
	return s
}

// resolveOrg prefers the flag, falls back to config, and validates.
func (e Env) resolveOrg(flagOrg string) (string, error) {
	cfg := e.Cfg
	if flagOrg != "" {
		cfg.Org = flagOrg
	}
	if err := cfg.Validate(); err != nil {
		return "", err
	}
	return cfg.Org, nil
}

func splitTypes(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// ExitCode maps write results to a process exit code. Anything other than a
// verified success makes the run non zero, so a partially applied batch never
// looks like success to a script.
func ExitCode(results []executor.Result) int {
	_, _, failed := executor.Summarise(results)
	if failed > 0 {
		return 1
	}
	return 0
}

// Dispatch routes a subcommand.
func Dispatch(env Env, args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(env.Stderr, "no subcommand given")
		return 2
	}

	switch args[0] {
	case "list":
		return runList(env, args[1:])
	case "review":
		return runReview(env, args[1:])
	case "show":
		return runShow(env, args[1:])
	case "discover":
		return runDiscover(env, args[1:])
	case "triage":
		return runTriage(env, args[1:])
	case "close":
		return runClose(env, args[1:])
	default:
		fmt.Fprintf(env.Stderr, "unknown subcommand %q\n", args[0])
		return 2
	}
}
