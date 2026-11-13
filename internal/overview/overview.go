// Package overview describes what JavaScript files are and what they do, and
// their observable structure and entry paths, without executing them. Feature
// areas come only from names and text that recur in the code.
package overview

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type Source struct {
	Name      string
	Data      []byte
	SourceMap string
	// Map holds an already loaded and validated source map, if available.
	Map     []byte
	MapName string
}

type Result struct {
	Version string `json:"version,omitempty"`
	Sources int    `json:"sources"`
	Bytes   int    `json:"bytes"`
	// Overall answers the same questions for the whole set. It is present only
	// when more than one source was analysed.
	Overall *Overall `json:"overall,omitempty"`
	Files   []File   `json:"files"`
}

type File struct {
	Name          string          `json:"name"`
	Bytes         int             `json:"bytes"`
	Purpose       *Purpose        `json:"purpose,omitempty"`
	Summary       string          `json:"summary,omitempty"`
	Format        string          `json:"format"`
	Build         string          `json:"build,omitempty"`
	Minified      bool            `json:"minified,omitempty"`
	SourceMap     bool            `json:"source_map_reference,omitempty"`
	SourceMapUsed string          `json:"source_map_applied,omitempty"`
	Shape         []string        `json:"shape,omitempty"`
	Imports       []string        `json:"imports,omitempty"`
	Exports       []string        `json:"exports,omitempty"`
	Loads         []string        `json:"loads,omitempty"` // other input files named in a string literal
	EntryPoints   []Entry         `json:"entry_points,omitempty"`
	Registrations []Entry         `json:"event_registrations,omitempty"`
	Units         []Unit          `json:"units,omitempty"`
	MoreUnits     int             `json:"more_units,omitempty"`
	Interactions  []string        `json:"interactions,omitempty"`
	Links         []Link          `json:"observed_links,omitempty"`
	Bundle        *Bundle         `json:"bundle,omitempty"`
	Compiled      *Compiled       `json:"compiled,omitempty"`
	Insights      *Insights       `json:"insights,omitempty"`
	Modules       *BundledModules `json:"bundled_modules,omitempty"`
	Dispatches    []Dispatch      `json:"dispatch,omitempty"`
	Diagram       string          `json:"diagram,omitempty"`
	Notes         []string        `json:"notes,omitempty"`

	// Text report only; never encoded. restored holds the flow labels a source
	// map renamed, mapHint says how to load a referenced map that was not
	// applied, and loadingHidden marks a file whose first entry path has a
	// minified name, so the report does not present a later one as the first.
	restored      map[string]struct{}
	mapHint       string
	loadingHidden bool
	closure       bool // opens with a Closure Compiler module prologue
	// kind and entry feed the text report for several sources: what sort of
	// file this is and whether the roll-up names it an entry file.
	kind  string
	entry bool
}

type Entry struct {
	Trigger      string `json:"trigger"`
	Action       string `json:"action"`
	Line         int    `json:"line"`
	RegisteredBy string `json:"registered_by,omitempty"`
}

type Unit struct {
	Name         string   `json:"name"`
	Kind         string   `json:"kind"`
	Line         int      `json:"line"`
	OriginalName string   `json:"original_name,omitempty"`
	Origin       string   `json:"origin,omitempty"`
	Calls        []string `json:"calls,omitempty"`
	Interactions []string `json:"interactions,omitempty"`
}

type Link struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Relation string `json:"relation,omitempty"`
	Line     int    `json:"line"`
}

type unitRange struct {
	Unit
	start  int
	end    int
	nameAt int // declaration name token, or -1
}

const maxUnits = 16

// maxFlowTokens covers roughly 30 MiB of minified JavaScript.
const maxFlowTokens = 8_000_000

