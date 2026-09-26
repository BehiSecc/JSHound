package overview

import (
	"sort"
	"strings"
)

// Insights contains useful interfaces that can be tied to source locations.
// API entries describe request definitions, not requests observed at runtime.
type Insights struct {
	Summary    string             `json:"summary,omitempty"`
	Components []InsightComponent `json:"components,omitempty"`
	Routes     []InsightRoute     `json:"ui_routes,omitempty"`
	API        []InsightAPI       `json:"api_definitions,omitempty"`
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
	result.API = rankInsightAPIs(result.API)
	result.Summary = insightSummary(result.Components, result.Routes, result.API)
	if len(result.API) > 0 {
		result.Notes = append(result.Notes, "HTTP entries describe request wrappers or direct call expressions; their presence alone does not establish that a network request was sent.")
	}
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
	for i := s.start; i+2 < s.end; i++ {
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
	return routes
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
			if t[i+1].kind != 'i' {
				continue
			}
			nameAt, open = i+1, i+2
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
		functions = append(functions, insightFunction{name: name, nameAt: nameAt, start: body + 1, end: t[body].match})
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
					path = insightResolve(s, value, bindings, 0)
					break
				}
			}
			if path == "" && objectArg > 0 {
				path = insightResolve(s, insightSpan{args[0][0], args[0][1]}, bindings, 0)
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
				Line: t[fn.nameAt].line, Column: t[fn.nameAt].column})
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
		if at(t, i) != "fetch" || at(t, i+1) != "(" || t[i+1].match <= i+1 || t[i+1].match >= s.end {
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
		path := insightResolve(s, insightSpan{args[0][0], args[0][1]}, bindings, 0)
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
			Line: t[i].line, Column: t[i].column})
	}
	return api
}

func insightFetchParameterShadows(s *insightScope, functions []insightFunction) []insightSpan {
	t := s.tokens
	var ranges []insightSpan
	for _, fn := range functions {
		params := fn.nameAt + 1
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

func uniqueInsightAPIs(entries []InsightAPI) []InsightAPI {
	seen := make(map[string]struct{})
	var result []InsightAPI
	for _, entry := range entries {
		key := entry.Runtime + "\x00" + entry.Module + "\x00" + entry.Name + "\x00" + entry.Kind + "\x00" + entry.Method + "\x00" + entry.Path
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, entry)
	}
	return result
}

func rankInsightAPIs(entries []InsightAPI) []InsightAPI {
	// Keep every definition in JSON, but put distinct path areas and methods
	// first so a short report shows the breadth of the client.
	sort.SliceStable(entries, func(i, j int) bool {
		return insightAPIScore(entries[i]) > insightAPIScore(entries[j])
	})
	return entries
}

func insightAPIScore(entry InsightAPI) int {
	depth := strings.Count(strings.Trim(entry.Path, "/"), "/")
	score := depth * 3
	if entry.Method != "GET" {
		score += 2
	}
	if len(entry.Parameters) > 0 {
		score++
	}
	return score
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
			if word == "jsx" || word == "jsxs" || word == "createelement" {
				jsx++
			}
			if strings.Contains(word, "verif") {
				verify++
			}
			if strings.Contains(word, "email") {
				email++
			}
			if strings.Contains(word, "mobile") || strings.Contains(word, "phone") {
				mobile++
			}
			if strings.Contains(word, "code") {
				code++
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
		case jsx > 0 && verify >= 2 && email > 0 && mobile > 0 && code > 0:
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
