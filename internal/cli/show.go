package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"strconv"
)

func runShow(env Env, args []string) int {
	fs := flag.NewFlagSet("show", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	g := GlobalFlags(fs)
	if err := fs.Parse(args); err != nil {
		return 2
	}

	org, err := env.resolveOrg(g.Org)
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 2
	}

	rest := fs.Args()
	if len(rest) != 2 {
		fmt.Fprintln(env.Stderr, "usage: gh secretz show <repo> <alert-number>")
		return 2
	}
	repo := rest[0]
	alertNum, err := strconv.Atoi(rest[1])
	if err != nil {
		fmt.Fprintf(env.Stderr, "alert number %q is not an integer\n", rest[1])
		return 2
	}

	var alert map[string]any
	alertPath := fmt.Sprintf("repos/%s/%s/secret-scanning/alerts/%d", org, repo, alertNum)
	if err := env.T.GetJSON(alertPath, &alert); err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 1
	}
	// The secret value itself is never printed.
	delete(alert, "secret")

	var request map[string]any
	reqPath := fmt.Sprintf("repos/%s/%s/dismissal-requests/secret-scanning/%d", org, repo, alertNum)
	if err := env.T.GetJSON(reqPath, &request); err != nil {
		request = nil
	}

	enc := json.NewEncoder(env.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(map[string]any{"alert": alert, "dismissal_request": request})
	return 0
}
