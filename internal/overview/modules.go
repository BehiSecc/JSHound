package overview

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// BundledModules summarizes the original modules visible in a bundle, either
// from bundler path markers or from a source map.
type BundledModules struct {
	Source   string        `json:"source"`
	Total    int           `json:"total"`
	Own      int           `json:"own"`
	Areas    []ModuleGroup `json:"areas,omitempty"`
	Packages []ModuleGroup `json:"packages,omitempty"`
}

// ModuleGroup is a source directory or third-party package. Line is the first
// generated line that belongs to it when known.
type ModuleGroup struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
	Line  int    `json:"line,omitempty"`
}

type moduleSpan struct {
	path string
	line int
}

const maxModuleAreas = 12

var modulePathPattern = regexp.MustCompile(`^[A-Za-z0-9_@.~+\-/()\[\]$]+\.(?:[cm]?[jt]sx?|json|vue|svelte|css|scss|less|html|wasm|node)$`)

// esbuildModuleComments reads the "// path/to/module.ts" markers esbuild
// writes before each module in non-minified output.
func esbuildModuleComments(raw string) []moduleSpan {
	var spans []moduleSpan
	seen := make(map[string]struct{})
	line := 0
	for rest := raw; len(rest) > 0; {
		line++
		var current string
		if end := strings.IndexByte(rest, '\n'); end >= 0 {
			current, rest = rest[:end], rest[end+1:]
		} else {
			current, rest = rest, ""
		}
		trimmed := strings.TrimLeft(current, " ")
		if len(current)-len(trimmed) > 2 || !strings.HasPrefix(trimmed, "// ") {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, "// "))
		if !modulePathPattern.MatchString(value) {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		spans = append(spans, moduleSpan{path: value, line: line})
	}
	return spans
}

// esbuildModuleKeys reads __commonJS({"path"(exports, module) {...}}) keys,
// which survive when comments are stripped.
func esbuildModuleKeys(tokens []token) []moduleSpan {
	var spans []moduleSpan
	for i := 0; i+2 < len(tokens); i++ {
		if tokens[i].kind != 's' || at(tokens, i+1) != "(" || at(tokens, i-1) != "{" && at(tokens, i-1) != "," {
			continue
		}
		value := stringValue(tokens[i].text)
		if modulePathPattern.MatchString(value) && strings.Contains(value, "/") {
			spans = append(spans, moduleSpan{path: value, line: tokens[i].line})
		}
	}
	return spans
}

func mergeModuleSpans(groups ...[]moduleSpan) []moduleSpan {
	var result []moduleSpan
	seen := make(map[string]struct{})
	for _, group := range groups {
		for _, span := range group {
			if _, ok := seen[span.path]; ok {
				continue
			}
			seen[span.path] = struct{}{}
			result = append(result, span)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return result[i].line < result[j].line })
	return result
}

// normalizeModulePath removes bundler namespaces and relative prefixes. It
// returns "" for virtual modules that do not correspond to a source file.
func normalizeModulePath(value string) string {
	if value == "" || value[0] == 0 || strings.HasPrefix(value, "(webpack)") || strings.HasPrefix(value, "webpack/") ||
		strings.HasPrefix(value, "external ") || strings.HasPrefix(value, "ignored|") || strings.HasPrefix(value, "<") {
		return ""
	}
	if scheme := strings.Index(value, "://"); scheme >= 0 {
		value = value[scheme+3:]
		if strings.HasPrefix(value, "/") {
			value = value[1:]
		} else if slash := strings.IndexByte(value, '/'); slash >= 0 && !strings.Contains(value[:slash], ".") {
			// webpack://app-name/./src/x.js
			value = value[slash+1:]
		}
	}
	if query := strings.IndexAny(value, "?#"); query >= 0 {
		value = value[:query]
	}
	for {
		switch {
		case strings.HasPrefix(value, "./"):
			value = value[2:]
		case strings.HasPrefix(value, "../"):
			value = value[3:]
		case strings.HasPrefix(value, "/"):
			value = value[1:]
		default:
			if strings.HasPrefix(value, "vite/") || strings.HasPrefix(value, "commonjsHelpers") || value == "" {
				return ""
			}
			return value
		}
	}
}

