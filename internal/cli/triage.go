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
)

func runTriage(env Env, args []string) int {
	fs := flag.NewFlagSet("triage", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	g := GlobalFlags(fs)
	repo := fs.String("repo", "", "repository to triage")
	all := fs.Bool("all", false, "triage every repository in the discovery cache")
	resolution := fs.String("resolution", "", "revoked, false_positive, used_in_tests, or wont_fix")
	comment := fs.String("comment", "", "resolution comment recorded on every close")
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
	for _, name := range targets {
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
	}

	header := fmt.Sprintf("%d open alerts with no dismissal request, %d secret types enumerated",
		len(rows), typesQueried)

	dec, err := ui.Run(rows, ui.ModeTriage, header)
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 1
	}
	if dec.Action == "" {
		fmt.Fprintln(env.Stdout, "aborted, nothing sent")
		return 0
	}

	// Closing has no requester reason to inherit, so both fields are required
	// rather than defaulted.
	if err := executor.ValidateResolution(*resolution); err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 2
	}
	if err := executor.ValidateMessage(*comment); err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 2
	}

	ex := executor.Executor{T: env.T, Actor: env.Actor, AuditPath: env.auditPath()}
	results, err := ex.Run(dec.Rows, executor.ActionClose, *comment, *resolution)
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 2
	}

	report(env, results)
	return ExitCode(results)
}
