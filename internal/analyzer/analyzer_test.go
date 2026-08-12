package analyzer

import (
	"reflect"
	"strings"
	"testing"
)

func TestHuntProxyParity(t *testing.T) {
	result := Analyze([]byte(`
      <a href="/account/settings?tab=security">Settings</a>
      <script>
        const api = '/api/v2/users/:id';
        const escaped = 'https:\/\/api.target.test\/v1\/users';
        const websocket = 'wss://events.target.test/socket';
        const email = 'security@target.test';
        const placeholder = 'admin@example.com';
        const secret = 'sk_live_012345678901234567890123';
      </script>`))
	wantEndpoints := []string{"/account/settings?tab=security", "/api/v2/users/:id"}
	wantURLs := []string{"https://api.target.test/v1/users", "wss://events.target.test/socket"}
	if !reflect.DeepEqual(result.Endpoints, wantEndpoints) {
		t.Fatalf("endpoints: got %#v want %#v", result.Endpoints, wantEndpoints)
	}
	if !reflect.DeepEqual(result.URLs, wantURLs) {
		t.Fatalf("urls: got %#v want %#v", result.URLs, wantURLs)
	}
	if !reflect.DeepEqual(result.Emails, []string{"security@target.test"}) {
		t.Fatalf("emails: %#v", result.Emails)
	}
	if !reflect.DeepEqual(result.Parameters, []string{"tab"}) {
		t.Fatalf("parameters: %#v", result.Parameters)
	}
}

func TestFiltersAndLiteralSynchronization(t *testing.T) {
	result := Analyze([]byte(`'/images/logo.png' '/api/logo.png' 'mongodb://user:pass@db.target.test/x' "".concat(prefix,"/aigc/cover-art")`))
	if !reflect.DeepEqual(result.Endpoints, []string{"/aigc/cover-art"}) {
		t.Fatalf("endpoints: %#v", result.Endpoints)
	}
	if len(result.Emails) != 0 {
		t.Fatalf("emails: %#v", result.Emails)
	}
	input := `"` + strings.Repeat("x", 3000) + `".concat(prefix,"/api/visible")`
	if got := Analyze([]byte(input)).Endpoints; !reflect.DeepEqual(got, []string{"/api/visible"}) {
		t.Fatalf("after oversized literal: %#v", got)
	}
}

func TestRelativeRoutesAndQuotedURLWithSpaces(t *testing.T) {
	result := Analyze([]byte(`'auth/callback' 'assets/app.js' 'random/library/path' 'https://cdn.example.test/contracts/Program Terms.pdf'`))
	if !reflect.DeepEqual(result.Endpoints, []string{"assets/app.js", "auth/callback"}) {
		t.Fatalf("endpoints: %#v", result.Endpoints)
	}
	if !reflect.DeepEqual(result.URLs, []string{"https://cdn.example.test/contracts/Program Terms.pdf"}) {
		t.Fatalf("urls: %#v", result.URLs)
	}
}

func TestFilesParametersAndGraphQL(t *testing.T) {
	result := Analyze([]byte(`
      <input name="csrf_token"><script src="/assets/app.min.js?v=42"></script>
      fetch('/graphql?tenant=acme'); const p = new URLSearchParams(); p.append('page_size', '50');
      const q = gql` + "`" + `query CurrentUser($id: ID!) { user(id: $id) { name } }` + "`" + `;
      const m = "mutation UpdateProfile($input: ProfileInput!) { updateProfile(input: $input) { id } }";
    `))
	if !reflect.DeepEqual(result.Files, []string{"/assets/app.min.js?v=42"}) {
		t.Fatalf("files: %#v", result.Files)
	}
	wantParams := []string{"csrf_token", "page_size", "tenant", "v"}
	if !reflect.DeepEqual(result.Parameters, wantParams) {
		t.Fatalf("params: got %#v want %#v", result.Parameters, wantParams)
	}
	wantOps := []string{"mutation UpdateProfile", "query CurrentUser"}
	if !reflect.DeepEqual(result.GraphQLOperations, wantOps) {
		t.Fatalf("operations: got %#v want %#v", result.GraphQLOperations, wantOps)
	}
}

func TestFileExtensionDoesNotMatchHostnamePrefix(t *testing.T) {
	result := Analyze([]byte(`const api = "https://api.target.test/v2/users"`))
	if len(result.Files) != 0 {
		t.Fatalf("hostname prefix was classified as a file: %#v", result.Files)
	}
}

func TestDeduplicatesProtocolRelativeURL(t *testing.T) {
	result := Analyze([]byte(`"https://api.target.test/v1" "//api.target.test/v1"`))
	if !reflect.DeepEqual(result.URLs, []string{"https://api.target.test/v1"}) {
		t.Fatalf("urls: %#v", result.URLs)
	}
}

func TestPathRegexDoesNotScanAcrossOversizedCandidate(t *testing.T) {
	input := `'` + "/" + strings.Repeat("a", 3000) + `' '/api/after'`
	result := Analyze([]byte(input))
	if !reflect.DeepEqual(result.Endpoints, []string{"/api/after"}) {
		t.Fatalf("endpoints: %#v", result.Endpoints)
	}
}
