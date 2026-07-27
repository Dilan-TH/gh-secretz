// Package config loads the optional TOML configuration from
// ~/.gh-secretz/config.toml.
//
// There are deliberately no built in defaults for the organization or the
// repo prefixes. The tool is not specific to any organization, and an empty
// prefix list matches nothing rather than everything, because a wildcard
// sweep of a large org would be thousands of API calls.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// ErrNoOrg is returned when no organization was supplied by flag or config.
var ErrNoOrg = errors.New("no organization given")

// Config is the on disk configuration.
type Config struct {
	Org          string   `toml:"org"`
	RepoPrefixes []string `toml:"repo_prefixes"`
	SecretTypes  []string `toml:"secret_types"`
	// TestPathPatterns overrides DefaultTestPathPatterns when non-empty.
	TestPathPatterns []string `toml:"test_path_patterns"`
}

// DefaultTestPathPatterns is used when TestPathPatterns is unset. Unlike
// RepoPrefixes, an unmatched path only under-flags a row rather than
// mis-scoping an expensive API sweep, so a built-in default is safe here.
var DefaultTestPathPatterns = []string{
	"test/", "tests/",
	"fixture/", "fixtures/",
	"e2e/",
	"spec/", "specs/",
	"__mocks__/", "mock/", "mocks/",
	"testdata/",
	"_test.", ".test.", ".spec.",
}

// Dir returns the tool's state directory, which holds config.toml,
// cache.json, and audit.jsonl.
func Dir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("locating home directory: %w", err)
	}
	return filepath.Join(home, ".gh-secretz"), nil
}

// Load reads the config. A missing file yields a zero Config and no error,
// because a first run legitimately has none. Malformed TOML is an error
// rather than being silently ignored, since silently discarding a repo prefix
// list would silently shrink a sweep.
func Load(path string) (Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, fmt.Errorf("reading %s: %w", path, err)
	}

	var c Config
	if err := toml.Unmarshal(b, &c); err != nil {
		return Config{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return c, nil
}

// Validate checks the minimum needed to make any API call.
func (c Config) Validate() error {
	if strings.TrimSpace(c.Org) == "" {
		return fmt.Errorf("%w: pass --org or set org in ~/.gh-secretz/config.toml", ErrNoOrg)
	}
	return nil
}

// TestPathPatternsOrDefault returns the configured test path patterns, or
// DefaultTestPathPatterns when none are configured.
func (c Config) TestPathPatternsOrDefault() []string {
	if len(c.TestPathPatterns) > 0 {
		return c.TestPathPatterns
	}
	return DefaultTestPathPatterns
}

// MatchesPrefix reports whether repo begins with any configured prefix.
// An empty prefix list matches nothing.
func (c Config) MatchesPrefix(repo string) bool {
	for _, p := range c.RepoPrefixes {
		if p == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(repo), strings.ToLower(p)) {
			return true
		}
	}
	return false
}
