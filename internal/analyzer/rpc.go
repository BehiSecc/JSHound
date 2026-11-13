package analyzer

import (
	"strings"
)

// RPC methods are recognised by how they are declared or called, never by what
// a service happens to be named. Each recognizer reads one declaration family
// and is precise on its own; a service-like name (Service, Api, Rpc) only adds
// confidence where the structure alone is thin.
//
//   - rpc_path.go: strings that are RPC paths, "/pkg.v1.Svc/Method" with any
//     prefix or "/Svc.Method", and the wire id of a registration that pairs an
//     id with such a path in one call.
//   - rpc_stub.go: service stubs that register methods through a helper.
//   - rpc_descriptor.go: Connect-ES service descriptors and JSON-RPC messages.
//   - rpc_dotted.go: "Svc.Method" strings, accepted on evidence.
//
// Values are "Service.Method" with the service as written and no package.
const (
	// maxBodyScan bounds the search for the braces around a registration.
	maxBodyScan = 32 << 10
	// maxStringScan bounds the search for the other quote of a string.
	maxStringScan = 2 << 10
)

// RPC is what a source declares.
type RPC struct {
	// Methods are the sorted, unique "Service.Method" values.
	Methods []string
	// WireIDs maps a method to the id that is registered for it. A method that
	// is registered under several ids has them joined with a comma.
	WireIDs map[string]string
	// Paths maps a method to the root-relative path it was found through, as
	// written. A method written under several spellings keeps the fullest.
	Paths map[string]string
	// paths are all the root-relative RPC path literals, as written.
	paths []string
}

// RPCMethods returns the sorted, unique RPC methods declared in source.
func RPCMethods(source string) []string { return ReadRPC(source).Methods }

// ReadRPC reads every RPC declaration family in source.
func ReadRPC(source string) RPC {
	scan := rpcScan{source: source, methods: set(), ids: map[string]map[string]struct{}{}, paths: set()}
	scan.literals()
	stubRPCMethods(source, scan.methods)
	scan.connectDescriptors()
	scan.jsonRPCMethods()
	scan.dottedMethods()
	if len(scan.methods) == 0 {
		return RPC{}
	}
	rpc := RPC{Methods: sorted(scan.methods), paths: sorted(scan.paths)}
	if len(rpc.paths) > 0 {
		rpc.Paths = make(map[string]string, len(rpc.paths))
		for _, path := range rpc.paths {
			service, method, _ := RPCPath(path)
			name := service + "." + method
			if current, ok := rpc.Paths[name]; !ok || FullerRPCPath(path, current) {
				rpc.Paths[name] = path
			}
		}
	}
	if len(scan.ids) > 0 {
		rpc.WireIDs = make(map[string]string, len(scan.ids))
		for method, ids := range scan.ids {
			rpc.WireIDs[method] = strings.Join(sorted(ids), ",")
		}
	}
	return rpc
}

type rpcScan struct {
	source  string
	methods map[string]struct{}
	ids     map[string]map[string]struct{}
	paths   map[string]struct{}
	dotted  []dottedUse
}

// literals reads every quoted string once and hands it to the recognizers
// that work on whole strings. It reads strings the way the endpoint scanner
// does, so both see the same literals.
func (s *rpcScan) literals() {
	source := s.source
	for cursor := 0; cursor < len(source); {
		quote := source[cursor]
		if quote != '\'' && quote != '"' && quote != '`' {
			cursor++
			continue
		}
		start, end, escaped, closed := cursor+1, cursor+1, false, false
		for end < len(source) {
			b := source[end]
			if escaped {
				escaped = false
				end++
				continue
			}
			if b == '\\' {
				escaped = true
				end++
				continue
			}
			if b == quote {
				closed = true
				break
			}
			if quote != '`' && (b == '\r' || b == '\n') {
				break
			}
			end++
		}
		if !closed {
			cursor = start
			continue
		}
		if end-start <= MaxCandidateLength {
			s.literal(start, end)
		}
		cursor = end + 1
	}
}

// literal offers the string source[start:end] to the string recognizers.
func (s *rpcScan) literal(start, end int) {
	value := s.source[start:end]
	if strings.IndexByte(value, '/') >= 0 {
		if service, method, ok := RPCPath(value); ok {
			name := service + "." + method
			s.methods[name] = struct{}{}
			if value[0] == '/' && !strings.HasPrefix(value, "//") && !strings.Contains(value, "${") && !strings.Contains(value, "://") {
				s.paths[value] = struct{}{}
			}
			s.pairWireID(name, start, end)
		}
		return
	}
	if name, method, ok := dottedParts(value); ok {
		s.dotted = append(s.dotted, dottedUse{name: name, method: method})
	}
}

// serviceSuffixes are the endings that make a name read like a service. They
// add confidence where structure alone is thin and are never required.
var serviceSuffixes = []string{"Service", "Api", "API", "Rpc", "RPC"}

// serviceLike reports a service-like name: a stem and a service suffix.
func serviceLike(name string) bool {
	for _, suffix := range serviceSuffixes {
		if len(name) > len(suffix) && strings.HasSuffix(name, suffix) {
			return true
		}
	}
	return false
}

// capitalIdentifier reports [A-Z][A-Za-z0-9]*.
func capitalIdentifier(value string) bool {
	if value == "" || value[0] < 'A' || value[0] > 'Z' {
		return false
	}
	for i := 1; i < len(value); i++ {
		if !isAlphaNumeric(value[i]) {
			return false
		}
	}
	return true
}

func hasLower(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] >= 'a' && value[i] <= 'z' {
			return true
		}
	}
	return false
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