// Analyze keeps each source separate so an observed call never becomes an
// invented cross-file relationship. With several sources it also rolls the
// per-file results up into Result.Overall, without reading any file again;
// the only cross-file relationship it records is a file naming another input
// file in a string literal.
func Analyze(sources []Source) Result {
	result := Result{Sources: len(sources), Files: make([]File, 0, len(sources))}
	var sink *rollupSink
	if len(sources) > 1 {
		sink = newRollupSink(sources)
	}
	for i, source := range sources {
		result.Bytes += len(source.Data)
		if sink != nil {
			sink.current = i
		}
		result.Files = append(result.Files, analyzeFile(source, sink))
	}
	if sink != nil {
		result.Overall = sink.overall(result.Files)
	}
	return result
}

func analyzeFile(source Source, sink *rollupSink) File {
	raw := string(source.Data)
	code, html := scriptCode(raw, source.Name)
	tokens := lex(code)
	file := File{
		Name: source.Name, Bytes: len(source.Data), Format: "script",
		SourceMap: source.SourceMap != "" || hasSourceMapDirective(raw),
		Minified:  minified(raw),
	}
	// The prologue is conclusive: Closure output is minified however its
	// lines are wrapped.
	if file.closure = closurePrologue(raw); file.closure {
		file.Minified = true
	}
	if note := lexNote(code, tokens); note != "" {
		file.Notes = append(file.Notes, note)
	}
	if html {
		file.Format = "HTML inline scripts"
	}
	if file.Minified {
		file.Build = "minified"
	}
	chunks := findChunks(tokens)
	var chunk *chunkInfo
	if len(chunks) > 0 {
		chunk = chunks[0]
	}
	if chunk != nil {
		file.Build = "Webpack-style bundle"
	} else if esbuildRuntime(tokens) {
		file.Build = "esbuild bundle"
	} else if hasTokenPrefix(tokens, "webpackChunk") || hasToken(tokens, "__webpack_require__") {
		file.Build = "Webpack bundle"
	} else if hasToken(tokens, "__vite__") || hasToken(tokens, "__vitePreload") || hasToken(tokens, "__vite__mapDeps") {
		file.Build = "Vite bundle"
	}
	var moduleSpans []moduleSpan
	if file.Build == "esbuild bundle" {
		moduleSpans = mergeModuleSpans(esbuildModuleComments(code), esbuildModuleKeys(tokens))
		file.Modules = summarizeModules("esbuild module markers", moduleSpans)
	}
	var smap *sourceMap
	if len(source.Map) > 0 && !html {
		if parsed, err := parseSourceMap(source.Map); err != nil {
			file.Notes = append(file.Notes, "The source map could not be decoded: "+err.Error())
		} else {
			smap = parsed
			file.SourceMapUsed = source.MapName
			if modules := summarizeModules("the source map", smap.modules()); modules != nil {
				file.Modules = modules
			}
		}
	}
	if file.SourceMap && file.SourceMapUsed == "" && len(source.Map) == 0 && !html {
		file.mapHint = sourceMapHint(source.Name, source.SourceMap, raw)
	}
	if chunk == nil {
		file.Imports = imports(tokens)
	}
	file.Exports = exports(tokens)
	if sink != nil {
		file.Loads = sink.link(tokens, raw, html)
	}
	if hasStaticModuleSyntax(tokens) {
		file.Format = "ES module"
		if html {
			file.Format = "HTML inline ES module"
		}
	} else if chunk == nil && (hasTopLevelSequence(tokens, "module", ".", "exports") || hasTopLevelSequence(tokens, "exports", ".")) {
		file.Format = "CommonJS module"
	} else if looksLikeIIFE(tokens) {
		file.Format = "IIFE script"
	}
	file.Insights = analyzeInsights(tokens, chunks)
	if chunk != nil {
		analyzeCompiled(&file, tokens, chunk)
		for i, note := range file.Notes {
			if strings.HasPrefix(note, "No direct startup module require was identified") {
				file.Notes = append(file.Notes[:i], file.Notes[i+1:]...)
				break
			}
		}
		for _, additional := range chunks[1:] {
			file.Shape = append(file.Shape, chunkDescription(additional))
			file.EntryPoints = append(file.EntryPoints, Entry{Trigger: "module load", Action: "registers chunk " + additional.id + " with loader", Line: additional.line})
		}
		file.Bundle = analyzeBundle(tokens, chunks, file.Insights)
		mergeBundleComponents(&file, chunks)
		if file.Bundle.Startup == nil {
			file.Notes = append(file.Notes, "No direct startup module require was identified; the host runtime decides when factories run.")
		}
		file.Diagram = bundleDiagram(file.Bundle, chunks, file.Insights)
		file.Dispatches = rankDispatches(findDispatches(tokens, nil))
		file.Summary = describeFile(&file, tokens, raw, nil, sink)
		return file
	}
	if file.Build != "" || isUMD(tokens) {
		format := file.Format
		analyzeCompiled(&file, tokens, nil)
		if !isUMD(tokens) && (file.Build == "minified" || strings.Contains(format, "module")) {
			file.Format = format
		}
		file.Compiled = analyzeCompiledDetail(tokens)
	}

	// Call analysis is linear in practice, so even multi-megabyte bundles are
	// analyzed completely. Only pathological inputs fall back to a complete
	// statement prefix while compiled and insight summaries cover everything.
	originalTokenCount := len(tokens)
	analysisTokens := tokens
	if len(tokens) > maxFlowTokens {
		end := maxFlowTokens
		for end > maxFlowTokens-50000 && (tokens[end-1].depth != 0 || tokens[end-1].paren != 0 || tokens[end-1].text != ";") {
			end--
		}
		if end <= maxFlowTokens-50000 {
			analysisTokens = nil
		} else {
			analysisTokens = tokens[:end]
		}
		file.Notes = append(file.Notes, "Local declaration and call analysis is bounded for this large file; structural and insight facts cover the full source.")
	}
	tokens = analysisTokens

	pageMask, pageFunctions, pageScripts := pageScriptMask(tokens)
	units := declarations(tokens)
	if len(units) <= 1 && (looksLikeIIFE(tokens) || isUMD(tokens) || enclosingIIFE(tokens)) {
		if wrapped := iifeDeclarations(tokens); len(wrapped) > len(units) {
			units = wrapped
		}
	}
	if pageScripts {
		maskPageFunctions(pageMask, pageFunctions, units)
	}
	if !file.Minified && minifiedNames(units, tokens) {
		file.Minified = true
		if file.Build == "" {
			file.Build = "minified"
		}
	}
	if smap != nil {
		applySourceMap(smap, code, tokens, units)
	} else if len(moduleSpans) > 0 {
		for i := range units {
			units[i].Origin = moduleAt(moduleSpans, units[i].Line)
		}
	}
	names := make(map[string]struct{}, len(units))
	inUnit := make([]bool, len(tokens))
	for _, unit := range units {
		names[unit.Name] = struct{}{}
		for i := unit.start; i < unit.end && i < len(tokens); i++ {
			inUnit[i] = true
		}
	}
	seenLinks := make(map[string]struct{})
	allInteractions := make(map[string]struct{})
	for i := range units {
		if units[i].Kind == "class" {
			continue
		}
		calls := callsIn(tokens, units[i].start, units[i].end)
		nested := nestedFunctionMask(tokens, units[i].start, units[i].end)
		called := make(map[string]struct{})
		for j := units[i].start; j < units[i].end; j++ {
			if isMasked(pageMask, j) {
				continue
			}
			for _, item := range tokenInteractions(tokens, j) {
				allInteractions[item] = struct{}{}
				units[i].Interactions = appendUnique(units[i].Interactions, item)
			}
		}
		for _, call := range calls {
			target := call.name
			if call.receiver == "this" && units[i].Kind == "method" {
				target = strings.SplitN(units[i].Name, ".", 2)[0] + "." + call.name
			} else if call.receiver != "" {
				target = ""
			}
			if !nested[call.index-units[i].start] && target != "" && target != units[i].Name {
				if _, ok := names[target]; ok {
					called[target] = struct{}{}
					addLink(&file.Links, seenLinks, units[i].Name+"()", target+"()", call.line)
				}
			}
			if isMasked(pageMask, call.index) {
				continue
			}
			for _, item := range callInteractions(tokens, call) {
				allInteractions[item] = struct{}{}
				units[i].Interactions = appendUnique(units[i].Interactions, item)
			}
			if !nested[call.index-units[i].start] {
				addEventEntry(&file, tokens, call, units[i].Name+"()", names, seenLinks)
			}
		}
		units[i].Calls = sortedKeys(called)
		sort.Strings(units[i].Interactions)
	}
	for _, call := range callsIn(tokens, 0, len(tokens)) {
		if isMasked(pageMask, call.index) {
			continue
		}
		for _, item := range callInteractions(tokens, call) {
			allInteractions[item] = struct{}{}
		}
		if tokens[call.index].depth != 0 || tokens[call.index].paren != 0 || inUnit[call.index] {
			continue
		}
		if call.name == "addEventListener" || call.name == "on" {
			addEventEntry(&file, tokens, call, "", names, seenLinks)
			continue
		}
		if call.name == "require" || call.name == "define" || isNoiseCall(call) {
			continue
		}
		file.EntryPoints = appendEntry(file.EntryPoints, Entry{Trigger: "module load", Action: topLevelAction(tokens, call), Line: call.line})
		if call.receiver == "" {
			if _, ok := names[call.name]; ok {
				addLink(&file.Links, seenLinks, "module load", call.name+"()", call.line)
			}
		}
	}
	for i := range tokens {
		if storage := storageUse(tokens, i); storage != "" && !isMasked(pageMask, i) {
			allInteractions[storage] = struct{}{}
		}
	}
	if pageScripts {
		allInteractions["scripts evaluated in browser pages"] = struct{}{}
	}
	addReachableRegistrations(&file, seenLinks)
	file.Dispatches = findDispatches(tokens, units)
	scores := unitScores(units, file.Links, file.Dispatches)
	file.Links = orderLinks(file.Links, scores)
	for _, interaction := range file.Interactions {
		allInteractions[interaction] = struct{}{}
	}
	file.Interactions = sortedKeys(allInteractions)
	file.Units, file.MoreUnits = selectUnits(units, scores)
	if originalTokenCount == 0 {
		file.Notes = append(file.Notes, "No JavaScript code found.")
	} else if len(file.EntryPoints) == 0 {
		if len(file.Exports) > 0 {
			file.Notes = append(file.Notes, "No local startup path found; this module exposes code for an importer to call.")
		} else {
			file.Notes = append(file.Notes, "No explicit startup path found in this file.")
		}
	}
	if smap != nil {
		file.Notes = append(file.Notes, "Original names and source locations come from the source map; line numbers refer to the generated file.")
	} else if file.Build == "minified" || strings.Contains(file.Build, "bundle") {
		file.Notes = append(file.Notes, "Bundled code can hide original module boundaries; a source map may reveal them.")
	}
	for i := range file.Dispatches {
		index := file.Dispatches[i].index
		if smap != nil {
			file.Dispatches[i].Origin = smap.origin(code, tokens, index)
		} else if len(moduleSpans) > 0 {
			file.Dispatches[i].Origin = moduleAt(moduleSpans, tokens[index].line)
		}
	}
	file.Dispatches = rankDispatches(file.Dispatches)
	useOriginalNames(&file, units)
	file.Summary = describeFile(&file, tokens, raw, packageTokenFilter(smap, moduleSpans, code, tokens), sink)
	file.Diagram = buildDiagram(file)
	return file
}

