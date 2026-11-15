package analyzer

import (
	"reflect"
	"strings"
	"testing"
)

func TestAnalyzeExtractsCoreArtifacts(t *testing.T) {
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
	if !reflect.DeepEqual(result.Parameters, []string{"id", "tab"}) {
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
	wantParams := []string{"csrf_token", "id", "input", "page_size", "tenant", "v"}
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

func TestRequestRouteAndGraphQLParameters(t *testing.T) {
	result := Analyze([]byte(`fetch('/api/users/:id',{body:JSON.stringify({username:'a', remember_me:true})}); query User($userId: ID!) { user { id } }`))
	want := []string{"id", "remember_me", "userId", "username"}
	if !reflect.DeepEqual(result.Parameters, want) {
		t.Fatalf("parameters: got %#v want %#v", result.Parameters, want)
	}
}

func TestStaticAssetsDoNotBecomeFileFindings(t *testing.T) {
	result := Analyze([]byte(`"/images/avatar.png" "icons/logo.svg" "styles/app.css" "app.js.map" "fonts/ui.woff2" "app.js" "terms.pdf"`))
	want := []string{"app.js", "terms.pdf"}
	if !reflect.DeepEqual(result.Files, want) {
		t.Fatalf("files: got %#v want %#v", result.Files, want)
	}
}

func TestStaticAssetsDoNotBecomeURLFindings(t *testing.T) {
	result := Analyze([]byte(`"https://cdn.test/logo.png" "https://cdn.test/app.css?v=1" "https://cdn.test/app.js.map" "https://cdn.test/app.js" "https://cdn.test/terms.pdf"`))
	want := []string{"https://cdn.test/app.js", "https://cdn.test/terms.pdf"}
	if !reflect.DeepEqual(result.URLs, want) {
		t.Fatalf("urls: got %#v want %#v", result.URLs, want)
	}
}

func TestGraphQLOperationsRequireSelectionSet(t *testing.T) {
	result := Analyze([]byte(`subscription will renew. query CurrentUser($id: ID!) { viewer { id } } mutation Update { save }`))
	want := []string{"mutation Update", "query CurrentUser"}
	if !reflect.DeepEqual(result.GraphQLOperations, want) {
		t.Fatalf("operations: got %#v want %#v", result.GraphQLOperations, want)
	}
}

func TestDeduplicatesProtocolRelativeURL(t *testing.T) {
	result := Analyze([]byte(`"https://api.target.test/v1" "//api.target.test/v1"`))
	if !reflect.DeepEqual(result.URLs, []string{"https://api.target.test/v1"}) {
		t.Fatalf("urls: %#v", result.URLs)
	}
}

func TestFiltersFrameworkDummyURLBases(t *testing.T) {
	result := Analyze([]byte(`new URL(value,"http://n"); new URL(path,"http://f"); "http://localhost:3000" "http://a.test" "http://x/path"`))
	want := []string{"http://a.test", "http://localhost:3000", "http://x/path"}
	if !reflect.DeepEqual(result.URLs, want) {
		t.Fatalf("urls: got %#v want %#v", result.URLs, want)
	}
}

func TestAbsoluteURLIsNotAlsoAFile(t *testing.T) {
	result := Analyze([]byte(`"https://reactjs.org/docs/error-decoder.html?invariant=" "/docs/local.pdf"`))
	if !reflect.DeepEqual(result.URLs, []string{"https://reactjs.org/docs/error-decoder.html?invariant="}) {
		t.Fatalf("urls: %#v", result.URLs)
	}
	if !reflect.DeepEqual(result.Files, []string{"/docs/local.pdf"}) {
		t.Fatalf("files: %#v", result.Files)
	}
}

func TestFiltersW3CNamespaceIdentifiers(t *testing.T) {
	result := Analyze([]byte(`"http://www.w3.org/1998/Math/MathML" "http://www.w3.org/1999/xhtml" "http://www.w3.org/1999/xlink" "http://www.w3.org/2000/svg" "http://www.w3.org/XML/1998/namespace" "https://www.w3.org/TR/html5/"`))
	if !reflect.DeepEqual(result.URLs, []string{"https://www.w3.org/TR/html5/"}) {
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
