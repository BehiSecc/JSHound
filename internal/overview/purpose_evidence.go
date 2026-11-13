package overview

import (
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/BehiSecc/JSHound/internal/analyzer"
)

// maxFeatureExamples caps the paths or services shown per kind of evidence.
const maxFeatureExamples = 3

// Evidence totals say how many API calls and screens the file references in
// all, so the report can point to the full list the scanner prints.
type Evidence struct {
	APICalls int `json:"api_calls,omitempty"`
	Screens  int `json:"screens,omitempty"`
}

// evidenceItem is one API call or UI route found in the file, with the
// domain words and actions its path names.
type evidenceItem struct {
	key    string // what makes two items the same, as in the claimed set
	screen bool
	label  string // path or GraphQL operation shown to the reader
	group  string // RPC service or GraphQL, empty for a plain path
	nouns  []string
	verbs  []string
}

type evidence struct {
	items   []evidenceItem
	claimed map[string]struct{} // path keys already counted as evidence
	byNoun  map[string][]int
	// tableKeys are the tokens of property names whose words went into an API
	// call from an endpoint table, so they are not read as names a second time.
	tableKeys map[int]struct{}
}

var (
	// versionSegment matches "v1", "v1.2", "v2beta1", and similar API versions.
	versionSegment = regexp.MustCompile(`^v\d+(?:\.\d+)*(?:alpha|beta)?\d*$`)
	// numberedSegment matches "0.1" and "v1.2": a version, not a file name.
	numberedSegment = regexp.MustCompile(`^v?\d+(?:\.\d+)+$`)
	placeholder     = regexp.MustCompile(`\$?\{[^{}]*\}`)
	// paramSegment is a router parameter: ":id" but not the port in ":8443".
	paramSegment = regexp.MustCompile(`^(?::[A-Za-z_].*|\{.+\}|\$\{.+\}|\[.+\]|.+:[A-Za-z_][A-Za-z0-9_.-]*)$`)
	// actionSegment is the last segment of "/api/cameras.update": a resource, a
	// dot, and an action name.
	actionSegment = regexp.MustCompile(`^[A-Za-z0-9_-]+\.[A-Za-z][A-Za-z0-9_]*$`)
)

// fileExtensions end the names of files, which are not routes or calls. A dotted
// last segment with another suffix, as in "/api/cameras.update", is an action.
var fileExtensions = setOf("js", "mjs", "cjs", "jsx", "ts", "tsx", "map", "css", "scss", "less", "json", "html", "htm", "xml", "xsl",
	"txt", "md", "csv", "tsv", "yaml", "yml", "toml", "ini", "log", "png", "jpg", "jpeg", "gif", "svg", "webp", "avif", "ico", "bmp",
	"tif", "tiff", "woff", "woff2", "ttf", "otf", "eot", "mp3", "mp4", "webm", "ogg", "wav", "m4a", "mov", "avi", "pdf", "doc",
	"docx", "xls", "xlsx", "ppt", "pptx", "zip", "gz", "tgz", "tar", "rar", "7z", "wasm", "bin", "dat", "exe", "dll", "so", "py",
	"rb", "java", "php", "asp", "aspx", "jsp", "do", "cgi", "pl", "sh", "proto", "graphql", "gql", "sql", "swf", "apk", "dmg",
	"iso", "key", "pem", "crt", "cer", "env", "lock", "tmpl", "ejs", "hbs", "vue", "svelte", "pb", "bundle", "chunk")

// fileExtension reports whether a segment ends in a known file extension.
func fileExtension(segment string) bool {
	dot := strings.LastIndexByte(segment, '.')
	if dot < 0 {
		return false
	}
	_, ok := fileExtensions[strings.ToLower(segment[dot+1:])]
	return ok
}

// apiSegments mark a path as a server call rather than a screen.
var apiSegments = setOf("api", "rest", "graphql", "rpc", "trpc", "oauth", "oauth2", "webhook", "webhooks")