func hasSourceMapDirective(raw string) bool {
	// Source map directives are comments near the tail of generated JS. A
	// sourceMappingURL string embedded in CSS or application data is not one.
	if len(raw) > 4096 {
		raw = raw[len(raw)-4096:]
	}
	line := strings.TrimSpace(raw)
	if last := strings.LastIndexByte(line, '\n'); last >= 0 {
		line = strings.TrimSpace(line[last+1:])
	}
	for _, marker := range []string{"//# sourceMappingURL=", "//@ sourceMappingURL=", "/*# sourceMappingURL=", "/*@ sourceMappingURL="} {
		if strings.HasPrefix(line, marker) && !strings.ContainsRune(strings.TrimPrefix(line, marker), '`') {
			return true
		}
	}
	return false
}

func hasToken(tokens []token, value string) bool {
	for _, t := range tokens {
		if t.kind == 'i' && t.text == value {
			return true
		}
	}
	return false
}

func hasTokenPrefix(tokens []token, prefix string) bool {
	for _, t := range tokens {
		if t.kind == 'i' && strings.HasPrefix(t.text, prefix) {
			return true
		}
	}
	return false
}

func hasStaticModuleSyntax(tokens []token) bool {
	for i, t := range tokens {
		if t.depth == 0 && (t.text == "export" || t.text == "import" && at(tokens, i+1) != "(" && at(tokens, i+1) != ".") {
			return true
		}
	}
	return false
}

