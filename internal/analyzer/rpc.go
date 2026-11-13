package analyzer

import (
	"regexp"
	"strings"
)

// RPC methods whose service and method names are written as separate strings
// are not paths, so the endpoint rules never see them. Two shapes are read, and
// both need a service name that ends in "Service" (as real generated stubs and
// RPC clients name them), so ordinary dotted names such as "Symbol.iterator"
// or "React.Fragment" and event names never match:
//
//   - a dotted string, "FooService.DoThing", whose method starts with a capital;
//   - a service stub: a function body that holds one "FooService" string and
//     registers methods through a helper, one call per method, as in
//     this.get = register(this, "getFoo"). A helper counts only after it has
//     registered at least minStubMethods methods in bodies with a service name,
//     so method binding such as bind(this, "onClick") is not read as RPC.
//
// Values are "Service.method". Path-style RPC ("/pkg.Service/Method") is already
// an endpoint and is not read again.
const (
	minStubMethods = 3
	// maxBodyScan bounds the search for the braces around a registration.
	maxBodyScan = 32 << 10
	// maxStringScan bounds the search for the other quote of a string.
	maxStringScan = 2 << 10
)

var serviceString = regexp.MustCompile(`["'` + "`" + `]([A-Z][A-Za-z0-9]*Service)["'` + "`" + `]`)

// RPCMethods returns the sorted, unique RPC methods declared in source.
func RPCMethods(source string) []string {
	if !strings.Contains(source, "Service") {
		return nil
	}
	methods := set()
	dottedRPCMethods(source, methods)
	stubRPCMethods(source, methods)
	if len(methods) == 0 {
		return nil
	}
	return sorted(methods)
}

func dottedRPCMethods(source string, methods map[string]struct{}) {
	const marker = "Service."
	for from := 0; ; {
		index := strings.Index(source[from:], marker)
		if index < 0 {
			return
		}
		end := from + index + len(marker) // first byte of the method
		from = end
		start := from - len(marker)
		for start > 0 && isAlphaNumeric(source[start-1]) {
			start--
		}
		if start == from-len(marker) || start == 0 || source[start] < 'A' || source[start] > 'Z' || !isQuote(source[start-1]) {
			continue
		}
		methodEnd := end
		for methodEnd < len(source) && isAlphaNumeric(source[methodEnd]) {
			methodEnd++
		}
		if methodEnd == end || source[end] < 'A' || source[end] > 'Z' || methodEnd >= len(source) || source[methodEnd] != source[start-1] {
			continue
		}
		methods[source[start:end-1]+"."+source[end:methodEnd]] = struct{}{}
	}
}

type stubCall struct {
	helper, method string
	body           [2]int
	service        string
}

func stubRPCMethods(source string, methods map[string]struct{}) {
	var calls []stubCall
	var body [2]int
	var service string
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
		if at < body[0] || at >= body[1] {
			start, end, found := enclosingBody(source, at)
			if !found {
				continue
			}
			body, service = [2]int{start, end}, ""
			if names := serviceStrings(source[start:end]); len(names) == 1 {
				service = names[0]
			}
		}
		if service != "" {
			calls = append(calls, stubCall{helper: helper, method: method, service: service})
		}
	}
	perHelper := map[string]int{}
	for _, call := range calls {
		perHelper[call.helper]++
	}
	for _, call := range calls {
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

// serviceStrings returns the distinct "FooService" strings of a body.
func serviceStrings(body string) []string {
	var names []string
	for _, match := range serviceString.FindAllStringSubmatch(body, -1) {
		found := false
		for _, name := range names {
			found = found || name == match[1]
		}
		if !found {
			names = append(names, match[1])
		}
	}
	return names
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

// stringStart returns the opening quote of the string that ends at index close.
func stringStart(source string, close int) int {
	quote := source[close]
	for i := close - 1; i >= 0 && close-i <= maxStringScan; i-- {
		if source[i] == '\n' && quote != '`' {
			return -1
		}
		if source[i] == quote && escapes(source, i)%2 == 0 {
			return i
		}
	}
	return -1
}

// stringEnd returns the closing quote of the string that opens at index open.
func stringEnd(source string, open int) int {
	quote := source[open]
	for i := open + 1; i < len(source) && i-open <= maxStringScan; i++ {
		switch source[i] {
		case '\\':
			i++
		case '\n':
			if quote != '`' {
				return -1
			}
		case quote:
			return i
		}
	}
	return -1
}

// escapes counts the backslashes right before index i.
func escapes(source string, i int) int {
	count := 0
	for i--; i >= 0 && source[i] == '\\'; i-- {
		count++
	}
	return count
}

func skipSpace(source string, i int) int {
	for i < len(source) && isASCIIWhitespace(source[i]) {
		i++
	}
	return i
}

func skipSpaceBack(source string, i int) int {
	for i >= 0 && isASCIIWhitespace(source[i]) {
		i--
	}
	return i
}

func isQuote(b byte) bool { return b == '"' || b == '\'' || b == '`' }
func isAlphaNumeric(b byte) bool {
	return b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z' || b >= '0' && b <= '9'
}
func isIdentifierByte(b byte) bool { return isAlphaNumeric(b) || b == '_' || b == '$' }
