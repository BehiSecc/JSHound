// Package analyzer extracts useful artifacts from JavaScript and HTML without
// executing untrusted input. Its route heuristics intentionally track
// HuntProxy's PageAnalyzer.
package analyzer

import (
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
)

const MaxCandidateLength = 2048

var (
	// Go's RE2 limits individual counted repetitions to 1000, so adjacent
	// bounded repetitions retain PageAnalyzer's cap without an unbounded match.
	absoluteURL         = regexp.MustCompile(`(?i)(?:https?|wss?)://[^\s"'<>\\]{4,1000}[^\s"'<>\\]{0,1000}[^\s"'<>\\]{0,48}`)
	protocolRelativeURL = regexp.MustCompile(`(?i)//[a-z0-9](?:[a-z0-9.-]*\.)[a-z]{2,24}(?::\d{1,5})?(?:/[^\s"'<>\\]{0,1000}[^\s"'<>\\]{0,1000})?`)
	directPath          = regexp.MustCompile(`(?i)["']((?:/|\./|\.\./)[^"'<>\\\s]{2,1000}[^"'<>\\\s]{0,1000}[^"'<>\\\s]{0,48})["']`)
	emailPattern        = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.-]+\.[a-z]{2,24}`)
	staticAsset         = regexp.MustCompile(`(?i)\.(?:css|png|jpe?g|gif|svg|ico|webp|avif|bmp|woff2?|ttf|eot|map|mp4|webm|mp3|wav|ogg|flac|aac)(?:[?#].*)?$`)
	routeSegment        = regexp.MustCompile(`^(?::?[A-Za-z0-9_.@%\-]+|\{[A-Za-z0-9_.\-]+\}|[A-Za-z0-9_.@%\-]+:[A-Za-z0-9_.\-]+)$`)
	filePattern         = regexp.MustCompile(`(?i)^(?:https?://[^\s"'<>\\]+|(?:/|\./|\.\./)?[a-z0-9_@%+~.-]+(?:/[a-z0-9_@%+~.()\-]+)*)\.(?:7z|apk|asp|aspx|bak|backup|bin|bz2|cfg|conf|config|crt|csv|db|docx?|env|gz|html?|ini|jar|js|json|jsx|key|log|mjs|old|orig|p12|pdf|pem|pfx|php|properties|rar|sql|tar|tgz|toml|ts|tsx|txt|war|wasm|xlsx?|xml|ya?ml|zip)(?:[?#][^\s"'<>\\]*)?$`)
	graphqlOperation    = regexp.MustCompile(`(?m)\b(query|mutation|subscription)\s+([_A-Za-z][_0-9A-Za-z]*)\s*(?:\([^)]{0,1000}\)\s*)?(?:@[A-Za-z_][^\{]{0,1000})?\{`)
	graphqlVariable     = regexp.MustCompile(`\$([_A-Za-z][_0-9A-Za-z]*)\s*:`)
	queryParameter      = regexp.MustCompile(`(?:[?&]|&amp;)([A-Za-z_][A-Za-z0-9_.\-]{0,127})=`)
	parameterMethod     = regexp.MustCompile(`(?i)\b(?:URLSearchParams|FormData)?\s*\.?\s*(?:append|set|get|has|delete)\s*\(\s*["']([A-Za-z_][A-Za-z0-9_.\-]{0,127})["']`)
	requestBody         = regexp.MustCompile(`(?is)(?:JSON\.stringify\s*\(|axios\.(?:post|put|patch)\s*\([^,]+,\s*)\{([^{}]{1,1000}[^{}]{0,1000}[^{}]{0,1000}[^{}]{0,1000}[^{}]{0,96})\}`)
	objectKey           = regexp.MustCompile(`(?:^|,)\s*(?:["']([A-Za-z_][A-Za-z0-9_.\-]{0,127})["']|([A-Za-z_][A-Za-z0-9_.\-]{0,127}))\s*:`)
	htmlName            = regexp.MustCompile(`(?is)<(?:input|select|textarea|button)\b[^>]*?\bname\s*=\s*(?:"([^"]+)"|'([^']+)'|([^\s"'=<>` + "`" + `]+))`)
)

var relativeRoutePrefixes = []string{
	"account/", "accounts/", "admin/", "api/", "auth/", "callback/", "config/",
	"credential/", "credentials/", "dashboard/", "debug/", "download/", "graphql/",
	"internal/", "login/", "logout/", "member/", "members/", "oauth/", "org/",
	"orgs/", "private/", "project/", "projects/", "rest/", "settings/", "token/",
	"upload/", "user/", "users/", "v1/", "v2/", "v3/", "webhook/", "webhooks/",
}

var routeKeywords = map[string]struct{}{
	"account": {}, "accounts": {}, "admin": {}, "api": {}, "auth": {}, "callback": {},
	"config": {}, "create": {}, "delete": {}, "download": {}, "export": {}, "graphql": {},
	"import": {}, "internal": {}, "list": {}, "login": {}, "logout": {}, "member": {},
	"members": {}, "oauth": {}, "org": {}, "orgs": {}, "private": {}, "profile": {},
	"project": {}, "projects": {}, "rest": {}, "search": {}, "settings": {}, "sso": {},
	"token": {}, "update": {}, "upload": {}, "user": {}, "users": {}, "v1": {}, "v2": {},
	"v3": {}, "webhook": {}, "webhooks": {},
}

type Stats struct {
	Endpoints         int `json:"endpoints"`
	URLs              int `json:"urls"`
	Emails            int `json:"emails"`
	Files             int `json:"files"`
	Parameters        int `json:"parameters"`
	GraphQLOperations int `json:"graphql_operations"`
	RPCMethods        int `json:"rpc_methods"`
}

type Result struct {
	Endpoints         []string `json:"endpoints,omitempty"`
	URLs              []string `json:"urls,omitempty"`
	Emails            []string `json:"emails,omitempty"`
	Files             []string `json:"files,omitempty"`
	Parameters        []string `json:"parameters,omitempty"`
	GraphQLOperations []string `json:"graphql_operations,omitempty"`
	RPCMethods        []string `json:"rpc_methods,omitempty"`
	// RPCWireIDs maps an RPC method to the id registered for it.
	RPCWireIDs map[string]string `json:"rpc_wire_ids,omitempty"`
	Stats      Stats             `json:"stats"`
}

type Options struct {
	IncludeParameters bool
	// SkipRPC leaves out RPC methods, for callers that read them separately.
	SkipRPC bool
}

// Analyze scans JavaScript, HTML, or any other text-like response body.
func Analyze(content []byte) Result {
	return AnalyzeWithOptions(content, Options{IncludeParameters: true})
}

// AnalyzeWithOptions allows callers to skip optional, relatively expensive
// parameter discovery when it was not requested.
func AnalyzeWithOptions(content []byte, opts Options) Result {
	source := normalizeSource(string(content))
	endpoints, urls, emails := set(), set(), set()
	files, parameters, operations := set(), set(), set()

	for _, loc := range absoluteURL.FindAllStringIndex(source, -1) {
		if loc[1] < len(source) && isASCIIWhitespace(source[loc[1]]) {
			continue
		}
		if value, ok := cleanURL(source[loc[0]:loc[1]]); ok {
			urls[value] = struct{}{}
		}
	}
	for _, loc := range protocolRelativeURL.FindAllStringIndex(source, -1) {
		if loc[0] > 0 && source[loc[0]-1] == ':' {
			continue
		}
		if value, ok := cleanURL(source[loc[0]:loc[1]]); ok {
			urls[value] = struct{}{}
		}
	}
	for _, match := range directPath.FindAllStringSubmatch(source, -1) {
		if !strings.HasPrefix(match[1], "//") {
			if value, ok := cleanEndpoint(match[1]); ok {
				endpoints[value] = struct{}{}
			}
		}
	}
	literals := quotedValues(source)
	for _, value := range literals {
		value = strings.TrimSpace(value)
		if len(value) > MaxCandidateLength {
			continue
		}
		if hasURLPrefix(value) {
			if clean, ok := cleanURL(value); ok {
				urls[clean] = struct{}{}
			}
		} else if clean, ok := cleanEndpoint(value); ok {
			endpoints[clean] = struct{}{}
		}
	}
	// An RPC path is also an endpoint, as written: it is what a reader would
	// request. Paths the endpoint rules do not accept, such as "/$rpc/...", join.
	var rpc RPC
	if !opts.SkipRPC {
		rpc = ReadRPC(source)
		for _, path := range rpc.paths {
			endpoints[path] = struct{}{}
		}
	}
	for _, loc := range emailPattern.FindAllStringIndex(source, -1) {
		value := source[loc[0]:loc[1]]
		if validEmail(source, loc[0], value) {
			emails[value] = struct{}{}
		}
	}
	fileCandidates := append([]string{}, literals...)
	for value := range endpoints {
		fileCandidates = append(fileCandidates, value)
	}
	// Absolute URLs belong exclusively to the URL category.
	for _, candidate := range fileCandidates {
		value := strings.TrimSpace(candidate)
		if hasURLPrefix(value) {
			continue
		}
		if len(value) <= MaxCandidateLength && filePattern.MatchString(value) {
			files[value] = struct{}{}
		}
	}
	if opts.IncludeParameters {
		for _, match := range queryParameter.FindAllStringSubmatch(source, -1) {
			parameters[match[1]] = struct{}{}
		}
		if strings.Contains(source, "URLSearchParams") || strings.Contains(source, "FormData") || strings.Contains(source, ".append(") || strings.Contains(source, ".set(") {
			for _, match := range parameterMethod.FindAllStringSubmatch(source, -1) {
				parameters[match[1]] = struct{}{}
			}
		}
		if strings.Contains(strings.ToLower(source), "<input") || strings.Contains(strings.ToLower(source), "<select") || strings.Contains(strings.ToLower(source), "<textarea") {
			for _, match := range htmlName.FindAllStringSubmatch(source, -1) {
				for i := 1; i < len(match); i++ {
					if match[i] != "" {
						parameters[match[i]] = struct{}{}
						break
					}
				}
			}
		}
		if strings.Contains(source, "JSON.stringify") || strings.Contains(source, "axios.post") || strings.Contains(source, "axios.put") || strings.Contains(source, "axios.patch") {
			for _, body := range requestBody.FindAllStringSubmatch(source, -1) {
				for _, match := range objectKey.FindAllStringSubmatch(body[1], -1) {
					if match[1] != "" {
						parameters[match[1]] = struct{}{}
					} else {
						parameters[match[2]] = struct{}{}
					}
				}
			}
		}
	}
	if strings.Contains(source, "query") || strings.Contains(source, "mutation") || strings.Contains(source, "subscription") {
		for _, match := range graphqlOperation.FindAllStringSubmatch(source, -1) {
			operations[match[1]+" "+match[2]] = struct{}{}
		}
		if opts.IncludeParameters {
			for _, match := range graphqlVariable.FindAllStringSubmatch(source, -1) {
				parameters[match[1]] = struct{}{}
			}
		}
	}
	for value := range appendSets(endpoints, urls) {
		if opts.IncludeParameters {
			collectURLParameters(value, parameters)
			collectRouteParameters(value, parameters)
		}
	}

	// Prefer explicit-scheme URLs over protocol-relative duplicates.
	for value := range urls {
		if !strings.HasPrefix(value, "//") {
			continue
		}
		for _, scheme := range []string{"http:", "https:", "ws:", "wss:"} {
			if _, exists := urls[scheme+value]; exists {
				delete(urls, value)
				break
			}
		}
	}

	result := Result{
		Endpoints: sorted(endpoints), URLs: sorted(urls), Emails: sorted(emails),
		Files: sorted(files), Parameters: sorted(parameters), GraphQLOperations: sorted(operations),
		RPCMethods: rpc.Methods, RPCWireIDs: rpc.WireIDs,
	}
	result.Stats = Stats{len(result.Endpoints), len(result.URLs), len(result.Emails), len(result.Files), len(result.Parameters), len(result.GraphQLOperations), len(result.RPCMethods)}
	return result
}

func normalizeSource(content string) string {
	return strings.NewReplacer(`\u002f`, "/", `\u002F`, "/", `\x2f`, "/", `\x2F`, "/", `\/`, "/").Replace(html.UnescapeString(content))
}

func quotedValues(source string) []string {
	values := make([]string, 0, 32)
	for cursor := 0; cursor < len(source); {
		quote := source[cursor]
		if quote != '\'' && quote != '"' && quote != '`' {
			cursor++
			continue
		}
		start, end, escaped, closed := cursor+1, cursor+1, false, false
		for end < len(source) {
			b := source[end]
			if escaped {
				escaped = false
				end++
				continue
			}
			if b == '\\' {
				escaped = true
				end++
				continue
			}
			if b == quote {
				closed = true
				break
			}
			if quote != '`' && (b == '\r' || b == '\n') {
				break
			}
			end++
		}
		if closed {
			if end-start <= MaxCandidateLength {
				values = append(values, source[start:end])
			}
			cursor = end + 1
		} else {
			cursor = start
		}
	}
	return values
}

func cleanURL(candidate string) (string, bool) {
	value := strings.TrimSpace(candidate)
	value = strings.TrimRight(value, `\\,.;:)]}*`)
	if len(value) < 8 || len(value) > MaxCandidateLength || strings.Contains(value, "${") || strings.Contains(value, "{") || staticAsset.MatchString(value) {
		return "", false
	}
	parsedValue := value
	if strings.HasPrefix(value, "//") {
		parsedValue = "https:" + value
	}
	parsed, err := url.Parse(parsedValue)
	if err != nil || parsed.Hostname() == "" || !oneOf(parsed.Scheme, "http", "https", "ws", "wss") {
		return "", false
	}
	// Frameworks such as Next.js use single-letter dummy origins (http://n,
	// http://f) solely as bases for parsing relative URLs. They are not useful
	// network targets. Preserve localhost, IPs, and meaningful hostnames.
	host := parsed.Hostname()
	if strings.EqualFold(host, "www.w3.org") && isW3CNamespacePath(parsed.Path) {
		return "", false
	}
	if (parsed.Scheme == "http" || parsed.Scheme == "https") && len(host) == 1 && parsed.Port() == "" && (parsed.Path == "" || parsed.Path == "/") && parsed.RawQuery == "" {
		return "", false
	}
	return value, true
}

func isW3CNamespacePath(path string) bool {
	switch strings.ToLower(strings.TrimSuffix(path, "/")) {
	case "/1998/math/mathml", "/1999/xhtml", "/1999/xlink", "/2000/svg", "/xml/1998/namespace":
		return true
	default:
		return false
	}
}

func collectRouteParameters(value string, parameters map[string]struct{}) {
	pathname := strings.SplitN(strings.SplitN(value, "?", 2)[0], "#", 2)[0]
	for _, segment := range strings.Split(pathname, "/") {
		segment = strings.TrimSpace(segment)
		if strings.HasPrefix(segment, ":") {
			segment = strings.TrimPrefix(segment, ":")
		} else if strings.HasPrefix(segment, "{") && strings.HasSuffix(segment, "}") {
			segment = strings.TrimSuffix(strings.TrimPrefix(segment, "{"), "}")
		} else {
			continue
		}
		if segment != "" {
			parameters[segment] = struct{}{}
		}
	}
}

func cleanEndpoint(candidate string) (string, bool) {
	value := strings.Trim(candidate, `\\"'`)
	value = strings.TrimRight(value, `\\,;)]}>`)
	if len(value) < 3 || len(value) > MaxCandidateLength || strings.IndexFunc(value, func(r rune) bool { return r == ' ' || r == '\t' || r == '\r' || r == '\n' }) >= 0 ||
		strings.HasPrefix(value, "#") || strings.HasPrefix(value, "data:") || strings.Contains(value, "${") || staticAsset.MatchString(value) {
		return "", false
	}
	lower := strings.ToLower(value)
	if oneOf(lower, "application/json", "application/xml", "application/x-www-form-urlencoded", "multipart/form-data", "text/css", "text/html", "text/javascript", "text/plain") {
		return "", false
	}
	rootRelative := strings.HasPrefix(value, "/") && !strings.HasPrefix(value, "//")
	dotRelative := strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../")
	relative := !rootRelative && !dotRelative
	if relative && !strings.Contains(value, "/") {
		return "", false
	}
	structural := strings.SplitN(strings.SplitN(value, "?", 2)[0], "#", 2)[0]
	for strings.HasPrefix(structural, "../") {
		structural = strings.TrimPrefix(structural, "../")
	}
	structural = strings.TrimPrefix(structural, "./")
	parts := strings.FieldsFunc(strings.Trim(structural, "/"), func(r rune) bool { return r == '/' })
	if len(parts) == 0 {
		return "", false
	}
	allShort := true
	for _, part := range parts {
		if len(part) >= 2 {
			allShort = false
		}
		if !routeSegment.MatchString(part) {
			return "", false
		}
	}
	if allShort {
		return "", false
	}
	if relative {
		highPrefix := false
		for _, prefix := range relativeRoutePrefixes {
			if strings.HasPrefix(lower, prefix) {
				highPrefix = true
				break
			}
		}
		highSegment, parameterized := false, false
		for _, part := range parts {
			key := strings.ToLower(strings.TrimPrefix(part, ":"))
			_, high := routeKeywords[key]
			highSegment = highSegment || high
			parameterized = parameterized || strings.HasPrefix(part, ":")
		}
		last := strings.ToLower(parts[len(parts)-1])
		webExtension := false
		for _, ext := range []string{".action", ".asp", ".aspx", ".cgi", ".do", ".html", ".js", ".json", ".jsp", ".php", ".txt", ".xml"} {
			if strings.HasSuffix(last, ext) {
				webExtension = true
				break
			}
		}
		if !highPrefix && !highSegment && !parameterized && !webExtension {
			return "", false
		}
	}
	return value, true
}

func validEmail(content string, start int, value string) bool {
	parts := strings.Split(value, "@")
	domain := strings.ToLower(parts[len(parts)-1])
	if oneOf(domain, "example.com", "test.com", "domain.com", "placeholder.com", "email.com") || strings.Contains(domain, "w3.org") {
		return false
	}
	if start > 0 && content[start-1] == ':' {
		return false
	}
	prefix := strings.ToLower(content[max(0, start-12):start])
	return !strings.Contains(prefix, "://") && !strings.Contains(prefix, "mongodb") && !strings.Contains(prefix, "postgres") && !strings.Contains(prefix, "mysql")
}

func collectURLParameters(value string, parameters map[string]struct{}) {
	parsedValue := value
	if strings.HasPrefix(value, "//") {
		parsedValue = "https:" + value
	} else if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "./") || strings.HasPrefix(value, "../") {
		parsedValue = "https://jshound.invalid/" + strings.TrimLeft(value, "./")
	}
	if parsed, err := url.Parse(parsedValue); err == nil {
		for key := range parsed.Query() {
			if key != "" {
				parameters[key] = struct{}{}
			}
		}
	}
}

func hasURLPrefix(value string) bool {
	for _, prefix := range []string{"http://", "https://", "ws://", "wss://", "//"} {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return false
}
func oneOf(value string, choices ...string) bool {
	for _, choice := range choices {
		if value == choice {
			return true
		}
	}
	return false
}
func isASCIIWhitespace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\r' || b == '\n' || b == '\v' || b == '\f'
}
func set() map[string]struct{} { return make(map[string]struct{}) }
func appendSets(sets ...map[string]struct{}) map[string]struct{} {
	result := set()
	for _, values := range sets {
		for value := range values {
			result[value] = struct{}{}
		}
	}
	return result
}
func sorted(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool {
		a, b := strings.ToLower(result[i]), strings.ToLower(result[j])
		if a == b {
			return result[i] < result[j]
		}
		return a < b
	})
	return result
}
