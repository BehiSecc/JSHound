package overview

import (
	"bytes"
	"strings"
	"testing"
)

func TestAnalyzeBuildsDeterministicReconBrief(t *testing.T) {
	result := Analyze([]Source{{Name: "app.js", Data: []byte(`
window.__NEXT_DATA__ = {};
self.webpackChunkapp = self.webpackChunkapp || [];
fetch('/api/v1/login', { method: 'POST', credentials: 'include', headers: { Authorization: 'Bearer token' } });
localStorage.setItem('access_token', token);
const query = ` + "`" + `query CurrentUser { viewer { id } }` + "`" + `;
new WebSocket('wss://events.example.test/socket');
window.addEventListener('message', event => render(event.data));
node.innerHTML = content;
eval(code);
location.assign('/account');
fetch('https://js.stripe.com/v3/');
//# sourceMappingURL=app.js.map
`)}})

	if got, want := result.Frameworks, []string{"Next.js"}; !sameStrings(got, want) {
		t.Fatalf("frameworks: got %q want %q", got, want)
	}
	if got, want := result.Bundlers, []string{"Webpack"}; !sameStrings(got, want) {
		t.Fatalf("bundlers: got %q want %q", got, want)
	}
	if !result.SourceMap {
		t.Fatal("source-map reference was not detected")
	}
	if !hasRequest(result.Requests, "/api/v1/login", "POST") {
		t.Fatalf("login request missing: %#v", result.Requests)
	}
	if !contains(result.Authentication, "Authorization header handling") || !contains(result.Authentication, "Cookie credentials") {
		t.Fatalf("authentication: %#v", result.Authentication)
	}
	if len(result.Storage) != 1 || result.Storage[0] != (StorageOperation{Store: "localStorage", Key: "access_token", Action: "write"}) {
		t.Fatalf("storage: %#v", result.Storage)
	}
	if !contains(result.GraphQLOperations, "query CurrentUser") || !contains(result.ThirdParties, "Stripe (payments)") || !contains(result.Capabilities, "WebSocket") {
		t.Fatalf("overview omitted observed behavior: %#v", result)
	}
	for _, value := range []string{"HTML injection sink present", "dynamic code-execution sink present", "postMessage receiver without recognizable event.origin validation"} {
		if !hasReview(result.ReviewTargets, value) {
			t.Errorf("missing review target %q: %#v", value, result.ReviewTargets)
		}
	}

	var output bytes.Buffer
	if err := WriteText(&output, result); err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"APPLICATION OVERVIEW", "PROFILE", "OBSERVED API SURFACE", "AUTHENTICATION", "STORAGE", "REVIEW TARGETS", "LIMITATIONS"} {
		if !strings.Contains(output.String(), section) {
			t.Errorf("missing %s:\n%s", section, output.String())
		}
	}
}

func TestAnalyzeIgnoresCommentedSignals(t *testing.T) {
	result := Analyze([]Source{{Name: "app.js", Data: []byte(`
// eval(code); node.innerHTML = untrusted; fetch('/internal/debug')
/* window.addEventListener('message', handler); */
const label = 'normal text';
`)}})
	if len(result.ReviewTargets) != 0 || len(result.Routes) != 0 || len(result.Requests) != 0 {
		t.Fatalf("comments became behavior: %#v", result)
	}
}

func sameStrings(got, want []string) bool {
	return strings.Join(got, "\x00") == strings.Join(want, "\x00")
}
func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func hasRequest(values []Request, target, method string) bool {
	for _, value := range values {
		if value.Target == target && value.Method == method {
			return true
		}
	}
	return false
}
func hasReview(values []ReviewTarget, want string) bool {
	for _, value := range values {
		if value.Value == want {
			return true
		}
	}
	return false
}
