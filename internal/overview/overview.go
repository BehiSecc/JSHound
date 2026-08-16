// Package overview builds a deterministic security-recon brief for JavaScript
// and HTML without executing input or following discovered targets.
package overview

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/jshound/jshound/internal/analyzer"
)

type Source struct {
	Name      string
	Data      []byte
	SourceMap string
}

// Insight is a concise conclusion backed by lexical evidence in the input.
type Insight struct {
	Title    string `json:"title"`
	Detail   string `json:"detail,omitempty"`
	Evidence int    `json:"evidence_count"`
}

type StorageOperation struct {
	Store  string `json:"store"`
	Key    string `json:"key"`
	Action string `json:"action"`
}

type ReviewTarget struct {
	Status   string `json:"status"`
	Title    string `json:"title"`
	Why      string `json:"why"`
	Evidence int    `json:"evidence_count"`
}

// Result intentionally contains no raw endpoint or URL inventory. That is the
// normal scanner's job; overview mode explains the observed application story.
type Result struct {
	Version        string             `json:"version,omitempty"`
	Sources        int                `json:"sources"`
	Bytes          int                `json:"bytes"`
	Frameworks     []string           `json:"frameworks,omitempty"`
	Bundlers       []string           `json:"bundlers,omitempty"`
	Minified       bool               `json:"minified"`
	SourceMap      bool               `json:"source_map_available"`
	AtAGlance      []string           `json:"at_a_glance,omitempty"`
	Behavior       []Insight          `json:"application_behavior,omitempty"`
	Authentication []Insight          `json:"authentication,omitempty"`
	Storage        []StorageOperation `json:"storage,omitempty"`
	ThirdParties   []string           `json:"third_parties,omitempty"`
	Capabilities   []string           `json:"browser_capabilities,omitempty"`
	ReviewTargets  []ReviewTarget     `json:"review_targets,omitempty"`
}

var (
	storageCall  = regexp.MustCompile(`(?i)\b(localStorage|sessionStorage)\s*\.\s*(getItem|setItem|removeItem)\s*\(\s*["']([^"']{1,128})["']`)
	storageIndex = regexp.MustCompile(`(?i)\b(localStorage|sessionStorage)\s*\[\s*["']([^"']{1,128})["']\s*\]`)
)

// Analyze builds an aggregated overview from already-loaded sources.
func Analyze(sources []Source) Result {
	frameworks, bundlers := stringSet(), stringSet()
	thirdParties, capabilities := stringSet(), stringSet()
	storage := make(map[string]StorageOperation)
	var corpus strings.Builder
	var targets []string
	var graphQLOperations int
	var bytesCount int
	var sourceMap, minified bool

	for _, source := range sources {
		raw := string(source.Data)
		content := stripComments(raw)
		lower := strings.ToLower(content)
		bytesCount += len(source.Data)
		sourceMap = sourceMap || source.SourceMap != "" || strings.Contains(strings.ToLower(raw), "sourcemappingurl=")
		minified = minified || looksMinified(content)
		addProfile(lower, frameworks, bundlers)
		addCapabilities(lower, capabilities)
		addStorage(content, storage)
		corpus.WriteString("\n")
		corpus.WriteString(lower)

		facts := analyzer.AnalyzeWithOptions([]byte(content), analyzer.Options{IncludeParameters: false})
		targets = append(targets, facts.Endpoints...)
		targets = append(targets, facts.URLs...)
		graphQLOperations += len(facts.GraphQLOperations)
		for _, rawURL := range facts.URLs {
			if provider := providerFor(rawURL); provider != "" {
				thirdParties[provider] = struct{}{}
			}
		}
	}

	lower := corpus.String()
	behavior := behaviorInsights(targets, lower, graphQLOperations)
	auth := authenticationInsights(targets, lower)
	reviews := reviewTargets(targets, lower, storage)
	return Result{
		Sources: len(sources), Bytes: bytesCount, Frameworks: sorted(frameworks), Bundlers: sorted(bundlers),
		Minified: minified, SourceMap: sourceMap, AtAGlance: atAGlance(behavior, auth),
		Behavior: behavior, Authentication: auth, Storage: sortedStorage(storage),
		ThirdParties: sorted(thirdParties), Capabilities: sorted(capabilities), ReviewTargets: reviews,
	}
}

