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
	for _, title := range []string{"Object-level authorization across ticket operations", "Download and export authorization", "Trusted-flagger enrollment and verification", "postMessage origin validation", "HTML injection sinks", "Dynamic code construction"} {
		if !hasReview(result.ReviewTargets, title) {
			t.Errorf("missing review target %q: %#v", title, result.ReviewTargets)
		}
	}

	var textOutput bytes.Buffer
	if err := WriteText(&textOutput, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, section := range []string{"JSHOUND · JAVASCRIPT ATTACK-SURFACE RECON", "AT A GLANCE", "APPLICATION BEHAVIOR", "AUTHENTICATION & SESSION", "PRIORITIZED REVIEW TARGETS"} {
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

func TestAnalyzeCapturesPostMessageOriginsAndValidation(t *testing.T) {
	result := Analyze([]Source{{Data: []byte(`
window.addEventListener('message', event => {
  if (event.origin !== 'https://trusted.example') return;
  if (event.source !== child.contentWindow) return;
  handle(event.data);
});
child.contentWindow.postMessage(payload, 'https://trusted.example');
other.postMessage(publicData, '*');
third.postMessage(payload, configuredOrigin);
`)}})
	message := result.Messaging
	if message == nil {
		t.Fatal("cross-window messaging was not detected")
	}
	if message.Receivers != 1 || message.Senders != 3 || message.OriginChecks != 1 || message.SourceChecks != 1 {
		t.Fatalf("unexpected messaging summary: %#v", message)
	}
	if message.FixedTargets != 1 || message.WildcardTargets != 1 || message.DynamicTargets != 0 || message.UnknownSenders != 1 || !contains(message.FixedTargetOrigins, "https://trusted.example") {
		t.Fatalf("unexpected target origins: %#v", message)
	}
	if !contains(message.ValidatedOrigins, "https://trusted.example") {
		t.Fatalf("validated origin missing: %#v", message)
	}
	if hasReview(result.ReviewTargets, "postMessage origin validation") {
		t.Fatalf("validated receiver was flagged: %#v", result.ReviewTargets)
	}
	if !hasReview(result.ReviewTargets, "Wildcard postMessage target origin") {
		t.Fatalf("wildcard sender was not flagged: %#v", result.ReviewTargets)
	}

	var output bytes.Buffer
	if err := WriteText(&output, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"CROSS-WINDOW MESSAGING", "origin + source validation", "https://trusted.example", "Window wildcard targetOrigin", "unresolved/local"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("missing %q:\n%s", expected, output.String())
		}
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

func TestPostMessageParsingBalancesArgumentsAndSeparatesTransports(t *testing.T) {
	result := Analyze([]Source{{Data: []byte(`
const worker = new Worker('worker.js'); worker.postMessage('ping');
const channel = new MessageChannel(); channel.port2.postMessage(0);
const broadcast = new BroadcastChannel('updates'); broadcast.postMessage(data);
comObj.postMessage({reason: assert(err, 'cancel must have a valid reason')}, transfers);
window.parent.postMessage({a: fn(1, 'not an origin'), b: [1, 2]}, '*');
window.postMessage(data, {targetOrigin: 'https://trusted.example', transfer: [port]});
`)}})
	message := result.Messaging
	if message == nil {
		t.Fatal("messaging missing")
	}
	if message.WindowSenders != 2 || message.WorkerSenders != 1 || message.PortSenders != 1 || message.BroadcastSenders != 1 || message.UnknownSenders != 1 {
		t.Fatalf("transport classification: %#v", message)
	}
	if message.WildcardTargets != 1 || message.FixedTargets != 1 || !contains(message.FixedTargetOrigins, "https://trusted.example") {
		t.Fatalf("Window targets: %#v", message)
	}
	for _, invalid := range []string{"cancel must have a valid reason", "not an origin"} {
		if contains(message.FixedTargetOrigins, invalid) {
			t.Fatalf("non-origin %q was accepted: %#v", invalid, message)
		}
	}
}

func TestPostMessageValidationCoverageIsPerHandler(t *testing.T) {
	result := Analyze([]Source{{Data: []byte(`
window.addEventListener('message', event => { if (event.origin !== trusted) return; if (event.source !== child) return; use(event.data); });
window.addEventListener('message', e => { if (allowed.has(e.origin)) use(e.data); });
window.addEventListener('message', msg => { if (msg.source !== child) return; use(msg.data); });
window.addEventListener('message', event => use(event.data));
window.addEventListener('message', event => { if (event.origin.includes('trusted.example')) use(event.data); });
window.addEventListener('message', importedHandler);
object.addEventListener('message', event => { if (event.origin === trusted) use(event.data); });
`)}})
	message := result.Messaging
	if message == nil || message.Receivers != 6 || message.UnknownReceivers != 1 {
		t.Fatalf("receiver classification: %#v", message)
	}
	if message.OriginAndSource != 1 || message.OriginOnly != 1 || message.SourceOnly != 1 || message.Unvalidated != 1 || message.WeakOrigin != 1 || message.ValidationUnknown != 1 {
		t.Fatalf("validation coverage: %#v", message)
	}
}

func TestTextGroupsStorageAndCollapsesSDKProbes(t *testing.T) {
	result := Analyze([]Source{{Data: []byte(`
localStorage.getItem('_ranger-test-key'); localStorage.setItem('_ranger-test-key', 'x'); localStorage.removeItem('_ranger-test-key');
sessionStorage.setItem('account_tt_auth_code', code); sessionStorage.getItem('accepted_plan_key'); sessionStorage.setItem('accepted_plan_key', plan);
localStorage.getItem('enable-prod-debug');
`)}})
	var output bytes.Buffer
	if err := WriteText(&output, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	for _, expected := range []string{"SECURITY / AUTH", "account_tt_auth_code", "DEBUG / FEATURE FLAGS", "enable-prod-debug", "accepted_plan_key]  ·  read/write", "1 store/key pair · 3 operations collapsed"} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q:\n%s", expected, text)
		}
	}
	if strings.Contains(text, "_ranger-test-key") {
		t.Fatalf("probe key was not collapsed:\n%s", text)
	}
}

func TestAnalyzeCorrelatesOAuthRedirectStateAndDistinctEvidence(t *testing.T) {
	content := strings.Repeat("oauth ", 50) + ` code_verifier; sessionStorage.setItem('account_tt_auth_code', code); sessionStorage.setItem('tt_oauth_redirect_uri', redirect);`
	result := Analyze([]Source{{Data: []byte(content)}})
	if !hasInsight(result.Authentication, "OAuth redirect flow") {
		t.Fatalf("OAuth flow missing: %#v", result.Authentication)
	}
	oauth := insightByTitle(result.Authentication, "OAuth/OIDC")
	if oauth == nil || oauth.Evidence != 2 {
		t.Fatalf("OAuth evidence is occurrence-based: %#v", oauth)
	}
	if !contains(result.AtAGlance, "OAuth redirect data written to sessionStorage") || !hasReview(result.ReviewTargets, "OAuth redirect integrity") {
		t.Fatalf("OAuth summary/review missing: %#v", result)
	}
}

func TestOAuthCorrelationRequiresStorageWrite(t *testing.T) {
	result := Analyze([]Source{{Data: []byte(`oauth; sessionStorage.removeItem('tt_oauth_redirect_uri')`)}})
	if hasInsight(result.Authentication, "OAuth redirect flow") || hasReview(result.ReviewTargets, "OAuth redirect integrity") {
		t.Fatalf("remove-only storage became a persisted OAuth flow: %#v", result)
	}
}

func TestMessagingIgnoresCommentsStringsAndDefinitions(t *testing.T) {
	result := Analyze([]Source{{Data: []byte(`
window.addEventListener('message', e => { console.log('e.origin === trusted'); /* e.origin === trusted */ use(e.data); });
// window.postMessage(data, '*');
class Port { postMessage(data, targetOrigin) {} }
function postMessage(data, targetOrigin) {}
`)}})
	if result.Messaging == nil || result.Messaging.Unvalidated != 1 {
		t.Fatalf("validation prose became executable validation: %#v", result.Messaging)
	}
	if result.Messaging.Senders != 0 {
		t.Fatalf("comments or definitions became sender calls: %#v", result.Messaging)
	}
}

func TestMessagingScannerSkipsRegexDelimiters(t *testing.T) {
	result := Analyze([]Source{{Data: []byte(`
const chrome = agent.match(/Chrome\/(\d+)/); const edge = agent.match(/Edge\//);
window.postMessage({value: /[),}]/.test(input)}, '*');
`)}})
	if result.Messaging == nil || result.Messaging.WindowSenders != 1 || result.Messaging.WildcardTargets != 1 {
		t.Fatalf("regex delimiters disrupted call parsing: %#v", result.Messaging)
	}
}

func TestUnresolvedMessageHandlerIsActionableWithoutMissingClaim(t *testing.T) {
	result := Analyze([]Source{{Data: []byte(`window.addEventListener('message', importedHandler)`)}})
	if result.Messaging == nil || result.Messaging.ValidationUnknown != 1 || !hasReview(result.ReviewTargets, "Unresolved postMessage validation") {
		t.Fatalf("unresolved handler was hidden: %#v", result)
	}
	if !contains(result.AtAGlance, "1 message receiver · 1 unresolved") {
		t.Fatalf("unresolved handler summary missing: %#v", result.AtAGlance)
	}
}

func TestExplicitWindowSenderRemainsWindowWithoutValidTarget(t *testing.T) {
	result := Analyze([]Source{{Data: []byte(`window.postMessage(data); window.postMessage(data, 'opaque')`)}})
	if result.Messaging == nil || result.Messaging.WindowSenders != 2 || result.Messaging.DynamicTargets != 2 {
		t.Fatalf("explicit Window senders were lost: %#v", result.Messaging)
	}
}

func TestGenericVerificationIsNotAuthentication(t *testing.T) {
	result := Analyze([]Source{{Data: []byte(`fetch('/payments/verify'); fetch('/schema/validate'); fetch('/devices/register')`)}})
	if hasInsight(result.Behavior, "Account onboarding and verification") || hasInsight(result.Authentication, "Authentication entry points") {
		t.Fatalf("generic verification became authentication: %#v", result)
	}
}

func TestWorkflowInferenceRequiresMultipleLifecycleOperations(t *testing.T) {
	one := Analyze([]Source{{Data: []byte(`fetch('/ticket/create')`)}})
	if hasInsight(one.Behavior, "Ticket and case management") || hasReview(one.ReviewTargets, "Object-level authorization across ticket operations") {
		t.Fatalf("single operation became a lifecycle: %#v", one)
	}
	two := Analyze([]Source{{Data: []byte(`fetch('/ticket/create'); fetch('/ticket/comment/add')`)}})
	if !hasInsight(two.Behavior, "Ticket and case management") || !hasReview(two.ReviewTargets, "Object-level authorization across ticket operations") {
		t.Fatalf("multi-operation lifecycle missing: %#v", two)
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
func insightByTitle(values []Insight, want string) *Insight {
	for index := range values {
		if values[index].Title == want {
			return &values[index]
		}
	}
	return nil
}
