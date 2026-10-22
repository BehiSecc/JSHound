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
	names := make(map[string]struct{}, len(units))
	for _, unit := range units {
		names[unit.Name] = struct{}{}
	}
	result = append(result, routeDispatches(tokens, names)...)
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
		count, lookups := 0, 0
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
			if lookupCase(tokens, j+3) {
				lookups++
			}
		}
		// A switch whose cases only return constants is a lookup table.
		if count < minDispatchCases || !nameLikeCases(cases) || lookups*5 >= count*4 {
			continue
		}
		result = append(result, Dispatch{Kind: "switch", On: expressionText(tokens, i+2, closeParen), Cases: cases, Count: count, Line: tokens[i].line, index: i})
	}
	return result
}

// handlerMapDispatches finds handlers[key](...) calls whose object is a local
// literal of functions declared earlier in the same or an enclosing block.
func handlerMapDispatches(tokens []token) []Dispatch {
	lookups := make(map[string][]int) // object name -> lookup tokens
	for i := 0; i+1 < len(tokens); i++ {
		if tokens[i].kind != 'i' || at(tokens, i+1) != "[" || tokens[i+1].match < 0 || at(tokens, i-1) == "." || at(tokens, i-1) == "?." {
			continue
		}
		closeBracket := tokens[i+1].match
		next := at(tokens, closeBracket+1)
		if next == "?." {
			next = at(tokens, closeBracket+2)
		}
		if next != "(" || closeBracket == i+2 && tokens[i+2].kind != 'i' {
			continue
		}
		if len(lookups[tokens[i].text]) < 64 {
			lookups[tokens[i].text] = append(lookups[tokens[i].text], i)
		}
	}
	if len(lookups) == 0 {
		return nil
	}
	var result []Dispatch
	for i := 1; i+3 < len(tokens); i++ {
		name := tokens[i].text
		candidates, ok := lookups[name]
		if !ok || tokens[i].kind != 'i' || at(tokens, i+1) != "=" || at(tokens, i+2) != "{" {
			continue
		}
		switch at(tokens, i-1) {
		case "const", "let", "var":
		default:
			continue
		}
		blockEnd := enclosingBlockEnd(tokens, i)
		lookup := -1
		for _, candidate := range candidates {
			if candidate > i && candidate < blockEnd {
				lookup = candidate
				break
			}
		}
		if lookup < 0 {
			continue
		}
		cases, count := functionProperties(tokens, i+2)
		if count < minDispatchCases {
			continue
		}
		result = append(result, Dispatch{Kind: "handler map " + name, On: expressionText(tokens, lookup+2, tokens[lookup+1].match),
			Cases: cases, Count: count, Line: tokens[lookup].line, index: lookup})
		delete(lookups, name)
	}
	return result
}

// enclosingBlockEnd returns the closing brace of the block that contains a
// declaration, or the end of the file at top level.
func enclosingBlockEnd(tokens []token, index int) int {
	depth := tokens[index].depth
	if depth == 0 {
		return len(tokens)
	}
	for j := index - 1; j >= 0; j-- {
		if tokens[j].text == "{" && tokens[j].depth == depth-1 && tokens[j].match > index {
			return tokens[j].match
		}
	}
	return len(tokens)
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

// HTTP clients share method names with servers but send requests.
var clientReceivers = map[string]struct{}{
	"axios": {}, "http": {}, "https": {}, "api": {}, "client": {}, "request": {}, "fetch": {}, "ky": {},
	"got": {}, "superagent": {}, "$": {}, "jQuery": {}, "httpClient": {}, "apiClient": {}, "instance": {},
}

// routeDispatches groups app.get('/path', handler)-style registrations by
// receiver. The handler must be a function, or a function declared here.
func routeDispatches(tokens []token, names map[string]struct{}) []Dispatch {
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
		if _, client := clientReceivers[receiver]; client || !handlerArgument(tokens, args[len(args)-1], names) {
			continue
		}
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
func handlerArgument(tokens []token, arg [2]int, names map[string]struct{}) bool {
	first := arg[0]
	if at(tokens, first) == "async" {
		first++
	}
	switch {
	case at(tokens, first) == "function":
		return true
	case at(tokens, first) == "(" && tokens[first].match > 0 && at(tokens, tokens[first].match+1) == "=>":
		return true
	case first < len(tokens) && tokens[first].kind == 'i' && at(tokens, first+1) == "=>":
		return true
	case arg[1]-arg[0] == 1 && tokens[first].kind == 'i':
		_, ok := names[tokens[first].text]
		return ok
	}
	return false
}

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

// lookupCase reports whether a case body falls through or only returns or
// assigns a literal.
func lookupCase(tokens []token, body int) bool {
	switch at(tokens, body) {
	case "case", "default":
		return true
	case "return":
		literal := body + 1
		if at(tokens, literal) == "-" {
			literal++
		}
		if literal >= len(tokens) || tokens[literal].kind != 's' && tokens[literal].kind != 'n' && at(tokens, literal) != "true" && at(tokens, literal) != "false" && at(tokens, literal) != "null" {
			return false
		}
		next := at(tokens, literal+1)
		return next == ";" || next == "case" || next == "default" || next == "}"
	}
	if body+3 < len(tokens) && tokens[body].kind == 'i' && at(tokens, body+1) == "=" && (tokens[body+2].kind == 's' || tokens[body+2].kind == 'n') {
		next := at(tokens, body+3)
		if next == ";" || next == "," {
			following := body + 4
			if next == ";" && at(tokens, following) == "break" {
				return true
			}
		}
	}
	return false
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
