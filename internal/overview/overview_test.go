package overview

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestAnalyzeBuildsSecurityReconStoryWithoutRawTargets(t *testing.T) {
	result := Analyze([]Source{{Name: "app.js", Data: []byte(`
window.__NEXT_DATA__ = {};
self.webpackChunkapp = self.webpackChunkapp || [];
fetch('/set/ticket/create/v1', { method: 'POST', credentials: 'include', headers: { Authorization: 'Bearer token' } });
fetch('/set/ticket/download_all/v1');
fetch('/trusted_flagger/verify/v1');
fetch('/set/profile/update/v1');
fetch('/set/feelgood/access_token/v1');
localStorage.setItem('access_token', token);
new WebSocket('wss://events.example.test/socket');
window.addEventListener('message', event => render(event.data));
node.innerHTML = content;
eval(code);
fetch('https://js.stripe.com/v3/');
//# sourceMappingURL=app.js.map
`)}})

	if !contains(result.Frameworks, "Next.js") || !contains(result.Bundlers, "Webpack") || !result.SourceMap {
		t.Fatalf("profile missing: %#v", result)
	}
	for _, title := range []string{"Ticket and case management", "Download and export workflow", "Trusted-flagger workflow", "Profile and organization management"} {
		if !hasInsight(result.Behavior, title) {
			t.Errorf("missing behavior %q: %#v", title, result.Behavior)
		}
	}
	for _, title := range []string{"Bearer authorization", "Authorization header handling", "Access-token acquisition", "Cookie-authenticated requests"} {
		if !hasInsight(result.Authentication, title) {
			t.Errorf("missing auth insight %q: %#v", title, result.Authentication)
		}
	}
	if len(result.Storage) != 1 || result.Storage[0] != (StorageOperation{Store: "localStorage", Key: "access_token", Action: "write"}) {
		t.Fatalf("storage: %#v", result.Storage)
	}
	for _, title := range []string{"Object-level authorization across ticket operations", "Download and bulk-export authorization", "Trusted-flagger enrollment and verification", "postMessage origin validation", "HTML injection sinks", "Dynamic code construction"} {
		if !hasReview(result.ReviewTargets, title) {
			t.Errorf("missing review target %q: %#v", title, result.ReviewTargets)
		}
	}

	var textOutput bytes.Buffer
	if err := WriteText(&textOutput, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"J S H O U N D", "AT A GLANCE", "APPLICATION BEHAVIOR", "AUTHENTICATION & SESSION", "PRIORITIZED REVIEW TARGETS"} {
		if !strings.Contains(textOutput.String(), section) {
			t.Errorf("missing %s:\n%s", section, textOutput.String())
		}
	}
	for _, unwanted := range []string{"ANALYSIS NOTES", "[HIGH]", "[MEDIUM]", "confidence", "observed · high"} {
		if strings.Contains(textOutput.String(), unwanted) {
			t.Errorf("unexpected %q:\n%s", unwanted, textOutput.String())
		}
	}
	for _, rawTarget := range []string{"/set/ticket/create/v1", "https://js.stripe.com/v3/"} {
		if strings.Contains(textOutput.String(), rawTarget) {
			t.Errorf("raw target leaked into overview: %s", rawTarget)
		}
	}

	var jsonOutput bytes.Buffer
	if err := WriteJSON(&jsonOutput, result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(jsonOutput.String(), `/set/ticket/create/v1`) || strings.Contains(jsonOutput.String(), `https://js.stripe.com`) {
		t.Fatalf("raw targets leaked into JSON: %s", jsonOutput.String())
	}
	var decoded map[string]any
	if err := json.Unmarshal(jsonOutput.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if _, exists := decoded["requests"]; exists {
		t.Fatalf("legacy request inventory remains: %#v", decoded)
	}
}

func TestTextColorIsExplicitAndRespectsPlainOutput(t *testing.T) {
	result := Analyze([]Source{{Data: []byte(`fetch('/set/ticket/get/v1')`)}})
	var plain, colored bytes.Buffer
	if err := WriteText(&plain, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := WriteText(&colored, result, TextOptions{Color: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.String(), "\x1b[") {
		t.Fatal("plain output contains ANSI escapes")
	}
	if !strings.Contains(colored.String(), "\x1b[") {
		t.Fatal("colored output has no ANSI styling")
	}
}

func TestAnalyzeIgnoresCommentedSignals(t *testing.T) {
	result := Analyze([]Source{{Name: "app.js", Data: []byte(`
// eval(code); node.innerHTML = untrusted; fetch('/set/ticket/download/v1')
/* window.addEventListener('message', handler); */
const label = 'normal text';
`)}})
	if len(result.Behavior) != 0 || len(result.ReviewTargets) != 0 {
		t.Fatalf("comments became behavior: %#v", result)
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
func hasInsight(values []Insight, want string) bool {
	for _, value := range values {
		if value.Title == want {
			return true
		}
	}
	return false
}
func hasReview(values []ReviewTarget, want string) bool {
	for _, value := range values {
		if value.Title == want {
			return true
		}
	}
	return false
}
