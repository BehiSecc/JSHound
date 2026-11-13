package analyzer

import "strings"

const (
	// maxCallScan bounds how far back the call around a registration is looked
	// for, and so how long its argument list may be.
	maxCallScan = 128 << 10
	// maxWireCandidates bounds the calls tried for one path.
	maxWireCandidates = 8
	minWireIDLength   = 3
	maxWireIDLength   = 16
)

// webFileExtensions end names such as "/Index.Html" that are files, not RPC.
var webFileExtensions = setOf("html", "htm", "json", "xml", "js", "mjs", "cjs", "css", "ts", "tsx", "jsx", "txt", "md", "yml", "yaml", "csv", "pdf", "php", "asp", "aspx", "jsp")

func setOf(values ...string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}

// RPCPath reads an RPC path and returns its service and method, without the
// package. This is the one definition of a path-style RPC method; the scanner
// and the overview both use it. Two shapes are RPC:
//
//   - "pkg.v1.Service/Method": a lower-case dotted package, a capitalised
//     service, a slash, and a capitalised method, at the end of the string and
//     after any prefix ("/$rpc/", "/twirp/", a base URL);
//   - "/Service.Method": the whole string, a slash and a capitalised service
//     and method around a dot.
//
// A bare "/Settings/Profile" is a plain path: the dotted package or the dotted
// separator is what makes it RPC.
func RPCPath(path string) (service, method string, ok bool) {
	slash := strings.LastIndexByte(path, '/')
	if slash < 0 {
		return "", "", false
	}
	if slash == 0 {
		return singleSegmentRPC(path)
	}
	method = path[slash+1:]
	head := path[:slash]
	segment := head[strings.LastIndexByte(head, '/')+1:]
	dot := strings.LastIndexByte(segment, '.')
	if dot <= 0 || !capitalIdentifier(method) || !capitalIdentifier(segment[dot+1:]) {
		return "", "", false
	}
	for _, part := range strings.Split(segment[:dot], ".") {
		if !packagePart(part) {
			return "", "", false
		}
	}
	if strings.ContainsAny(path, " \t\r\n\"'`\\") {
		return "", "", false
	}
	return segment[dot+1:], method, true
}

// FullerRPCPath reports whether path carries more than other for the same
// method: the longer spelling, which has the package and prefix, and the
// lexically smaller one when they are equally long.
func FullerRPCPath(path, other string) bool {
	if len(path) != len(other) {
		return len(path) > len(other)
	}
	return path < other
}

// singleSegmentRPC reads "/Service.Method".
func singleSegmentRPC(path string) (service, method string, ok bool) {
	service, method, found := strings.Cut(path[1:], ".")
	if !found || !capitalIdentifier(service) || !capitalIdentifier(method) {
		return "", "", false
	}
	if _, file := webFileExtensions[strings.ToLower(method)]; file {
		return "", "", false
	}
	return service, method, true
}

// packagePart reports a lower-case package name: [a-z][a-z0-9_]*.
func packagePart(part string) bool {
	if part == "" || part[0] < 'a' || part[0] > 'z' {
		return false
	}
	for i := 1; i < len(part); i++ {
		if b := part[i]; !(b >= 'a' && b <= 'z' || b >= '0' && b <= '9' || b == '_') {
			return false
		}
	}
	return true
}

// pairWireID looks for the wire id of a registration whose last argument is an
// array ending with the path literal source[start:end]: a call such as
// ctor("id", A, B, [x, y, "/Svc.Method"]). The call is found by its
// parentheses, not by what stands near the path, so a long argument such as a
// class expression between the id and the array cannot pair a path with the
// wrong id.
func (s *rpcScan) pairWireID(method string, start, end int) {
	source := s.source
	i := skipSpace(source, end+1)
	if i >= len(source) || source[i] != ']' {
		return
	}
	closer := skipSpace(source, i+1)
	if closer >= len(source) || source[closer] != ')' {
		return
	}
	tried := 0
	for open := start - 1; open >= 0 && closer-open <= maxCallScan && tried < maxWireCandidates; open-- {
		if source[open] != '(' {
			continue
		}
		id, ok := wireIDAt(source, open)
		if !ok {
			continue
		}
		tried++
		if last, ok := callArguments(source, open, closer); ok && source[skipSpace(source, last)] == '[' {
			if s.ids[method] == nil {
				s.ids[method] = set()
			}
			s.ids[method][id] = struct{}{}
			return
		}
	}
}

// wireIDAt reads the first argument of the call that opens at index open when
// it is a short opaque id string followed by a comma.
func wireIDAt(source string, open int) (string, bool) {
	i := skipSpace(source, open+1)
	if i >= len(source) || source[i] != '"' && source[i] != '\'' {
		return "", false
	}
	quote := source[i]
	start := i + 1
	for i = start; i < len(source) && i-start <= maxWireIDLength; i++ {
		b := source[i]
		if b == quote {
			break
		}
		if !(isAlphaNumeric(b) || b == '_' || b == '-') {
			return "", false
		}
	}
	if i >= len(source) || source[i] != quote || i-start < minWireIDLength || i-start > maxWireIDLength {
		return "", false
	}
	if next := skipSpace(source, i+1); next >= len(source) || source[next] != ',' {
		return "", false
	}
	return source[start:i], true
}

// callArguments matches the parentheses of the call that opens at index open
// and reports where its last argument starts. The call must close at exactly
// index closer.
func callArguments(source string, open, closer int) (last int, ok bool) {
	end, last, ok := matchBracket(source, open, closer+1)
	return last, ok && end == closer
}

// matchBracket finds the bracket that closes the one at index open, looking at
// most up to index limit. It also returns where the last top-level argument
// starts (after the last comma directly inside the bracket). Strings and
// comments are skipped; regular expressions are not understood, which can only
// make a match fail or end early, never run past the limit.
func matchBracket(source string, open, limit int) (end, last int, ok bool) {
	depth := 0
	last = open + 1
	for i := open; i < limit && i < len(source); i++ {
		switch c := source[i]; c {
		case '"', '\'', '`':
			if close := stringEnd(source, i); close > 0 {
				i = close
			}
		case '/':
			if i+1 < len(source) && source[i+1] == '/' {
				newline := strings.IndexByte(source[i:], '\n')
				if newline < 0 {
					return 0, 0, false
				}
				i += newline
			} else if i+1 < len(source) && source[i+1] == '*' {
				comment := strings.Index(source[i+2:], "*/")
				if comment < 0 {
					return 0, 0, false
				}
				i += comment + 3
			}
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
			if depth == 0 {
				return i, last, true
			}
		case ',':
			if depth == 1 {
				last = i + 1
			}
		}
	}
	return 0, 0, false
}
