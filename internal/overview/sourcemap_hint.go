package overview

import (
	"net/url"
	"path"
	"regexp"
	"strings"
)

const extractThenRead = "--output ./source, then jshound --overview ./source"

// sourceMapHint is the one line under a file's heading that says how to load
// a source map the file references but the overview did not apply. Overview
// reads only an inline map, or a map file at or below a local input's folder,
// and never fetches one, so the advice depends on where the input came from:
//
//   - a URL input: "jshound -s URL" fetches the map and writes the original
//     sources, which "jshound --overview" then reads;
//   - a local file: the map file next to it is read automatically; one that
//     is a URL or sits outside the folder goes through "jshound -s FILE";
//   - piped input has no folder, so only "jshound -s" on the map itself works.
//
// It returns "" when no command can help, such as an invalid inline map.
func sourceMapHint(name, header, raw string) string {
	reference := sourceMapReference(raw)
	if reference == "" {
		reference = strings.TrimSpace(header)
	}
	if strings.HasPrefix(reference, "data:") {
		return ""
	}
	const prefix = "Source map not applied. "
	switch {
	case isHTTPURL(name):
		return prefix + "Overview does not fetch maps; run jshound -s " + shellWord(name) + " " + extractThenRead + " to read the original sources."
	case reference == "":
		return ""
	case name == "stdin":
		if isHTTPURL(reference) {
			return prefix + "Run jshound -s " + shellWord(reference) + " " + extractThenRead + "."
		}
		return prefix + "Piped input has no folder to find it in; save the map as " + shellWord(reference) + ", run jshound -s " + shellWord(reference) + " " + extractThenRead + "."
	case isHTTPURL(reference):
		return prefix + "Overview does not fetch maps; run jshound -s " + shellWord(name) + " " + extractThenRead + " to fetch the map."
	}
	parsed, err := url.Parse(reference)
	if err != nil || parsed.Scheme != "" || parsed.Host != "" || strings.Contains(parsed.Path, ":") {
		return ""
	}
	clean := path.Clean(parsed.Path)
	if strings.HasPrefix(parsed.Path, "/") || clean == ".." || strings.HasPrefix(clean, "../") {
		return prefix + "Overview reads maps only from this file's folder; if the map exists, run jshound -s " + shellWord(name) + " " + extractThenRead + "."
	}
	return prefix + clean + " was not found in this file's folder or is not a valid source map; put it there and rerun to restore names."
}

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_./:@%+=,~-]+$`)

// shellWord quotes a file name or URL for the command line when needed.
func shellWord(value string) string {
	value = safeText(value)
	if shellSafe.MatchString(value) && !strings.HasPrefix(value, "-") {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}

func isHTTPURL(value string) bool {
	return strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://")
}

// sourceMapReference returns the value of the trailing sourceMappingURL
// comment, the one hasSourceMapDirective recognizes.
func sourceMapReference(raw string) string {
	if len(raw) > 4096 {
		raw = raw[len(raw)-4096:]
	}
	line := strings.TrimSpace(raw)
	if last := strings.LastIndexByte(line, '\n'); last >= 0 {
		line = strings.TrimSpace(line[last+1:])
	}
	for _, marker := range []string{"//# sourceMappingURL=", "//@ sourceMappingURL=", "/*# sourceMappingURL=", "/*@ sourceMappingURL="} {
		if value, ok := strings.CutPrefix(line, marker); ok {
			value = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(value), "*/"))
			if fields := strings.Fields(value); len(fields) > 0 {
				return fields[0]
			}
		}
	}
	return ""
}
