package analyzer

import "strings"

// A service stub is a function body that names its service in a string and
// registers one method per call through a helper, as in
// this.get = register(this, "getFoo").
//
// When the file holds the helper, the stub is read from the helper's own
// definition: a function of two parameters that returns a function passing a
// property of the first parameter and the second parameter to one call, as in
// function H(a, b) { return function () { return a.client.call(a.P, b) } }.
// The property is where the service name lives, so the service is the one
// string that the body assigns to this.P, whatever it is called. That is
// conclusive and needs no suffix and no minimum count.
//
// A helper that cannot be resolved (its definition is in another file) is read
// from weaker evidence that has to add up: the body assigns exactly one
// service-shaped string to a this property, the name reads like a service, and
// the helper registers at least minStubMethods methods in such bodies. Method
// binding such as bind(this, "onClick") and field definitions such as
// __publicField(this, "name", value) are never registrations.
const minStubMethods = 3

// notRegistrars are helpers that take (this, "name") for other reasons.
var notRegistrars = setOf("bind", "bindAll", "call", "apply", "defineProperty", "__publicField", "_defineProperty", "__defNormalProp", "addEventListener", "on", "once", "emit", "trigger")

type stubCall struct {
	helper, method string
	service        string
}

func stubRPCMethods(source string, methods map[string]struct{}) {
	if !strings.Contains(source, "this") {
		return
	}
	var helpers map[string][]string
	helpersRead := false
	var weak []stubCall
	var body [2]int
	var assigned []thisString
	var weakService string
	for from := 0; ; {
		index := strings.Index(source[from:], "this")
		if index < 0 {
			break
		}
		at := from + index
		from = at + len("this")
		helper, method, ok := registration(source, at)
		if !ok {
			continue
		}
		if _, no := notRegistrars[helper]; no {
			continue
		}
		if at < body[0] || at >= body[1] {
			start, end, found := enclosingBody(source, at)
			if !found {
				continue
			}
			body = [2]int{start, end}
			assigned = thisStrings(source[start:end])
			weakService = weakServiceName(assigned)
		}
		if !helpersRead {
			helpers, helpersRead = registrationHelpers(source), true
		}
		if service, ok := resolvedService(assigned, helpers[helper]); ok {
			methods[service+"."+method] = struct{}{}
		} else if weakService != "" {
			weak = append(weak, stubCall{helper: helper, method: method, service: weakService})
		}
	}
	perHelper := map[string]int{}
	for _, call := range weak {
		perHelper[call.helper]++
	}
	for _, call := range weak {
		if perHelper[call.helper] >= minStubMethods {
			methods[call.service+"."+call.method] = struct{}{}
		}
	}
}

// registration reads helper(this, "method") around the "this" at index at and
// returns the plain helper function name and the method.
func registration(source string, at int) (helper, method string, ok bool) {
	if at > 0 && isIdentifierByte(source[at-1]) || at+4 < len(source) && isIdentifierByte(source[at+4]) {
		return "", "", false
	}
	open := skipSpaceBack(source, at-1)
	if open < 1 || source[open] != '(' || !isIdentifierByte(source[open-1]) {
		return "", "", false
	}
	start := open
	for start > 0 && isIdentifierByte(source[start-1]) {
		start--
	}
	// "obj.helper(", "new Helper(", and "Helper.call(" are not plain helpers.
	if source[start] >= '0' && source[start] <= '9' || start > 0 && source[start-1] == '.' || strings.HasSuffix(source[:start], "new ") {
		return "", "", false
	}
	helper = source[start:open]
	switch helper {
	case "if", "for", "while", "switch", "catch", "return", "typeof", "function", "with", "void", "await", "yield":
		return "", "", false
	}
	cursor := skipSpace(source, at+4)
	if cursor >= len(source) || source[cursor] != ',' {
		return "", "", false
	}
	cursor = skipSpace(source, cursor+1)
	if cursor >= len(source) || source[cursor] != '"' && source[cursor] != '\'' {
		return "", "", false
	}
	quote := source[cursor]
	nameStart := cursor + 1
	nameEnd := nameStart
	for nameEnd < len(source) && isIdentifierByte(source[nameEnd]) {
		nameEnd++
	}
	if nameEnd == nameStart || nameEnd >= len(source) || source[nameEnd] != quote || source[nameStart] >= '0' && source[nameStart] <= '9' {
		return "", "", false
	}
	cursor = skipSpace(source, nameEnd+1)
	if cursor >= len(source) || source[cursor] != ')' {
		return "", "", false
	}
	return helper, source[nameStart:nameEnd], true
}

