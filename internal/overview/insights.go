package overview

import (
	"regexp"
	"sort"
	"strings"
)

// Insights contains useful interfaces that can be tied to source locations.
// API entries are internal evidence for component and architecture summaries.
type Insights struct {
	Summary    string             `json:"summary,omitempty"`
	Components []InsightComponent `json:"components,omitempty"`
	Routes     []InsightRoute     `json:"ui_routes,omitempty"`
	API        []InsightAPI       `json:"-"`
	Behaviors  []InsightBehavior  `json:"browser_behavior,omitempty"`
	Notes      []string           `json:"notes,omitempty"`
}

type InsightComponent struct {
	Name     string `json:"name"`
	Role     string `json:"role"`
	Module   string `json:"module,omitempty"`
	Runtime  string `json:"runtime,omitempty"`
	Line     int    `json:"line"`
	Column   int    `json:"column,omitempty"`
	Evidence string `json:"evidence"`
}

type InsightRoute struct {
	Path     string `json:"path"`
	Kind     string `json:"kind"` // declared route or navigation target
	Module   string `json:"module,omitempty"`
	Runtime  string `json:"runtime,omitempty"`
	Line     int    `json:"line"`
	Column   int    `json:"column,omitempty"`
	Evidence string `json:"evidence"`
	// listed marks a path named by a route property or a list of paths, not by
	// a router or a link: it says nothing about the paths beside it.
	listed bool
}

type InsightAPI struct {
	Name       string   `json:"name"`
	Kind       string   `json:"kind,omitempty"` // request wrapper or direct call expression
	Method     string   `json:"method"`
	Path       string   `json:"path"`
	Parameters []string `json:"parameters,omitempty"`
	Transport  string   `json:"transport,omitempty"`
	Module     string   `json:"module,omitempty"`
	Runtime    string   `json:"runtime,omitempty"`
	Line       int      `json:"line"`
	Column     int      `json:"column,omitempty"`
	client     bool     // read from a client method call such as axios.get()
	token      int      // index of the call or function name, to tell bundled packages apart
}

type InsightBehavior struct {
	Action   string `json:"action"`
	Module   string `json:"module,omitempty"`
	Runtime  string `json:"runtime,omitempty"`
	Line     int    `json:"line"`
	Column   int    `json:"column,omitempty"`
	Evidence string `json:"evidence"`
}

type insightScope struct {
	start, end int
	module     string
	global     string
	loader     string
	aliases    map[string]string
	constants  map[string]string
	prefixes   map[string]string
	tokens     []token
	modules    map[string]map[string]*insightModuleData // runtime -> module -> exported object
	members    map[string]map[string]string
	exports    map[string]string
	enumOwner  map[string]string
}

type insightModuleData struct {
	members   map[string]map[string]string
	exports   map[string]string
	enumOwner map[string]string
}

type insightSpan struct{ start, end int }

type insightFunction struct {
	name       string
	nameAt     int
	params     int // index of the "(" of the parameter list
	start, end int
}

func analyzeInsights(tokens []token, chunks []*chunkInfo) *Insights {
	if len(tokens) == 0 {
		return nil
	}
	scopes := insightScopes(tokens, chunks)
	for i := range scopes {
		scopes[i].constants, scopes[i].prefixes, scopes[i].members, scopes[i].exports, scopes[i].enumOwner = insightConstants(&scopes[i])
		if scopes[i].module != "" {
			if scopes[i].modules[scopes[i].global] == nil {
				scopes[i].modules[scopes[i].global] = make(map[string]*insightModuleData)
			}
			if _, duplicate := scopes[i].modules[scopes[i].global][scopes[i].module]; duplicate {
				// Repeated IDs in the same loader registry have ambiguous local
				// ownership without evaluating registration order.
				scopes[i].modules[scopes[i].global][scopes[i].module] = nil
			} else {
				scopes[i].modules[scopes[i].global][scopes[i].module] = &insightModuleData{
					members: scopes[i].members, exports: scopes[i].exports, enumOwner: scopes[i].enumOwner,
				}
			}
		}
	}
	result := &Insights{}
	for i := range scopes {
		s := &scopes[i]
		s.aliases = insightAliases(s)
		result.Routes = append(result.Routes, insightRoutes(s)...)
		result.API = append(result.API, insightAPIs(s)...)
		result.API = append(result.API, insightFetchAPIs(s)...)
		result.Behaviors = append(result.Behaviors, insightBehaviors(s)...)
	}
	result.Routes = uniqueInsightRoutes(result.Routes)
	result.API = uniqueInsightAPIs(result.API)
	result.Behaviors = uniqueInsightBehaviors(result.Behaviors)
	result.Components = insightComponents(scopes, result)
	result.Summary = insightSummary(result.Components, result.Routes, result.API)
	if len(result.Components) == 0 && len(result.Routes) == 0 && len(result.API) == 0 && len(result.Behaviors) == 0 {
		return nil
	}
	return result
}

func insightScopes(tokens []token, chunks []*chunkInfo) []insightScope {
	modules := make(map[string]map[string]*insightModuleData)
	var scopes []insightScope
	var covered []insightSpan
	for _, chunk := range chunks {
		if chunk == nil {
			continue
		}
		for _, factory := range chunk.factories {
			if factory.start < 0 || factory.end > len(tokens) || factory.start >= factory.end {
				continue
			}
			scopes = append(scopes, insightScope{start: factory.start, end: factory.end,
				module: factory.id, global: chunk.global, loader: factory.loader, tokens: tokens, modules: modules})
			covered = append(covered, insightSpan{factory.start, factory.end})
		}
	}
	if len(scopes) == 0 {
		scopes = append(scopes, insightScope{start: 0, end: len(tokens), tokens: tokens, modules: modules})
	} else {
		// Runtime callbacks and standalone code can coexist with factory
		// registrations. Analyze the uncovered spans without assigning them
		// to an unrelated module.
		sort.Slice(covered, func(i, j int) bool { return covered[i].start < covered[j].start })
		cursor := 0
		for _, span := range covered {
			if cursor < span.start {
				scopes = append(scopes, insightScope{start: cursor, end: span.start, tokens: tokens, modules: modules})
			}
			cursor = max(cursor, span.end)
		}
		if cursor < len(tokens) {
			scopes = append(scopes, insightScope{start: cursor, end: len(tokens), tokens: tokens, modules: modules})
		}
	}
	return scopes
}

