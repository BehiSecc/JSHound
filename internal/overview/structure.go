package overview

import (
	"fmt"
	"strings"
)

func imports(tokens []token) []string {
	seen := make(map[string]struct{})
	for i, t := range tokens {
		if t.depth == 0 && t.text == "import" && at(tokens, i+1) != "(" && at(tokens, i+1) != "." &&
			i+1 < len(tokens) && tokens[i+1].kind == 's' {
			seen[stringValue(tokens[i+1].text)] = struct{}{}
		} else if t.depth == 0 && (t.text == "import" && at(tokens, i+1) != "(" && at(tokens, i+1) != "." || t.text == "export") {
			j := i + 1
			if t.text == "export" && at(tokens, j) != "*" && at(tokens, j) != "{" {
				continue
			}
			for ; j < len(tokens) && j < i+80; j++ {
				if at(tokens, j) == "from" && j+1 < len(tokens) && tokens[j+1].kind == 's' {
					seen[stringValue(tokens[j+1].text)] = struct{}{}
					break
				}
				if at(tokens, j) == ";" || j > i+1 && tokens[j].depth == 0 && tokens[j].paren == 0 && tokens[j].line > tokens[j-1].line && isDeclarationKeyword(at(tokens, j)) {
					break
				}
				if tokens[j].match > j {
					j = tokens[j].match
				}
			}
		}
		if t.text == "require" && at(tokens, i+1) == "(" && i+2 < len(tokens) {
			if value := stringValue(tokens[i+2].text); value != "" {
				seen[value] = struct{}{}
			}
		}
	}
	return sortedKeys(seen)
}

func exports(tokens []token) []string {
	seen := make(map[string]struct{})
	for i, t := range tokens {
		if t.depth != 0 {
			continue
		}
		if t.text == "export" {
			next := i + 1
			isDefault := false
			if at(tokens, next) == "default" {
				seen["default"] = struct{}{}
				isDefault = true
				next++
			}
			if at(tokens, next) == "async" || at(tokens, next) == "declare" {
				next++
			}
			switch at(tokens, next) {
			case "function", "class", "const", "let", "var", "interface", "type", "enum":
				name := at(tokens, next+1)
				if name == "*" {
					name = at(tokens, next+2)
				}
				if !isDefault && name != "" && name != "(" && name != "{" {
					seen[name] = struct{}{}
				}
			case "{":
				end := tokens[next].match
				for j := next + 1; j < end; j++ {
					if tokens[j].kind != 'i' || tokens[j].text == "as" || tokens[j].text == "type" {
						continue
					}
					name := tokens[j].text
					if at(tokens, j+1) == "as" && j+2 < end {
						name = tokens[j+2].text
						j += 2
					}
					seen[name] = struct{}{}
				}
			case "*":
				for j := next + 1; j+1 < len(tokens) && j < next+5; j++ {
					if at(tokens, j) == "from" && tokens[j+1].kind == 's' {
						seen["* from "+stringValue(tokens[j+1].text)] = struct{}{}
						break
					}
				}
			}
		}
		if t.text == "module" && at(tokens, i+1) == "." && at(tokens, i+2) == "exports" {
			seen["module.exports"] = struct{}{}
		}
		if t.text == "exports" && at(tokens, i+1) == "." && i+2 < len(tokens) && tokens[i+2].kind == 'i' {
			seen[tokens[i+2].text] = struct{}{}
		}
	}
	return sortedKeys(seen)
}

func declarations(tokens []token) []unitRange {
	return withClassMethods(tokens, declarationsAtDepth(tokens, 0, 0, 0, len(tokens)))
}

func declarationsAtDepth(tokens []token, depth, paren int16, start, end int) []unitRange {
	var units []unitRange
	seen := make(map[string]struct{})
	for i := start; i < end; i++ {
		if tokens[i].depth != depth || tokens[i].paren != paren {
			continue
		}
		var unit unitRange
		switch tokens[i].text {
		case "function":
			prev := at(tokens, i-1)
			if prev == "async" {
				prev = at(tokens, i-2)
			}
			if prev == "=" || prev == ":" || prev == "(" || prev == "," || prev == "return" {
				continue
			}
			if at(tokens, i+1) == "(" && at(tokens, i-1) != "default" {
				continue
			}
			unit = functionUnit(tokens, i, end)
		case "class":
			unit = classUnit(tokens, i, end)
		case "const", "let", "var":
			unit = variableUnit(tokens, i, end)
		}
		if unit.Name == "" || isBundlerHelper(unit.Name) {
			continue
		}
		if _, exists := seen[unit.Name]; exists {
			continue
		}
		seen[unit.Name] = struct{}{}
		units = append(units, unit)
	}
	return units
}