// unixRoots and desktopRoots start paths to files on a machine. Only roots that
// are not plausible routes are listed: "/home/:feedId", "/users/:id", and
// "/library/:id" are ordinary screens. The desktop roots are matched with the
// capitals they really have.
var (
	unixRoots    = setOf("etc", "usr", "var", "tmp", "opt", "proc", "sbin", "bin", "mnt", "sys", "srv", "node_modules")
	desktopRoots = setOf("Users", "Library", "Applications", "Volumes", "Windows")
)

// machinePath reports a path into a machine's file system. Paths found by the
// insight pass skip this check, because the code around them shows they are
// routes or requests.
func machinePath(path string) bool {
	root := strings.Split(strings.Trim(path, "/"), "/")[0]
	_, unix := unixRoots[root]
	_, desktop := desktopRoots[root]
	return unix || desktop
}

// collectEvidence gathers API calls and UI routes from the scanner, the
// insight pass, and GraphQL operation names. The scanner reads only the string
// literals that could hold a path or an operation, which gives the same paths
// as scanning the whole file at a fraction of the cost. Strings that belong to
// bundled packages are skipped when their origin is known.
func collectEvidence(file *File, tokens []token, fromPackage func(int) bool) *evidence {
	ev := &evidence{claimed: map[string]struct{}{}, byNoun: map[string][]int{}}
	var literals strings.Builder
	// listed are screens named by a list or a property, which do not show that
	// the paths beside them are screens too.
	listed, routed := map[string]bool{}, map[string]bool{}
	for i, t := range tokens {
		if t.kind != 's' && t.kind != 't' || fromPackage != nil && fromPackage(i) {
			continue
		}
		if strings.Contains(t.text, "/") || strings.Contains(t.text, "query") || strings.Contains(t.text, "mutation") || strings.Contains(t.text, "subscription") {
			text := t.text
			if t.kind == 's' && text[0] == '`' && !strings.Contains(text, `"`) {
				// The scanner reads quoted paths; a plain template is one.
				text = `"` + text[1:len(text)-1] + `"`
			}
			literals.WriteString(text)
			literals.WriteByte('\n')
		}
	}
	// RPC methods are read from the whole file in describePurpose.
	result := analyzer.AnalyzeWithOptions([]byte(literals.String()), analyzer.Options{SkipRPC: true})
	if file.Insights != nil {
		for _, route := range file.Insights.Routes {
			ev.addPath(route.Path, pathScreen)
			if key := pathKey(route.Path); !route.listed {
				routed[key] = true
			} else if !routed[key] {
				listed[key] = true
			}
		}
		for _, call := range file.Insights.API {
			if call.client && fromPackage != nil && fromPackage(call.token) {
				continue
			}
			ev.addPath(call.Path, pathAPI)
		}
	}
	var unknown []string
	for _, endpoint := range result.Endpoints {
		if kind := classifyPath(endpoint); kind == pathUnknown {
			unknown = append(unknown, endpoint)
		} else {
			ev.addPath(endpoint, kind)
		}
	}
	// A plain path next to a declared screen with the same first segment, such
	// as "/orders/new" beside "orders/:id", is another screen of the same area.
	screens := map[string]bool{}
	for _, item := range ev.items {
		if item.screen && (!listed[item.key] || routed[item.key]) {
			screens[firstSegment(item.label)] = true
		}
	}
	for _, endpoint := range unknown {
		if screens[firstSegment(endpoint)] {
			ev.addPath(endpoint, pathScreen)
		}
	}
	// An endpoint table names plain paths that no other shape marks as server
	// calls, and a view table names plain paths of screens. A path that the
	// sibling rule made a screen is already claimed.
	apis, views := pathTables(tokens, fromPackage)
	for _, table := range []struct {
		entries []tableEntry
		kind    pathKind
	}{{apis, pathAPI}, {views, pathScreen}} {
		for _, entry := range table.entries {
			if ev.addPathWords(entry.path, table.kind, nameWords(entry.key)) {
				if ev.tableKeys == nil {
					ev.tableKeys = map[int]struct{}{}
				}
				ev.tableKeys[entry.token] = struct{}{}
			}
		}
	}
	for _, operation := range result.GraphQLOperations {
		ev.addOperation(operation)
	}
	ev.index()
	return ev
}