func insightConstants(s *insightScope) (map[string]string, map[string]string, map[string]map[string]string, map[string]string, map[string]string) {
	t := s.tokens
	constants := make(map[string]string)
	prefixes := make(map[string]string)
	members := make(map[string]map[string]string)
	exports := make(map[string]string)
	enumOwner := make(map[string]string)
	enumBodies := make(map[string][]insightSpan)
	moduleDepth := t[s.start].depth
	for i := s.start; i+3 < s.end; i++ {
		if at(t, i+1) == "." && t[i+2].kind == 'i' && at(t, i+3) == "=" {
			name := t[i+2].text
			if t[i].kind == 'i' && i+4 < s.end && t[i+4].kind == 's' && insightInEnumBody(enumBodies[t[i].text], i) {
				value := stringValue(t[i+4].text)
				if value != "" {
					if members[t[i].text] == nil {
						members[t[i].text] = make(map[string]string)
					}
					insightSetUnique(members[t[i].text], name, value)
				}
			}
			if at(t, i) == "this" && insightPrefixName(name) {
				for j := i + 4; j < min(i+22, s.end); j++ {
					if t[j].kind == 's' && isRoutePath(stringValue(t[j].text)) {
						insightSetUnique(prefixes, name, stringValue(t[j].text))
						break
					}
					if at(t, j) == ";" || at(t, j) == "}" {
						break
					}
				}
			}
		}
		if t[i].depth == moduleDepth && t[i].kind == 'i' && at(t, i+1) == "=" && t[i+2].kind == 's' &&
			(at(t, i-1) == "const" || at(t, i-1) == "let" || at(t, i-1) == "var" || at(t, i-1) == ",") {
			insightSetUnique(constants, t[i].text, stringValue(t[i+2].text))
		} else if t[i].depth == moduleDepth && t[i].kind == 'i' && at(t, i+1) == "=" &&
			at(t, i+2) != "=" && at(t, i-1) != "." && at(t, i-1) != "=" {
			if _, exists := constants[t[i].text]; exists {
				constants[t[i].text] = ""
			}
		}
		// Webpack's export table maps public properties to local symbols.
		if i+5 < s.end && t[i].kind == 'i' && at(t, i+1) == ":" && at(t, i+2) == "(" &&
			t[i+2].match == i+3 && at(t, i+4) == "=>" && t[i+5].kind == 'i' {
			insightSetUnique(exports, t[i].text, t[i+5].text)
		}
		// A compiled enum often uses local=function(receiver){ return
		// receiver.KEY="/route", ... }({}). Keep that receiver attached to
		// the exported local rather than treating KEY as a module-wide name.
		if t[i].kind == 'i' && at(t, i+1) == "=" && at(t, i+2) == "function" &&
			at(t, i+3) == "(" && t[i+4].kind == 'i' && at(t, i+5) == ")" &&
			at(t, i+6) == "{" && t[i+6].match > i+6 {
			insightSetUnique(enumOwner, t[i].text, t[i+4].text)
			enumBodies[t[i+4].text] = append(enumBodies[t[i+4].text], insightSpan{i + 7, t[i+6].match})
		}
		if t[i].depth == moduleDepth && t[i].kind == 'i' && at(t, i+1) == "=" && at(t, i+2) == "{" && t[i+2].match > i+2 {
			for key, span := range insightObjectProperties(t, i+2) {
				if span.end == span.start+1 && t[span.start].kind == 's' {
					if members[t[i].text] == nil {
						members[t[i].text] = make(map[string]string)
					}
					insightSetUnique(members[t[i].text], key, stringValue(t[span.start].text))
				}
			}
		}
	}
	return constants, prefixes, members, exports, enumOwner
}

func insightInEnumBody(bodies []insightSpan, index int) bool {
	for _, body := range bodies {
		if body.start <= index && index < body.end {
			return true
		}
	}
	return false
}

func insightSetUnique(values map[string]string, key, value string) {
	if old, ok := values[key]; ok && old != value {
		values[key] = ""
	} else if !ok {
		values[key] = value
	}
}

func insightPrefixName(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, "prefix") || strings.Contains(lower, "baseurl") || strings.Contains(lower, "basepath")
}

func insightAliases(s *insightScope) map[string]string {
	aliases := make(map[string]string)
	invalid := make(map[string]bool)
	if s.loader == "" {
		return aliases
	}
	t := s.tokens
	moduleDepth := t[s.start].depth
	for i := s.start; i+2 < s.end; i++ {
		if t[i].depth != moduleDepth || t[i].kind != 'i' || at(t, i+1) != "=" || at(t, i-1) == "." ||
			at(t, i-1) == "=" || at(t, i+2) == "=" {
			continue
		}
		name := t[i].text
		if invalid[name] {
			continue
		}
		if at(t, i+2) == s.loader && at(t, i+3) == "(" && t[i+3].match == i+5 && t[i+4].kind == 'n' {
			if old, exists := aliases[name]; exists && old != t[i+4].text {
				delete(aliases, name)
				invalid[name] = true
			} else {
				aliases[name] = t[i+4].text
			}
		} else {
			if _, exists := aliases[name]; exists {
				delete(aliases, name)
				invalid[name] = true
			}
		}
	}
	return aliases
}

func insightBindings(t []token, start, end int) map[string]insightSpan {
	bindings := make(map[string]insightSpan)
	for i := start + 1; i+2 < end; i++ {
		if t[i].kind != 'i' || at(t, i+1) != "=" || at(t, i+2) == "=" ||
			at(t, i-1) == "." || at(t, i-1) == "=" {
			continue
		}
		declaration := at(t, i-1) == "const" || at(t, i-1) == "let" || at(t, i-1) == "var" || at(t, i-1) == ","
		if !declaration {
			if _, exists := bindings[t[i].text]; exists {
				bindings[t[i].text] = insightSpan{}
			}
			continue
		}
		valueStart := i + 2
		valueEnd := insightExpressionEnd(t, valueStart, end)
		if valueEnd > valueStart {
			bindings[t[i].text] = insightSpan{valueStart, valueEnd}
		}
		i = valueEnd
	}
	return bindings
}

func insightExpressionEnd(t []token, start, limit int) int {
	for i := start; i < limit; i++ {
		if at(t, i) == "," || at(t, i) == ";" {
			return i
		}
		if t[i].match > i {
			i = t[i].match
		}
	}
	return limit
}

func insightResolve(s *insightScope, span insightSpan, bindings map[string]insightSpan, depth int) string {
	if depth > 6 || span.start < s.start || span.end > s.end || span.start >= span.end {
		return ""
	}
	t := s.tokens
	for span.start < span.end && (at(t, span.start) == "(" || at(t, span.start) == "!") {
		if at(t, span.start) == "!" {
			span.start++
			continue
		}
		if t[span.start].match == span.end-1 {
			span.start++
			span.end--
			continue
		}
		break
	}
	if span.start >= span.end {
		return ""
	}
	if span.end == span.start+1 {
		switch t[span.start].kind {
		case 's':
			return stringValue(t[span.start].text)
		case 't':
			return insightTemplate(s, t[span.start].text, bindings, depth+1)
		case 'i':
			name := t[span.start].text
			if binding, ok := bindings[name]; ok && binding.start != span.start {
				return insightResolve(s, binding, bindings, depth+1)
			}
			return s.constants[name]
		}
	}
	if at(t, span.start) == "this" && at(t, span.start+1) == "." && span.end == span.start+3 {
		return s.prefixes[at(t, span.start+2)]
	}
	if t[span.start].kind == 'i' && at(t, span.start+1) == "." && span.end >= span.start+3 {
		if module, ok := s.aliases[t[span.start].text]; ok {
			return insightImportedMember(s, module, span)
		}
	}
	// A generated client often assigns a URL returned by genBaseURL("/path").
	for i := span.start; i+1 < span.end; i++ {
		if (at(t, i) == "genBaseURL" || at(t, i) == "getBaseURL") && at(t, i+1) == "(" && t[i+1].match < span.end {
			args := argumentRanges(t, i+1)
			if len(args) > 0 {
				return insightResolve(s, insightSpan{args[0][0], args[0][1]}, bindings, depth+1)
			}
		}
	}
	// Resolve simple string concatenation without evaluating arbitrary JS.
	var parts []insightSpan
	partStart := span.start
	for i := span.start; i < span.end; i++ {
		if at(t, i) == "+" {
			parts = append(parts, insightSpan{partStart, i})
			partStart = i + 1
		} else if t[i].match > i {
			i = t[i].match
		}
	}
	if len(parts) > 0 {
		parts = append(parts, insightSpan{partStart, span.end})
		var value strings.Builder
		for _, part := range parts {
			piece := insightResolve(s, part, bindings, depth+1)
			if piece == "" {
				return ""
			}
			value.WriteString(piece)
		}
		return value.String()
	}
	return ""
}

// insightPiece is one part of a URL built by joining strings: its text when the
// file shows it, or unknown when it is a variable, a parameter, or a call.
type insightPiece struct {
	text  string
	known bool
}

// insightRequestPath resolves the URL argument of a request. A URL that is
// written out resolves as insightResolve does, and "".concat(a, b) is read like
// a + b. A URL that starts with a part the file does not show and then continues
// with a path literal, as in base + "/orders/list" or `${base}/orders/list`, is
// that literal path: the base is the server, and the request context says the
// path is a call. A part after the literal that is unknown becomes {dynamic}
// when it starts a path segment, and ends the path otherwise.
func insightRequestPath(s *insightScope, span insightSpan, bindings map[string]insightSpan) string {
	if path := insightResolve(s, span, bindings, 0); path != "" {
		return path
	}
	pieces := insightPieces(s, span, bindings, 0)
	if len(pieces) < 2 {
		return ""
	}
	var path strings.Builder
	for _, piece := range pieces {
		switch {
		case path.Len() == 0 && !piece.known:
		case path.Len() == 0 && !strings.HasPrefix(piece.text, "/"):
			if piece.text != "" {
				return "" // a known start that is not a path, such as a full URL
			}
		case piece.known && piece.text != "" && strings.HasSuffix(path.String(), "{dynamic}") && !strings.HasPrefix(piece.text, "/"):
			return insightTail(path.String()) // "&_=" after an id is no segment
		case piece.known:
			path.WriteString(piece.text)
		case strings.HasSuffix(path.String(), "/"):
			path.WriteString("{dynamic}")
		default:
			return insightTail(path.String())
		}
	}
	return insightTail(path.String())
}

