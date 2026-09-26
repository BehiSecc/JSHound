// Package overview describes the observable structure and entry paths of
// JavaScript without executing it or guessing application-specific intent.
package overview

import (
	"fmt"
	"sort"
	"strings"
)

type Source struct {
	Name      string
	Data      []byte
	SourceMap string
}

type Result struct {
	Version string `json:"version,omitempty"`
	Sources int    `json:"sources"`
	Bytes   int    `json:"bytes"`
	Files   []File `json:"files"`
}

type File struct {
	Name          string   `json:"name"`
	Bytes         int      `json:"bytes"`
	Format        string   `json:"format"`
	Build         string   `json:"build,omitempty"`
	SourceMap     bool     `json:"source_map_reference,omitempty"`
	Shape         []string `json:"shape,omitempty"`
	Imports       []string `json:"imports,omitempty"`
	Exports       []string `json:"exports,omitempty"`
	EntryPoints   []Entry  `json:"entry_points,omitempty"`
	Registrations []Entry  `json:"event_registrations,omitempty"`
	Units         []Unit   `json:"units,omitempty"`
	MoreUnits     int      `json:"more_units,omitempty"`
	Interactions  []string `json:"interactions,omitempty"`
	Links         []Link   `json:"observed_links,omitempty"`
	Diagram       string   `json:"diagram,omitempty"`
	Notes         []string `json:"notes,omitempty"`
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
	Calls        []string `json:"calls,omitempty"`
	Interactions []string `json:"interactions,omitempty"`
}

type Link struct {
	From string `json:"from"`
	To   string `json:"to"`
	Line int    `json:"line"`
}

type unitRange struct {
	Unit
	start int
	end   int
}

const maxUnits = 16

// Analyze keeps each source separate so an observed call never becomes an
// invented cross-file relationship.
func Analyze(sources []Source) Result {
	result := Result{Sources: len(sources), Files: make([]File, 0, len(sources))}
	for _, source := range sources {
		result.Bytes += len(source.Data)
		result.Files = append(result.Files, analyzeFile(source))
	}
	return result
}

func analyzeFile(source Source) File {
	raw := string(source.Data)
	code, html := scriptCode(raw, source.Name)
	tokens := lex(code)
	file := File{
		Name: source.Name, Bytes: len(source.Data), Format: "script",
		SourceMap: source.SourceMap != "" || strings.Contains(raw, "sourceMappingURL="),
	}
	if html {
		file.Format = "HTML inline scripts"
	}
	if minified(raw) {
		file.Build = "minified"
	}
	chunk := findChunk(tokens)
	if chunk != nil {
		file.Build = "Webpack-style bundle"
	} else if hasTokenPrefix(tokens, "webpackChunk") || hasToken(tokens, "__webpack_require__") {
		file.Build = "Webpack bundle"
	} else if hasToken(tokens, "__vite__") || hasToken(tokens, "__vitePreload") {
		file.Build = "Vite bundle"
	}
	file.Imports = imports(tokens)
	file.Exports = exports(tokens)
	if hasStaticModuleSyntax(tokens) {
		file.Format = "ES module"
		if html {
			file.Format = "HTML inline ES module"
		}
	} else if file.Build == "" && (hasTopLevelSequence(tokens, "module", ".", "exports") || hasTopLevelSequence(tokens, "exports", ".")) {
		file.Format = "CommonJS module"
	} else if looksLikeIIFE(tokens) {
		file.Format = "IIFE script"
	}
	if file.Build != "" || isUMD(tokens) {
		analyzeCompiled(&file, tokens, chunk)
		return file
	}

	units := declarations(tokens)
	if len(units) == 0 && looksLikeIIFE(tokens) {
		units = iifeDeclarations(tokens)
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
		calls := callsIn(tokens, units[i].start, units[i].end)
		called := make(map[string]struct{})
		for j := units[i].start; j < units[i].end; j++ {
			for _, item := range tokenInteractions(tokens, j) {
				allInteractions[item] = struct{}{}
				units[i].Interactions = appendUnique(units[i].Interactions, item)
			}
		}
		for _, call := range calls {
			if call.receiver == "" && call.name != units[i].Name {
				if _, ok := names[call.name]; ok {
					called[call.name] = struct{}{}
					addLink(&file.Links, seenLinks, units[i].Name+"()", call.name+"()", call.line)
				}
			}
			for _, item := range callInteractions(tokens, call) {
				allInteractions[item] = struct{}{}
				units[i].Interactions = appendUnique(units[i].Interactions, item)
			}
			addEventEntry(&file, tokens, call, units[i].Name+"()", names, seenLinks)
		}
		units[i].Calls = sortedKeys(called)
		sort.Strings(units[i].Interactions)
	}
	for _, call := range callsIn(tokens, 0, len(tokens)) {
		for _, item := range callInteractions(tokens, call) {
			allInteractions[item] = struct{}{}
		}
		if tokens[call.index].depth != 0 || tokens[call.index].paren != 0 || inUnit[call.index] {
			continue
		}
		if call.receiver == "" {
			if _, ok := names[call.name]; ok {
				file.EntryPoints = appendEntry(file.EntryPoints, Entry{Trigger: "module load", Action: call.name + "()", Line: call.line})
				addLink(&file.Links, seenLinks, "module load", call.name+"()", call.line)
			}
		}
		if call.name == "addEventListener" || call.name == "on" {
			addEventEntry(&file, tokens, call, "", names, seenLinks)
		}
		if isMountCall(call) || isTopLevelInteraction(call) {
			file.EntryPoints = appendEntry(file.EntryPoints, Entry{Trigger: "module load", Action: call.display(), Line: call.line})
		}
	}
	for _, t := range tokens {
		switch t.text {
		case "localStorage":
			allInteractions["localStorage"] = struct{}{}
		case "sessionStorage":
			allInteractions["sessionStorage"] = struct{}{}
		}
	}
	file.Interactions = sortedKeys(allInteractions)
	for _, unit := range units {
		if len(file.Units) < maxUnits {
			file.Units = append(file.Units, unit.Unit)
		} else {
			file.MoreUnits++
		}
	}
	if len(tokens) == 0 {
		file.Notes = append(file.Notes, "No JavaScript code found.")
	} else if len(file.EntryPoints) == 0 {
		if len(file.Exports) > 0 {
			file.Notes = append(file.Notes, "No local startup path found; this module exposes code for an importer to call.")
		} else {
			file.Notes = append(file.Notes, "No explicit startup path found in this file.")
		}
	}
	if file.Build == "minified" || strings.Contains(file.Build, "bundle") {
		file.Notes = append(file.Notes, "Bundled code can hide original module boundaries; a source map may reveal them.")
	}
	file.Diagram = buildDiagram(file)
	return file
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

func minified(raw string) bool {
	if len(raw) < 4096 {
		return false
	}
	longest, current := 0, 0
	for i := 0; i < len(raw); i++ {
		if raw[i] == '\n' {
			longest = max(longest, current)
			current = 0
		} else {
			current++
		}
	}
	return max(longest, current) > 2000
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
	key := from + "\x00" + to
	if _, exists := seen[key]; exists {
		return
	}
	seen[key] = struct{}{}
	*links = append(*links, Link{From: from, To: to, Line: line})
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
