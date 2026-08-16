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

// MessageBoundary summarizes cross-window messaging without attempting to
// prove that every receiver is paired with every validation check.
type MessageBoundary struct {
	Receivers          int      `json:"receivers"`
	UnknownReceivers   int      `json:"unknown_receivers"`
	OriginAndSource    int      `json:"origin_and_source"`
	OriginOnly         int      `json:"origin_only"`
	SourceOnly         int      `json:"source_only"`
	WeakOrigin         int      `json:"weak_origin"`
	Unvalidated        int      `json:"unvalidated"`
	ValidationUnknown  int      `json:"validation_unknown"`
	Senders            int      `json:"senders"`
	WindowSenders      int      `json:"window_senders"`
	WorkerSenders      int      `json:"worker_senders"`
	PortSenders        int      `json:"message_port_senders"`
	BroadcastSenders   int      `json:"broadcast_channel_senders"`
	UnknownSenders     int      `json:"unknown_senders"`
	OriginChecks       int      `json:"origin_checks"`
	SourceChecks       int      `json:"source_checks"`
	ValidatedOrigins   []string `json:"validated_origins,omitempty"`
	FixedTargets       int      `json:"fixed_targets"`
	FixedTargetOrigins []string `json:"fixed_target_origins,omitempty"`
	WildcardTargets    int      `json:"wildcard_targets"`
	DynamicTargets     int      `json:"dynamic_targets"`
}

type ReviewTarget struct {
	Status   string `json:"status"`
	Title    string `json:"title"`
	Why      string `json:"why"`
	Evidence int    `json:"evidence_count"`
}

