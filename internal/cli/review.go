package cli

import (
	"flag"
	"fmt"

	"github.com/Dilan-TH/gh-secretz/internal/executor"
	"github.com/Dilan-TH/gh-secretz/internal/ui"
)

func runReview(env Env, args []string) int {
	fs := flag.NewFlagSet("review", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	g := GlobalFlags(fs)
	spec := FilterFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	org, err := env.resolveOrg(g.Org)
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 2
	}

	// review changes state, so it demands an explicit scope.
	if err := spec.Validate(true); err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 2
	}

	if !env.Interactive {
		fmt.Fprintln(env.Stderr,
			"review needs a terminal for its multi select. Use \"gh secretz list\" for scriptable output.")
		return 2
	}

	rows, skips, types, err := loadRows(env, org, g.TimePeriod, spec.Repo, splitTypes(g.SecretTypes))
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 1
	}
	printSkips(env, skips)
	rows = spec.Apply(rows)

	header := fmt.Sprintf("%d pending, filters: %v, %d secret types enumerated",
		len(rows), spec.Names(), types)

	dec, err := ui.Run(rows, ui.ModeReview, header, snippetFetcher(env))
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 1
	}
	if dec.Action == "" {
		fmt.Fprintln(env.Stdout, "aborted, nothing sent")
		return 0
	}

	act := executor.ActionApprove
	if dec.Action == "deny" {
		act = executor.ActionDeny
	}

	ex := executor.Executor{T: env.T, Actor: env.Actor, AuditPath: env.auditPath()}
	results, err := ex.Run(dec.Rows, act, dec.Comment, "")
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 2
	}

	report(env, results)
	return ExitCode(results)
}

// report prints one line per write, using the verified outcome.
func report(env Env, results []executor.Result) {
	for _, r := range results {
		line := fmt.Sprintf("%-16s %s %s", r.Outcome, r.Key, r.Detail)
		if r.Outcome == executor.OutcomeDone {
			fmt.Fprintln(env.Stdout, line)
			continue
		}
		fmt.Fprintln(env.Stderr, line)
	}
	done, benign, failed := executor.Summarise(results)
	fmt.Fprintf(env.Stdout, "\n%d verified, %d already handled, %d failed\n", done, benign, failed)
}
