package discover

import (
	"errors"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/Dilan-TH/gh-secretz/internal/alerts"
	"github.com/Dilan-TH/gh-secretz/internal/config"
	"github.com/Dilan-TH/gh-secretz/internal/transport"
)

func staticLister(names ...string) RepoLister {
	return func(string) ([]string, error) { return names, nil }
}

// openAlert registers one open alert for repo under both enumeration paths.
func openAlert(f *transport.Fake, owner, repo string, number int) {
	opts := alerts.Options{Owner: owner, Repo: repo, State: "open"}
	body := `[{"number":` + strconv.Itoa(number) + `,"state":"open","secret_type":"password",` +
		`"secret_type_display_name":"Password","validity":"unknown"}]`
	f.SetPage(alerts.DefaultPath(opts), `[]`)
	f.SetPage(alerts.UnionPath(opts), body)
}

func TestSweepFiltersByConfiguredPrefix(t *testing.T) {
	f := transport.NewFake()
	openAlert(f, "acme", "svc-billing", 1)
	openAlert(f, "acme", "web-portal", 2)

	s := Sweep{
		Org:     "acme",
		Workers: 4,
		Lister:  staticLister("svc-billing", "web-portal"),
		Cfg:     config.Config{Org: "acme", RepoPrefixes: []string{"svc-"}},
	}

	got, err := s.Run(f, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(got.Repos) != 1 {
		t.Fatalf("got %d hits, want 1; web-portal is outside the prefix", len(got.Repos))
	}
	if got.Repos[0].Repo != "svc-billing" {
		t.Errorf("hit = %q, want svc-billing", got.Repos[0].Repo)
	}
	if got.Repos[0].OpenAlerts != 1 {
		t.Errorf("OpenAlerts = %d, want 1", got.Repos[0].OpenAlerts)
	}
}

func TestSweepOmitsReposWithNoOpenAlerts(t *testing.T) {
	f := transport.NewFake()
	opts := alerts.Options{Owner: "acme", Repo: "svc-empty", State: "open"}
	f.SetPage(alerts.DefaultPath(opts), `[]`)
	f.SetPage(alerts.UnionPath(opts), `[]`)

	s := Sweep{Org: "acme", Workers: 2, Lister: staticLister("svc-empty"),
		Cfg: config.Config{Org: "acme", RepoPrefixes: []string{"svc-"}}}

	got, err := s.Run(f, nil)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(got.Repos) != 0 {
		t.Errorf("got %d hits, want 0", len(got.Repos))
	}
}

func TestSweepTolerates404Repos(t *testing.T) {
	// Repos with scanning disabled or without access 404. During a sweep of
	// hundreds of repos that is the common case, not a failure.
	f := transport.NewFake()
	openAlert(f, "acme", "svc-good", 1)
	// svc-denied has no registered paths, so it 404s.

	s := Sweep{Org: "acme", Workers: 4, Lister: staticLister("svc-good", "svc-denied"),
		Cfg: config.Config{Org: "acme", RepoPrefixes: []string{"svc-"}}}

	got, err := s.Run(f, nil)
	if err != nil {
		t.Fatalf("Run() should tolerate per repo 404, got error = %v", err)
	}
	if len(got.Repos) != 1 {
		t.Errorf("got %d hits, want 1", len(got.Repos))
	}
}

func TestSweepIsDeterministicUnderConcurrency(t *testing.T) {
	// Workers race, so the output must be sorted rather than in completion
	// order, otherwise the cache churns on every run.
	f := transport.NewFake()
	for _, r := range []string{"svc-c", "svc-a", "svc-b"} {
		openAlert(f, "acme", r, 1)
	}
	s := Sweep{Org: "acme", Workers: 8, Lister: staticLister("svc-c", "svc-a", "svc-b"),
		Cfg: config.Config{Org: "acme", RepoPrefixes: []string{"svc-"}}}

	for i := 0; i < 5; i++ {
		got, err := s.Run(f, nil)
		if err != nil {
			t.Fatalf("Run() error = %v", err)
		}
		if len(got.Repos) != 3 {
			t.Fatalf("got %d hits, want 3", len(got.Repos))
		}
		if got.Repos[0].Repo != "svc-a" || got.Repos[2].Repo != "svc-c" {
			t.Fatalf("results not sorted: %+v", got.Repos)
		}
	}
}

func TestSweepReportsProgress(t *testing.T) {
	f := transport.NewFake()
	openAlert(f, "acme", "svc-a", 1)
	openAlert(f, "acme", "svc-b", 1)

	var mu sync.Mutex
	var calls int
	var lastTotal int
	s := Sweep{Org: "acme", Workers: 2, Lister: staticLister("svc-a", "svc-b"),
		Cfg: config.Config{Org: "acme", RepoPrefixes: []string{"svc-"}}}

	if _, err := s.Run(f, func(done, total, hits int) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		lastTotal = total
	}); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if calls != 2 {
		t.Errorf("progress called %d times, want 2", calls)
	}
	if lastTotal != 2 {
		t.Errorf("total = %d, want 2", lastTotal)
	}
}

func TestSweepDefaultsWorkersWhenUnset(t *testing.T) {
	f := transport.NewFake()
	openAlert(f, "acme", "svc-a", 1)
	s := Sweep{Org: "acme", Lister: staticLister("svc-a"),
		Cfg: config.Config{Org: "acme", RepoPrefixes: []string{"svc-"}}}

	if _, err := s.Run(f, nil); err != nil {
		t.Fatalf("Run() with zero Workers should default rather than deadlock, got %v", err)
	}
}

func TestSweepPropagatesListerError(t *testing.T) {
	f := transport.NewFake()
	want := errors.New("gh repo list failed")
	s := Sweep{Org: "acme", Lister: func(string) ([]string, error) { return nil, want },
		Cfg: config.Config{Org: "acme", RepoPrefixes: []string{"svc-"}}}

	if _, err := s.Run(f, nil); !errors.Is(err, want) {
		t.Errorf("err = %v, want the lister error", err)
	}
}

func TestSaveAndLoadCacheRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "cache.json")
	want := Cache{
		Org:         "acme",
		GeneratedAt: time.Date(2026, 7, 24, 10, 0, 0, 0, time.UTC),
		Repos:       []RepoHit{{Repo: "svc-a", OpenAlerts: 3}},
	}
	if err := Save(p, want); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	got, err := LoadCache(p)
	if err != nil {
		t.Fatalf("LoadCache() error = %v", err)
	}
	if got.Org != "acme" || len(got.Repos) != 1 || got.Repos[0].OpenAlerts != 3 {
		t.Errorf("round trip mismatch: %+v", got)
	}
	if !got.GeneratedAt.Equal(want.GeneratedAt) {
		t.Errorf("GeneratedAt = %v, want %v", got.GeneratedAt, want.GeneratedAt)
	}
}

func TestLoadCacheMissingIsErrNoCache(t *testing.T) {
	// triage --all must be able to tell "never swept" from "swept and empty"
	// so it can tell the operator to run discover.
	_, err := LoadCache(filepath.Join(t.TempDir(), "absent.json"))
	if !errors.Is(err, ErrNoCache) {
		t.Errorf("err = %v, want ErrNoCache", err)
	}
}

func TestCacheAge(t *testing.T) {
	now := time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC)
	c := Cache{GeneratedAt: now.Add(-3 * time.Hour)}
	if got := c.Age(now); got != 3*time.Hour {
		t.Errorf("Age() = %v, want 3h", got)
	}
}