func functionUnit(tokens []token, i, end int) unitRange {
	nameAt := i + 1
	if at(tokens, nameAt) == "*" {
		nameAt++
	}
	name := at(tokens, nameAt)
	if name == "(" {
		name = "default"
	}
	if name == "" || name == "{" {
		return unitRange{}
	}
	open := nameAt + 1
	if name == "default" {
		open = nameAt
	}
	for open < end && at(tokens, open) != "(" && tokens[open].depth == tokens[i].depth {
		open++
	}
	if open >= end || tokens[open].match < 0 {
		return unitRange{}
	}
	for j := tokens[open].match + 1; j < end && j < tokens[open].match+40; j++ {
		if at(tokens, j) == "{" && tokens[j].match > j {
			return unitRange{Unit: Unit{Name: name, Kind: "function", Line: tokens[i].line}, start: j + 1, end: tokens[j].match, nameAt: functionNameAt(name, nameAt)}
		}
		if at(tokens, j) == ";" {
			break
		}
	}
	return unitRange{}
}

func functionNameAt(name string, at int) int {
	if name == "default" {
		return -1
	}
	return at
}

func classUnit(tokens []token, i, end int) unitRange {
	name := at(tokens, i+1)
	if name == "" || name == "{" || name == "extends" {
		return unitRange{}
	}
	for j := i + 2; j < end; j++ {
		if tokens[j].depth != tokens[i].depth {
			continue
		}
		if tokens[j].text == "{" && tokens[j].match > j {
			return unitRange{Unit: Unit{Name: name, Kind: "class", Line: tokens[i].line}, start: j + 1, end: tokens[j].match, nameAt: i + 1}
		}
		if tokens[j].text == ";" {
			break
		}
	}
	return unitRange{}
}

func variableUnit(tokens []token, i, end int) unitRange {
	if i+2 >= end || tokens[i+1].kind != 'i' {
		return unitRange{}
	}
	name := tokens[i+1].text
	equal := i + 2
	for equal < end && equal < i+32 && tokens[equal].depth == tokens[i].depth {
		if at(tokens, equal) == "=" {
			break
		}
		if at(tokens, equal) == ";" {
			return unitRange{}
		}
		equal++
	}
	if equal >= end || at(tokens, equal) != "=" {
		return unitRange{}
	}
	limit := statementEnd(tokens, equal+1, end, tokens[i].depth, tokens[i].paren)
	if at(tokens, equal+1) == "function" || at(tokens, equal+1) == "async" && at(tokens, equal+2) == "function" {
		start := equal + 1
		if at(tokens, start) == "async" {
			start++
		}
		for j := start + 1; j < limit; j++ {
			if at(tokens, j) == "{" && tokens[j].match > j {
				return unitRange{Unit: Unit{Name: name, Kind: "function", Line: tokens[i].line}, start: j + 1, end: tokens[j].match, nameAt: i + 1}
			}
		}
	}
	for j := equal + 1; j < limit; j++ {
		if at(tokens, j) != "=>" || tokens[j].depth != tokens[i].depth {
			continue
		}
		body := j + 1
		if at(tokens, body) == "{" && tokens[body].match > body {
			return unitRange{Unit: Unit{Name: name, Kind: "function", Line: tokens[i].line}, start: body + 1, end: tokens[body].match, nameAt: i + 1}
		}
		return unitRange{Unit: Unit{Name: name, Kind: "function", Line: tokens[i].line}, start: body, end: limit, nameAt: i + 1}
	}
	return unitRange{}
}

func statementEnd(tokens []token, start, end int, depth, paren int16) int {
	for j := start; j < end; j++ {
		if tokens[j].depth != depth || tokens[j].paren != paren {
			continue
		}
		if tokens[j].text == ";" {
			return j
		}
		if j > start && tokens[j].line > tokens[j-1].line {
			if isDeclarationKeyword(tokens[j].text) || canEndExpression(tokens[j-1]) && canStartStatement(tokens[j]) {
				return j
			}
		}
	}
	return end
}

func canEndExpression(t token) bool {
	return t.kind == 'i' || t.kind == 's' || t.kind == 'n' || t.text == ")" || t.text == "]" || t.text == "}"
}

func canStartStatement(t token) bool {
	return t.kind == 'i' && t.text != "as" && t.text != "satisfies" && t.text != "in" && t.text != "instanceof"
}

func isDeclarationKeyword(value string) bool {
	switch value {
	case "const", "let", "var", "function", "class", "export", "import":
		return true
	}
	return false
}

func looksLikeIIFE(tokens []token) bool {
	if len(tokens) < 8 {
		return false
	}
	if tokens[0].text != "(" && tokens[0].text != "!" && tokens[0].text != ";" {
		return false
	}
	for i := 0; i < min(12, len(tokens)); i++ {
		if tokens[i].text == "function" || tokens[i].text == "=>" {
			return true
		}
	}
	return false
}

