package cli

import (
	"flag"
	"fmt"
	"strconv"

	"github.com/Dilan-TH/gh-secretz/internal/executor"
	"github.com/Dilan-TH/gh-secretz/internal/model"
)

func runClose(env Env, args []string) int {
	fs := flag.NewFlagSet("close", flag.ContinueOnError)
	fs.SetOutput(env.Stderr)
	g := GlobalFlags(fs)
	resolution := fs.String("resolution", "", "revoked, false_positive, used_in_tests, or wont_fix")
	comment := fs.String("comment", "", "resolution comment")
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
		fmt.Fprintln(env.Stderr,
			"usage: gh secretz close <repo> <alert-number> --resolution <r> --comment <c>")
		return 2
	}
	repo := rest[0]
	alertNum, err := strconv.Atoi(rest[1])
	if err != nil {
		fmt.Fprintf(env.Stderr, "alert number %q is not an integer\n", rest[1])
		return 2
	}

	row := model.Row{Alert: &model.Alert{Number: alertNum, Owner: org, Repo: repo}}
	ex := executor.Executor{T: env.T, Actor: env.Actor, AuditPath: env.auditPath()}

	results, err := ex.Run([]model.Row{row}, executor.ActionClose, *comment, *resolution)
	if err != nil {
		fmt.Fprintln(env.Stderr, err)
		return 2
	}

	report(env, results)
	return ExitCode(results)
}