// insightTail accepts a path that was joined from parts: it needs at least one
// segment and must not be a file such as a script or an image.
func insightTail(path string) string {
	if len(path) < 2 || path[0] != '/' || strings.HasPrefix(path, "/{dynamic}") {
		return ""
	}
	last := path[strings.LastIndexByte(path, '/')+1:]
	if fileExtension(last) {
		return ""
	}
	return path
}

// insightPieces splits a URL expression into the parts that are joined: the
// operands of +, the arguments of concat, and the parts of a template literal.
func insightPieces(s *insightScope, span insightSpan, bindings map[string]insightSpan, depth int) []insightPiece {
	t := s.tokens
	if depth > 6 || span.start < s.start || span.end > s.end || span.start >= span.end {
		return nil
	}
	for span.start < span.end && at(t, span.start) == "(" && t[span.start].match == span.end-1 {
		span.start++
		span.end--
	}
	if span.start >= span.end {
		return nil
	}
	var pieces []insightPiece
	partStart := span.start
	for i := span.start; i < span.end; i++ {
		if at(t, i) == "+" {
			pieces = append(pieces, insightPieces(s, insightSpan{partStart, i}, bindings, depth+1)...)
			partStart = i + 1
		} else if t[i].match > i {
			i = t[i].match
		}
	}
	if partStart > span.start {
		return append(pieces, insightPieces(s, insightSpan{partStart, span.end}, bindings, depth+1)...)
	}
	switch {
	case span.end == span.start+1 && t[span.start].kind == 't':
		return insightTemplatePieces(s, t[span.start].text, bindings, depth)
	case span.end == span.start+1 && t[span.start].kind == 'i':
		if binding, ok := bindings[t[span.start].text]; ok && binding.start != span.start {
			return insightPieces(s, binding, bindings, depth+1)
		}
	case t[span.start].kind == 's' && at(t, span.start+1) == "." && at(t, span.start+2) == "concat":
		// "".concat(a, "/b").concat(c): the receiver, then each argument.
		pieces = []insightPiece{{text: stringValue(t[span.start].text), known: true}}
		for i := span.start + 1; i+2 < span.end && at(t, i) == "." && at(t, i+1) == "concat" && at(t, i+2) == "(" && t[i+2].match > i+2; i = t[i+2].match + 1 {
			for _, arg := range argumentRanges(t, i+2) {
				pieces = append(pieces, insightPieces(s, insightSpan{arg[0], arg[1]}, bindings, depth+1)...)
			}
			if t[i+2].match+1 == span.end {
				return pieces
			}
		}
		return nil
	}
	text := insightResolve(s, span, bindings, depth+1)
	known := text != "" || span.end == span.start+1 && t[span.start].kind == 's'
	return []insightPiece{{text: text, known: known}}
}

