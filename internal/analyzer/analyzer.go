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
	// RPCPaths maps an RPC method to the path it was found through, as written;
	// the paths are not endpoints. RPCSpellings holds every such path, for
	// callers that read the words of each spelling.
	RPCPaths     map[string]string `json:"rpc_paths,omitempty"`
	RPCSpellings []string          `json:"rpc_spellings,omitempty"`
	Stats        Stats             `json:"stats"`
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
	// Module paths are not endpoints, but they stay file candidates as before.
	var modulePaths []string
	for _, m := range directPath.FindAllStringSubmatchIndex(source, -1) {
		if match := source[m[2]:m[3]]; !strings.HasPrefix(match, "//") {
			at := literalAt{source, m[0], m[1] - 1}
			if value, ok := cleanEndpoint(match, at); ok {
				endpoints[value] = struct{}{}
			} else if value, module := modulePath(match, at); module {
				modulePaths = append(modulePaths, value)
			}
		}
	}
	literals, opens := quotedValues(source)
	for i, value := range literals {
		quote := literalAt{source, opens[i], opens[i] + 1 + len(value)}
		value = strings.TrimSpace(value)
		if len(value) > MaxCandidateLength {
			continue
		}
		if hasURLPrefix(value) {
			if clean, ok := cleanURL(value); ok {
				urls[clean] = struct{}{}
			}
		} else if clean, ok := cleanEndpoint(value, quote); ok {
			endpoints[clean] = struct{}{}
		}
	}
	// An RPC path joins the endpoints until the parameters are read from it,
	// so that they are read from every spelling, such as "/$rpc/...".
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
	fileCandidates = append(fileCandidates, modulePaths...)
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

	// An RPC method is reported once, as an RPC method with its path.
	for _, path := range rpc.paths {
		delete(endpoints, path)
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
		RPCMethods: rpc.Methods, RPCWireIDs: rpc.WireIDs, RPCPaths: rpc.Paths, RPCSpellings: rpc.paths,
	}
	result.Stats = Stats{len(result.Endpoints), len(result.URLs), len(result.Emails), len(result.Files), len(result.Parameters), len(result.GraphQLOperations), len(result.RPCMethods)}
	return result
}

func normalizeSource(content string) string {
	return strings.NewReplacer(`\u002f`, "/", `\u002F`, "/", `\x2f`, "/", `\x2F`, "/", `\/`, "/").Replace(html.UnescapeString(content))
}

// quotedValues returns the contents of every quoted literal and the offset of
// each literal's opening quote in source.
func quotedValues(source string) ([]string, []int) {
	values := make([]string, 0, 32)
	opens := make([]int, 0, 32)
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
				opens = append(opens, start-1)
			}
			cursor = end + 1
		} else {
			cursor = start
		}
	}
	return values, opens
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

// nonRequestNamespaces are prefixes of values that are names in a well-known
// namespace and never request paths: Google's client-side monitoring metrics
// ("/client_streamz/<area>/<metric>") and the keys of the Google API client
// configuration ("googleapis.config/<area>/<key>").
var nonRequestNamespaces = []string{"/client_streamz/", "googleapis.config/"}

// isNonRequestValue reports a value that has the shape of a path but is
// something else, on evidence specific to each kind: a namespace of names, a
// reference into a JSON schema, a path in a JSON Patch operation, or the JSON
// pointer example in a sentence that says so.
func isNonRequestValue(value string, at literalAt) bool {
	for _, prefix := range nonRequestNamespaces {
		if strings.HasPrefix(value, prefix) {
			return true
		}
	}
	return isSchemaReference(value) || at.inJSONPatchOperation() || at.isJSONPointerExample()
}

