package overview

import (
	"sort"
	"strings"
)

// Dispatch is a place where one value selects among many handlers: a switch
// over string cases, a handler map indexed by key, or route registrations.
// It is usually where an RPC, message, or request protocol lives.
type Dispatch struct {
	Kind   string   `json:"kind"`
	In     string   `json:"in,omitempty"`
	On     string   `json:"on,omitempty"`
	Cases  []string `json:"cases"`
	Count  int      `json:"count"`
	Line   int      `json:"line"`
	Origin string   `json:"origin,omitempty"`
	index  int
}

const (
	minDispatchCases = 3
	maxDispatchCases = 12
	maxDispatches    = 24
)

func findDispatches(tokens []token, units []unitRange) []Dispatch {
	var result []Dispatch
	result = append(result, switchDispatches(tokens)...)
	result = append(result, handlerMapDispatches(tokens)...)
	result = append(result, routeDispatches(tokens)...)
	if len(result) == 0 {
		return nil
	}
	seen := make(map[string]struct{})
	unique := result[:0]
	for _, dispatch := range result {
		// Bundles often inline the same helper several times.
		key := dispatch.Kind + "\x00" + dispatch.On + "\x00" + strings.Join(dispatch.Cases, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		dispatch.In = enclosingUnit(units, dispatch.index)
		unique = append(unique, dispatch)
	}
	return unique
}

// rankDispatches puts project code before bundled packages, then larger
// dispatch points first.
func rankDispatches(dispatches []Dispatch) []Dispatch {
	sort.SliceStable(dispatches, func(i, j int) bool {
		leftPackage := strings.Contains(dispatches[i].Origin, "node_modules/")
		rightPackage := strings.Contains(dispatches[j].Origin, "node_modules/")
		if leftPackage != rightPackage {
			return !leftPackage
		}
		return dispatches[i].Count > dispatches[j].Count
	})
	if len(dispatches) > maxDispatches {
		dispatches = dispatches[:maxDispatches]
	}
	return dispatches
}

func switchDispatches(tokens []token) []Dispatch {
	var result []Dispatch
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].text != "switch" || tokens[i].kind != 'i' || at(tokens, i+1) != "(" || tokens[i+1].match < 0 {
			continue
		}
		closeParen := tokens[i+1].match
		body := closeParen + 1
		if at(tokens, body) != "{" || tokens[body].match < body {
			continue
		}
		if at(tokens, i+2) == "typeof" {
			continue
		}
		var cases []string
		count := 0
		depth := tokens[body].depth + 1
		for j := body + 1; j < tokens[body].match; j++ {
			if tokens[j].depth != depth || tokens[j].text != "case" || tokens[j].kind != 'i' {
				continue
			}
			value := stringValue(at(tokens, j+1))
			if value == "" || at(tokens, j+2) != ":" || len(value) > 64 || typeName(value) {
				continue
			}
			count++
			if len(cases) < maxDispatchCases {
				cases = append(cases, value)
			}
		}
		if count < minDispatchCases || !nameLikeCases(cases) {
			continue
		}
		result = append(result, Dispatch{Kind: "switch", On: expressionText(tokens, i+2, closeParen), Cases: cases, Count: count, Line: tokens[i].line, index: i})
	}
	return result
}

// handlerMapDispatches finds handlers[key](...) calls whose object is a local
// literal of functions.
func handlerMapDispatches(tokens []token) []Dispatch {
	lookups := make(map[string]int) // object name -> first lookup token
	keys := make(map[string]string)
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].kind != 'i' || at(tokens, i+1) != "[" || tokens[i+1].match < 0 || at(tokens, i-1) == "." {
			continue
		}
		closeBracket := tokens[i+1].match
		next := at(tokens, closeBracket+1)
		if next == "?." {
			next = at(tokens, closeBracket+2)
		}
		if next != "(" || closeBracket == i+2 && tokens[i+2].kind != 'i' && !strings.Contains(tokens[i+2].text, ".") {
			continue
		}
		if _, seen := lookups[tokens[i].text]; !seen {
			lookups[tokens[i].text] = i
			keys[tokens[i].text] = expressionText(tokens, i+2, closeBracket)
		}
	}
	if len(lookups) == 0 {
		return nil
	}
	var result []Dispatch
	for i := 0; i+3 < len(tokens); i++ {
		name := tokens[i].text
		if _, ok := lookups[name]; !ok || tokens[i].kind != 'i' || at(tokens, i+1) != "=" || at(tokens, i+2) != "{" {
			continue
		}
		switch at(tokens, i-1) {
		case "const", "let", "var":
		default:
			continue
		}
		open := i + 2
		cases, count := functionProperties(tokens, open)
		if count < minDispatchCases {
			continue
		}
		lookup := lookups[name]
		result = append(result, Dispatch{Kind: "handler map " + name, On: keys[name], Cases: cases, Count: count, Line: tokens[lookup].line, index: lookup})
		delete(lookups, name)
	}
	return result
}

