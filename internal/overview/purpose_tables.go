package overview

import (
	"regexp"
	"strings"
)

// constantKey matches property names such as SEND_CODE and LOGOUT.
var constantKey = regexp.MustCompile(`^[A-Z][A-Z0-9]*(?:_[A-Z0-9]+)*$`)

// minTablePaths is how many property values of an object literal must be path
// strings for it to be an endpoint table.
const minTablePaths = 3

// tableEntry is one property of an endpoint table: the constant-style key that
// names the call, and the path it holds.
type tableEntry struct {
	key   string
	path  string
	token int // index of the key token
}

// endpointTables finds objects such as
//
//	{SEND_CODE: base + "/mobile/send_code/", VERIFY_CODE: base + "/mobile/verify/"}
//
// which large applications use to name every server call in one place. The
// object is a table when at least minTablePaths of its property values are
// paths (a string, or a name or string followed by more strings) that could be
// server calls, and those are at least three quarters of its properties. A route
// declaration, which has a "path" property, is not one. Property values that are
// paths with a router parameter are screens and do not count, and neither do
// protocol-relative URLs, file names, and machine paths, so a table of links
// that the scanner reads as UI routes stays a set of screens. Single-segment
// paths are common in link tables, so either at least half of the paths have two
// or more segments, or every key is an UPPER_SNAKE constant, as in a table that
// names each call of a service.
func endpointTables(tokens []token, fromPackage func(int) bool) (entries []tableEntry) {
	for open, t := range tokens {
		if t.kind != 'p' || t.text != "{" || t.match <= open || !tableStart(tokens, open) || fromPackage != nil && fromPackage(open) {
			continue
		}
		if table, ok := readTable(tokens, open); ok {
			entries = append(entries, table...)
		}
	}
	return entries
}

// tableStart is the cheap first test: "{" and then a key and a colon.
func tableStart(tokens []token, open int) bool {
	return open+2 < len(tokens) && (tokens[open+1].kind == 'i' || tokens[open+1].kind == 's' || tokens[open+1].kind == 'n') && tokens[open+2].text == ":"
}

// readTable reads the properties of the object that opens at open. It is not a
// table when it has anything but plain "key: value" properties.
func readTable(tokens []token, open int) (entries []tableEntry, ok bool) {
	end := tokens[open].match
	properties, deep, constants := 0, 0, 0
	for i := open + 1; i < end; {
		if i+1 >= end || tokens[i].kind != 'i' && tokens[i].kind != 's' && tokens[i].kind != 'n' || tokens[i+1].text != ":" {
			return nil, false
		}
		key := tokens[i].text
		if tokens[i].kind == 's' {
			key = stringValue(key)
		}
		if key == "path" {
			return nil, false
		}
		valueEnd := i + 2
		for valueEnd < end && tokens[valueEnd].text != "," {
			if tokens[valueEnd].match > valueEnd {
				valueEnd = tokens[valueEnd].match
			}
			valueEnd++
		}
		properties++
		if path, found := tablePath(tokens[i+2 : valueEnd]); found {
			entries = append(entries, tableEntry{key: key, path: path, token: i})
			if strings.Count(strings.Trim(path, "/"), "/") > 0 {
				deep++
			}
			if constantKey.MatchString(key) {
				constants++
			}
		}
		i = valueEnd + 1
	}
	if len(entries) < minTablePaths || len(entries)*4 < properties*3 || deep*2 < len(entries) && constants < len(entries) {
		return nil, false
	}
	return entries, true
}

// tablePath reads a property value that is a path: strings, or a name and then
// strings, joined with "+". The name is a base URL that the file does not show,
// so the path is the part that is written out.
func tablePath(value []token) (string, bool) {
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
	if !strings.HasPrefix(text, "/") || classifyPath(text) != pathUnknown && classifyPath(text) != pathAPI {
		return "", false
	}
	return text, true
}