func modulePackage(value string) (string, bool) {
	index := strings.LastIndex(value, "node_modules/")
	if index < 0 {
		return "", false
	}
	parts := strings.SplitN(value[index+len("node_modules/"):], "/", 3)
	if len(parts) == 0 || parts[0] == "" {
		return "", false
	}
	if strings.HasPrefix(parts[0], "@") && len(parts) > 1 {
		return parts[0] + "/" + parts[1], true
	}
	return parts[0], true
}

func summarizeModules(source string, spans []moduleSpan) *BundledModules {
	if len(spans) < 2 {
		return nil
	}
	result := &BundledModules{Source: source}
	packages := make(map[string]*ModuleGroup)
	var own []moduleSpan
	for _, span := range spans {
		normalized := normalizeModulePath(span.path)
		if normalized == "" {
			continue
		}
		result.Total++
		if name, ok := modulePackage(normalized); ok {
			if packages[name] == nil {
				packages[name] = &ModuleGroup{Name: name, Line: span.line}
			}
			packages[name].Count++
			continue
		}
		own = append(own, moduleSpan{path: normalized, line: span.line})
	}
	if result.Total < 2 {
		return nil
	}
	result.Own = len(own)
	result.Areas = moduleAreas(own)
	for _, group := range packages {
		result.Packages = append(result.Packages, *group)
	}
	sortGroups(result.Packages)
	return result
}

// moduleAreas groups project files by directory. The largest area is split
// into subdirectories while the result stays readable.
func moduleAreas(spans []moduleSpan) []ModuleGroup {
	if len(spans) == 0 {
		return nil
	}
	type area struct {
		dir     []string
		members []int
		fixed   bool
	}
	dirs := make([][]string, len(spans))
	for i, span := range spans {
		if dir := path.Dir(span.path); dir != "." {
			dirs[i] = strings.Split(dir, "/")
		}
	}
	common := 0
	for common < len(dirs[0]) {
		same := true
		for _, dir := range dirs[1:] {
			if len(dir) <= common || dir[common] != dirs[0][common] {
				same = false
				break
			}
		}
		if !same {
			break
		}
		common++
	}
	all := make([]int, len(spans))
	for i := range all {
		all[i] = i
	}
	areas := []*area{{dir: dirs[0][:common], members: all}}
	for {
		var largest *area
		for _, candidate := range areas {
			if !candidate.fixed && (largest == nil || len(candidate.members) > len(largest.members)) {
				largest = candidate
			}
		}
		if largest == nil {
			break
		}
		depth := len(largest.dir)
		children := make(map[string]*area)
		var order []string
		var direct []int
		for _, member := range largest.members {
			if len(dirs[member]) <= depth {
				direct = append(direct, member)
				continue
			}
			name := dirs[member][depth]
			if children[name] == nil {
				children[name] = &area{dir: dirs[member][:depth+1]}
				order = append(order, name)
			}
			children[name].members = append(children[name].members, member)
		}
		size := len(areas) - 1 + len(children)
		if len(direct) > 0 {
			size++
		}
		if len(children) == 0 || size > maxModuleAreas {
			largest.fixed = true
			continue
		}
		next := make([]*area, 0, size)
		for _, existing := range areas {
			if existing != largest {
				next = append(next, existing)
			}
		}
		if len(direct) > 0 {
			next = append(next, &area{dir: largest.dir, members: direct, fixed: true})
		}
		for _, name := range order {
			next = append(next, children[name])
		}
		areas = next
	}
	result := make([]ModuleGroup, 0, len(areas))
	for _, item := range areas {
		name := strings.Join(item.dir, "/")
		if name == "" {
			name = "."
		}
		group := ModuleGroup{Name: name, Count: len(item.members), Line: spans[item.members[0]].line}
		for _, member := range item.members {
			group.Line = min(group.Line, spans[member].line)
		}
		result = append(result, group)
	}
	sortGroups(result)
	return result
}

func sortGroups(groups []ModuleGroup) {
	sort.Slice(groups, func(i, j int) bool {
		if groups[i].Count != groups[j].Count {
			return groups[i].Count > groups[j].Count
		}
		return groups[i].Name < groups[j].Name
	})
}

// moduleAt returns the module whose marker most recently precedes line.
func moduleAt(spans []moduleSpan, line int) string {
	index := sort.Search(len(spans), func(i int) bool { return spans[i].line > line }) - 1
	if index < 0 {
		return ""
	}
	return normalizeModulePath(spans[index].path)
}