// iifeDeclarations reads declarations from the function body that wraps most
// of the file. In UMD output that is the factory, not the small wrapper.
func iifeDeclarations(tokens []token) []unitRange {
	for i := 0; i < min(len(tokens), 400); i++ {
		t := tokens[i]
		if t.text != "{" || t.match <= i+1 || (t.match-i)*2 < len(tokens) {
			continue
		}
		if prev := at(tokens, i-1); prev != ")" && prev != "=>" {
			continue
		}
		inner := tokens[i+1]
		return withClassMethods(tokens, declarationsAtDepth(tokens, inner.depth, inner.paren, i+1, t.match))
	}
	return nil
}

func withClassMethods(tokens []token, units []unitRange) []unitRange {
	result := make([]unitRange, 0, len(units))
	for _, unit := range units {
		result = append(result, unit)
		if unit.Kind != "class" || unit.start >= len(tokens) {
			continue
		}
		depth, paren := tokens[unit.start].depth, tokens[unit.start].paren
		for i := unit.start; i+1 < unit.end; i++ {
			if tokens[i].depth != depth || tokens[i].paren != paren || tokens[i].kind != 'i' || at(tokens, i+1) != "(" {
				continue
			}
			close := tokens[i+1].match
			if close < 0 || close >= unit.end {
				continue
			}
			body := close + 1
			for body < unit.end && body < close+20 && at(tokens, body) != "{" && at(tokens, body) != ";" {
				body++
			}
			if at(tokens, body) != "{" || tokens[body].match <= body {
				continue
			}
			result = append(result, unitRange{
				Unit:  Unit{Name: unit.Name + "." + tokens[i].text, Kind: "method", Line: tokens[i].line},
				start: body + 1, end: tokens[body].match, nameAt: i,
			})
			i = tokens[body].match
		}
	}
	return result
}

type call struct {
	name     string
	receiver string
	line     int
	index    int
	open     int
}

func (c call) display() string {
	if c.receiver == ")" || c.receiver == "]" {
		return c.name + "() on returned value"
	}
	if c.receiver != "" {
		return c.receiver + "." + c.name + "()"
	}
	return c.name + "()"
}

func topLevelAction(tokens []token, c call) string {
	action := c.display()
	args := argumentRanges(tokens, c.open)
	if len(args) == 0 {
		return action
	}
	first := args[0][0]
	if c.name == "render" && at(tokens, first) == "<" {
		component := at(tokens, first+1)
		if component != "" && component[0] >= 'A' && component[0] <= 'Z' {
			return action + " with <" + component + ">"
		}
	}
	if c.name == "createApp" && args[0][1]-first == 1 && tokens[first].kind == 'i' {
		return action + " with " + tokens[first].text
	}
	return action
}

func callsIn(tokens []token, start, end int) []call {
	var result []call
	for i := start; i+1 < end; i++ {
		if tokens[i].kind != 'i' || at(tokens, i+1) != "(" || tokens[i+1].match < 0 {
			continue
		}
		if !isCall(tokens, i) {
			continue
		}
		receiver := ""
		if at(tokens, i-1) == "." || at(tokens, i-1) == "?." {
			receiver = at(tokens, i-2)
		}
		result = append(result, call{name: tokens[i].text, receiver: receiver, line: tokens[i].line, index: i, open: i + 1})
	}
	return result
}

// Calls inside nested function bodies are not direct calls from their enclosing
// unit. This includes event, timer, promise, and locally stored callbacks.
func nestedFunctionMask(tokens []token, start, end int) []bool {
	masked := make([]bool, end-start)
	mark := func(from, to int) {
		for i := max(start, from); i < min(end, to); i++ {
			masked[i-start] = true
		}
	}
	for i := start; i < end; i++ {
		if at(tokens, i) == "=>" && i+1 < end {
			body := i + 1
			if at(tokens, body) == "{" && tokens[body].match > body {
				mark(body+1, tokens[body].match)
				continue
			}
			bodyEnd := end
			for j := body + 1; j < end; j++ {
				if tokens[j].depth < tokens[body].depth || tokens[j].paren < tokens[body].paren ||
					tokens[j].depth == tokens[body].depth && tokens[j].paren == tokens[body].paren && (at(tokens, j) == "," || at(tokens, j) == ";") ||
					tokens[j].line > tokens[j-1].line && canEndExpression(tokens[j-1]) && canStartStatement(tokens[j]) {
					bodyEnd = j
					break
				}
			}
			mark(body, bodyEnd)
		}
		if at(tokens, i) == "function" {
			for j := i + 1; j < end && j < i+30; j++ {
				if at(tokens, j) == "{" && tokens[j].match > j {
					mark(j+1, tokens[j].match)
					break
				}
			}
		}
	}
	return masked
}