// isSchemaReference reports "<file>.json#/$defs/<Name>": a file name without
// directories and a fragment that points into the schema's definitions. A
// route with a hash fragment has a directory or a route before the "#".
func isSchemaReference(value string) bool {
	file, fragment, found := strings.Cut(value, "#")
	if !found || file == "" || strings.Contains(file, "/") {
		return false
	}
	lower := strings.ToLower(file)
	if !strings.HasSuffix(lower, ".json") && !strings.HasSuffix(lower, ".yaml") && !strings.HasSuffix(lower, ".yml") {
		return false
	}
	return strings.HasPrefix(fragment, "/$defs/") || strings.HasPrefix(fragment, "/definitions/") || strings.HasPrefix(fragment, "/components/schemas/")
}

var jsonPatchOperation = regexp.MustCompile(`(?:^|[,{\s])["']?op["']?\s*:\s*["'](?:add|remove|replace|move|copy|test)["']`)

// inJSONPatchOperation reports a literal that is the "path" of a JSON Patch
// operation: the value of a path property in an object that also has an "op"
// property naming a patch operation. The object is read from the braces around
// the literal; if they cannot be matched nearby, it is not one.
func (l literalAt) inJSONPatchOperation() bool {
	if l.source == "" {
		return false
	}
	before := strings.TrimRight(l.source[max(0, l.open-16):l.open], " \t\r\n")
	if !strings.HasSuffix(before, ":") || identifierSuffix(strings.TrimRight(strings.TrimSuffix(before, ":"), `"'`)) != "path" {
		return false
	}
	const reach = 240
	start, depth := -1, 0
	for i := l.open - 1; i >= 0 && l.open-i <= reach; i-- {
		if c := l.source[i]; c == '}' {
			depth++
		} else if c == '{' {
			if depth == 0 {
				start = i
				break
			}
			depth--
		}
	}
	if start < 0 {
		return false
	}
	end, depth := -1, 0
	for i := l.close + 1; i < len(l.source) && i-l.close <= reach; i++ {
		if c := l.source[i]; c == '{' {
			depth++
		} else if c == '}' {
			if depth == 0 {
				end = i
				break
			}
			depth--
		}
	}
	return end > 0 && jsonPatchOperation.MatchString(l.source[start:end])
}