type authContext struct {
	OAuth       bool
	RedirectURI bool
	AuthCode    bool
	State       bool
	PKCE        bool
	DebugFlag   bool
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
	Messaging      *MessageBoundary   `json:"cross_window_messaging,omitempty"`
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
	var messageCorpus strings.Builder
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
		addIntegrationSignals(lower, thirdParties)
		addStorage(content, storage)
		corpus.WriteString("\n")
		corpus.WriteString(content)
		messageCorpus.WriteString("\n")
		messageCorpus.WriteString(raw)

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

	content := corpus.String()
	lower := strings.ToLower(content)
	targets = uniqueTargets(targets)
	messaging := analyzeMessaging(messageCorpus.String())
	if messaging != nil {
		if messaging.Receivers > 0 {
			capabilities["postMessage receiver"] = struct{}{}
		}
		if messaging.WindowSenders > 0 {
			capabilities["postMessage sender"] = struct{}{}
		}
	}
	context := buildAuthContext(lower, storage)
	behavior := behaviorInsights(targets, lower, graphQLOperations)
	auth := authenticationInsights(targets, lower, context)
	reviews := reviewTargets(targets, lower, storage, messaging, context)
	return Result{
		Sources: len(sources), Bytes: bytesCount, Frameworks: sorted(frameworks), Bundlers: sorted(bundlers),
		Minified: minified, SourceMap: sourceMap, AtAGlance: atAGlance(behavior, auth, messaging, context),
		Behavior: behavior, Authentication: auth, Storage: sortedStorage(storage), Messaging: messaging,
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
	ticketOps := operationKinds(targets, []string{"ticket/"}, map[string][]string{
		"create": {"create"}, "retrieve": {"/get", "/detail", "/list"}, "update": {"update", "edit"},
		"comment": {"comment"}, "summary": {"summary"}, "withdraw/close": {"withdraw", "close"}, "download": {"download", "export"},
	})
	if len(ticketOps) >= 2 {
		add("Ticket and case management", "Observed operations: "+strings.Join(ticketOps, ", ")+".", len(ticketOps))
	} else if len(ticketOps) == 1 {
		add("Ticket-related behavior", "Observed operation: "+ticketOps[0]+".", 1)
	}
	downloadOps := operationKinds(targets, nil, map[string][]string{"individual download": {"download"}, "bulk download": {"download_all", "bulk_download"}, "export": {"export"}})
	add("Download and export workflow", "Observed capabilities: "+strings.Join(downloadOps, ", ")+".", len(downloadOps))
	add("Trusted-flagger workflow", "Enrollment, binding or verification behavior is present.", targetCount(targets, "trusted_flagger", "trusted-flagger"))
	profileOps := operationKinds(targets, []string{"profile/", "agency_info", "organization"}, map[string][]string{
		"read": {"/get", "/list", "detail"}, "update": {"update", "edit"}, "create": {"create"}, "delete": {"delete"}, "bind": {"bind"},
	})
	if len(profileOps) >= 2 || containsAny(strings.Join(profileOps, " "), "update", "create", "delete", "bind") {
		add("Profile and organization management", "Observed operations: "+strings.Join(profileOps, ", ")+".", len(profileOps))
	} else if len(profileOps) == 1 {
		add("Profile data access", "Observed operation: "+profileOps[0]+".", 1)
	}
	accountOps := authenticationOperationKinds(targets)
	add("Account onboarding and verification", "Observed stages: "+strings.Join(accountOps, ", ")+".", len(accountOps))
	feedbackOps := operationKinds(targets, nil, map[string][]string{"feedback": {"feedback"}, "comments": {"comment/"}, "messages": {"post_message"}})
	add("Feedback and messaging", "Observed capabilities: "+strings.Join(feedbackOps, ", ")+".", len(feedbackOps))
	add("Localization and regional behavior", "Language or region-specific application behavior is present.", targetCount(targets, "language_options", "region"))
	telemetryKinds := operationKinds(targets, nil, map[string][]string{"browser monitoring": {"monitor_browser"}, "collection": {"/collect/"}, "telemetry": {"telemetry"}})
	add("Client telemetry", "Observed mechanisms: "+strings.Join(telemetryKinds, ", ")+".", len(telemetryKinds))
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

func authenticationInsights(targets []string, lower string, context authContext) []Insight {
	var result []Insight
	add := func(title, detail string, count int) {
		if count > 0 {
			result = append(result, Insight{Title: title, Detail: detail, Evidence: count})
		}
	}
	if context.OAuth && (context.RedirectURI || context.AuthCode) {
		parts := make([]string, 0, 2)
		if context.RedirectURI {
			parts = append(parts, "redirect URI")
		}
		if context.AuthCode {
			parts = append(parts, "authorization code")
		}
		add("OAuth redirect flow", "Browser session storage persists: "+strings.Join(parts, " and ")+".", len(parts))
	}
	add("Bearer authorization", "Bearer credentials are constructed or attached to requests.", countPresent(lower, "bearer"))
	add("Authorization header handling", "Request authorization headers are handled in the client.", countPresent(lower, "authorization"))
	add("Access-token acquisition", "A dedicated access-token acquisition path is referenced.", targetCount(targets, "access_token"))
	add("Token refresh", "Refresh-token or token-renewal behavior is present.", countPresent(lower, "refresh_token", "/refresh"))
	add("OAuth/OIDC", "OAuth, OpenID Connect or PKCE behavior is present.", countPresent(lower, "oauth", "openid", "code_verifier", "code_challenge"))
	add("Cookie-authenticated requests", "Requests can include browser credentials or cookies.", countPresent(compact(lower), "credentials:'include'", "credentials:\"include\"", "withcredentials"))
	add("Authentication entry points", "Login, registration or account verification is exposed by the client.", len(authenticationOperationKinds(targets)))
	return result
}

func reviewTargets(targets []string, lower string, storage map[string]StorageOperation, messaging *MessageBoundary, context authContext) []ReviewTarget {
	var result []ReviewTarget
	add := func(status, title, why string, evidence int) {
		if evidence > 0 {
			result = append(result, ReviewTarget{Status: status, Title: title, Why: why, Evidence: evidence})
		}
	}
	if context.OAuth && (context.RedirectURI || context.AuthCode) {
		add("observed", "OAuth redirect integrity", "Review redirect allowlisting, state and authorization-code binding, replay resistance, and cleanup after the flow completes.", boolCount(context.RedirectURI)+boolCount(context.AuthCode)+boolCount(context.State)+boolCount(context.PKCE))
	}
	if messaging != nil && messaging.Unvalidated+messaging.SourceOnly > 0 {
		add("potential", "postMessage origin validation", "Some resolved Window message handlers have no recognizable origin validation; review each handler's accepted senders and message schema.", messaging.Unvalidated+messaging.SourceOnly)
	}
	if messaging != nil && messaging.ValidationUnknown > 0 {
		add("potential", "Unresolved postMessage validation", "Some Window message handlers could not be resolved lexically; inspect their callback implementations for exact origin and source validation.", messaging.ValidationUnknown)
	}
	if messaging != nil && messaging.WeakOrigin > 0 {
		add("potential", "Weak postMessage origin matching", "Substring-style origin checks can accept lookalike origins; replace them with exact origin allowlisting where applicable.", messaging.WeakOrigin)
	}
	if messaging != nil && messaging.WildcardTargets > 0 {
		add("potential", "Wildcard postMessage target origin", "Confirmed Window senders use '*' as targetOrigin; verify that disclosed message data is safe for every reachable origin.", messaging.WildcardTargets)
	}
	ticketOps := operationKinds(targets, []string{"ticket/"}, map[string][]string{"create": {"create"}, "retrieve": {"/get", "/detail", "/list"}, "update": {"update", "edit"}, "comment": {"comment"}, "withdraw/close": {"withdraw", "close"}, "download": {"download", "export"}})
	if len(ticketOps) >= 2 {
		add("observed", "Object-level authorization across ticket operations", "Multiple ticket lifecycle operations suggest an object boundary worth testing across users and roles.", len(ticketOps))
	}
	downloadOps := operationKinds(targets, nil, map[string][]string{"individual": {"download"}, "bulk": {"download_all", "bulk_download"}, "export": {"export"}})
	add("observed", "Download and export authorization", "Download behavior should enforce ownership, role and tenant boundaries for every individual, bulk, or export action present.", len(downloadOps))
	add("observed", "Trusted-flagger enrollment and verification", "Binding and verification flows are privileged trust transitions; review replay, role assignment and account-binding controls.", targetCount(targets, "trusted_flagger", "trusted-flagger"))
	profileOps := operationKinds(targets, []string{"profile/", "agency_info", "organization"}, map[string][]string{"read": {"/get", "/list", "detail"}, "update": {"update", "edit"}, "create": {"create"}, "delete": {"delete"}, "bind": {"bind"}})
	if len(profileOps) > 0 {
		if targetCount(targets, "agency_info", "organization") > 0 {
			add("observed", "Profile and organization boundary checks", "Profile and organization operations should consistently enforce role and tenant separation.", len(profileOps))
		} else {
			add("observed", "Profile data boundary checks", "Profile data access should enforce ownership and account boundaries.", len(profileOps))
		}
	}
	add("observed", "Bearer-token lifecycle", "Trace token acquisition, attachment, expiry and clearing across authentication and logout behavior.", countPresent(lower, "bearer")+boolToInt(targetCount(targets, "access_token") > 0))
	if context.DebugFlag {
		add("observed", "Production debug controls", "A production debug flag is read from browser storage; verify who can enable it and what privileged behavior it exposes.", 1)
	}
	add("potential", "HTML injection sinks", "Trace whether location, message, storage or API-controlled values can reach HTML-writing operations.", countPresent(lower, ".innerhtml", ".outerhtml", "insertadjacenthtml", "document.write("))
	add("potential", "Dynamic code construction", "Dynamic evaluation is present; determine whether any attacker-controlled string can reach it before treating it as exploitable.", countPresent(lower, "eval(", "new function(", "settimeout(\"", "settimeout('"))
	for _, item := range storage {
		key := strings.ToLower(item.Key)
		if item.Store == "localStorage" && (strings.Contains(key, "token") || strings.Contains(key, "auth") || strings.Contains(key, "session")) {
			add("observed", "Persistent authentication material", "Authentication-like state is stored in localStorage; review XSS exposure and logout clearing.", 1)
			break
		}
	}
	return result
}

func atAGlance(behavior, auth []Insight, messaging *MessageBoundary, context authContext) []string {
	result := make([]string, 0, 6)
	if context.OAuth && (context.RedirectURI || context.AuthCode) {
		result = append(result, "OAuth redirect data written to sessionStorage")
	}
	if messaging != nil && messaging.Receivers > 0 {
		uncovered := messaging.Unvalidated + messaging.SourceOnly
		if uncovered > 0 || messaging.ValidationUnknown > 0 {
			value := fmt.Sprintf("%d message %s", messaging.Receivers, plural(messaging.Receivers, "receiver"))
			if uncovered > 0 {
				value += fmt.Sprintf(" · %d without origin validation", uncovered)
			}
			if messaging.ValidationUnknown > 0 {
				value += fmt.Sprintf(" · %d unresolved", messaging.ValidationUnknown)
			}
			result = append(result, value)
		}
	}
	if messaging != nil && messaging.WildcardTargets > 0 {
		result = append(result, fmt.Sprintf("%d wildcard Window postMessage %s", messaging.WildcardTargets, plural(messaging.WildcardTargets, "sender")))
	}
	if context.DebugFlag {
		result = append(result, "Production debug flag referenced")
	}
	if containsInsightTitle(behavior, "Download and export workflow") && containsInsightTitle(behavior, "Profile and organization management") {
		result = append(result, "Download/export and profile-management workflows")
	}
	if containsInsightTitle(behavior, "Client telemetry") {
		result = append(result, "Client telemetry referenced")
	}
	for _, item := range auth {
		if len(result) == 6 {
			break
		}
		if item.Title == "Bearer authorization" || item.Title == "Access-token acquisition" || item.Title == "OAuth/OIDC" {
			result = appendUnique(result, item.Title)
		}
	}
	for _, item := range behavior {
		if len(result) == 6 {
			break
		}
		result = appendUnique(result, item.Title)
	}
	return result
}

func addProfile(lower string, frameworks, bundlers map[string]struct{}) {
	for _, candidate := range []struct{ marker, name string }{
		{"__next_data__", "Next.js"}, {"/_next/", "Next.js"}, {"next/router", "Next.js"},
		{"reactdom", "React"}, {"react/jsx-runtime", "React"}, {"react.element", "React"}, {"react.fragment", "React"}, {"__vue__", "Vue"},
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
		{"serviceworker", "Service worker"}, {"new worker(", "Web worker"},
		{"navigator.clipboard", "Clipboard"}, {"navigator.geolocation", "Geolocation"}, {"getusermedia", "Camera or microphone"},
		{"notification.requestpermission", "Notifications"}, {"createelement(\"script\")", "Dynamic script creation"}, {"createelement('script')", "Dynamic script creation"},
	} {
		if strings.Contains(lower, candidate.marker) {
			capabilities[candidate.name] = struct{}{}
		}
	}
}

func authenticationOperationKinds(targets []string) []string {
	found := stringSet()
	for _, target := range targets {
		lower := strings.ToLower(target)
		if strings.Contains(lower, "/login") {
			found["login"] = struct{}{}
		}
		authScoped := containsAny(lower, "/auth/", "/oauth/", "passport", "/account/", "account_")
		if authScoped && strings.Contains(lower, "register") {
			found["registration"] = struct{}{}
		}
		if authScoped && containsAny(lower, "verify", "validate") {
			found["verification"] = struct{}{}
		}
	}
	return sorted(found)
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
		{"auth0.com", "Auth0 — authentication"}, {"okta.com", "Okta — authentication"}, {"spotify.com", "Spotify — music platform"},
		{"iapplogcdn.com", "ByteDance AppLog — analytics"}, {"slardar", "Slardar — monitoring"},
	} {
		if strings.Contains(lower, candidate.marker) {
			return candidate.name
		}
	}
	return ""
}

func addIntegrationSignals(lower string, integrations map[string]struct{}) {
	for _, candidate := range []struct{ marker, name string }{
		{"spotify", "Spotify — music platform"}, {"sentry", "Sentry — monitoring"}, {"slardar", "Slardar — monitoring"},
		{"applog", "ByteDance AppLog — analytics"}, {"rangers", "Rangers — analytics/experimentation"},
	} {
		if strings.Contains(lower, candidate.marker) {
			integrations[candidate.name] = struct{}{}
		}
	}
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

func buildAuthContext(lower string, storage map[string]StorageOperation) authContext {
	context := authContext{
		OAuth: countPresent(lower, "oauth", "openid", "code_verifier", "code_challenge") > 0,
		State: countPresent(lower, "oauth_state", "stateparam", "state_param") > 0,
		PKCE:  countPresent(lower, "code_verifier", "code_challenge") > 0,
	}
	for _, item := range storage {
		key := strings.ToLower(item.Key)
		written := item.Action == "write" || item.Action == "read/write"
		context.RedirectURI = context.RedirectURI || written && strings.Contains(key, "redirect_uri")
		context.AuthCode = context.AuthCode || written && (strings.Contains(key, "auth_code") || strings.Contains(key, "authorization_code"))
		context.State = context.State || written && strings.Contains(key, "oauth_state")
		context.PKCE = context.PKCE || written && (strings.Contains(key, "code_verifier") || strings.Contains(key, "code_challenge"))
		context.DebugFlag = context.DebugFlag || strings.Contains(key, "prod-debug") || strings.Contains(key, "production_debug")
	}
	return context
}

func operationKinds(targets, scope []string, operations map[string][]string) []string {
	found := stringSet()
	for _, target := range targets {
		lower := strings.ToLower(target)
		if len(scope) > 0 && !containsAny(lower, scope...) {
			continue
		}
		for operation, markers := range operations {
			if containsAny(lower, markers...) {
				found[operation] = struct{}{}
			}
		}
	}
	return sorted(found)
}

func uniqueTargets(values []string) []string {
	seen := stringSet()
	result := make([]string, 0, len(values))
	for _, value := range values {
		key := strings.ToLower(value)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func countPresent(value string, markers ...string) int {
	count := 0
	for _, marker := range markers {
		if strings.Contains(value, marker) {
			count++
		}
	}
	return count
}

func compact(value string) string {
	return strings.NewReplacer(" ", "", "\n", "", "\r", "", "\t", "").Replace(value)
}
func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
func boolCount(values ...bool) int {
	count := 0
	for _, value := range values {
		count += boolToInt(value)
	}
	return count
}
func containsInsightTitle(values []Insight, title string) bool {
	for _, value := range values {
		if value.Title == title {
			return true
		}
	}
	return false
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