func behaviorInsights(targets []string, lower string, graphQLOperations int) []Insight {
	var result []Insight
	add := func(title, detail string, count int) {
		if count > 0 {
			result = append(result, Insight{Title: title, Detail: detail, Evidence: count})
		}
	}
	add("Ticket and case management", "Creation, retrieval, updates, comments, summaries or withdrawal behavior is present.", targetCount(targets, "ticket/"))
	add("Download and export workflow", "Individual or bulk download behavior is present.", targetCount(targets, "download", "export"))
	add("Trusted-flagger workflow", "Enrollment, binding or verification behavior is present.", targetCount(targets, "trusted_flagger", "trusted-flagger"))
	add("Profile and organization management", "Profile, agency or organization data can be read or changed.", targetCount(targets, "profile/", "agency_info", "organization"))
	add("Account onboarding and verification", "Login, registration or account-verification behavior is present.", targetCount(targets, "/login", "register", "account_region_validate"))
	add("Feedback and messaging", "Feedback, comments or message-submission behavior is present.", targetCount(targets, "feedback", "comment/", "post_message"))
	add("Localization and regional behavior", "Language or region-specific application behavior is present.", targetCount(targets, "language_options", "region"))
	add("Client telemetry", "Browser monitoring or security telemetry collection is referenced.", targetCount(targets, "monitor_browser", "/collect/", "telemetry"))
	if graphQLOperations > 0 {
		add("GraphQL API", fmt.Sprintf("%d named GraphQL %s observed.", graphQLOperations, plural(graphQLOperations, "operation")), graphQLOperations)
	}
	if count := countAny(lower, "new websocket", ".websocket", "eventsource("); count > 0 {
		add("Realtime communication", "WebSocket or server-sent event behavior is present.", count)
	}
	if count := environmentCount(targets); count > 0 {
		add("Multiple deployment environments", "Test, pre-production or alternate regional service references are present.", count)
	}
	return result
}

func authenticationInsights(targets []string, lower string) []Insight {
	var result []Insight
	add := func(title, detail string, count int) {
		if count > 0 {
			result = append(result, Insight{Title: title, Detail: detail, Evidence: count})
		}
	}
	add("Bearer authorization", "Bearer credentials are constructed or attached to requests.", countAny(lower, "bearer"))
	add("Authorization header handling", "Request authorization headers are handled in the client.", countAny(lower, "authorization"))
	add("Access-token acquisition", "A dedicated access-token acquisition path is referenced.", targetCount(targets, "access_token"))
	add("Token refresh", "Refresh-token or token-renewal behavior is present.", countAny(lower, "refresh_token", "/refresh"))
	add("OAuth/OIDC", "OAuth, OpenID Connect or PKCE behavior is present.", countAny(lower, "oauth", "openid", "code_verifier", "code_challenge"))
	add("Cookie-authenticated requests", "Requests can include browser credentials or cookies.", countCompact(lower, "credentials:'include'", "credentials:\"include\"", "withcredentials"))
	add("Authentication entry points", "Login, registration or account verification is exposed by the client.", targetCount(targets, "/login", "register_verify", "account_region_validate"))
	return result
}