// addRPCMethods records RPC methods, given as "Service.method", as API calls.
// A method that is also a path-style RPC endpoint is the same call.
func (e *evidence) addRPCMethods(methods []string) {
	for _, method := range methods {
		service, name, ok := strings.Cut(method, ".")
		if !ok {
			continue
		}
		key := rpcKey(service, name)
		if _, ok := e.claimed[key]; ok {
			continue
		}
		e.claimed[key] = struct{}{}
		item := evidenceItem{key: key, label: name, group: service}
		item.nouns, item.verbs = splitWords(append(nameWords(service), nameWords(name)...))
		e.items = append(e.items, item)
	}
	e.index()
}

// rpcKey makes "/pkg.v1.Service/Method", "/Service.Method", and the method
// "Service.method" the same item.
func rpcKey(service, method string) string {
	return "rpc " + strings.ToLower(service) + "." + strings.ToLower(method)
}

type pathKind int

const (
	pathUnknown pathKind = iota
	pathAPI
	pathScreen
	pathIgnored
)

// classifyPath decides what a scanner path is without seeing the code around
// it. Server-call shapes are APIs, router parameters mark screens, and
// anything else stays unknown.
func classifyPath(path string) pathKind {
	path = cleanEvidencePath(path)
	if path == "" {
		return pathIgnored
	}
	if _, _, ok := analyzer.RPCPath(path); ok {
		return pathAPI
	}
	if !evidencePath(path) || machinePath(path) {
		return pathIgnored
	}
	kind := pathUnknown
	for _, segment := range strings.Split(strings.Trim(path, "/"), "/") {
		switch {
		case strings.HasPrefix(segment, "{") || strings.HasPrefix(segment, "${"):
			return pathAPI
		case paramSegment.MatchString(segment):
			kind = pathScreen
		case versionSegment.MatchString(segment):
			return pathAPI
		default:
			if _, ok := apiSegments[strings.ToLower(segment)]; ok {
				return pathAPI
			}
		}
	}
	return kind
}

