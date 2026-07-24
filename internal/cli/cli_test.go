package cli

import (
	"bytes"
	"strings"
	"testing"

	"github.com/Dilan-TH/gh-secretz/internal/config"
	"github.com/Dilan-TH/gh-secretz/internal/executor"
	"github.com/Dilan-TH/gh-secretz/internal/queue"
	"github.com/Dilan-TH/gh-secretz/internal/transport"
)

func env(t *testing.T, f *transport.Fake, interactive bool) (Env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errOut bytes.Buffer
	return Env{
		T:           f,
		Cfg:         config.Config{Org: "acme"},
		Dir:         t.TempDir(),
		Actor:       "tester",
		Stdout:      &out,
		Stderr:      &errOut,
		Interactive: interactive,
	}, &out, &errOut
}

func TestListRunsWithoutFilters(t *testing.T) {
	f := transport.NewFake()
	opts := queue.Options{Org: "acme", RequestStatus: "open", TimePeriod: "month"}
	f.SetPage(queue.Path(opts), `[{
		"id":1,"number":5,"repository":{"name":"alpha","full_name":"acme/alpha"},
		"organization":{"name":"acme"},"requester":{"actor_name":"alice"},
		"data":[{"reason":"revoked","secret_type":"Password","alert_number":"18"}],
		"resource_identifier":"18","status":"pending",
		"created_at":"2026-07-24T00:00:00Z","expires_at":"2026-07-31T00:00:00Z"}]`)

	e, out, _ := env(t, f, false)
	if code := Dispatch(e, []string{"list"}); code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "#18") {
		t.Errorf("output should list alert 18, got:\n%s", out.String())
	}
}

func TestReviewRefusesWithoutFilter(t *testing.T) {
	f := transport.NewFake()
	e, _, errOut := env(t, f, true)

	code := Dispatch(e, []string{"review"})
	if code == 0 {
		t.Fatal("review must refuse to run without a filter")
	}
	if !strings.Contains(errOut.String(), "--repo") {
		t.Errorf("stderr should list available filters, got: %s", errOut.String())
	}
	if len(f.Patches) != 0 {
		t.Errorf("sent %d writes, want 0", len(f.Patches))
	}
}

func TestReviewRefusesWhenNotInteractive(t *testing.T) {
	// Piping review into a script would hang on a TUI, so it fails fast and
	// points at the scriptable command instead.
	f := transport.NewFake()
	e, _, errOut := env(t, f, false)

	code := Dispatch(e, []string{"review", "--repo", "alpha"})
	if code == 0 {
		t.Fatal("review must refuse without a TTY")
	}
	if !strings.Contains(errOut.String(), "list") {
		t.Errorf("stderr should point at the list command, got: %s", errOut.String())
	}
}

func TestTriageRefusesWithoutRepoOrAll(t *testing.T) {
	f := transport.NewFake()
	e, _, errOut := env(t, f, true)

	if code := Dispatch(e, []string{"triage"}); code == 0 {
		t.Fatal("triage must require --repo or --all")
	}
	if !strings.Contains(errOut.String(), "--all") {
		t.Errorf("stderr should mention --all, got: %s", errOut.String())
	}
}

func TestMissingOrgIsReported(t *testing.T) {
	f := transport.NewFake()
	e, _, errOut := env(t, f, false)
	e.Cfg = config.Config{}

	if code := Dispatch(e, []string{"list"}); code == 0 {
		t.Fatal("a missing org must be an error")
	}
	if !strings.Contains(errOut.String(), "--org") {
		t.Errorf("stderr should tell the operator to pass --org, got: %s", errOut.String())
	}
}

func TestExitCodeNonZeroWhenAnyItemFailed(t *testing.T) {
	// A partially applied batch must not look like success to a script.
	rs := []executor.Result{
		{Outcome: executor.OutcomeDone},
		{Outcome: executor.OutcomeForbidden},
	}
	if got := ExitCode(rs); got == 0 {
		t.Errorf("ExitCode() = %d, want non zero when an item failed", got)
	}
	if got := ExitCode([]executor.Result{{Outcome: executor.OutcomeDone}}); got != 0 {
		t.Errorf("ExitCode() = %d, want 0 when everything succeeded", got)
	}
}

func TestExitCodeNonZeroForRequestCreated(t *testing.T) {
	// A close that silently became a request is not success.
	rs := []executor.Result{{Outcome: executor.OutcomeRequestCreated}}
	if got := ExitCode(rs); got == 0 {
		t.Error("ExitCode() should be non zero when a close became a request")
	}
}