func reviewTargets(targets []string, lower string, storage map[string]StorageOperation) []ReviewTarget {
	var result []ReviewTarget
	add := func(status, title, why string, evidence int) {
		if evidence > 0 {
			result = append(result, ReviewTarget{Status: status, Title: title, Why: why, Evidence: evidence})
		}
	}
	add("observed", "Object-level authorization across ticket operations", "Create, read, update, comment and withdrawal operations suggest a multi-step object lifecycle worth testing across users and roles.", targetCount(targets, "ticket/"))
	add("observed", "Download and bulk-export authorization", "Download behavior should enforce ownership, role and tenant boundaries for both individual and bulk actions.", targetCount(targets, "download", "export"))
	add("observed", "Trusted-flagger enrollment and verification", "Binding and verification flows are privileged trust transitions; review replay, role assignment and account-binding controls.", targetCount(targets, "trusted_flagger", "trusted-flagger"))
	add("observed", "Profile and organization boundary checks", "Profile and agency operations should consistently enforce tenant and role separation.", targetCount(targets, "profile/", "agency_info", "organization"))
	add("observed", "Bearer-token lifecycle", "Trace token acquisition, attachment, expiry and clearing across authentication and logout behavior.", countAny(lower, "bearer")+targetCount(targets, "access_token"))
	if count := countAny(lower, "addeventlistener(\"message\"", "addeventlistener('message'"); count > 0 && !strings.Contains(lower, "event.origin") {
		add("potential", "postMessage origin validation", "A message receiver was observed without a recognizable event.origin check; confirm validation in the surrounding handler.", count)
	}
	add("potential", "HTML injection sinks", "Trace whether location, message, storage or API-controlled values can reach HTML-writing operations.", countAny(lower, ".innerhtml", ".outerhtml", "insertadjacenthtml", "document.write("))
	add("potential", "Dynamic code construction", "Dynamic evaluation is present; determine whether any attacker-controlled string can reach it before treating it as exploitable.", countAny(lower, "eval(", "new function(", "settimeout(\"", "settimeout('"))
	for _, item := range storage {
		key := strings.ToLower(item.Key)
		if item.Store == "localStorage" && (strings.Contains(key, "token") || strings.Contains(key, "auth") || strings.Contains(key, "session")) {
			add("observed", "Persistent authentication material", "Authentication-like state is stored in localStorage; review XSS exposure and logout clearing.", 1)
			break
		}
	}
	return result
}

func atAGlance(behavior, auth []Insight) []string {
	result := make([]string, 0, 6)
	for _, item := range auth {
		if item.Title == "Bearer authorization" || item.Title == "Access-token acquisition" || item.Title == "OAuth/OIDC" {
			result = append(result, item.Title)
		}
	}
	for _, item := range behavior {
		if len(result) == 6 {
			break
		}
		result = append(result, item.Title)
	}
	return result
}

func addProfile(lower string, frameworks, bundlers map[string]struct{}) {
	for _, candidate := range []struct{ marker, name string }{
		{"__next_data__", "Next.js"}, {"/_next/", "Next.js"}, {"next/router", "Next.js"},
		{"reactdom", "React"}, {"react/jsx-runtime", "React"}, {"__vue__", "Vue"},
		{"vue-router", "Vue"}, {"@angular/", "Angular"}, {"svelte", "Svelte"},
	} {
		if strings.Contains(lower, candidate.marker) {
			frameworks[candidate.name] = struct{}{}
		}
	}
	for _, candidate := range []struct{ marker, name string }{
		{"webpackchunk", "Webpack"}, {"__webpack_require__", "Webpack"}, {"webpackjsonp", "Webpack"},
		{"import.meta", "Vite"}, {"__vite_", "Vite"}, {"rollupplugin", "Rollup"}, {"browserify", "Browserify"},
	} {
		if strings.Contains(lower, candidate.marker) {
			bundlers[candidate.name] = struct{}{}
		}
	}
}

func addCapabilities(lower string, capabilities map[string]struct{}) {
	for _, candidate := range []struct{ marker, name string }{
		{"new websocket", "WebSocket"}, {".websocket", "WebSocket"}, {"eventsource(", "Server-sent events"},
		{"addeventlistener(\"message\"", "postMessage receiver"}, {"addeventlistener('message'", "postMessage receiver"},
		{".postmessage(", "postMessage sender"}, {"serviceworker", "Service worker"}, {"new worker(", "Web worker"},
		{"navigator.clipboard", "Clipboard"}, {"navigator.geolocation", "Geolocation"}, {"getusermedia", "Camera or microphone"},
		{"notification.requestpermission", "Notifications"}, {"createelement(\"script\")", "Dynamic script creation"}, {"createelement('script')", "Dynamic script creation"},
	} {
		if strings.Contains(lower, candidate.marker) {
			capabilities[candidate.name] = struct{}{}
		}
	}
}

