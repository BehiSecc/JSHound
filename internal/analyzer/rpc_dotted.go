package analyzer

import "strings"

// A string "Name.Method" with no slash is ambiguous: Symbol.iterator,
// Intl.DateTimeFormat, React.Fragment, a polyfill name, and a translation key
// all look alike, and a call that takes the string says nothing more. It is
// read as an RPC method only on evidence, by either of two rules:
//
//   - the file already declares Name as an RPC service in a shape that is
//     conclusive on its own (a path, a registration, a stub), so the string is
//     one more method of a known service; or
//   - Name reads like a service (Service, Api, Rpc).
//
// The method must be a capitalised word with a lower-case letter in it, which
// leaves out constants such as Math.PI, and the name must not be a global of
// the language or the browser.
type dottedUse struct{ name, method string }

// platformNames are globals whose properties are written as "Name.Property".
var platformNames = setOf(
	"Array", "ArrayBuffer", "Atomics", "BigInt", "Boolean", "DataView", "Date", "Error", "Function", "Intl",
	"JSON", "Map", "Math", "Number", "Object", "Promise", "Proxy", "Reflect", "RegExp", "Set", "String",
	"Symbol", "WeakMap", "WeakSet", "WebAssembly", "Document", "Window", "Element", "Node", "Event",
	"Navigator", "Notification", "CSS", "React", "ReactDOM", "Fragment", "Temporal", "XMLHttpRequest",
	"WebSocket", "Headers", "Request", "Response", "URL", "Blob", "File", "Image", "Audio", "Text",
	"Range", "Selection", "Performance", "Storage", "History", "Location", "Screen", "Component",
	"PropTypes", "Zone", "Buffer", "Process", "Console", "Worker", "Crypto", "AsyncContext",
)

// dottedParts reads "Name.Method" with nothing else around it.
func dottedParts(value string) (name, method string, ok bool) {
	name, method, found := strings.Cut(value, ".")
	if !found || !capitalIdentifier(name) || !capitalIdentifier(method) {
		return "", "", false
	}
	return name, method, true
}

// dottedMethods adds the dotted names that pass either rule. It runs after the
// other recognizers, whose services it consults.
func (s *rpcScan) dottedMethods() {
	if len(s.dotted) == 0 {
		return
	}
	known := set()
	for method := range s.methods {
		service, _, _ := strings.Cut(method, ".")
		known[service] = struct{}{}
	}
	for _, use := range s.dotted {
		if _, global := platformNames[use.name]; global || !hasLower(use.method) {
			continue
		}
		if _, declared := known[use.name]; declared || serviceLike(use.name) {
			s.methods[use.name+"."+use.method] = struct{}{}
		}
	}
}