// cleanEvidencePath drops query strings, fragments, and trailing slashes.
func cleanEvidencePath(path string) string {
	if i := strings.IndexAny(path, "?#"); i >= 0 {
		path = path[:i]
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	if path == "/" || strings.ContainsAny(path, " \t\n\\*") {
		return ""
	}
	return path
}

// evidencePath rejects relative module imports, URLs, file names, and regular
// expressions. RPC paths are checked before this, so their dots are allowed.
func evidencePath(path string) bool {
	if strings.HasPrefix(path, ".") || strings.HasPrefix(path, ":") || strings.Contains(path, "://") || strings.HasPrefix(path, "//") {
		return false
	}
	segments := strings.Split(strings.Trim(path, "/"), "/")
	letters, numbered, content := false, false, false
	for i, segment := range segments {
		if strings.Contains(segment, ".") && !dottedSegment(segments, i) {
			return false
		}
		if numberedSegment.MatchString(segment) {
			numbered = true
		} else if _, api := apiSegments[strings.ToLower(segment)]; !api && !versionSegment.MatchString(segment) {
			content = true
		}
		if paramSegment.MatchString(segment) {
			continue
		}
		if len(segment) == 1 && i == len(segments)-1 && letters {
			// "/error/i" is a regular expression, not a route.
			return false
		}
		for i := 0; i < len(segment); i++ {
			if segment[i] >= 'A' && segment[i] <= 'Z' || segment[i] >= 'a' && segment[i] <= 'z' {
				letters = true
			}
		}
	}
	// "/api/0.1" is the prefix of a service, not a call.
	return letters && (content || !numbered)
}

// dottedSegment reports whether a segment with a dot is a version ("0.1",
// "v1.2") or the "resource.action" end of an API path ("/api/cameras.update").
// Anything else with a dot is a file name.
func dottedSegment(segments []string, i int) bool {
	segment := segments[i]
	if numberedSegment.MatchString(segment) {
		return true
	}
	if i != len(segments)-1 || !actionSegment.MatchString(segment) || fileExtension(segment) {
		return false
	}
	for _, before := range segments[:i] {
		if _, api := apiSegments[strings.ToLower(before)]; api || versionSegment.MatchString(before) || numberedSegment.MatchString(before) {
			return true
		}
	}
	return false
}

func firstSegment(path string) string {
	segment, _, _ := strings.Cut(strings.Trim(path, "/"), "/")
	return strings.ToLower(segment)
}

// pathKey makes "/orders/new" and "orders/new/" the same item.
func pathKey(path string) string {
	return strings.ToLower(strings.TrimPrefix(cleanEvidencePath(path), "/"))
}

func (e *evidence) addPath(path string, kind pathKind) bool {
	return e.addPathWords(path, kind, nil)
}

// addPathWords is addPath with more words for the item, such as the key of an
// endpoint table entry.
func (e *evidence) addPathWords(path string, kind pathKind, extra []string) bool {
	if kind != pathAPI && kind != pathScreen {
		return false
	}
	if strings.Contains(path, "://") {
		parsed, err := url.Parse(path)
		if err != nil || parsed.Path == "" {
			return false
		}
		path = parsed.Path
	}
	path = cleanEvidencePath(path)
	if path == "" || e.claimed == nil {
		return false
	}
	key := pathKey(path)
	if _, ok := e.claimed[key]; ok {
		return false
	}
	item := evidenceItem{key: key, screen: kind == pathScreen, label: path}
	words := append([]string(nil), extra...)
	if service, method, ok := analyzer.RPCPath(path); ok {
		// The package says where a service lives, not what it is about.
		rpc := rpcKey(service, method)
		if _, ok := e.claimed[rpc]; ok {
			return false
		}
		e.claimed[rpc] = struct{}{}
		item.key = rpc
		item.group = service
		item.screen = false
		words = append(words, append(nameWords(service), nameWords(method)...)...)
	} else if !evidencePath(path) {
		return false
	} else {
		for _, segment := range strings.Split(strings.Trim(path, "/"), "/") {
			if !paramSegment.MatchString(segment) {
				// A placeholder inside a segment, as in "bytes{id}", is a
				// parameter, not a word.
				words = append(words, nameWords(placeholder.ReplaceAllString(segment, " "))...)
			}
		}
	}
	item.nouns, item.verbs = splitWords(words)
	e.claimed[key] = struct{}{}
	e.items = append(e.items, item)
	return true
}

// addOperation records a GraphQL operation given as "query Name".
func (e *evidence) addOperation(operation string) {
	_, name, ok := strings.Cut(operation, " ")
	if !ok {
		return
	}
	key := "graphql " + strings.ToLower(name)
	if _, ok := e.claimed[key]; ok {
		return
	}
	e.claimed[key] = struct{}{}
	item := evidenceItem{key: key, label: name, group: "GraphQL"}
	item.nouns, item.verbs = splitWords(nameWords(name))
	e.items = append(e.items, item)
}

// splitWords separates the domain nouns from the action verbs in a word list.
func splitWords(words []string) (nouns, verbs []string) {
	for _, word := range words {
		switch {
		case actionVerbs[word] != "":
			verbs = appendUnique(verbs, actionVerbs[word])
		case domainNoun(word):
			nouns = appendUnique(nouns, word)
		}
	}
	return nouns, verbs
}

// claims reports whether a path string is already counted as API or route
// evidence, so it is not read a second time as a name.
func (e *evidence) claims(value string) bool {
	if e == nil {
		return false
	}
	_, ok := e.claimed[pathKey(value)]
	return ok
}

// tableKey reports whether a token is a property name of an endpoint table.
func (e *evidence) tableKey(index int) bool {
	if e == nil {
		return false
	}
	_, ok := e.tableKeys[index]
	return ok
}

func (e *evidence) totals() *Evidence {
	if e == nil || len(e.items) == 0 {
		return nil
	}
	total := &Evidence{}
	for _, item := range e.items {
		if item.screen {
			total.Screens++
		} else {
			total.APICalls++
		}
	}
	return total
}

// support returns the items whose paths name every word of an area.
func (e *evidence) support(area string) []evidenceItem {
	if e == nil {
		return nil
	}
	words := strings.Fields(area)
	var result []evidenceItem
	for _, index := range e.byNoun[words[0]] {
		item := e.items[index]
		if len(words) == 1 || containsAll(item.nouns, words[1:]) {
			result = append(result, item)
		}
	}
	return result
}

func containsAll(have, want []string) bool {
	for _, word := range want {
		found := false
		for _, candidate := range have {
			if candidate == word {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

// attach adds the counts, examples, and actions of the items that support an
// area to its feature.
func (f *Feature) attach(items []evidenceItem, actions map[string]int) {
	var api, screens []evidenceItem
	for _, item := range items {
		if item.screen {
			screens = append(screens, item)
		} else {
			api = append(api, item)
		}
		for _, verb := range item.verbs {
			actions[verb]++
		}
	}
	f.APICalls, f.Screens = len(api), len(screens)
	f.APIExamples = apiExamples(api, f.Area)
	f.ScreenExamples = screenExamples(screens)
}

// apiExamples names the services calls belong to, most calls first, then
// plain paths, up to the example limit. The paths that spell the area come
// first, then those that also name an action, shortest first among equals: they
// say best what the area does.
func apiExamples(items []evidenceItem, area string) []string {
	groups := map[string]int{}
	var plain []evidenceItem
	for _, item := range items {
		if item.group != "" {
			groups[item.group]++
		} else {
			plain = append(plain, item)
		}
	}
	var names []string
	for group := range groups {
		names = append(names, group)
	}
	sort.Slice(names, func(i, j int) bool {
		if groups[names[i]] != groups[names[j]] {
			return groups[names[i]] > groups[names[j]]
		}
		return names[i] < names[j]
	})
	spells := func(item evidenceItem) bool {
		label := strings.ToLower(item.label)
		for _, word := range strings.Fields(area) {
			if !strings.Contains(label, word) {
				return false
			}
		}
		return true
	}
	sort.Slice(plain, func(i, j int) bool {
		if named := spells(plain[i]); named != spells(plain[j]) {
			return named
		}
		if acts := len(plain[i].verbs) > 0; acts != (len(plain[j].verbs) > 0) {
			return acts
		}
		if len(plain[i].label) != len(plain[j].label) {
			return len(plain[i].label) < len(plain[j].label)
		}
		return plain[i].label < plain[j].label
	})
	for _, item := range plain {
		names = append(names, item.label)
	}
	return firstN(names, maxFeatureExamples)
}

// screenExamples lists the shortest paths first: the section roots read best.
func screenExamples(items []evidenceItem) []string {
	return firstN(sortedScreenPaths(items), maxFeatureExamples)
}

// sortedScreenPaths orders screen paths shortest first.
func sortedScreenPaths(items []evidenceItem) []string {
	var paths []string
	for _, item := range items {
		paths = append(paths, item.label)
	}
	sort.Slice(paths, func(i, j int) bool {
		if len(paths[i]) != len(paths[j]) {
			return len(paths[i]) < len(paths[j])
		}
		return paths[i] < paths[j]
	})
	return paths
}

func firstN(items []string, n int) []string {
	if len(items) > n {
		return items[:n]
	}
	return items
}

// evidenceLine renders "10 API calls (Service) · 8 screens (/a, /b)".
func (f Feature) evidenceLine() string {
	var parts []string
	if f.APICalls > 0 {
		parts = append(parts, countPhrase(f.APICalls, "API call", f.APIExamples))
	}
	if f.Screens > 0 {
		parts = append(parts, countPhrase(f.Screens, "screen", f.ScreenExamples))
	}
	return strings.Join(parts, " · ")
}

func countPhrase(count int, noun string, examples []string) string {
	phrase := strconv.Itoa(count) + " " + plural(count, noun)
	if len(examples) > 0 {
		phrase += " (" + strings.Join(examples, ", ") + ")"
	}
	return phrase
}
