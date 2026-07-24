// Package discover sweeps an organization's repositories for open secret
// scanning alerts and caches which repos have them.
//
// A sweep is needed because the org wide alert endpoint,
// GET /orgs/{org}/secret-scanning/alerts, is gated on org owner or security
// manager and returns 404 to a custom role reviewer. The org security
// overview page in the web UI showing that data is not evidence the API will.
package discover

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/Dilan-TH/gh-secretz/internal/alerts"
	"github.com/Dilan-TH/gh-secretz/internal/config"
	"github.com/Dilan-TH/gh-secretz/internal/transport"
)

// ErrNoCache means no sweep has been recorded yet, which is distinct from a
// sweep that found nothing.
var ErrNoCache = errors.New("no discovery cache found")

// defaultWorkers bounds concurrency. Sweeps run hundreds of repos and the
// core rate limit is 5000 per hour, so this is about wall clock time rather
// than avoiding throttling.
const defaultWorkers = 8

// RepoLister returns the repository names, without owner, in an org.
type RepoLister func(org string) ([]string, error)

// RepoHit is a repository found to have open alerts.
type RepoHit struct {
	Repo       string `json:"repo"`
	OpenAlerts int    `json:"open_alerts"`
}

// Cache is the persisted sweep result.
type Cache struct {
	Org         string    `json:"org"`
	GeneratedAt time.Time `json:"generated_at"`
	Repos       []RepoHit `json:"repos"`
}

// Age reports how stale the cache is, so commands can print it.
func (c Cache) Age(now time.Time) time.Duration {
	return now.Sub(c.GeneratedAt)
}

// Sweep configures a discovery run.
type Sweep struct {
	Org     string
	Workers int
	Lister  RepoLister
	Cfg     config.Config
}

// Run probes every repo matching the configured prefixes and returns those
// with open alerts.
//
// progress may be nil. When set it is called once per completed repo and must
// be safe for concurrent use, since workers call it.
func (s Sweep) Run(t transport.Transport, progress func(done, total, hits int)) (Cache, error) {
	names, err := s.Lister(s.Org)
	if err != nil {
		return Cache{}, fmt.Errorf("listing repositories: %w", err)
	}

	var candidates []string
	for _, n := range names {
		if s.Cfg.MatchesPrefix(n) {
			candidates = append(candidates, n)
		}
	}

	workers := s.Workers
	if workers <= 0 {
		workers = defaultWorkers
	}

	var (
		mu    sync.Mutex
		hits  []RepoHit
		done  int
		jobs  = make(chan string)
		wg    sync.WaitGroup
		total = len(candidates)
	)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for repo := range jobs {
				n := s.countOpen(t, repo)

				mu.Lock()
				if n > 0 {
					hits = append(hits, RepoHit{Repo: repo, OpenAlerts: n})
				}
				done++
				d, h := done, len(hits)
				mu.Unlock()

				if progress != nil {
					progress(d, total, h)
				}
			}
		}()
	}

	for _, r := range candidates {
		jobs <- r
	}
	close(jobs)
	wg.Wait()

	// Sort rather than leaving completion order, so repeated sweeps produce
	// an identical cache instead of churning.
	sort.Slice(hits, func(i, j int) bool { return hits[i].Repo < hits[j].Repo })

	return Cache{Org: s.Org, GeneratedAt: time.Now().UTC(), Repos: hits}, nil
}

// countOpen returns the number of open alerts visible in a repo. Errors are
// swallowed deliberately: during a sweep of hundreds of repos, 404 from
// scanning being disabled or access being absent is the common case, and
// alerts.List already tolerates it.
func (s Sweep) countOpen(t transport.Transport, repo string) int {
	res, err := alerts.List(t, alerts.Options{
		Owner:       s.Org,
		Repo:        repo,
		State:       "open",
		SecretTypes: s.Cfg.SecretTypes,
	})
	if err != nil {
		return 0
	}
	return len(res.Alerts)
}

// GHRepoLister lists repositories via the gh CLI, which pages through the org
// far more conveniently than the REST endpoint.
func GHRepoLister(org string) ([]string, error) {
	cmd := exec.Command("gh", "repo", "list", org,
		"--limit", "5000", "--no-archived", "--json", "name")
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("gh repo list %s: %w", org, err)
	}

	var payload []struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(out, &payload); err != nil {
		return nil, fmt.Errorf("decoding gh repo list output: %w", err)
	}

	names := make([]string, 0, len(payload))
	for _, p := range payload {
		names = append(names, p.Name)
	}
	return names, nil
}

// Save writes the cache, creating the parent directory if needed.
func Save(path string, c Cache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("creating cache directory: %w", err)
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding cache: %w", err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	return nil
}

// LoadCache reads a previous sweep. A missing file is ErrNoCache so callers
// can distinguish "never swept" from "swept and found nothing".
func LoadCache(path string) (Cache, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Cache{}, ErrNoCache
		}
		return Cache{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var c Cache
	if err := json.Unmarshal(b, &c); err != nil {
		return Cache{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return c, nil
}