func addStorage(content string, storage map[string]StorageOperation) {
	for _, match := range storageCall.FindAllStringSubmatch(content, -1) {
		action := map[string]string{"getitem": "read", "setitem": "write", "removeitem": "remove"}[strings.ToLower(match[2])]
		item := StorageOperation{Store: canonicalStore(match[1]), Key: match[3], Action: action}
		storage[item.Store+"\x00"+item.Key+"\x00"+item.Action] = item
	}
	for _, match := range storageIndex.FindAllStringSubmatch(content, -1) {
		item := StorageOperation{Store: canonicalStore(match[1]), Key: match[2], Action: "read/write"}
		storage[item.Store+"\x00"+item.Key+"\x00"+item.Action] = item
	}
}

func canonicalStore(value string) string {
	if strings.EqualFold(value, "localStorage") {
		return "localStorage"
	}
	return "sessionStorage"
}

func providerFor(rawURL string) string {
	lower := strings.ToLower(rawURL)
	for _, candidate := range []struct{ marker, name string }{
		{"stripe.com", "Stripe — payments"}, {"sentry.io", "Sentry — monitoring"}, {"datadoghq.com", "Datadog — monitoring"},
		{"segment.com", "Segment — analytics"}, {"google-analytics.com", "Google Analytics — analytics"},
		{"googletagmanager.com", "Google Tag Manager — analytics"}, {"intercom.io", "Intercom — chat"},
		{"auth0.com", "Auth0 — authentication"}, {"okta.com", "Okta — authentication"},
	} {
		if strings.Contains(lower, candidate.marker) {
			return candidate.name
		}
	}
	return ""
}

func targetCount(targets []string, markers ...string) int {
	count := 0
	for _, target := range targets {
		lower := strings.ToLower(target)
		for _, marker := range markers {
			if strings.Contains(lower, marker) {
				count++
				break
			}
		}
	}
	return count
}

func environmentCount(targets []string) int {
	return targetCount(targets, "-boe.", ".test", "staging", "preprod", "sandbox")
}
func countAny(value string, markers ...string) int {
	count := 0
	for _, marker := range markers {
		count += strings.Count(value, marker)
	}
	return count
}
func countCompact(value string, markers ...string) int {
	compact := strings.NewReplacer(" ", "", "\n", "", "\r", "", "\t", "").Replace(value)
	return countAny(compact, markers...)
}

func looksMinified(content string) bool {
	lines := strings.Split(content, "\n")
	if len(content) > 4096 && len(lines) <= 3 {
		return true
	}
	var longest int
	for _, line := range lines {
		if len(line) > longest {
			longest = len(line)
		}
	}
	return len(content) > 8192 && longest > 2000
}

// stripComments avoids treating explanatory comments as observed behavior.
// String literals are retained because they carry the semantic evidence used
// to infer application behavior.
func stripComments(content string) string {
	var result strings.Builder
	result.Grow(len(content))
	for i := 0; i < len(content); {
		if content[i] == '\'' || content[i] == '"' || content[i] == '`' {
			quote := content[i]
			result.WriteByte(content[i])
			i++
			for i < len(content) {
				result.WriteByte(content[i])
				if content[i] == '\\' && i+1 < len(content) {
					i++
					result.WriteByte(content[i])
				} else if content[i] == quote {
					i++
					break
				}
				i++
			}
			continue
		}
		if content[i] == '/' && i+1 < len(content) && content[i+1] == '/' {
			i += 2
			for i < len(content) && content[i] != '\n' {
				i++
			}
			continue
		}
		if content[i] == '/' && i+1 < len(content) && content[i+1] == '*' {
			i += 2
			for i+1 < len(content) && (content[i] != '*' || content[i+1] != '/') {
				if content[i] == '\n' {
					result.WriteByte('\n')
				}
				i++
			}
			if i+1 < len(content) {
				i += 2
			}
			continue
		}
		result.WriteByte(content[i])
		i++
	}
	return result.String()
}

func stringSet() map[string]struct{} { return make(map[string]struct{}) }
func sorted(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
func sortedStorage(values map[string]StorageOperation) []StorageOperation {
	result := make([]StorageOperation, 0, len(values))
	for _, item := range values {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Store != result[j].Store {
			return result[i].Store < result[j].Store
		}
		if result[i].Key != result[j].Key {
			return result[i].Key < result[j].Key
		}
		return result[i].Action < result[j].Action
	})
	return result
}

func plural(count int, word string) string {
	if count == 1 {
		return word
	}
	return word + "s"
}
