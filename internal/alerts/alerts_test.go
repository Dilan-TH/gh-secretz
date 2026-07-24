package alerts

import (
	"strings"
	"testing"

	"github.com/Dilan-TH/gh-secretz/internal/transport"
)

func TestGenericSecretTypesCoversTheHiddenFamily(t *testing.T) {
	// These types are omitted from the default alert listing. Naming them
	// explicitly is the only way to see them.
	required := []string{"password", "http_bearer_authentication_header", "http_basic_authentication_header"}
	for _, want := range required {
		found := false
		for _, got := range GenericSecretTypes {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("GenericSecretTypes is missing %q, which the default list hides", want)
		}
	}
}

func TestUnionPathNamesTypesExplicitly(t *testing.T) {
	got := UnionPath(Options{Owner: "acme", Repo: "r", State: "open", SecretTypes: []string{"password", "jwt_header"}})
	if !strings.Contains(got, "secret_type=password%2Cjwt_header") && !strings.Contains(got, "secret_type=password,jwt_header") {
		t.Errorf("path %q must pass a comma separated secret_type list", got)
	}
	if !strings.Contains(got, "state=open") {
		t.Errorf("path %q must carry the state filter", got)
	}
}

func TestListUnionsDefaultAndExplicitAndDeduplicates(t *testing.T) {
	// The core behaviour. The default list returns a subset, the explicit
	// query returns the hidden types, and alert 14 appears in both.
	f := transport.NewFake()
	opts := Options{Owner: "acme", Repo: "r", State: "open", SecretTypes: []string{"password"}}

	f.SetPage(DefaultPath(opts), `[
		{"number":14,"state":"open","secret_type":"token_value","secret_type_display_name":"Token value","validity":"unknown"},
		{"number":15,"state":"open","secret_type":"secret_value","secret_type_display_name":"Secret value","validity":"unknown"}
	]`)
	f.SetPage(UnionPath(opts), `[
		{"number":14,"state":"open","secret_type":"token_value","secret_type_display_name":"Token value","validity":"unknown"},
		{"number":2,"state":"open","secret_type":"password","secret_type_display_name":"Password","validity":"active",
		 "closure_request_comment":"already requested"}
	]`)

	res, err := List(f, opts)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(res.Alerts) != 3 {
		t.Fatalf("got %d alerts, want 3 deduplicated (14, 15, 2)", len(res.Alerts))
	}

	idx := Index(res.Alerts)
	for _, n := range []int{2, 14, 15} {
		if _, ok := idx[n]; !ok {
			t.Errorf("alert %d missing from the union", n)
		}
	}
	if idx[2].SecretType != "password" {
		t.Errorf("alert 2 secret type = %q, want password", idx[2].SecretType)
	}
	if idx[2].Validity != "active" {
		t.Errorf("alert 2 validity = %q, want active", idx[2].Validity)
	}
	if !idx[2].HasClosureRequest() {
		t.Error("alert 2 has a closure request comment so HasClosureRequest should be true")
	}
	if idx[14].HasClosureRequest() {
		t.Error("alert 14 has no closure request comment")
	}
	if idx[2].Owner != "acme" || idx[2].Repo != "r" {
		t.Errorf("owner/repo not stamped onto alerts: %s/%s", idx[2].Owner, idx[2].Repo)
	}
}

func TestListReportsSecretTypesQueried(t *testing.T) {
	f := transport.NewFake()
	opts := Options{Owner: "acme", Repo: "r", State: "open", SecretTypes: []string{"password", "jwt_header"}}
	f.SetPage(DefaultPath(opts), `[]`)
	f.SetPage(UnionPath(opts), `[]`)

	res, err := List(f, opts)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	// Surfacing this count is how the completeness assumption stays visible
	// rather than implied.
	if res.SecretTypesQueried != 2 {
		t.Errorf("SecretTypesQueried = %d, want 2", res.SecretTypesQueried)
	}
}

func TestListDefaultsToGenericTypesWhenUnset(t *testing.T) {
	f := transport.NewFake()
	opts := Options{Owner: "acme", Repo: "r", State: "open"}
	f.SetPage(DefaultPath(opts), `[]`)
	f.SetPage(UnionPath(Options{Owner: "acme", Repo: "r", State: "open", SecretTypes: GenericSecretTypes}), `[]`)

	res, err := List(f, opts)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if res.SecretTypesQueried != len(GenericSecretTypes) {
		t.Errorf("SecretTypesQueried = %d, want %d", res.SecretTypesQueried, len(GenericSecretTypes))
	}
}

func TestListTolerates404OnOneQuery(t *testing.T) {
	// A repo with scanning disabled 404s. That is not a failure of the run.
	f := transport.NewFake()
	opts := Options{Owner: "acme", Repo: "r", State: "open", SecretTypes: []string{"password"}}
	// Neither path registered, so both 404.
	res, err := List(f, opts)
	if err != nil {
		t.Fatalf("List() should tolerate 404 and return empty, got error = %v", err)
	}
	if len(res.Alerts) != 0 {
		t.Errorf("got %d alerts, want 0", len(res.Alerts))
	}
}
