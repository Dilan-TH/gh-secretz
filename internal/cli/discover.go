package cli

import (
	"flag"
	"fmt"
	"time"

	"github.com/Dilan-TH/gh-secretz/internal/config"
	"github.com/Dilan-TH/gh-secretz/internal/discover"
	"github.com/schollz/progressbar/v3"
)

func runDiscover(env Env, args []string) int {
	fs := flag.NewFlagSet("discover", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	g := GlobalFlags(fs)
	workers := fs.Int("workers", 8, "concurrent repo probes")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	org, err := env.resolveOrg(g.Org)
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 2
	}

	cfg := config.Config{Org: org, RepoPrefixes: env.Cfg.RepoPrefixes, SecretTypes: env.Cfg.SecretTypes}
	if len(splitTypes(g.SecretTypes)) > 0 {
		cfg.SecretTypes = splitTypes(g.SecretTypes)
	}
	if len(cfg.RepoPrefixes) == 0 {
		fmt.Fprintln(env.Stderr,
			"no repo_prefixes configured. Set repo_prefixes in ~/.gh-secretz/config.toml, "+
				"since an empty list deliberately matches nothing rather than sweeping every repo")
		return 2
	}

	s := discover.Sweep{Org: org, Workers: *workers, Lister: discover.GHRepoLister, Cfg: cfg}

	// The bar is created lazily because total is only known once Sweep.Run
	// has filtered candidates by prefix, which happens inside Run.
	var bar *progressbar.ProgressBar
	cache, err := s.Run(env.T, func(done, total, hits int) {
		if bar == nil {
			bar = newProgressBar(env.Stderr, total, "probing repos")
		}
		bar.Describe(fmt.Sprintf("probing repos, %d with alerts", hits))
		_ = bar.Set(done)
	})
	if bar != nil {
		_ = bar.Finish()
	}
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 1
	}

	if err := discover.Save(env.cachePath(), cache); err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 1
	}
	fmt.Fprintf(env.Stdout, "%d repos with open alerts cached to %s\n", len(cache.Repos), env.cachePath())
	return 0
}

// nowUTC is a package level clock so cache age formatting is consistent.
func nowUTC() time.Time { return time.Now().UTC() }
