package overview

import (
	"net/url"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

// inputIndex finds which input files a string literal names. A literal names
// an input when its base name equals the input's base name, such as
// "./chunk-Ab12Cd.js" or "assets/chunk-Ab12Cd.js". Only the inputs are ever
// considered; nothing else is read or fetched.
type inputIndex struct {
	names  []string
	clean  []string         // normalized names, for resolving paths
	byBase map[string][]int // base name -> input positions
}

// loadableExtensions are the file types a script or page loads.
var loadableExtensions = map[string]struct{}{
	".js": {}, ".mjs": {}, ".cjs": {}, ".jsx": {}, ".html": {}, ".htm": {},
}

// htmlScriptSource matches the src attribute of a script tag in an HTML input.
var htmlScriptSource = regexp.MustCompile(`(?is)<script\b[^>]*?\bsrc\s*=\s*(?:"([^"]*)"|'([^']*)')`)

func newInputIndex(sources []Source) *inputIndex {
	x := &inputIndex{byBase: make(map[string][]int, len(sources))}
	for i, source := range sources {
		x.names = append(x.names, source.Name)
		clean := cleanInputName(source.Name)
		x.clean = append(x.clean, clean)
		base := path.Base(clean)
		x.byBase[base] = append(x.byBase[base], i)
	}
	return x
}

// cleanInputName makes file paths and URLs comparable: slashes, no query, no
// fragment, no dot segments.
func cleanInputName(name string) string {
	if isHTTPURL(name) {
		if parsed, err := url.Parse(name); err == nil {
			parsed.RawQuery, parsed.Fragment = "", ""
			parsed.Path = path.Clean(parsed.Path)
			return parsed.String()
		}
		return name
	}
	return filepath.ToSlash(filepath.Clean(name))
}

// loads returns the positions of the input files that the file at self names
// in a string literal, in input order. Literals in HTML script tags count
// too. An ambiguous base name (the same name at several inputs) links only
// when the literal's path singles out one of them.
func (x *inputIndex) loads(self int, tokens []token, raw string, html bool) []int {
	found := map[int]struct{}{}
	consider := func(literal string) {
		if target := x.resolve(self, literal); target >= 0 {
			found[target] = struct{}{}
		}
	}
	for _, t := range tokens {
		if t.kind == 's' {
			consider(stringValue(t.text))
		}
	}
	if html {
		for _, match := range htmlScriptSource.FindAllStringSubmatch(raw, -1) {
			consider(match[1] + match[2])
		}
	}
	if len(found) == 0 {
		return nil
	}
	result := make([]int, 0, len(found))
	for i := range x.names {
		if _, ok := found[i]; ok {
			result = append(result, i)
		}
	}
	return result
}

// resolve returns the input a literal names, or -1.
func (x *inputIndex) resolve(self int, literal string) int {
	if len(literal) < 3 || len(literal) > 400 || strings.ContainsAny(literal, " \t\r\n\\*{}()") {
		return -1
	}
	if cut := strings.IndexAny(literal, "?#"); cut >= 0 {
		literal = literal[:cut]
	}
	base := path.Base(literal)
	if _, ok := loadableExtensions[strings.ToLower(path.Ext(base))]; !ok {
		return -1
	}
	candidates := x.byBase[base]
	if len(candidates) == 0 {
		return -1
	}
	if len(candidates) == 1 {
		if candidates[0] == self {
			return -1
		}
		return candidates[0]
	}
	// The same base name at several inputs: only a path that selects exactly
	// one of them links.
	if !strings.Contains(strings.Trim(literal, "/"), "/") {
		return -1
	}
	match := -1
	for _, i := range candidates {
		if i == self || !x.pathNames(self, literal, i) {
			continue
		}
		if match >= 0 {
			return -1
		}
		match = i
	}
	return match
}

// pathNames reports whether a literal with directories points at the input.
// A relative literal is resolved against the importing file's folder; any
// other literal must be a trailing part of the input's path.
func (x *inputIndex) pathNames(self int, literal string, target int) bool {
	from, to := x.clean[self], x.clean[target]
	if strings.HasPrefix(literal, "./") || strings.HasPrefix(literal, "../") {
		if isHTTPURL(from) {
			base, err := url.Parse(from)
			ref, refErr := url.Parse(literal)
			if err != nil || refErr != nil {
				return false
			}
			return cleanInputName(base.ResolveReference(ref).String()) == to
		}
		return path.Join(path.Dir(from), literal) == to
	}
	trimmed := strings.TrimLeft(literal, "/")
	return to == trimmed || strings.HasSuffix(to, "/"+trimmed)
}