// thisString is a string assigned to a property of this, as in this.P = "v".
type thisString struct{ property, value string }

// thisStrings returns the strings that a body assigns to properties of this.
func thisStrings(body string) []thisString {
	var result []thisString
	for from := 0; ; {
		index := strings.Index(body[from:], "this.")
		if index < 0 {
			return result
		}
		at := from + index
		from = at + len("this.")
		if at > 0 && isIdentifierByte(body[at-1]) {
			continue
		}
		end := from
		for end < len(body) && isIdentifierByte(body[end]) {
			end++
		}
		cursor := skipSpace(body, end)
		if end == from || cursor+1 >= len(body) || body[cursor] != '=' || body[cursor+1] == '=' {
			continue
		}
		cursor = skipSpace(body, cursor+1)
		if cursor >= len(body) || body[cursor] != '"' && body[cursor] != '\'' {
			continue
		}
		close := strings.IndexByte(body[cursor+1:], body[cursor])
		if close < 0 || strings.ContainsAny(body[cursor+1:cursor+1+close], "\\\n") {
			continue
		}
		result = append(result, thisString{property: body[from:end], value: body[cursor+1 : cursor+1+close]})
	}
}

// serviceName reads a service name from an assigned string: a capitalised
// identifier, optionally behind a package ("pkg.v1.Name").
func serviceName(value string) (string, bool) {
	name := value[strings.LastIndexByte(value, '.')+1:]
	if !capitalIdentifier(name) {
		return "", false
	}
	for _, part := range strings.Split(strings.TrimSuffix(value, name), ".") {
		if part != "" && !isIdentifier(part) {
			return "", false
		}
	}
	return name, true
}

func isIdentifier(value string) bool {
	for i := 0; i < len(value); i++ {
		if !isIdentifierByte(value[i]) {
			return false
		}
	}
	return value != ""
}

// resolvedService returns the service of a body for a helper that forwards the
// given properties: the only string the body assigns to such a property.
func resolvedService(assigned []thisString, properties []string) (string, bool) {
	for _, property := range properties {
		value, count := "", 0
		for _, item := range assigned {
			if item.property == property && item.value != value {
				value, count = item.value, count+1
			}
		}
		if count != 1 {
			continue
		}
		if name, ok := serviceName(value); ok {
			return name, true
		}
	}
	return "", false
}

// weakServiceName returns the service of a body when it assigns exactly one
// service-shaped string to a this property and the name reads like a service.
func weakServiceName(assigned []thisString) string {
	found := ""
	for _, item := range assigned {
		name, ok := serviceName(item.value)
		if !ok {
			continue
		}
		if found != "" && found != name {
			return ""
		}
		found = name
	}
	if serviceLike(found) {
		return found
	}
	return ""
}

// registrationHelpers finds the helper functions of the file that match the
// definition above and returns the property each one forwards.
func registrationHelpers(source string) map[string][]string {
	const marker = "{return function"
	helpers := map[string][]string{}
	for from := 0; ; {
		index := strings.Index(source[from:], marker)
		if index < 0 {
			return helpers
		}
		at := from + index
		from = at + len(marker)
		name, first, second, ok := twoParameterFunction(source, at)
		if !ok {
			continue
		}
		body, ok := returnedFunctionBody(source, from)
		if !ok {
			continue
		}
		if property, ok := forwardedProperty(body, first, second); ok {
			helpers[name] = append(helpers[name], property)
		}
	}
}

// twoParameterFunction reads "function name(a,b)" or "name=function(a,b)" right
// before the brace at index brace.
func twoParameterFunction(source string, brace int) (name, first, second string, ok bool) {
	closer := skipSpaceBack(source, brace-1)
	if closer < 0 || source[closer] != ')' {
		return "", "", "", false
	}
	opener := closer - 1
	for opener >= 0 && source[opener] != '(' && closer-opener <= 80 {
		opener--
	}
	if opener < 0 || source[opener] != '(' {
		return "", "", "", false
	}
	params := strings.Split(source[opener+1:closer], ",")
	if len(params) != 2 {
		return "", "", "", false
	}
	first, second = strings.TrimSpace(params[0]), strings.TrimSpace(params[1])
	if !isIdentifier(first) || !isIdentifier(second) {
		return "", "", "", false
	}
	word, wordStart := identifierBefore(source, opener)
	if word == "" {
		return "", "", "", false
	}
	if word == "function" {
		// name=function(a,b) or name:function(a,b)
		before := skipSpaceBack(source, wordStart-1)
		if before < 0 || source[before] != '=' && source[before] != ':' {
			return "", "", "", false
		}
		name, nameStart := identifierBefore(source, before)
		if name == "" || nameStart > 0 && source[nameStart-1] == '.' {
			return "", "", "", false
		}
		return name, first, second, true
	}
	if keyword, _ := identifierBefore(source, wordStart); keyword != "function" {
		return "", "", "", false
	}
	return word, first, second, true
}