func hasTopLevelSequence(tokens []token, sequence ...string) bool {
	for i := 0; i+len(sequence) <= len(tokens); i++ {
		if tokens[i].depth != 0 || tokens[i].paren != 0 {
			continue
		}
		ok := true
		for j, value := range sequence {
			if tokens[i+j].text != value {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}

// A readable bundle may embed one long data line; minified code keeps most
// of its bytes on very long lines.
func minified(raw string) bool {
	if len(raw) < 4096 {
		return false
	}
	long, start := 0, 0
	for i := 0; i <= len(raw); i++ {
		if i == len(raw) || raw[i] == '\n' {
			if i-start > 2000 {
				long += i - start
			}
			start = i + 1
		}
	}
	return long*2 > len(raw)
}

// A compiler may wrap its output into short lines and still rename every
// function, so the declared names are judged as well: with at least
// minNamedDeclarations functions and classes, nine in ten unreadable. Readable
// code stays far below that, since its short names are the exception.
const (
	minNamedDeclarations = 20
	minifiedNamePercent  = 90
)

func minifiedNames(units []unitRange, tokens []token) bool {
	declared, unreadable := assignedDeclarations(tokens)
	for _, unit := range units {
		if unit.Kind == "method" {
			continue
		}
		declared++
		if !readableName(unit.Name) {
			unreadable++
		}
	}
	return declared >= minNamedDeclarations && unreadable*100 >= declared*minifiedNamePercent
}

// assignedDeclarations counts functions and classes that are declared by
// assignment, as Closure Compiler writes them: "x=function(){}" and
// "_.x=class{}". A declaration with var, let, or const is already a unit. Only
// a bare name or a member of an unreadable receiver counts; a member of a
// named object, such as exports.run, may be short in readable code.
func assignedDeclarations(tokens []token) (declared, unreadable int) {
	for i := 3; i < len(tokens); i++ {
		if tokens[i].kind != 'i' || tokens[i].text != "function" && tokens[i].text != "class" || tokens[i-1].text != "=" {
			continue
		}
		name := tokens[i-2]
		if name.kind != 'i' {
			continue
		}
		switch tokens[i-3].text {
		case "var", "let", "const":
			continue
		case ".":
			if i < 5 || tokens[i-4].kind != 'i' || readableName(tokens[i-4].text) || at(tokens, i-5) == "." {
				continue
			}
		}
		declared++
		if !readableName(name.text) {
			unreadable++
		}
	}
	return declared, unreadable
}

// closureModule matches the prologue Closure Compiler writes around a module's
// code: "this.default_Name=this.default_Name||{};(function(_){var window=this;".
var closureModule = regexp.MustCompile(`this\.([\w$]+)=this\.([\w$]+)\|\|\{\};\s*\(function\(_\)\{\s*var window=this;`)

func closurePrologue(raw string) bool {
	m := closureModule.FindStringSubmatch(raw[:min(len(raw), 1<<16)])
	return m != nil && m[1] == m[2]
}

func scriptCode(raw, name string) (string, bool) {
	lowerName := strings.ToLower(name)
	trimmed := strings.TrimSpace(raw)
	html := strings.HasSuffix(lowerName, ".html") || strings.HasSuffix(lowerName, ".htm") ||
		strings.HasPrefix(strings.ToLower(trimmed), "<!doctype html") || strings.HasPrefix(strings.ToLower(trimmed), "<html")
	if !html {
		return raw, false
	}
	masked := []byte(raw)
	for i, c := range masked {
		if c != '\n' {
			masked[i] = ' '
		}
	}
	lower := strings.ToLower(raw)
	for pos := 0; pos < len(raw); {
		start := strings.Index(lower[pos:], "<script")
		if start < 0 {
			break
		}
		start += pos
		tagEnd := strings.IndexByte(lower[start:], '>')
		if tagEnd < 0 {
			break
		}
		contentStart := start + tagEnd + 1
		close := strings.Index(lower[contentStart:], "</script")
		if close < 0 {
			break
		}
		contentEnd := contentStart + close
		copy(masked[contentStart:contentEnd], raw[contentStart:contentEnd])
		pos = contentEnd + len("</script")
	}
	return string(masked), true
}

func appendUnique(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func sortedKeys(values map[string]struct{}) []string {
	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func addLink(links *[]Link, seen map[string]struct{}, from, to string, line int) {
	addRelatedLink(links, seen, from, to, "", line)
}

func addRelatedLink(links *[]Link, seen map[string]struct{}, from, to, relation string, line int) {
	key := from + "\x00" + to + "\x00" + relation
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}
	*links = append(*links, Link{From: from, To: to, Relation: relation, Line: line})
}

func addReachableRegistrations(file *File, seen map[string]struct{}) {
	visited := make(map[string]struct{})
	queue := []string{"module load"}
	for _, entry := range file.EntryPoints {
		if entry.Trigger != "module load" {
			queue = appendUnique(queue, entry.Trigger)
		}
	}
	bySource := make(map[string][]string)
	for _, link := range file.Links {
		bySource[link.From] = append(bySource[link.From], link.To)
	}
	byRegistrar := make(map[string][]Entry)
	for _, entry := range file.Registrations {
		byRegistrar[entry.RegisteredBy] = append(byRegistrar[entry.RegisteredBy], entry)
	}
	for len(queue) > 0 {
		from := queue[0]
		queue = queue[1:]
		if _, exists := visited[from]; exists {
			continue
		}
		visited[from] = struct{}{}
		queue = append(queue, bySource[from]...)
		for _, entry := range byRegistrar[from] {
			addRelatedLink(&file.Links, seen, entry.RegisteredBy, entry.Trigger, "registers", entry.Line)
			queue = append(queue, entry.Trigger)
			if entry.Action != "inline callback" {
				addLink(&file.Links, seen, entry.Trigger, entry.Action, entry.Line)
				queue = append(queue, entry.Action)
			}
		}
	}
}

func appendEntry(entries []Entry, value Entry) []Entry {
	for _, existing := range entries {
		if existing.Trigger == value.Trigger && existing.Action == value.Action && existing.RegisteredBy == value.RegisteredBy {
			return entries
		}
	}
	return append(entries, value)
}

func (entry Entry) String() string {
	if entry.RegisteredBy != "" {
		return fmt.Sprintf("%s (registered in %s) → %s", entry.Trigger, entry.RegisteredBy, entry.Action)
	}
	return entry.Trigger + " → " + entry.Action
}