// templateExpressionEnd is the index of the "}" that closes a ${ expression,
// counting the braces of templates nested in it, or -1.
func templateExpressionEnd(rest string) int {
	depth := 1
	for i := 0; i < len(rest); i++ {
		switch rest[i] {
		case '{':
			depth++
		case '}':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

// insightTemplatePieces splits a template literal at its ${} references.
func insightTemplatePieces(s *insightScope, raw string, bindings map[string]insightSpan, depth int) []insightPiece {
	if len(raw) < 2 || raw[0] != '`' || raw[len(raw)-1] != '`' {
		return nil
	}
	raw = raw[1 : len(raw)-1]
	var pieces []insightPiece
	for len(raw) > 0 {
		start := strings.Index(raw, "${")
		if start < 0 {
			break
		}
		if start > 0 {
			pieces = append(pieces, insightPiece{text: raw[:start], known: true})
		}
		rest := raw[start+2:]
		end := templateExpressionEnd(rest)
		if end < 0 {
			return nil
		}
		text := insightTemplateReference(s, strings.TrimSpace(rest[:end]), bindings, depth+1)
		pieces = append(pieces, insightPiece{text: text, known: text != ""})
		raw = rest[end+1:]
	}
	if raw != "" {
		pieces = append(pieces, insightPiece{text: raw, known: true})
	}
	return pieces
}

func insightTemplate(s *insightScope, raw string, bindings map[string]insightSpan, depth int) string {
	if len(raw) < 2 || raw[0] != '`' || raw[len(raw)-1] != '`' {
		return ""
	}
	raw = raw[1 : len(raw)-1]
	var value strings.Builder
	for len(raw) > 0 {
		start := strings.Index(raw, "${")
		if start < 0 {
			value.WriteString(raw)
			break
		}
		value.WriteString(raw[:start])
		rest := raw[start+2:]
		end := strings.IndexByte(rest, '}')
		if end < 0 {
			return ""
		}
		expression := strings.TrimSpace(rest[:end])
		piece := insightTemplateReference(s, expression, bindings, depth+1)
		if piece == "" {
			// A trailing interpolation often appends query parameters. Keep
			// only a stable path prefix and never invent an unknown segment.
			if end+1 == len(rest) && strings.HasPrefix(value.String(), "/") {
				return value.String() + "{dynamic}"
			}
			return ""
		}
		value.WriteString(piece)
		raw = rest[end+1:]
	}
	return value.String()
}

func insightTemplateReference(s *insightScope, expression string, bindings map[string]insightSpan, depth int) string {
	if depth > 6 || expression == "" {
		return ""
	}
	parts := strings.Split(expression, ".")
	for _, part := range parts {
		if !insightIdentifier(part) {
			return ""
		}
	}
	if len(parts) == 2 && parts[0] == "this" {
		return s.prefixes[parts[1]]
	}
	if len(parts) > 1 {
		if module, ok := s.aliases[parts[0]]; ok {
			return insightImportedPath(s, module, parts[1:])
		}
		return ""
	}
	if binding, ok := bindings[expression]; ok {
		return insightResolve(s, binding, bindings, depth+1)
	}
	return s.constants[expression]
}

func insightImportedMember(s *insightScope, module string, span insightSpan) string {
	parts := make([]string, 0, 3)
	for i := span.start + 2; i < span.end; i += 2 {
		if i > span.start+2 && at(s.tokens, i-1) != "." {
			return ""
		}
		parts = append(parts, at(s.tokens, i))
	}
	return insightImportedPath(s, module, parts)
}

func insightImportedPath(s *insightScope, module string, parts []string) string {
	data := s.modules[s.global][module]
	if data == nil || len(parts) != 2 {
		return ""
	}
	local := data.exports[parts[0]]
	if local == "" {
		return ""
	}
	owner := data.enumOwner[local]
	if owner == "" {
		owner = local
	}
	return data.members[owner][parts[1]]
}

func insightIdentifier(value string) bool {
	if value == "" || !identifierStart(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		if !identifierPart(value[i]) {
			return false
		}
	}
	return true
}

func insightObjectProperties(t []token, open int) map[string]insightSpan {
	props := make(map[string]insightSpan)
	if at(t, open) != "{" || t[open].match <= open {
		return props
	}
	end := t[open].match
	for i := open + 1; i+1 < end; i++ {
		if (t[i].kind != 'i' && t[i].kind != 's') || at(t, i+1) != ":" {
			if t[i].match > i {
				i = t[i].match
			}
			continue
		}
		name := t[i].text
		if t[i].kind == 's' {
			name = stringValue(name)
		}
		start := i + 2
		valueEnd := insightExpressionEnd(t, start, end)
		props[name] = insightSpan{start, valueEnd}
		i = valueEnd
	}
	return props
}

func insightEnclosing(t []token, atIndex, floor int, opener string) int {
	for i := atIndex - 1; i >= max(floor, atIndex-160); i-- {
		if at(t, i) == opener && t[i].match > atIndex {
			return i
		}
	}
	return -1
}

func insightRoutes(s *insightScope) []InsightRoute {
	t := s.tokens
	var routes []InsightRoute
	lists := &insightRouteLists{}
	for i := s.start; i+2 < s.end; i++ {
		lists.visit(s, i)
		if (at(t, i) == "route" || at(t, i) == "routerLink") && at(t, i+1) == ":" && (at(t, i-1) == "{" || at(t, i-1) == ",") {
			// A menu or router entry names its screen: route: "/orders/list".
			if open := insightEnclosing(t, i, s.start, "{"); open >= 0 {
				for _, path := range insightPropertyPaths(s, insightSpan{i + 2, insightExpressionEnd(t, i+2, t[open].match)}, 0) {
					routes = append(routes, InsightRoute{Path: path, Kind: "navigation target", Module: s.module, Runtime: s.global,
						Line: t[i].line, Column: t[i].column, Evidence: t[i].text + " property", listed: true})
				}
			}
			continue
		}
		if t[i].kind == 's' && len(t[i].text) == len("'routerLink'") && stringValue(t[i].text) == "routerLink" && at(t, i+1) == "," {
			// Compiled Angular templates bind it as property("routerLink", value).
			if open := insightEnclosing(t, i, s.start, "("); open >= 0 {
				for _, path := range insightPropertyPaths(s, insightSpan{i + 2, insightExpressionEnd(t, i+2, t[open].match)}, 0) {
					routes = append(routes, InsightRoute{Path: path, Kind: "navigation target", Module: s.module, Runtime: s.global,
						Line: t[i].line, Column: t[i].column, Evidence: "routerLink binding", listed: true})
				}
			}
			continue
		}
		if at(t, i) == "path" && at(t, i+1) == ":" {
			open := insightEnclosing(t, i, s.start, "{")
			if open < 0 {
				continue
			}
			props := insightObjectProperties(t, open)
			_, component := props["Component"]
			_, element := props["element"]
			inRouter := insightWithinRouterCall(t, open, s.start)
			if !inRouter && !(component || element) {
				continue
			}
			if !inRouter && !insightJSXProps(t, open, s.start) {
				continue
			}
			span := props["path"]
			path := insightResolve(s, span, nil, 0)
			if !insightDeclaredPath(path) {
				continue
			}
			routes = append(routes, InsightRoute{Path: path, Kind: "declared route", Module: s.module, Runtime: s.global,
				Line: t[i].line, Column: t[i].column, Evidence: "path property in a route component/configuration"})
			continue
		}
		if (at(t, i) == "href" || at(t, i) == "to") && at(t, i+1) == ":" {
			open := insightEnclosing(t, i, s.start, "{")
			if open < 0 || !insightJSXProps(t, open, s.start) {
				continue
			}
			props := insightObjectProperties(t, open)
			span, ok := props[t[i].text]
			if !ok {
				continue
			}
			for _, path := range insightPathsIn(s, span) {
				routes = append(routes, InsightRoute{Path: path, Kind: "navigation target", Module: s.module, Runtime: s.global,
					Line: t[i].line, Column: t[i].column, Evidence: t[i].text + " prop"})
			}
		}
		if at(t, i) == "open" && at(t, i-1) == "." && at(t, i-2) == "window" && at(t, i+1) == "(" {
			args := argumentRanges(t, i+1)
			if len(args) == 0 {
				continue
			}
			for _, path := range insightPathsIn(s, insightSpan{args[0][0], args[0][1]}) {
				routes = append(routes, InsightRoute{Path: path, Kind: "navigation target", Module: s.module, Runtime: s.global,
					Line: t[i].line, Column: t[i].column, Evidence: "window.open() target"})
			}
		}
		if at(t, i) == "navigate" && at(t, i+1) == "(" {
			args := argumentRanges(t, i+1)
			if len(args) == 0 {
				continue
			}
			for _, path := range insightPathsIn(s, insightSpan{args[0][0], args[0][1]}) {
				routes = append(routes, InsightRoute{Path: path, Kind: "navigation target", Module: s.module, Runtime: s.global,
					Line: t[i].line, Column: t[i].column, Evidence: "navigate() target"})
			}
		}
	}
	return append(routes, lists.routes(s)...)
}

// insightPropertyPaths reads the UI paths a route or routerLink property holds:
// a path, or an object whose values are paths, as in a menu entry with a path per
// role. A path under an API segment is a request, not a screen.
func insightPropertyPaths(s *insightScope, span insightSpan, depth int) []string {
	t := s.tokens
	if depth > 2 || span.start >= span.end {
		return nil
	}
	if at(t, span.start) == "{" && t[span.start].match == span.end-1 {
		var paths []string
		props := insightObjectProperties(t, span.start)
		for _, name := range sortedSpanKeys(props) {
			paths = append(paths, insightPropertyPaths(s, props[name], depth+1)...)
		}
		return paths
	}
	path := insightResolve(s, span, nil, 0)
	if !insightUIPath(path) || classifyPath(path) == pathAPI {
		return nil
	}
	return []string{path}
}

func sortedSpanKeys(props map[string]insightSpan) []string {
	keys := make([]string, 0, len(props))
	for key := range props {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// insightRouteLists finds arrays of path strings that the code compares with the
// current location, as in ["/login", "/signup"].includes(router.url) or
// PUBLIC.includes(location.pathname): the list names screens. An array that is
// only declared is not evidence, so the use decides.
type insightRouteLists struct {
	arrays map[string]insightPathList
	used   map[string]struct{}
	direct []insightPathList
}

type insightPathList struct {
	paths []string
	at    int
}

// visit looks at token i: an array of path strings, or a method call that
// compares something with the location.
func (l *insightRouteLists) visit(s *insightScope, i int) {
	t := s.tokens
	if at(t, i) == "[" && t[i].match > i+2 && t[i+1].kind == 's' && strings.HasPrefix(stringValue(t[i+1].text), "/") {
		list, ok := insightPathArray(s, i)
		if !ok {
			return
		}
		after := t[i].match + 1
		if at(t, after) == "." && insightLocationMethod(at(t, after+1)) && at(t, after+2) == "(" && insightUsesLocation(t, after+2) {
			l.direct = append(l.direct, list)
		} else if (at(t, i-1) == "=" || at(t, i-1) == ":") && i >= 2 && t[i-2].kind == 'i' {
			if l.arrays == nil {
				l.arrays = make(map[string]insightPathList)
			}
			l.arrays[t[i-2].text] = list
		}
		return
	}
	if t[i].kind == 'i' && at(t, i+1) == "." && insightLocationMethod(at(t, i+2)) && at(t, i+3) == "(" && insightUsesLocation(t, i+3) {
		if l.used == nil {
			l.used = make(map[string]struct{})
		}
		l.used[t[i].text] = struct{}{}
	}
}

func (l *insightRouteLists) routes(s *insightScope) []InsightRoute {
	t := s.tokens
	lists := append([]insightPathList(nil), l.direct...)
	names := make([]string, 0, len(l.used))
	for name := range l.used {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if list, ok := l.arrays[name]; ok {
			lists = append(lists, list)
		}
	}
	var routes []InsightRoute
	for _, list := range lists {
		for _, path := range list.paths {
			routes = append(routes, InsightRoute{Path: path, Kind: "navigation target", Module: s.module, Runtime: s.global,
				Line: t[list.at].line, Column: t[list.at].column, Evidence: "path list compared with the current location", listed: true})
		}
	}
	return routes
}

// insightPathArray reads an array whose elements are all strings, and keeps the
// ones that are UI paths. It needs two, since one string is a comparison.
func insightPathArray(s *insightScope, open int) (insightPathList, bool) {
	t := s.tokens
	list := insightPathList{at: open}
	for i := open + 1; i < t[open].match; i++ {
		if t[i].kind != 's' && t[i].kind != 't' {
			return list, false
		}
		if path := insightResolve(s, insightSpan{i, i + 1}, nil, 0); insightUIPath(path) && classifyPath(path) != pathAPI {
			list.paths = append(list.paths, path)
		}
		if i+1 < t[open].match && at(t, i+1) != "," {
			return list, false
		}
		i++
	}
	return list, len(list.paths) >= 2
}

func insightLocationMethod(name string) bool {
	switch name {
	case "includes", "indexOf", "some", "find", "findIndex":
		return true
	}
	return false
}

// insightUsesLocation reports a call whose arguments read the current location.
func insightUsesLocation(t []token, open int) bool {
	for i := open + 1; i < t[open].match; i++ {
		if at(t, i) == "pathname" || at(t, i) == "location" || at(t, i) == "url" && at(t, i-1) == "." {
			return true
		}
	}
	return false
}

func insightJSXProps(t []token, open, floor int) bool {
	call := insightEnclosing(t, open, floor, "(")
	if call < 0 {
		return false
	}
	callee := insightCallee(t, call)
	if !strings.HasSuffix(callee, "jsx") && !strings.HasSuffix(callee, "jsxs") && !strings.HasSuffix(callee, "createElement") {
		return false
	}
	args := argumentRanges(t, call)
	return len(args) >= 2 && args[1][0] == open
}

func insightWithinRouterCall(t []token, open, floor int) bool {
	for i := open - 1; i >= max(floor, open-512); i-- {
		if at(t, i) != "(" || t[i].match <= open {
			continue
		}
		callee := insightCallee(t, i)
		if strings.HasSuffix(callee, "createBrowserRouter") || strings.HasSuffix(callee, "createHashRouter") || strings.HasSuffix(callee, "useRoutes") {
			return true
		}
	}
	return false
}

func insightPathsIn(s *insightScope, span insightSpan) []string {
	return insightPathsInDepth(s, span, 0)
}

func insightPathsInDepth(s *insightScope, span insightSpan, depth int) []string {
	if depth > 3 || span.start >= span.end {
		return nil
	}
	t := s.tokens
	seen := make(map[string]struct{})
	add := func(path string) {
		if insightUIPath(path) {
			seen[path] = struct{}{}
		}
	}
	add(insightResolve(s, span, nil, 0))
	if len(seen) > 0 {
		return sortedKeys(seen)
	}
	if at(t, span.start) == "(" && t[span.start].match == span.end-1 {
		return insightPathsInDepth(s, insightSpan{span.start + 1, span.end - 1}, depth+1)
	}
	if at(t, span.start) == "[" && t[span.start].match == span.end-1 {
		// Angular navigation commonly passes a one-element command array.
		for i := span.start + 1; i < span.end-1; i++ {
			if at(t, i) == "," {
				return nil
			}
			if t[i].match > i {
				i = t[i].match
			}
		}
		return insightPathsInDepth(s, insightSpan{span.start + 1, span.end - 1}, depth+1)
	}
	question, colon := -1, -1
	for i := span.start; i < span.end; i++ {
		if at(t, i) == "?" && question < 0 {
			question = i
		} else if at(t, i) == ":" && question >= 0 {
			colon = i
			break
		}
		if t[i].match > i {
			i = t[i].match
		}
	}
	if question >= 0 && colon > question {
		for _, alternative := range []insightSpan{{question + 1, colon}, {colon + 1, span.end}} {
			for _, path := range insightPathsInDepth(s, alternative, depth+1) {
				seen[path] = struct{}{}
			}
		}
	}
	return sortedKeys(seen)
}

func insightUIPath(path string) bool {
	if path == "/" || path == "/*" {
		return true
	}
	if !insightAbsolutePath(path) || strings.Contains(path, "{dynamic}") {
		return false
	}
	for _, suffix := range []string{".js", ".css", ".svg", ".png", ".jpg", ".jpeg", ".webp", ".woff", ".woff2"} {
		if strings.HasSuffix(strings.ToLower(path), suffix) {
			return false
		}
	}
	return true
}

func insightDeclaredPath(path string) bool {
	if insightUIPath(path) || path == "*" {
		return true
	}
	if path == "" || strings.HasPrefix(path, "/") || strings.Contains(path, "://") ||
		strings.ContainsAny(path, " <>\\\n\r\"';?") {
		return false
	}
	for _, suffix := range []string{".js", ".css", ".svg", ".png", ".jpg", ".jpeg", ".webp", ".woff", ".woff2"} {
		if strings.HasSuffix(strings.ToLower(path), suffix) {
			return false
		}
	}
	return true
}

func uniqueInsightRoutes(routes []InsightRoute) []InsightRoute {
	seen := make(map[string]int)
	var result []InsightRoute
	for _, route := range routes {
		key := route.Runtime + "\x00" + route.Module + "\x00" + route.Path
		if prior, ok := seen[key]; ok {
			if result[prior].Kind != "declared route" && route.Kind == "declared route" {
				result[prior] = route
			}
			continue
		}
		seen[key] = len(result)
		result = append(result, route)
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].Kind != result[j].Kind {
			return result[i].Kind == "declared route"
		}
		if result[i].Runtime != result[j].Runtime {
			return result[i].Runtime < result[j].Runtime
		}
		if result[i].Module != result[j].Module {
			return result[i].Module < result[j].Module
		}
		return result[i].Column < result[j].Column
	})
	return result
}

func insightFunctions(s *insightScope) []insightFunction {
	t := s.tokens
	var functions []insightFunction
	seen := make(map[int]struct{})
	for i := s.start; i+3 < s.end; i++ {
		nameAt, open := i, i+1
		if at(t, i) == "function" {
			if t[i+1].kind == 'i' {
				nameAt, open = i+1, i+2
			} else if i >= 3 && (at(t, i-1) == "=" || at(t, i-1) == ":") && t[i-2].kind == 'i' && at(t, i-3) != "=" {
				// A function held by a name, as in t.GetConfig=function(){...}
				// which compilers emit for each method of a class.
				nameAt, open = i-2, i+1
			} else {
				continue
			}
		} else if t[i].kind != 'i' || at(t, i-1) == "." || at(t, i-1) == "function" {
			continue
		}
		if at(t, open) != "(" || t[open].match <= open {
			continue
		}
		body := t[open].match + 1
		if at(t, body) != "{" || t[body].match <= body || t[body].match > s.end {
			continue
		}
		name := t[nameAt].text
		switch name {
		case "if", "for", "while", "switch", "catch", "with", "return", "new", "function", "do":
			continue
		}
		if _, ok := seen[body]; ok {
			continue
		}
		seen[body] = struct{}{}
		functions = append(functions, insightFunction{name: name, nameAt: nameAt, params: open, start: body + 1, end: t[body].match})
	}
	return functions
}

func insightAPIs(s *insightScope) []InsightAPI {
	t := s.tokens
	var api []InsightAPI
	functions := insightFunctions(s)
	owners := insightMethodOwners(s, functions)
	for _, fn := range functions {
		found := 0
		for i := fn.start; i+2 < fn.end && found < 32; i++ {
			if at(t, i) != "method" || at(t, i+1) != ":" {
				continue
			}
			if owners[i] != fn.nameAt {
				continue
			}
			object := insightEnclosing(t, i, fn.start-1, "{")
			if object < 0 {
				continue
			}
			props := insightObjectProperties(t, object)
			methodSpan, ok := props["method"]
			if !ok || methodSpan.start != i+2 {
				continue
			}
			bindings := insightBindings(t, fn.start, i)
			method := strings.ToUpper(insightResolve(s, methodSpan, bindings, 0))
			if !insightHTTPMethod(method) {
				continue
			}
			call := insightEnclosing(t, object, fn.start-1, "(")
			if call < 0 || t[call].match <= t[object].match {
				continue // an options object is not evidence of a request call
			}
			transport := insightCallee(t, call)
			if !insightRequestCallee(t, call, transport) {
				continue
			}
			if transport == "fetch" || transport == "window.fetch" || transport == "globalThis.fetch" || transport == "self.fetch" {
				continue // native fetch is classified by its call expression
			}
			args := argumentRanges(t, call)
			objectArg := -1
			for j, arg := range args {
				if arg[0] <= object && t[object].match < arg[1] {
					objectArg = j
					break
				}
			}
			if objectArg < 0 {
				continue
			}
			path := ""
			for _, key := range []string{"url", "uri", "path"} {
				if value, ok := props[key]; ok {
					path = insightRequestPath(s, value, bindings)
					break
				}
			}
			if path == "" && objectArg > 0 {
				path = insightRequestPath(s, insightSpan{args[0][0], args[0][1]}, bindings)
			}
			if !insightAPIPath(path) {
				continue
			}
			var params []string
			for _, key := range []string{"params", "data", "body"} {
				if value, ok := props[key]; ok {
					params = insightParameterKeys(s, value, bindings)
					if len(params) > 0 {
						break
					}
				}
			}
			kind := "direct call expression"
			if insightReturnedCall(t, call) {
				kind = "request wrapper"
			}
			api = append(api, InsightAPI{Name: fn.name, Kind: kind, Method: method, Path: path,
				Parameters: params, Transport: transport, Module: s.module, Runtime: s.global,
				Line: t[fn.nameAt].line, Column: t[fn.nameAt].column, token: fn.nameAt})
			found++
		}
	}
	return api
}

func insightMethodOwners(s *insightScope, functions []insightFunction) map[int]int {
	t := s.tokens
	sort.Slice(functions, func(i, j int) bool { return functions[i].start < functions[j].start })
	owners := make(map[int]int)
	var active []insightFunction
	functionIndex := 0
	for i := s.start; i+1 < s.end; i++ {
		for len(active) > 0 && active[len(active)-1].end <= i {
			active = active[:len(active)-1]
		}
		for functionIndex < len(functions) && functions[functionIndex].start <= i {
			if functions[functionIndex].end > i {
				active = append(active, functions[functionIndex])
			}
			functionIndex++
		}
		if at(t, i) == "method" && at(t, i+1) == ":" && len(active) > 0 {
			owners[i] = active[len(active)-1].nameAt
		}
	}
	return owners
}

func insightFetchAPIs(s *insightScope) []InsightAPI {
	t := s.tokens
	functions := insightFunctions(s)
	sort.Slice(functions, func(i, j int) bool { return functions[i].start < functions[j].start })
	shadowRanges := insightFetchParameterShadows(s, functions)
	shadowedFetch := false
	for i := s.start; i+2 < s.end; i++ {
		if at(t, i) == "function" && at(t, i+1) == "fetch" && at(t, i+2) == "(" ||
			(at(t, i) == "const" || at(t, i) == "let" || at(t, i) == "var") && at(t, i+1) == "fetch" && at(t, i+2) == "=" {
			shadowedFetch = true
			break
		}
	}
	var api []InsightAPI
	var active []insightFunction
	var activeShadows []insightSpan
	functionIndex := 0
	shadowIndex := 0
	for i := s.start; i+1 < s.end; i++ {
		for len(active) > 0 && active[len(active)-1].end <= i {
			active = active[:len(active)-1]
		}
		for functionIndex < len(functions) && functions[functionIndex].start <= i {
			if functions[functionIndex].end > i {
				active = append(active, functions[functionIndex])
			}
			functionIndex++
		}
		for len(activeShadows) > 0 && activeShadows[len(activeShadows)-1].end <= i {
			activeShadows = activeShadows[:len(activeShadows)-1]
		}
		for shadowIndex < len(shadowRanges) && shadowRanges[shadowIndex].start <= i {
			if shadowRanges[shadowIndex].end > i {
				activeShadows = append(activeShadows, shadowRanges[shadowIndex])
			}
			shadowIndex++
		}
		if at(t, i+1) != "(" || t[i+1].match <= i+1 || t[i+1].match >= s.end {
			continue
		}
		if _, client := clientVerbs[at(t, i)]; client && at(t, i-1) == "." {
			fn := insightFunction{name: at(t, i), nameAt: i, start: s.start, end: s.end}
			if len(active) > 0 {
				fn = active[len(active)-1]
			}
			if call, ok := insightClientCall(s, i, fn); ok {
				api = append(api, call)
			}
			continue
		}
		if at(t, i) != "fetch" {
			continue
		}
		if at(t, i-1) == "." && at(t, i-2) != "window" && at(t, i-2) != "globalThis" && at(t, i-2) != "self" {
			continue
		}
		if at(t, i-1) != "." && (shadowedFetch || len(activeShadows) > 0) {
			continue
		}
		fn := insightFunction{name: "fetch", nameAt: i, start: s.start, end: s.end}
		if len(active) > 0 {
			fn = active[len(active)-1]
		}
		args := argumentRanges(t, i+1)
		if len(args) == 0 {
			continue
		}
		bindings := insightBindings(t, max(fn.start, i-2000), i)
		path := insightRequestPath(s, insightSpan{args[0][0], args[0][1]}, bindings)
		if !insightAPIPath(path) {
			continue
		}
		method := "GET"
		if len(args) > 1 {
			if at(t, args[1][0]) == "{" {
				props := insightObjectProperties(t, args[1][0])
				if span, ok := props["method"]; ok {
					method = strings.ToUpper(insightResolve(s, span, bindings, 0))
					if !insightHTTPMethod(method) {
						continue
					}
				}
			} else {
				continue // dynamic options may override the default method
			}
		}
		transport := "fetch"
		if at(t, i-1) == "." {
			transport = at(t, i-2) + ".fetch"
		}
		api = append(api, InsightAPI{Name: fn.name, Kind: "direct call expression", Method: method, Path: path,
			Transport: transport, Module: s.module, Runtime: s.global,
			Line: t[i].line, Column: t[i].column, token: i})
	}
	return api
}

// clientVerbs are the method names of HTTP clients: axios, $http, jQuery,
// Angular's HttpClient, XMLHttpRequest, and the wrappers built on them.
var clientVerbs = setOf("get", "post", "put", "patch", "delete", "head", "options", "request", "ajax", "getJSON", "open")

// Receivers that are not HTTP clients even when a method has a client's name:
// maps, caches, and stores keyed by path, and servers that register routes.
var (
	notClientReceiver = regexp.MustCompile(`map|cache|store|storage|registry|handlers|lookup|dict|memo|headers|params|formdata|cookies|router|^(app|server|route|routes|express|fastify|koa|hono)$|(app|server)$`)
	clientReceiver    = regexp.MustCompile(`http|axios|request|client|ajax|fetch|rest|superagent|backend|remote|api|service|instance|agent|network|transport|xhr|^(\$|jquery|ky|got)$`)
)

// insightClientCall reads a request made through a client method at token i:
// axios.get("/orders/list"), http.post(base + "/orders"), $.ajax({url: ...}),
// and xhr.open("POST", "/orders/list"). The method name alone is not enough,
// so a call is a request only when:
//   - the receiver is not a map, cache, store, or router (notClientReceiver);
//   - no later argument is a function, which is how a server declares a route;
//   - the URL is a path with a segment, built from a literal (insightRequestPath),
//     and not a file;
//   - for get, delete, head, options, and request with a string, which maps and
//     caches share, something shows it is a request: a client-like receiver, a
//     promise or subscription on the result, await, or an API-shaped path.
//
// open needs an HTTP method string first; the call is then a request whatever
// the receiver.
func insightClientCall(s *insightScope, i int, fn insightFunction) (InsightAPI, bool) {
	t := s.tokens
	verb := t[i].text
	args := argumentRanges(t, i+1)
	if len(args) == 0 {
		return InsightAPI{}, false
	}
	receiver := ""
	if i >= 2 && t[i-2].kind == 'i' {
		receiver = strings.ToLower(t[i-2].text)
	}
	if verb != "open" && receiver != "" && notClientReceiver.MatchString(receiver) {
		return InsightAPI{}, false
	}
	for _, arg := range args[1:] {
		for j := arg[0]; j < arg[1]; j++ {
			if at(t, j) == "function" || at(t, j) == "=>" {
				return InsightAPI{}, false
			}
			if t[j].match > j {
				j = t[j].match
			}
		}
	}
	span := insightSpan{args[0][0], args[0][1]}
	method := strings.ToUpper(verb)
	var props map[string]insightSpan
	var bindings map[string]insightSpan
	var path string
	// The earlier statements bind a URL held in a variable. Reading them costs
	// a scan, so it is skipped for the many calls whose argument is not a URL.
	bind := func() map[string]insightSpan {
		if bindings == nil {
			bindings = insightBindings(t, max(fn.start, i-2000), i)
		}
		return bindings
	}
	switch {
	case verb == "open":
		if len(args) < 2 || args[0][1] != args[0][0]+1 || t[args[0][0]].kind != 's' {
			return InsightAPI{}, false
		}
		method = strings.ToUpper(stringValue(t[args[0][0]].text))
		if !insightHTTPMethod(method) {
			return InsightAPI{}, false
		}
		path = insightRequestPath(s, insightSpan{args[1][0], args[1][1]}, bind())
	case at(t, span.start) == "{" && t[span.start].match == span.end-1:
		props = insightObjectProperties(t, span.start)
		for _, key := range []string{"url", "uri"} {
			if value, ok := props[key]; ok {
				path = insightRequestPath(s, value, bind())
				break
			}
		}
		for _, key := range []string{"method", "type"} {
			if value, ok := props[key]; ok {
				method = strings.ToUpper(insightResolve(s, value, bind(), 0))
				break
			}
		}
		if verb == "request" || verb == "ajax" {
			if _, ok := props["method"]; !ok {
				if _, ok := props["type"]; !ok {
					method = "GET"
				}
			}
		}
	default:
		lone := span.end == span.start+1 && t[span.start].kind == 'i'
		if lone && !insightBodyVerb(verb) && !clientReceiver.MatchString(receiver) {
			return InsightAPI{}, false
		}
		path = insightRequestPath(s, span, nil)
		if path == "" && !(span.end == span.start+1 && t[span.start].kind == 's') {
			path = insightRequestPath(s, span, bind())
		}
	}
	if verb == "getJSON" {
		method = "GET"
	}
	if !insightHTTPMethod(method) || len(path) < 2 || !insightAPIPath(path) {
		return InsightAPI{}, false
	}
	// Maps and caches have get, delete, and request-like methods too.
	if !insightBodyVerb(verb) && verb != "open" && props == nil &&
		!(len(args) > 1 && at(t, args[1][0]) == "{") &&
		!clientReceiver.MatchString(receiver) && !insightPromiseCall(t, i) && classifyPath(path) != pathAPI {
		return InsightAPI{}, false
	}
	name := fn.name
	if fn.nameAt == i {
		name = verb
	}
	return InsightAPI{Name: name, Kind: "direct call expression", Method: method, Path: path,
		Transport: receiver + "." + verb, Module: s.module, Runtime: s.global,
		Line: t[i].line, Column: t[i].column, client: true, token: i}, true
}

// insightBodyVerb names the methods that maps and caches do not have.
func insightBodyVerb(verb string) bool {
	return verb == "post" || verb == "put" || verb == "patch" || verb == "ajax" || verb == "getJSON"
}

// insightPromiseCall reports a call whose result is awaited or continued as a
// promise or subscription, as an HTTP request is.
func insightPromiseCall(t []token, i int) bool {
	if end := t[i+1].match; at(t, end+1) == "." {
		switch at(t, end+2) {
		case "then", "catch", "finally", "subscribe", "pipe", "toPromise", "json":
			return true
		}
	}
	for j := i - 1; j >= max(0, i-8); j-- {
		if at(t, j) == "await" {
			return true
		}
		if t[j].kind != 'i' && at(t, j) != "." {
			break
		}
	}
	return false
}

func insightFetchParameterShadows(s *insightScope, functions []insightFunction) []insightSpan {
	t := s.tokens
	var ranges []insightSpan
	for _, fn := range functions {
		params := fn.params
		if at(t, params) == "(" && insightHasFetchParameter(t, params) {
			ranges = append(ranges, insightSpan{fn.start, fn.end})
		}
	}
	for i := s.start; i+1 < s.end; i++ {
		body := -1
		if at(t, i) == "(" && t[i].match > i && at(t, t[i].match+1) == "=>" && insightHasFetchParameter(t, i) {
			body = t[i].match + 2
		} else if at(t, i) == "fetch" && at(t, i+1) == "=>" {
			body = i + 2
		}
		if body < 0 || body >= s.end {
			continue
		}
		end := insightExpressionEnd(t, body, s.end)
		if at(t, body) == "{" && t[body].match > body {
			body++
			end = t[body-1].match
		}
		if body < end {
			ranges = append(ranges, insightSpan{body, end})
		}
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].start != ranges[j].start {
			return ranges[i].start < ranges[j].start
		}
		return ranges[i].end > ranges[j].end
	})
	return ranges
}

func insightHasFetchParameter(t []token, open int) bool {
	for _, parameter := range argumentRanges(t, open) {
		if parameter[0] < parameter[1] && at(t, parameter[0]) == "fetch" {
			return true
		}
	}
	return false
}

func insightHTTPMethod(value string) bool {
	switch value {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return true
	}
	return false
}

func insightAPIPath(path string) bool {
	return path == "/" || insightAbsolutePath(path)
}

func insightAbsolutePath(path string) bool {
	if len(path) < 2 || path[0] != '/' || path[1] == '/' {
		return false
	}
	for _, char := range path {
		if char <= ' ' || strings.ContainsRune("\"'<>\\;", char) {
			return false
		}
	}
	return true
}

func insightCallee(t []token, call int) string {
	end := call
	if at(t, call-1) == ")" && t[call-1].match >= 0 {
		end = call - 1
		call = t[end].match + 1
		// The common minifier wrapper (0, a.x3)(...) ends in a.x3.
		for i := end - 1; i >= call; i-- {
			if at(t, i) == "," {
				call = i + 1
				break
			}
		}
	} else {
		start := call - 1
		for start >= 0 && (t[start].kind == 'i' || at(t, start) == ".") && at(t, start) != "return" {
			start--
		}
		call = start + 1
	}
	var parts []string
	for i := call; i < end; i++ {
		if t[i].kind == 'i' || at(t, i) == "." {
			parts = append(parts, t[i].text)
		}
	}
	return strings.Join(parts, "")
}

func insightRequestCallee(t []token, call int, callee string) bool {
	if callee == "" {
		return false
	}
	lower := strings.ToLower(callee)
	if strings.Contains(lower, "request") || strings.HasSuffix(lower, "fetch") || strings.Contains(lower, "axios") {
		return true
	}
	// Minified transport names are opaque. Require the call to be returned
	// from the named wrapper, as in generated API clients.
	return insightReturnedCall(t, call)
}

func insightReturnedCall(t []token, call int) bool {
	for i := call - 1; i >= max(0, call-14); i-- {
		if at(t, i) == "return" {
			return true
		}
		if at(t, i) == ";" || at(t, i) == "{" {
			break
		}
	}
	return false
}

func insightParameterKeys(s *insightScope, value insightSpan, bindings map[string]insightSpan) []string {
	t := s.tokens
	if value.end == value.start+1 && t[value.start].kind == 'i' {
		if binding, ok := bindings[t[value.start].text]; ok {
			value = binding
		}
	}
	if at(t, value.start) != "{" || t[value.start].match != value.end-1 {
		return nil
	}
	props := insightObjectProperties(t, value.start)
	keys := make([]string, 0, len(props))
	for key := range props {
		if insightIdentifier(key) {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	if len(keys) > 12 {
		keys = keys[:12]
	}
	return keys
}

// uniqueInsightAPIs drops repeated entries. A client method call that a request
// wrapper or an earlier call already reports (the same method and path in the
// same module) is that request, not another.
func uniqueInsightAPIs(entries []InsightAPI) []InsightAPI {
	seen := make(map[string]struct{})
	requests := make(map[string]struct{})
	var result []InsightAPI
	for _, entry := range entries {
		key := entry.Runtime + "\x00" + entry.Module + "\x00" + entry.Name + "\x00" + entry.Kind + "\x00" + entry.Method + "\x00" + entry.Path
		if _, ok := seen[key]; ok {
			continue
		}
		request := entry.Runtime + "\x00" + entry.Module + "\x00" + entry.Method + "\x00" + entry.Path
		if _, ok := requests[request]; ok && entry.client {
			continue
		}
		seen[key] = struct{}{}
		requests[request] = struct{}{}
		result = append(result, entry)
	}
	return result
}

func insightBehaviors(s *insightScope) []InsightBehavior {
	t := s.tokens
	var behaviors []InsightBehavior
	baseDepth := t[s.start].depth
	for i := s.start; i+3 < s.end && len(behaviors) < 16; i++ {
		if at(t, i) == "__assetPrefix__" && at(t, i-1) == "." && at(t, i-2) == "window" && at(t, i+1) == "=" {
			value := ""
			if t[i+2].kind == 's' {
				value = stringValue(t[i+2].text)
			}
			action := "sets the window asset prefix"
			if value != "" {
				action += " to " + value
			}
			behaviors = append(behaviors, InsightBehavior{Action: action, Module: s.module, Runtime: s.global,
				Line: t[i].line, Column: t[i].column, Evidence: "window.__assetPrefix__ assignment"})
		}
		if at(t, i) == "slardar" && at(t, i-1) == "." && at(t, i-2) == "window" && at(t, i+1) == "(" {
			args := argumentRanges(t, i+1)
			if len(args) < 2 || insightResolve(s, insightSpan{args[0][0], args[0][1]}, nil, 0) != "sendEvent" {
				continue
			}
			name := ""
			if at(t, args[1][0]) == "{" {
				props := insightObjectProperties(t, args[1][0])
				name = insightResolve(s, props["name"], nil, 0)
			}
			action := "reports a browser analytics event"
			if name != "" {
				action = "reports the " + name + " analytics event"
			}
			behaviors = append(behaviors, InsightBehavior{Action: action, Module: s.module, Runtime: s.global,
				Line: t[i].line, Column: t[i].column, Evidence: "window.slardar(\"sendEvent\", ...)"})
		}
		if at(t, i) == "reportEvent" && at(t, i-1) == "." && at(t, i+1) == "(" && t[i].depth == baseDepth {
			behaviors = append(behaviors, InsightBehavior{Action: "calls analytics reporting during module evaluation",
				Module: s.module, Runtime: s.global, Line: t[i].line, Column: t[i].column,
				Evidence: "reportEvent(...) at factory scope"})
		}
	}
	return behaviors
}

func uniqueInsightBehaviors(entries []InsightBehavior) []InsightBehavior {
	seen := make(map[string]struct{})
	var result []InsightBehavior
	for _, entry := range entries {
		key := entry.Runtime + "\x00" + entry.Module + "\x00" + entry.Action
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, entry)
	}
	return result
}

func insightComponents(scopes []insightScope, result *Insights) []InsightComponent {
	var components []InsightComponent
	for _, scope := range scopes {
		t := scope.tokens
		routes, api := 0, 0
		for _, route := range result.Routes {
			if route.Module == scope.module && route.Runtime == scope.global && route.Kind == "declared route" {
				routes++
			}
		}
		for _, entry := range result.API {
			if entry.Module == scope.module && entry.Runtime == scope.global && entry.Kind == "request wrapper" {
				api++
			}
		}
		jsx, verify, email, mobile, code, regions, continents := 0, 0, 0, 0, 0, 0, 0
		assetPrefix := false
		for i := scope.start; i < scope.end; i++ {
			word := strings.ToLower(t[i].text)
			if t[i].kind == 's' {
				word = strings.ToLower(stringValue(t[i].text))
				if strings.HasPrefix(word, "./regions/") && strings.HasSuffix(word, ".json") {
					regions++
				}
				if strings.HasPrefix(word, "./continents/") && strings.HasSuffix(word, ".json") {
					continents++
				}
			}
			if word == "jsx" || word == "jsxs" || word == "createelement" && at(t, i-2) != "document" {
				jsx++
			}
			// Whole words only: "code" must not match decode or keyCode.
			if mayContainRoleWord(word) {
				for _, part := range identifierWords(t[i].text) {
					switch part {
					case "verify", "verification", "verified", "verifying":
						verify++
					case "email":
						email++
					case "mobile", "phone", "sms":
						mobile++
					case "code", "otp", "passcode":
						code++
					}
				}
			}
			if at(t, i) == "__assetPrefix__" && at(t, i-1) == "." && at(t, i-2) == "window" && at(t, i+1) == "=" {
				assetPrefix = true
			}
		}
		role, evidence := "", ""
		switch {
		case routes >= 2 && jsx > 0:
			role, evidence = "React route shell", "route components declare UI paths"
		case routes >= 2:
			role, evidence = "route configuration", "route objects declare UI paths"
		case api >= 2:
			role, evidence = "API client", "named wrappers define HTTP requests"
		case regions > 0 && continents > 0:
			role, evidence = "regional locale loader", "region and continent JSON chunks are mapped for loading"
		case jsx > 0 && verify >= 2 && email > 0 && mobile > 0 && code > 0 && focusedScope(scope):
			role, evidence = "account verification UI", "email, mobile, code and verification UI markers"
		case assetPrefix:
			role, evidence = "asset configuration", "assigns window.__assetPrefix__"
		}
		if role == "" {
			continue
		}
		name := "Script"
		if scope.module != "" {
			name = "Module " + scope.module
		}
		components = append(components, InsightComponent{Name: name, Role: role, Module: scope.module,
			Runtime: scope.global, Line: t[scope.start].line, Column: t[scope.start].column, Evidence: evidence})
	}
	sort.SliceStable(components, func(i, j int) bool {
		return insightRoleRank(components[i].Role) < insightRoleRank(components[j].Role)
	})
	return components
}

// Keyword roles describe one component. Across a whole multi-megabyte script
// the same words appear by chance, so they need a module or a small file.
func focusedScope(scope insightScope) bool {
	return scope.module != "" || scope.end-scope.start < 20000
}

func mayContainRoleWord(word string) bool {
	for _, marker := range []string{"verif", "email", "mobile", "phone", "sms", "code", "otp"} {
		if strings.Contains(word, marker) {
			return true
		}
	}
	return false
}

// identifierWords splits camelCase, snake_case, kebab-case, and dotted text
// into lowercase words.
func identifierWords(value string) []string {
	var words []string
	start := -1
	flush := func(end int) {
		if start >= 0 && end > start {
			words = append(words, strings.ToLower(value[start:end]))
		}
		start = -1
	}
	for i := 0; i < len(value); i++ {
		c := value[i]
		lower, upper, digit := c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9'
		switch {
		case upper:
			// Start a word at an uppercase letter unless it continues an
			// acronym (as in "SMSCode", where "Code" still starts a word).
			if start >= 0 && !(i > 0 && value[i-1] >= 'A' && value[i-1] <= 'Z' && !(i+1 < len(value) && value[i+1] >= 'a' && value[i+1] <= 'z')) {
				flush(i)
			}
			if start < 0 {
				start = i
			}
		case lower || digit:
			if start < 0 {
				start = i
			}
		default:
			flush(i)
		}
	}
	flush(len(value))
	return words
}

func insightRoleRank(role string) int {
	switch role {
	case "React route shell", "route configuration":
		return 1
	case "API client":
		return 2
	case "account verification UI":
		return 3
	case "regional locale loader":
		return 4
	case "asset configuration":
		return 5
	}
	return 6
}

func insightSummary(components []InsightComponent, routes []InsightRoute, api []InsightAPI) string {
	roles := make([]string, 0, len(components))
	seen := make(map[string]struct{})
	for _, component := range components {
		if _, ok := seen[component.Role]; ok {
			continue
		}
		seen[component.Role] = struct{}{}
		roles = append(roles, component.Role)
		if len(roles) == 4 {
			break
		}
	}
	if len(roles) > 0 {
		return "Contains " + strings.Join(roles, ", ") + "."
	}
	if len(routes) > 0 && len(api) > 0 {
		return "Defines UI navigation paths and HTTP interactions."
	}
	if len(routes) > 0 {
		return "Defines UI navigation paths."
	}
	if len(api) > 0 {
		hasWrapper, hasDirect := false, false
		for _, entry := range api {
			hasWrapper = hasWrapper || entry.Kind == "request wrapper"
			hasDirect = hasDirect || entry.Kind == "direct call expression"
		}
		if hasWrapper && hasDirect {
			return "Contains HTTP request wrappers and direct call expressions."
		}
		if hasDirect {
			return "Contains direct HTTP call expressions."
		}
		return "Defines HTTP request wrappers."
	}
	return ""
}