func TestUnknownSubcommandIsExitTwo(t *testing.T) {
	f := transport.NewFake()
	e, _, _ := env(t, f, false)
	if got := Dispatch(e, []string{"nope"}); got != 2 {
		t.Errorf("exit code = %d, want 2", got)
	}
}

func TestDiscoverRefusesWithoutRepoPrefixes(t *testing.T) {
	// An empty prefix list matches nothing by design, so sweeping with none
	// configured would silently do no work.
	f := transport.NewFake()
	e, _, errOut := env(t, f, false)

	if code := Dispatch(e, []string{"discover"}); code == 0 {
		t.Fatal("discover must refuse with no repo_prefixes configured")
	}
	if !strings.Contains(errOut.String(), "repo_prefixes") {
		t.Errorf("stderr should name repo_prefixes, got: %s", errOut.String())
	}
}

func TestTriageAllWithoutCacheTellsOperatorToDiscover(t *testing.T) {
	f := transport.NewFake()
	e, _, errOut := env(t, f, true)

	if code := Dispatch(e, []string{"triage", "--all"}); code == 0 {
		t.Fatal("triage --all must refuse when no cache exists")
	}
	if !strings.Contains(errOut.String(), "discover") {
		t.Errorf("stderr should point at discover, got: %s", errOut.String())
	}
}

func TestCloseRequiresRepoAndAlertNumber(t *testing.T) {
	f := transport.NewFake()
	e, _, errOut := env(t, f, false)

	if code := Dispatch(e, []string{"close", "alpha"}); code == 0 {
		t.Fatal("close must require both a repo and an alert number")
	}
	if !strings.Contains(errOut.String(), "usage:") {
		t.Errorf("stderr should show usage, got: %s", errOut.String())
	}
	if len(f.Patches) != 0 {
		t.Errorf("sent %d writes, want 0", len(f.Patches))
	}
}

func TestCloseRejectsNonNumericAlert(t *testing.T) {
	f := transport.NewFake()
	e, _, errOut := env(t, f, false)

	if code := Dispatch(e, []string{"close", "alpha", "notanumber"}); code == 0 {
		t.Fatal("close must reject a non numeric alert number")
	}
	if !strings.Contains(errOut.String(), "not an integer") {
		t.Errorf("stderr should say the alert number is not an integer, got: %s", errOut.String())
	}
	if len(f.Patches) != 0 {
		t.Errorf("sent %d writes, want 0", len(f.Patches))
	}
}

func TestExitCodeZeroWhenOnlyAlreadyReviewed(t *testing.T) {
	// A run that only encountered already reviewed requests did its job.
	// Exiting non zero would make a healthy scheduled run look broken.
	rs := []executor.Result{
		{Outcome: executor.OutcomeDone},
		{Outcome: executor.OutcomeAlreadyReviewed},
	}
	if got := ExitCode(rs); got != 0 {
		t.Errorf("ExitCode() = %d, want 0; already reviewed is benign", got)
	}
}

func TestReviewAllIsAnExplicitScope(t *testing.T) {
	// --all covers the whole queue, but typing it is a deliberate choice,
	// which is the property the filter requirement protects. Running bare
	// must still refuse.
	f := transport.NewFake()
	opts := queue.Options{Org: "acme", RequestStatus: "open", TimePeriod: "month"}
	f.SetPage(queue.Path(opts), `[]`)

	e, _, errOut := env(t, f, false)
	// Not interactive, so it stops at the TTY check rather than the scope
	// check. Reaching the TTY message proves the scope check passed.
	code := Dispatch(e, []string{"review", "--all"})
	if code == 0 {
		t.Fatal("review still needs a terminal")
	}
	if strings.Contains(errOut.String(), "no filter given") {
		t.Errorf("--all should satisfy the scope requirement, got: %s", errOut.String())
	}
	if !strings.Contains(errOut.String(), "terminal") {
		t.Errorf("expected the TTY refusal, got: %s", errOut.String())
	}
}

func TestReviewRefusalMentionsAll(t *testing.T) {
	f := transport.NewFake()
	e, _, errOut := env(t, f, true)
	Dispatch(e, []string{"review"})
	if !strings.Contains(errOut.String(), "--all") {
		t.Errorf("the refusal should mention --all as an option, got: %s", errOut.String())
	}
}
