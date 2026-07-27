package cli

import (
	"errors"
	"flag"
	"fmt"
	"time"

	"github.com/Dilan-TH/gh-secretz/internal/alerts"
	"github.com/Dilan-TH/gh-secretz/internal/discover"
	"github.com/Dilan-TH/gh-secretz/internal/enrich"
	"github.com/Dilan-TH/gh-secretz/internal/executor"
	"github.com/Dilan-TH/gh-secretz/internal/model"
	"github.com/Dilan-TH/gh-secretz/internal/queue"
	"github.com/Dilan-TH/gh-secretz/internal/ui"
	"github.com/schollz/progressbar/v3"
)

func runTriage(env Env, args []string) int {
	fs := flag.NewFlagSet("triage", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	g := GlobalFlags(fs)
	repo := fs.String("repo", "", "repository to triage")
	all := fs.Bool("all", false, "triage every repository in the discovery cache")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	org, err := env.resolveOrg(g.Org)
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 2
	}

	if *repo == "" && !*all {
		fmt.Fprintln(env.Stderr, "triage needs an explicit scope: pass --repo <name> or --all")
		return 2
	}
	if !env.Interactive {
		fmt.Fprintln(env.Stderr, "triage needs a terminal for its multi select")
		return 2
	}

	targets := []string{*repo}
	if *all {
		cache, err := discover.LoadCache(env.cachePath())
		if err != nil {
			if errors.Is(err, discover.ErrNoCache) {
				fmt.Fprintln(env.Stderr, "no discovery cache yet, run \"gh secretz discover\" first")
				return 2
			}
			fmt.Fprintln(env.Stderr, err)
			return 1
		}
		targets = nil
		for _, h := range cache.Repos {
			targets = append(targets, h.Repo)
		}
		fmt.Fprintf(env.Stdout, "using discovery cache from %s ago, %d repos\n",
			cache.Age(nowUTC()).Round(time.Minute), len(targets))
	}

	var rows []model.Row
	typesQueried := 0
	var bar *progressbar.ProgressBar
	if len(targets) > 0 {
		bar = newProgressBar(env.Stderr, len(targets), "processing repos")
		defer func() { _ = bar.Finish() }()
	}
	for i, name := range targets {
		res, err := alerts.List(env.T, alerts.Options{
			Owner: org, Repo: name, State: "open", SecretTypes: splitTypes(g.SecretTypes),
		})
		if err != nil {
			fmt.Fprintln(env.Stderr, err)
			return 1
		}
		typesQueried = res.SecretTypesQueried

		reqs, _, err := queue.List(env.T, queue.Options{
			Org: org, Repo: name, RequestStatus: "all", TimePeriod: g.TimePeriod,
		})
		if err != nil {
			// A repo with no request history is normal, not fatal.
			reqs = nil
		}

		got, dis := enrich.Unrequested(res.Alerts, reqs)
		for _, d := range dis {
			fmt.Fprintf(env.Stderr, "withheld %s: %s\n", d.Key, d.Detail)
		}
		rows = append(rows, got...)
		if bar != nil {
			_ = bar.Set(i + 1)
		}
	}

	header := fmt.Sprintf("%d open alerts with no dismissal request, %d secret types enumerated",
		len(rows), typesQueried)

	dec, err := ui.Run(rows, ui.ModeTriage, header, snippetFetcher(env))
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 1
	}
	if dec.Action == "" {
		fmt.Fprintln(env.Stdout, "aborted, nothing sent")
		return 0
	}

	// The reason and comment come from the screen, chosen alongside the rows,
	// so there is nothing left to validate after the fact and no way to lose
	// a selection to a missing flag.
	var writeBar *progressbar.ProgressBar
	if len(dec.Rows) > 0 {
		writeBar = newProgressBar(env.Stderr, len(dec.Rows), "applying")
	}
	ex := executor.Executor{T: env.T, Actor: env.Actor, AuditPath: env.auditPath(), Progress: func(done, total int) {
		if writeBar != nil {
			_ = writeBar.Set(done)
		}
	}}
	results, err := ex.Run(dec.Rows, executor.ActionClose, dec.Comment, dec.Resolution)
	if writeBar != nil {
		_ = writeBar.Finish()
	}
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 2
	}

	report(env, results)
	return ExitCode(results)
}
