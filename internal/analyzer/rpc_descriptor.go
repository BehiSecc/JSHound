package analyzer

import "strings"

// Two declaration families name a method in an object literal.
//
// A Connect-ES service descriptor (generated *_connect output) holds the full
// service name and one entry per method that carries its name and its kind:
//
//	{ typeName: "pkg.Name", methods: { key: { name: "Method", I: In, O: Out, kind: MethodKind.Unary } } }
//
// A JSON-RPC message is an object with a jsonrpc version and a method name:
//
//	{ jsonrpc: "2.0", id: 1, method: "name", params: {} }
//
// JSON-RPC methods are reported under a fixed service label, so the value has
// the same "Service.Method" shape as every other RPC method.
//
// Other generators (protobuf-ts, ts-proto) are not read: no generated output of
// them was available to check the shape against.
const (
	jsonRPCService = "JSONRPC"
	// maxDescriptorScan bounds an object literal that is read as a descriptor.
	maxDescriptorScan = 256 << 10
)

// connectDescriptors adds the methods of Connect-ES service descriptors.
func (s *rpcScan) connectDescriptors() {
	source := s.source
	const key = "typeName"
	for from := 0; ; {
		index := strings.Index(source[from:], key)
		if index < 0 {
			return
		}
		at := from + index
		from = at + len(key)
		if at > 0 && isIdentifierByte(source[at-1]) {
			continue
		}
		cursor := skipSpace(source, from)
		if cursor >= len(source) || source[cursor] != ':' {
			continue
		}
		name, next, ok := stringValue(source, skipSpace(source, cursor+1), func(b byte) bool { return isAlphaNumeric(b) || b == '_' || b == '.' })
		if !ok {
			continue
		}
		service := name[strings.LastIndexByte(name, '.')+1:]
		if !capitalIdentifier(service) {
			continue
		}
		// methods: { ... } is the next property of the same object.
		cursor = skipSpace(source, next)
		if cursor >= len(source) || source[cursor] != ',' {
			continue
		}
		cursor = skipSpace(source, cursor+1)
		if !strings.HasPrefix(source[cursor:], "methods") {
			continue
		}
		cursor = skipSpace(source, cursor+len("methods"))
		if cursor+1 >= len(source) || source[cursor] != ':' {
			continue
		}
		open := skipSpace(source, cursor+1)
		if open >= len(source) || source[open] != '{' {
			continue
		}
		end, _, ok := matchBracket(source, open, open+maxDescriptorScan)
		if !ok {
			continue
		}
		for i := open + 1; i < end; i++ {
			switch source[i] {
			case '"', '\'', '`':
				if close := stringEnd(source, i); close > 0 {
					i = close
				}
			case '{':
				entryEnd, _, ok := matchBracket(source, i, end)
				if !ok {
					break
				}
				// An entry has a name and a kind from MethodKind; any other
				// object in the map is not a method.
				method, found := objectString(source, i, entryEnd+1, "name", isIdentifierByte)
				if found && strings.Contains(source[i:entryEnd], "MethodKind") {
					s.methods[service+"."+method] = struct{}{}
				}
				i = entryEnd
			}
		}
	}
}

// jsonRPCMethods adds the methods of JSON-RPC message objects.
func (s *rpcScan) jsonRPCMethods() {
	source := s.source
	const key = "jsonrpc"
	for from := 0; ; {
		index := strings.Index(source[from:], key)
		if index < 0 {
			return
		}
		at := from + index
		from = at + len(key)
		if at > 0 && isIdentifierByte(source[at-1]) {
			continue
		}
		// A quoted key starts at its quote, which must not be read as a string end.
		keyStart := at
		if at > 0 && isQuote(source[at-1]) {
			keyStart--
		}
		start, end, ok := enclosingBody(source, keyStart)
		if !ok || source[start] != '{' {
			continue
		}
		if version, ok := objectString(source, start, end, key, isJSONRPCVersion); !ok || version != "2.0" {
			continue
		}
		if method, ok := objectString(source, start, end, "method", isJSONRPCMethodByte); ok && method[0] != '.' && method[0] != '/' && method[0] != '-' {
			s.methods[jsonRPCService+"."+method] = struct{}{}
		}
	}
}

func isJSONRPCVersion(b byte) bool { return b >= '0' && b <= '9' || b == '.' }
func isJSONRPCMethodByte(b byte) bool {
	return isAlphaNumeric(b) || b == '_' || b == '$' || b == '.' || b == '/' || b == ':' || b == '-'
}

// stringValue reads a quoted string at index i whose bytes all satisfy allowed
// and returns its value and the index after the closing quote.
func stringValue(source string, i int, allowed func(byte) bool) (string, int, bool) {
	if i >= len(source) || source[i] != '"' && source[i] != '\'' {
		return "", 0, false
	}
	quote := source[i]
	start := i + 1
	for j := start; j < len(source) && j-start <= 128; j++ {
		switch b := source[j]; {
		case b == quote:
			return source[start:j], j + 1, j > start
		case !allowed(b):
			return "", 0, false
		}
	}
	return "", 0, false
}

// objectString finds the property key of the object literal source[open:end],
// directly inside it and not in a nested object, and returns its string value.
func objectString(source string, open, end int, key string, allowed func(byte) bool) (string, bool) {
	depth := 0
	for i := open; i < end && i < len(source); i++ {
		switch c := source[i]; c {
		case '"', '\'', '`':
			if close := stringEnd(source, i); close > 0 {
				i = close
			}
		case '(', '[', '{', ',':
			if c == ',' && depth != 1 {
				continue
			}
			if c != ',' {
				depth++
				if depth != 1 {
					continue
				}
			}
			if value, ok := propertyString(source, i+1, key, allowed); ok {
				return value, true
			}
		case ')', ']', '}':
			depth--
		}
	}
	return "", false
}

// propertyString reads key: "value" at index i, with the key quoted or not.
func propertyString(source string, i int, key string, allowed func(byte) bool) (string, bool) {
	i = skipSpace(source, i)
	if i < len(source) && isQuote(source[i]) {
		i++
	}
	if !strings.HasPrefix(source[i:], key) {
		return "", false
	}
	i += len(key)
	if i < len(source) && isQuote(source[i]) {
		i++
	}
	i = skipSpace(source, i)
	if i >= len(source) || source[i] != ':' {
		return "", false
	}
	value, _, ok := stringValue(source, skipSpace(source, i+1), allowed)
	return value, ok
}
