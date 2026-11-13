package overview

import (
	"regexp"
	"strings"
)

// constantKey matches property names such as LIST_ORDERS and LOGOUT.
var constantKey = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)*$`)

// minTablePaths is how many property values of an object literal must be path
// strings for it to be an endpoint table.
const minTablePaths = 3

// tableEntry is one property of a table: the constant-style key that names the
// call or screen, and the path it holds.
type tableEntry struct {
	key   string
	path  string
	token int // index of the key token
}

// tableProp is one "key: value" property of an object literal.
type tableProp struct {
	tableEntry          // path is empty unless the value is a path string
	bare       bool     // the value is a single string, with no base before it
	kind       pathKind // what the path looks like on its own
}

// viewWords in the name of a table or a key say a path is a screen; requestWords
// say it is a server call. A name with both, or neither, settles nothing.
var (
	viewWords    = setOf("view", "route", "page", "screen", "path")
	requestWords = setOf("url", "uri", "api", "endpoint", "request", "service", "fetch", "query", "mutation", "rpc", "href", "link", "base")
)

// pathTables finds objects such as
//
//	{LIST_ORDERS: base + "/orders/list/", CANCEL_ORDER: base + "/orders/cancel/"}
//
// which large applications use to name every server call in one place, and
// objects such as {HOME: "/home", ORDER_VIEW: "/orders/view"} which name every
// screen. Each table is one or the other, or neither when the evidence does not
// settle it; none is both.
//
// A table of screens (viewTable) has at least minTablePaths plain paths and says
// so by its words (the name of the table or the key of each path names a view,
// route, page, or screen, and no word names a request), or by a path with a
// router parameter beside plain ones, as in "/artist/:id" beside "/artist/list".
//
// An endpoint table (endpointTable) has at least minTablePaths of its property
// values as paths (a string, or a name or string followed by more strings) that
// could be server calls, and those are at least three quarters of its properties.
// A route declaration, which has a "path" property, is neither. Property values
// that are paths with a router parameter are screens and do not count, and
// neither do protocol-relative URLs, file names, and machine paths, so a table of
// links that the scanner reads as UI routes stays a set of screens. Single-segment
// paths are common in link tables, so either at least half of the paths have two
// or more segments, or every key is an UPPER_SNAKE constant, as in a table that
// names each call of a service.
func pathTables(tokens []token, fromPackage func(int) bool) (apis, screens []tableEntry) {
	for open, t := range tokens {
		if t.kind != 'p' || t.text != "{" || t.match <= open || !tableStart(tokens, open) || fromPackage != nil && fromPackage(open) {
			continue
		}
		props, clean, declaration := readProperties(tokens, open)
		if declaration || len(props) < minTablePaths {
			continue
		}
		if view := viewTable(tokens, open, props); view != nil {
			screens = append(screens, view...)
		} else if clean {
			if table, ok := endpointTable(props); ok && !viewName(enclosingName(tokens, open)) {
				apis = append(apis, table...)
			}
		}
	}
	return apis, screens
}

// tableStart is the cheap first test: "{" and then a key and a colon.
func tableStart(tokens []token, open int) bool {
	return open+2 < len(tokens) && (tokens[open+1].kind == 'i' || tokens[open+1].kind == 's' || tokens[open+1].kind == 'n') && tokens[open+2].text == ":"
}

// readProperties reads the properties of the object that opens at open. It is
// clean when everything in it is a plain "key: value" property, and a
// declaration when it has a "path" property, which a route declaration has.
func readProperties(tokens []token, open int) (props []tableProp, clean, declaration bool) {
	end := tokens[open].match
	clean = true
	for i := open + 1; i < end; {
		valueEnd := i
		for valueEnd < end && tokens[valueEnd].text != "," {
			if tokens[valueEnd].match > valueEnd {
				valueEnd = tokens[valueEnd].match
			}
			valueEnd++
		}
		if i+1 >= end || tokens[i].kind != 'i' && tokens[i].kind != 's' && tokens[i].kind != 'n' || tokens[i+1].text != ":" {
			clean = false
			i = valueEnd + 1
			continue
		}
		key := tokens[i].text
		if tokens[i].kind == 's' {
			key = stringValue(key)
		}
		if key == "path" {
			return nil, false, true
		}
		prop := tableProp{tableEntry: tableEntry{key: key, token: i}}
		if text, found := tableText(tokens[i+2 : max(i+2, valueEnd)]); found {
			prop.bare = valueEnd == i+3
			prop.kind = classifyPath(text)
			prop.path = text
		}
		props = append(props, prop)
		i = valueEnd + 1
	}
	return props, clean, false
}

// endpointTable applies the endpoint table rule to the properties of an object.
func endpointTable(props []tableProp) (entries []tableEntry, ok bool) {
	deep, constants := 0, 0
	for _, prop := range props {
		if prop.path == "" || prop.kind != pathUnknown && prop.kind != pathAPI || !strings.HasPrefix(prop.path, "/") {
			continue
		}
		entries = append(entries, prop.tableEntry)
		if strings.Count(strings.Trim(prop.path, "/"), "/") > 0 {
			deep++
		}
		if constantKey.MatchString(prop.key) {
			constants++
		}
	}
	if len(entries) < minTablePaths || len(entries)*4 < len(props)*3 || deep*2 < len(entries) && constants < len(entries) {
		return nil, false
	}
	return entries, true
}

// viewTable returns the screens of a table of UI paths, or nil.
func viewTable(tokens []token, open int, props []tableProp) []tableEntry {
	var plain, params, keyed []tableEntry
	slashed := 0
	for _, prop := range props {
		if !prop.bare || !strings.HasPrefix(prop.path, "/") {
			continue
		}
		switch prop.kind {
		case pathScreen:
			params = append(params, prop.tableEntry)
		case pathUnknown:
			plain = append(plain, prop.tableEntry)
			if strings.HasSuffix(prop.path, "/") && len(prop.path) > 1 {
				slashed++
			}
			if viewName(prop.key) {
				keyed = append(keyed, prop.tableEntry)
			}
		}
	}
	// Server paths end in a slash more often than screens do.
	if len(plain) < minTablePaths || slashed*2 > len(plain) {
		return nil
	}
	switch {
	case viewName(objectName(tokens, open)) || viewName(enclosingName(tokens, open)):
		return plain
	case len(keyed) >= minTablePaths:
		return keyed
	case len(params) > 0:
		return plain
	}
	return nil
}

// viewName reports a name whose words say screens and not server calls.
func viewName(name string) bool {
	view, request := false, false
	for _, word := range identifierWords(name) {
		word = singular(word)
		_, isView := viewWords[word]
		_, isRequest := requestWords[word]
		view, request = view || isView, request || isRequest
	}
	return view && !request
}

// objectName is the name an object literal is assigned to or held under, as in
// "var routes = {" and "route: {".
func objectName(tokens []token, open int) string {
	if open < 2 || tokens[open-1].text != "=" && tokens[open-1].text != ":" {
		return ""
	}
	if tokens[open-2].kind == 's' {
		return stringValue(tokens[open-2].text)
	}
	if tokens[open-2].kind == 'i' {
		return tokens[open-2].text
	}
	return ""
}

// enclosingName is the name of the object that holds the object at open.
func enclosingName(tokens []token, open int) string {
	for i := open - 1; i >= max(0, open-160); i-- {
		if tokens[i].text == "{" && tokens[i].match > open {
			return objectName(tokens, i)
		}
	}
	return ""
}

// tableText reads a property value that is a path: strings, or a name and then
// strings, joined with "+". The name is a base URL that the file does not show,
// so the path is the part that is written out.
func tableText(value []token) (string, bool) {
	if len(value) == 0 || len(value)%2 == 0 {
		return "", false
	}
	var path strings.Builder
	for i, t := range value {
		switch {
		case i%2 == 1:
			if t.text != "+" {
				return "", false
			}
		case t.kind == 's':
			path.WriteString(stringValue(t.text))
		case t.kind != 'i' || i != 0:
			return "", false
		}
	}
	text := path.String()
	if !strings.HasPrefix(text, "/") {
		return "", false
	}
	return text, true
}
