package cli

import (
	"encoding/json"
	"flag"
	"fmt"

	"github.com/Dilan-TH/gh-secretz/internal/alerts"
	"github.com/Dilan-TH/gh-secretz/internal/enrich"
	"github.com/Dilan-TH/gh-secretz/internal/model"
	"github.com/Dilan-TH/gh-secretz/internal/queue"
	"github.com/Dilan-TH/gh-secretz/internal/ui"
)

// loadRows fetches pending requests and enriches them with their alerts.
// Enrichment fetches alerts once per repository rather than once per request.
func loadRows(env Env, org, timePeriod, repo string, secretTypes []string) ([]model.Row, []queue.Skip, int, error) {
	reqs, skips, err := queue.List(env.T, queue.Options{
		Org: org, Repo: repo, RequestStatus: "open", TimePeriod: timePeriod,
	})
	if err != nil {
		return nil, nil, 0, err
	}

	repos := map[string]bool{}
	for _, r := range reqs {
		repos[r.Repo] = true
	}

	byRepo := map[string]map[int]model.Alert{}
	typesQueried := 0
	for name := range repos {
		res, err := alerts.List(env.T, alerts.Options{
			Owner: org, Repo: name, State: "open", SecretTypes: secretTypes,
		})
		if err != nil {
			return nil, nil, 0, err
		}
		typesQueried = res.SecretTypesQueried
		byRepo[enrich.RepoKey(org, name)] = alerts.Index(res.Alerts)
	}

	return enrich.Join(reqs, byRepo), skips, typesQueried, nil
}

func printSkips(env Env, skips []queue.Skip) {
	for _, s := range skips {
		fmt.Fprintf(env.Stderr, "skipped %s request %d: %s\n", s.FullName, s.RequestNumber, s.Reason)
	}
}

func printRows(env Env, rows []model.Row, asJSON bool) {
	if asJSON {
		enc := json.NewEncoder(env.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rows)
		return
	}
	for _, r := range rows {
		fmt.Fprintln(env.Stdout, ui.Format(r, env.Width))
	}
}

func runList(env Env, args []string) int {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
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

	// list is read only, so it may run with no filter at all.
	if err := spec.Validate(false); err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 2
	}

	rows, skips, types, err := loadRows(env, org, g.TimePeriod, spec.Repo, splitTypes(g.SecretTypes))
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 1
	}
	printSkips(env, skips)

	rows = spec.Apply(rows)
	if !g.JSON {
		fmt.Fprintf(env.Stdout, "%d pending requests, enumerated alerts across %d secret types\n\n", len(rows), types)
	}
	printRows(env, rows, g.JSON)
	return 0
}
