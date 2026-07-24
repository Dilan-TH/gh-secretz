package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTemp(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatalf("writing temp config: %v", err)
	}
	return p
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	// A first run has no config. That must not be fatal.
	got, err := Load(filepath.Join(t.TempDir(), "absent.toml"))
	if err != nil {
		t.Fatalf("Load() error = %v, want nil for a missing file", err)
	}
	if got.Org != "" || len(got.RepoPrefixes) != 0 {
		t.Errorf("missing file should yield a zero Config, got %+v", got)
	}
}

func TestLoadReadsOrgAndPrefixes(t *testing.T) {
	p := writeTemp(t, `
org = "acme"
repo_prefixes = ["svc-", "lib-"]
secret_types = ["password", "jwt_header"]
`)
	got, err := Load(p)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got.Org != "acme" {
		t.Errorf("Org = %q, want acme", got.Org)
	}
	if len(got.RepoPrefixes) != 2 || got.RepoPrefixes[0] != "svc-" {
		t.Errorf("RepoPrefixes = %v", got.RepoPrefixes)
	}
	if len(got.SecretTypes) != 2 {
		t.Errorf("SecretTypes = %v", got.SecretTypes)
	}
}

func TestLoadMalformedTOMLIsAnError(t *testing.T) {
	p := writeTemp(t, `org = "unterminated`)
	if _, err := Load(p); err == nil {
		t.Fatal("Load() should report malformed TOML rather than silently ignoring it")
	}
}

func TestNoBuiltInOrgDefault(t *testing.T) {
	// The tool must not be specific to any organization. An empty config
	// yields no org, and Validate then demands one.
	c := Config{}
	err := c.Validate()
	if !errors.Is(err, ErrNoOrg) {
		t.Fatalf("err = %v, want ErrNoOrg", err)
	}
	if !strings.Contains(err.Error(), "--org") {
		t.Errorf("error %q should tell the operator how to supply an org", err.Error())
	}
}

func TestValidatePassesWithOrg(t *testing.T) {
	if err := (Config{Org: "acme"}).Validate(); err != nil {
		t.Errorf("Validate() error = %v", err)
	}
}

func TestMatchesPrefixEmptyListMatchesNothing(t *testing.T) {
	// An empty prefix list is not a wildcard. Sweeping every repo in a large
	// org by accident would be thousands of API calls.
	c := Config{Org: "acme"}
	if c.MatchesPrefix("anything") {
		t.Error("an empty prefix list must match nothing, not everything")
	}
}

func TestMatchesPrefix(t *testing.T) {
	c := Config{Org: "acme", RepoPrefixes: []string{"svc-", "lib-"}}
	tests := []struct {
		repo string
		want bool
	}{
		{"svc-billing", true},
		{"lib-common", true},
		{"web-portal", false},
		{"SVC-BILLING", true},
	}
	for _, tt := range tests {
		if got := c.MatchesPrefix(tt.repo); got != tt.want {
			t.Errorf("MatchesPrefix(%q) = %v, want %v", tt.repo, got, tt.want)
		}
	}
}

func TestDirIsUnderHome(t *testing.T) {
	got, err := Dir()
	if err != nil {
		t.Fatalf("Dir() error = %v", err)
	}
	if !strings.HasSuffix(got, ".gh-secretz") {
		t.Errorf("Dir() = %q, want it to end in .gh-secretz", got)
	}
	if !filepath.IsAbs(got) {
		t.Errorf("Dir() = %q, want an absolute path", got)
	}
}