func isCall(tokens []token, i int) bool {
	switch tokens[i].text {
	case "if", "for", "while", "switch", "catch", "with", "function", "typeof", "import", "return",
		"await", "yield", "new", "delete", "void", "throw", "case", "do", "else", "in", "instanceof", "extends":
		return false // a keyword followed by a parenthesized operand
	}
	if at(tokens, i-1) == "function" {
		return false
	}
	close := tokens[i+1].match
	if close >= 0 && at(tokens, close+1) == "{" {
		return false // class or object method definition
	}
	if close >= 0 && at(tokens, close+1) == "=>" {
		return false // "async (x) => ..." is an arrow function, not a call to async
	}
	return true
}

func callInteractions(tokens []token, c call) []string {
	switch c.name {
	case "fetch":
		if c.receiver == "" || c.receiver == "window" || c.receiver == "globalThis" {
			return []string{"fetch() calls"}
		}
	case "get", "post", "put", "patch", "delete", "request":
		if c.receiver == "axios" {
			return []string{"axios request methods"}
		}
	case "WebSocket":
		return []string{"WebSocket"}
	case "EventSource":
		return []string{"server-sent events"}
	case "Worker", "SharedWorker":
		return []string{"web worker"}
	case "postMessage":
		return []string{"message passing"}
	case "setTimeout", "setInterval", "requestAnimationFrame":
		return []string{"timers or animation frame"}
	case "createElement", "appendChild", "replaceChildren":
		if c.receiver == "document" || c.name != "createElement" {
			return []string{"DOM updates"}
		}
	case "pushState", "replaceState":
		if c.receiver == "history" {
			return []string{"browser navigation"}
		}
	case "addEventListener":
		return []string{"event listeners"}
	}
	return nil
}

func tokenInteractions(tokens []token, i int) []string {
	if at(tokens, i-1) == "." && (at(tokens, i) == "textContent" || at(tokens, i) == "innerHTML" || at(tokens, i) == "outerHTML") && at(tokens, i+1) == "=" {
		return []string{"DOM updates"}
	}
	if storage := storageUse(tokens, i); storage != "" {
		return []string{storage}
	}
	return nil
}

func addEventEntry(file *File, tokens []token, c call, registeredBy string, names map[string]struct{}, seenLinks map[string]struct{}) {
	if c.name != "addEventListener" && c.name != "on" {
		return
	}
	args := argumentRanges(tokens, c.open)
	if len(args) < 2 || args[0][1]-args[0][0] != 1 {
		return
	}
	event := stringValue(at(tokens, args[0][0]))
	if event == "" || len(event) > 48 {
		return
	}
	trigger := eventTrigger(c.receiver, event)
	callback := args[1]
	var actions []string
	if callback[1]-callback[0] == 1 {
		name := at(tokens, callback[0])
		if _, ok := names[name]; ok {
			actions = append(actions, name+"()")
		} else if name != "" && tokens[callback[0]].kind == 'i' {
			actions = append(actions, name+"()")
		}
	} else {
		for _, call := range callsIn(tokens, callback[0], callback[1]) {
			if call.receiver == "" {
				if _, ok := names[call.name]; ok {
					actions = appendUnique(actions, call.name+"()")
				}
			}
		}
	}
	if len(actions) == 0 {
		actions = []string{"inline callback"}
	}
	for _, action := range actions {
		entry := Entry{Trigger: trigger, Action: action, Line: c.line, RegisteredBy: registeredBy}
		if registeredBy == "" {
			file.EntryPoints = appendEntry(file.EntryPoints, entry)
		} else {
			file.Registrations = appendEntry(file.Registrations, entry)
		}
		if action != "inline callback" && registeredBy == "" {
			addLink(&file.Links, seenLinks, trigger, action, c.line)
		}
	}
}

func describeUnit(unit Unit) string {
	label := unit.Kind + " " + unit.Name
	if unit.OriginalName != "" {
		label += " (" + unit.OriginalName + ")"
	}
	var details []string
	if len(unit.Calls) > 0 {
		details = append(details, "calls "+strings.Join(limitStrings(unit.Calls, 4), ", "))
	}
	if len(unit.Interactions) > 0 {
		details = append(details, strings.Join(limitStrings(unit.Interactions, 3), ", "))
	}
	if len(details) > 0 {
		label += " — " + strings.Join(details, "; ")
	}
	if unit.Origin != "" {
		return fmt.Sprintf("%s (line %d · %s)", label, unit.Line, unit.Origin)
	}
	return fmt.Sprintf("%s (line %d)", label, unit.Line)
}

func limitStrings(items []string, limit int) []string {
	if len(items) <= limit {
		return items
	}
	result := append([]string{}, items[:limit]...)
	result = append(result, fmt.Sprintf("+%d more", len(items)-limit))
	return result
}
