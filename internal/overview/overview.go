// Package overview builds a deterministic, static recon brief for JavaScript
// and HTML. It intentionally never evaluates its input or follows discovered
// routes.
package overview

import (
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

type Request struct {
	Target string `json:"target"`
	Method string `json:"method,omitempty"`
}

type StorageOperation struct {
	Store  string `json:"store"`
	Key    string `json:"key"`
	Action string `json:"action"`
}

type ReviewTarget struct {
	Value      string `json:"value"`
	Confidence string `json:"confidence"`
}

// Result is the machine-readable contract for --overview.
type Result struct {
	Version           string             `json:"version,omitempty"`
	Sources           int                `json:"sources"`
	Bytes             int                `json:"bytes"`
	Frameworks        []string           `json:"frameworks,omitempty"`
	Bundlers          []string           `json:"bundlers,omitempty"`
	Minified          bool               `json:"minified"`
	SourceMap         bool               `json:"source_map_available"`
	Requests          []Request          `json:"requests,omitempty"`
	Routes            []string           `json:"routes,omitempty"`
	Authentication    []string           `json:"authentication,omitempty"`
	Storage           []StorageOperation `json:"storage,omitempty"`
	GraphQLOperations []string           `json:"graphql_operations,omitempty"`
	ThirdParties      []string           `json:"third_parties,omitempty"`
	Capabilities      []string           `json:"capabilities,omitempty"`
	ReviewTargets     []ReviewTarget     `json:"review_targets,omitempty"`
	Limitations       []string           `json:"limitations"`
}

var (
	storageCall  = regexp.MustCompile(`(?i)\b(localStorage|sessionStorage)\s*\.\s*(getItem|setItem|removeItem)\s*\(\s*["']([^"']{1,128})["']`)
	storageIndex = regexp.MustCompile(`(?i)\b(localStorage|sessionStorage)\s*\[\s*["']([^"']{1,128})["']\s*\]`)
	// RE2 permits counted repetitions up to 1000, so adjacent bounds retain
	// the candidate cap without an unbounded match.
	requestCall  = regexp.MustCompile(`(?is)\b(?:fetch|axios(?:\s*\.\s*(?:get|post|put|patch|delete))?)\s*\(\s*["']([^"']{2,1000}[^"']{0,1000}[^"']{0,48})["']([^)]{0,600})\)`)
	methodOption = regexp.MustCompile(`(?i)\bmethod\s*:\s*["'](GET|POST|PUT|PATCH|DELETE|HEAD|OPTIONS)["']`)
)

// Analyze builds an overview from already-loaded input sources.
func Analyze(sources []Source) Result {
	frameworks, bundlers, auth, thirdParties, capabilities := stringSet(), stringSet(), stringSet(), stringSet(), stringSet()
	routes := stringSet()
	requestMethods := make(map[string]string)
	storage := make(map[string]StorageOperation)
	graphql := stringSet()
	reviews := make(map[string]ReviewTarget)
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
		addAuthentication(lower, auth)
		addCapabilities(lower, capabilities)
		addStorage(content, storage)
		addReviewTargets(lower, storage, reviews)

		facts := analyzer.AnalyzeWithOptions([]byte(content), analyzer.Options{IncludeParameters: false})
		for _, endpoint := range facts.Endpoints {
			routes[endpoint] = struct{}{}
		}
		for _, operation := range facts.GraphQLOperations {
			graphql[operation] = struct{}{}
		}
		for target, method := range requests(content, facts) {
			if prior, ok := requestMethods[target]; !ok || prior == "" {
				requestMethods[target] = method
			}
		}
		for _, rawURL := range facts.URLs {
			if provider := providerFor(rawURL); provider != "" {
				thirdParties[provider] = struct{}{}
			}
		}
	}

	return Result{
		Sources: len(sources), Bytes: bytesCount, Minified: minified, SourceMap: sourceMap,
		Frameworks: sorted(frameworks), Bundlers: sorted(bundlers), Routes: sorted(routes),
		Requests: sortedRequests(requestMethods), Authentication: sorted(auth), Storage: sortedStorage(storage),
		GraphQLOperations: sorted(graphql), ThirdParties: sorted(thirdParties), Capabilities: sorted(capabilities),
		ReviewTargets: sortedReviews(reviews),
		Limitations:   []string{"Static analysis only; runtime reachability and server-side controls were not determined."},
	}
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
		{"import.meta", "Vite"}, {"__vite_", "Vite"}, {"rollupplugin", "Rollup"},
		{"browserify", "Browserify"},
	} {
		if strings.Contains(lower, candidate.marker) {
			bundlers[candidate.name] = struct{}{}
		}
	}
}

func addAuthentication(lower string, auth map[string]struct{}) {
	compact := strings.NewReplacer(" ", "", "\n", "", "\r", "", "\t", "").Replace(lower)
	for _, candidate := range []struct{ marker, name string }{
		{"authorization", "Authorization header handling"}, {"bearer ", "Bearer token handling"},
		{"refresh_token", "Token refresh handling"}, {"/refresh", "Token refresh handling"},
		{"oauth", "OAuth/OIDC handling"}, {"openid", "OAuth/OIDC handling"},
		{"code_verifier", "PKCE handling"}, {"code_challenge", "PKCE handling"},
		{"credentials:include", "Cookie credentials"}, {"credentials: include", "Cookie credentials"},
		{"credentials:\"include\"", "Cookie credentials"}, {"credentials:'include'", "Cookie credentials"},
		{"withcredentials", "Cookie credentials"},
	} {
		if strings.Contains(lower, candidate.marker) || strings.Contains(compact, candidate.marker) {
			auth[candidate.name] = struct{}{}
		}
	}
}