// isJSONPointerExample reports a quoted path inside a sentence that calls it a
// JSON pointer, such as a description saying "The JSON pointer to the field
// (e.g. '/items/0/name')". No other quote may stand between the words and the
// literal, so the words belong to the string the literal is in.
func (l literalAt) isJSONPointerExample() bool {
	if l.source == "" {
		return false
	}
	window := l.source[max(0, l.open-160):l.open]
	at := strings.LastIndex(strings.ToLower(window), "json pointer")
	return at >= 0 && !strings.ContainsAny(window[at:], "'\"`;{}\r\n")
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

// literalAt locates a quoted literal in the source so that its use can be read
// from the surrounding code. The zero value means the position is unknown.
type literalAt struct {
	source      string
	open, close int // offsets of the opening and closing quote
}

type literalUse int

const (
	useUnknown literalUse = iota
	useModule             // module specifier or module-map key
	useRequest            // argument of a request call or a url property
)

var moduleCallees = []string{"require", "import", "resolve", "context"}
var requestCallees = []string{"fetch", "get", "post", "put", "patch", "delete", "head", "request", "open", "ajax", "getjson", "sendbeacon", "eventsource"}

// moduleSourceExtensions are code and style extensions of module files. JSON
// is handled separately because it is also a common request target.
var moduleSourceExtensions = []string{".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx", ".vue", ".svelte", ".scss", ".sass", ".less", ".css"}

// use reads how the literal is used from the few bytes around it.
func (l literalAt) use() literalUse {
	if l.source == "" {
		return useUnknown
	}
	before := strings.TrimRight(l.source[max(0, l.open-64):l.open], " \t\r\n")
	if strings.HasSuffix(before, "(") {
		callee := strings.ToLower(identifierSuffix(strings.TrimSuffix(before, "(")))
		if containsString(moduleCallees, callee) {
			return useModule
		}
		if containsString(requestCallees, callee) {
			return useRequest
		}
	} else if word := identifierSuffix(before); word == "from" || word == "import" {
		return useModule
	} else if strings.HasSuffix(before, ":") && oneOf(strings.ToLower(identifierSuffix(strings.TrimSuffix(before, ":"))), "url", "uri", "endpoint") {
		return useRequest
	}
	after := strings.TrimLeft(l.source[min(len(l.source), l.close+1):min(len(l.source), l.close+160)], " \t\r\n")
	if !strings.HasPrefix(after, ":") {
		return useUnknown
	}
	// A ./ or ../ path used as an object key is a module map (bundler module
	// tables, require.context maps), not a request.
	if strings.HasSuffix(before, "{") || strings.HasSuffix(before, ",") {
		return useModule
	}
	// Bundlers also put comments between the key and `function`, `(e, t) => ...`
	// or `e => ...`.
	after = strings.TrimLeft(after[1:], " \t\r\n")
	for strings.HasPrefix(after, "/*") {
		end := strings.Index(after, "*/")
		if end < 0 {
			return useUnknown
		}
		after = strings.TrimLeft(after[end+2:], " \t\r\n")
	}
	if strings.HasPrefix(after, "(") || strings.HasPrefix(after, "function") || strings.HasPrefix(after, "async") {
		return useModule
	}
	if name := identifierPrefix(after); name != "" && strings.HasPrefix(strings.TrimLeft(after[len(name):], " \t"), "=>") {
		return useModule
	}
	return useUnknown
}

func isCalleeByte(b byte) bool {
	return b == '_' || b == '$' || b == '.' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// identifierSuffix returns the trailing identifier of s, keeping dotted
// member access so that `require.resolve` yields `resolve`.
func identifierSuffix(s string) string {
	end := len(s)
	start := end
	for start > 0 && isCalleeByte(s[start-1]) {
		start--
	}
	word := s[start:end]
	if dot := strings.LastIndexByte(word, '.'); dot >= 0 {
		word = word[dot+1:]
	}
	return word
}

func identifierPrefix(s string) string {
	end := 0
	for end < len(s) && isCalleeByte(s[end]) && s[end] != '.' {
		end++
	}
	return s[:end]
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func hasNodeModulesSegment(value string) bool {
	return strings.HasPrefix(value, "node_modules/") || strings.Contains(value, "/node_modules/")
}

// classifyPath reports how a literal path is used and whether it is a module
// path rather than a request target: anything under node_modules, a module
// specifier or module-map key, or a ./ or ../ code or style file without a
// query string (unless a request call names it).
func classifyPath(value string, at literalAt) (use literalUse, module bool) {
	if hasNodeModulesSegment(value) {
		return useUnknown, true
	}
	if !strings.HasPrefix(value, "./") && !strings.HasPrefix(value, "../") {
		return useUnknown, false
	}
	use = at.use()
	if use == useModule {
		return use, true
	}
	if use != useRequest && !strings.ContainsAny(value, "?#") {
		lower := strings.ToLower(value)
		for _, ext := range moduleSourceExtensions {
			if strings.HasSuffix(lower, ext) {
				return use, true
			}
		}
	}
	return use, false
}

// modulePath returns the trimmed value when candidate is a module path.
func modulePath(candidate string, at literalAt) (string, bool) {
	value := strings.Trim(candidate, `\\"'`)
	value = strings.TrimRight(value, `\\,;)]}>`)
	_, module := classifyPath(value, at)
	return value, module
}

// cleanEndpoint decides whether a quoted value is a request path. Values that
// start with ./ or ../ are also module specifiers and bundler source paths, so
// they are checked against how the literal is used in at.
func cleanEndpoint(candidate string, at literalAt) (string, bool) {
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
	use, module := classifyPath(value, at)
	if module || isNonRequestValue(value, at) {
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
	// Plain relative values and unclassified ./ and ../ values must pass the
	// keyword gate; a request call vouches for its argument.
	if relative || (dotRelative && use != useRequest) {
		gated := lower
		if dotRelative {
			gated = strings.ToLower(structural)
		}
		highPrefix := false
		for _, prefix := range relativeRoutePrefixes {
			if strings.HasPrefix(gated, prefix) {
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