// identifierBefore returns the identifier that ends before index end, skipping
// spaces, and where it starts.
func identifierBefore(source string, end int) (string, int) {
	stop := skipSpaceBack(source, end-1) + 1
	start := stop
	for start > 0 && isIdentifierByte(source[start-1]) {
		start--
	}
	return source[start:stop], start
}

// returnedFunctionBody returns the body of the function that starts right
// after "{return function" at index from.
func returnedFunctionBody(source string, from int) (string, bool) {
	i := skipSpace(source, from)
	for i < len(source) && isIdentifierByte(source[i]) {
		i++
	}
	i = skipSpace(source, i)
	if i >= len(source) || source[i] != '(' {
		return "", false
	}
	closer := strings.IndexByte(source[i:], ')')
	if closer < 0 || closer > 200 {
		return "", false
	}
	i = skipSpace(source, i+closer+1)
	if i >= len(source) || source[i] != '{' {
		return "", false
	}
	const maxHelperBody = 1 << 10
	depth := 0
	for end := i; end < len(source) && end-i <= maxHelperBody; end++ {
		switch source[end] {
		case '"', '\'', '`':
			if close := stringEnd(source, end); close > 0 {
				end = close
			}
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return source[i+1 : end], true
			}
		}
	}
	return "", false
}

// forwardedProperty finds the call in a helper's returned function that gets
// a property of the first parameter and the second parameter side by side, as
// in call(a.P, b) or call(b, a.P).
func forwardedProperty(body, first, second string) (string, bool) {
	prefix := first + "."
	for from := 0; ; {
		index := strings.Index(body[from:], prefix)
		if index < 0 {
			return "", false
		}
		at := from + index
		from = at + len(prefix)
		if before := skipSpaceBack(body, at-1); before < 0 || body[before] != '(' && body[before] != ',' || at > 0 && isIdentifierByte(body[at-1]) {
			continue
		}
		end := from
		for end < len(body) && isIdentifierByte(body[end]) {
			end++
		}
		if end == from {
			continue
		}
		property := body[from:end]
		if argumentNamed(body, skipSpace(body, end), second) || argumentBefore(body, at, second) {
			return property, true
		}
	}
}

// argumentNamed reports a comma and then the identifier name as a whole
// argument, starting the search at index i.
func argumentNamed(body string, i int, name string) bool {
	if i >= len(body) || body[i] != ',' {
		return false
	}
	i = skipSpace(body, i+1)
	if !strings.HasPrefix(body[i:], name) {
		return false
	}
	i = skipSpace(body, i+len(name))
	return i < len(body) && (body[i] == ',' || body[i] == ')')
}

// argumentBefore reports the identifier name as a whole argument and a comma
// right before index at.
func argumentBefore(body string, at int, name string) bool {
	i := skipSpaceBack(body, at-1)
	if i < 0 || body[i] != ',' {
		return false
	}
	i = skipSpaceBack(body, i-1)
	start := i + 1 - len(name)
	if start < 1 || body[start:i+1] != name {
		return false
	}
	before := skipSpaceBack(body, start-1)
	return before >= 0 && (body[before] == '(' || body[before] == ',') && !isIdentifierByte(body[start-1])
}

// enclosingBody returns the braces around index at: the function body that
// holds a registration. Braces inside strings are skipped. Regular expressions
// and comments are not understood, so an unbalanced result is rejected by the
// scan limit rather than guessed.
func enclosingBody(source string, at int) (start, end int, ok bool) {
	depth := 0
	for i := at - 1; i >= 0 && at-i <= maxBodyScan; i-- {
		switch c := source[i]; c {
		case '"', '\'', '`':
			if open := stringStart(source, i); open >= 0 {
				i = open
			}
		case '}':
			depth++
		case '{':
			if depth == 0 {
				start = i
				goto forward
			}
			depth--
		}
	}
	return 0, 0, false
forward:
	depth = 0
	for i := at; i < len(source) && i-at <= maxBodyScan; i++ {
		switch c := source[i]; c {
		case '"', '\'', '`':
			if close := stringEnd(source, i); close >= 0 {
				i = close
			}
		case '{':
			depth++
		case '}':
			if depth == 0 {
				return start, i + 1, true
			}
			depth--
		}
	}
	return 0, 0, false
}