func addCapabilities(lower string, capabilities map[string]struct{}) {
	for _, candidate := range []struct{ marker, name string }{
		{"new websocket", "WebSocket"}, {".websocket", "WebSocket"}, {"addEventListener(\"message\"", "postMessage receiver"},
		{"addEventListener('message'", "postMessage receiver"}, {".postmessage(", "postMessage sender"},
		{"serviceworker", "Service worker"}, {"new worker(", "Web worker"}, {"navigator.clipboard", "Clipboard"},
		{"navigator.geolocation", "Geolocation"}, {"getusermedia", "Camera or microphone"},
		{"notification.requestpermission", "Notifications"}, {"createelement(\"script\")", "Dynamic script creation"},
		{"createelement('script')", "Dynamic script creation"},
	} {
		if strings.Contains(lower, candidate.marker) {
			capabilities[candidate.name] = struct{}{}
		}
	}
}

func addStorage(content string, storage map[string]StorageOperation) {
	for _, match := range storageCall.FindAllStringSubmatch(content, -1) {
		action := map[string]string{"getitem": "read", "setitem": "write", "removeitem": "remove"}[strings.ToLower(match[2])]
		item := StorageOperation{Store: match[1], Key: match[3], Action: action}
		storage[item.Store+"\x00"+item.Key+"\x00"+item.Action] = item
	}
	for _, match := range storageIndex.FindAllStringSubmatch(content, -1) {
		item := StorageOperation{Store: match[1], Key: match[2], Action: "read/write"}
		storage[item.Store+"\x00"+item.Key+"\x00"+item.Action] = item
	}
}

func addReviewTargets(lower string, storage map[string]StorageOperation, reviews map[string]ReviewTarget) {
	add := func(value, confidence string) { reviews[value] = ReviewTarget{Value: value, Confidence: confidence} }
	if strings.Contains(lower, "addeventlistener(\"message\"") || strings.Contains(lower, "addeventlistener('message'") {
		if !strings.Contains(lower, "event.origin") {
			add("postMessage receiver without recognizable event.origin validation", "medium")
		}
	}
	if strings.Contains(lower, ".innerhtml") || strings.Contains(lower, ".outerhtml") || strings.Contains(lower, "insertadjacenthtml") || strings.Contains(lower, "document.write(") {
		add("HTML injection sink present", "medium")
	}
	if strings.Contains(lower, "eval(") || strings.Contains(lower, "new function(") || strings.Contains(lower, "settimeout(\"") || strings.Contains(lower, "settimeout('") {
		add("dynamic code-execution sink present", "high")
	}
	if strings.Contains(lower, "location.assign(") || strings.Contains(lower, "location.replace(") || strings.Contains(lower, "window.open(") {
		add("programmatic navigation present", "low")
	}
	for _, item := range storage {
		if item.Store == "localStorage" && (strings.Contains(strings.ToLower(item.Key), "token") || strings.Contains(strings.ToLower(item.Key), "auth") || strings.Contains(strings.ToLower(item.Key), "session")) {
			add("authentication-like value stored in localStorage", "medium")
			break
		}
	}
}

func requests(content string, facts analyzer.Result) map[string]string {
	result := make(map[string]string, len(facts.Endpoints)+len(facts.URLs))
	for _, match := range requestCall.FindAllStringSubmatch(content, -1) {
		method := ""
		if found := methodOption.FindStringSubmatch(match[2]); len(found) > 1 {
			method = strings.ToUpper(found[1])
		}
		result[match[1]] = method
	}
	for _, endpoint := range facts.Endpoints {
		if _, ok := result[endpoint]; !ok {
			result[endpoint] = ""
		}
	}
	for _, rawURL := range facts.URLs {
		if _, ok := result[rawURL]; !ok {
			result[rawURL] = ""
		}
	}
	return result
}

func providerFor(rawURL string) string {
	lower := strings.ToLower(rawURL)
	for _, candidate := range []struct{ marker, name string }{
		{"stripe.com", "Stripe (payments)"}, {"sentry.io", "Sentry (monitoring)"}, {"datadoghq.com", "Datadog (monitoring)"},
		{"segment.com", "Segment (analytics)"}, {"google-analytics.com", "Google Analytics (analytics)"},
		{"googletagmanager.com", "Google Tag Manager (analytics)"}, {"intercom.io", "Intercom (chat)"},
		{"auth0.com", "Auth0 (authentication)"}, {"okta.com", "Okta (authentication)"},
	} {
		if strings.Contains(lower, candidate.marker) {
			return candidate.name
		}
	}
	return ""
}

func looksMinified(content string) bool {
	lines := strings.Split(content, "\n")
	if len(content) > 4096 && len(lines) <= 3 {
		return true
	}
	if len(lines) == 0 {
		return false
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
// It deliberately retains string literals, which carry routes and URLs.
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
func sortedRequests(values map[string]string) []Request {
	result := make([]Request, 0, len(values))
	for target, method := range values {
		result = append(result, Request{Target: target, Method: method})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Target < result[j].Target })
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
func sortedReviews(values map[string]ReviewTarget) []ReviewTarget {
	result := make([]ReviewTarget, 0, len(values))
	for _, item := range values {
		result = append(result, item)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Value < result[j].Value })
	return result
}