func functionProperties(tokens []token, open int) ([]string, int) {
	end := tokens[open].match
	if end < 0 {
		return nil, 0
	}
	var names []string
	count := 0
	depth := tokens[open].depth + 1
	for j := open + 1; j < end; j++ {
		if tokens[j].depth != depth || (at(tokens, j-1) != "{" && at(tokens, j-1) != ",") {
			continue
		}
		key := tokens[j].text
		if tokens[j].kind == 's' {
			key = stringValue(key)
		} else if tokens[j].kind != 'i' {
			continue
		}
		valueAt := j + 1
		if at(tokens, j) == "async" && tokens[j+1].kind == 'i' && at(tokens, j+2) == "(" {
			key, valueAt = tokens[j+1].text, j+1
		}
		isFunction := false
		switch at(tokens, valueAt) {
		case "(": // method shorthand
			isFunction = tokens[valueAt].match > 0 && at(tokens, tokens[valueAt].match+1) == "{"
		case ":":
			value := valueAt + 1
			if at(tokens, value) == "async" {
				value++
			}
			isFunction = at(tokens, value) == "function" || at(tokens, value) == "(" && at(tokens, tokens[value].match+1) == "=>" ||
				tokens[value].kind == 'i' && at(tokens, value+1) == "=>" || tokens[value].kind == 'i' && (at(tokens, value+1) == "," || at(tokens, value+1) == "}")
		}
		if !isFunction || key == "" {
			continue
		}
		count++
		if len(names) < maxDispatchCases {
			names = append(names, key)
		}
	}
	return names, count
}

var routeMethods = map[string]string{"get": "GET", "post": "POST", "put": "PUT", "patch": "PATCH", "delete": "DELETE", "all": "ALL", "options": "OPTIONS", "head": "HEAD"}

// routeDispatches groups app.get('/path', handler)-style registrations by
// receiver.
func routeDispatches(tokens []token) []Dispatch {
	type group struct{ dispatch Dispatch }
	groups := make(map[string]*group)
	for i := 2; i+2 < len(tokens); i++ {
		method, ok := routeMethods[tokens[i].text]
		if !ok || at(tokens, i-1) != "." || at(tokens, i+1) != "(" || tokens[i-2].kind != 'i' {
			continue
		}
		args := argumentRanges(tokens, i+1)
		if len(args) < 2 || args[0][1]-args[0][0] != 1 {
			continue
		}
		path := stringValue(at(tokens, args[0][0]))
		if !strings.HasPrefix(path, "/") || len(path) > 96 {
			continue
		}
		receiver := tokens[i-2].text
		if groups[receiver] == nil {
			groups[receiver] = &group{dispatch: Dispatch{Kind: "routes", On: receiver, Line: tokens[i].line, index: i}}
		}
		g := groups[receiver]
		g.dispatch.Count++
		if len(g.dispatch.Cases) < maxDispatchCases {
			g.dispatch.Cases = append(g.dispatch.Cases, method+" "+path)
		}
	}
	var result []Dispatch
	for _, g := range groups {
		if g.dispatch.Count >= 2 {
			result = append(result, g.dispatch)
		}
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Line < result[j].Line })
	return result
}

// enclosingUnit names the innermost declaration (a method rather than its
// class) that contains a token.
func enclosingUnit(units []unitRange, index int) string {
	best := ""
	bestSize := -1
	for _, unit := range units {
		if index < unit.start || index >= unit.end {
			continue
		}
		if size := unit.end - unit.start; bestSize < 0 || size < bestSize {
			best, bestSize = unit.Name, size
		}
	}
	return best
}

func expressionText(tokens []token, start, end int) string {
	var out strings.Builder
	for i := start; i < end && out.Len() < 48; i++ {
		if i > start && tokens[i].kind == 'i' && tokens[i-1].kind == 'i' {
			out.WriteByte(' ')
		}
		out.WriteString(tokens[i].text)
	}
	text := out.String()
	if len(text) > 48 {
		text = text[:45] + "..."
	}
	return text
}

// Protocol values are names such as "session.start" or "click"; glob
// patterns, escapes, and "[object X]" tags are lookup tables.
func nameLikeCases(cases []string) bool {
	named := 0
	for _, value := range cases {
		if value != "" && (value[0] >= 'a' && value[0] <= 'z' || value[0] >= 'A' && value[0] <= 'Z') &&
			!strings.ContainsAny(value, " *[]\\()") {
			named++
		}
	}
	return named*4 >= len(cases)*3
}

func typeName(value string) bool {
	switch value {
	case "string", "number", "boolean", "object", "function", "undefined", "symbol", "bigint":
		return true
	}
	return false
}
