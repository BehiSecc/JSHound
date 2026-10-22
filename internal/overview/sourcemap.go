package overview

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// sourceMap is the subset of a v3 source map that overview needs: original
// files, names, and segment positions grouped by generated line.
type sourceMap struct {
	sources   []string
	names     []string
	firstLine []int // first generated line (one-based) per source, 0 when unmapped
	lines     [][]mapSegment
}

type mapSegment struct {
	column int32 // generated column in UTF-16 code units, zero-based
	source int32
	line   int32 // original line, zero-based
	name   int32
}

type rawSourceMap struct {
	SourceRoot string   `json:"sourceRoot"`
	Sources    []string `json:"sources"`
	Names      []string `json:"names"`
	Mappings   string   `json:"mappings"`
	Sections   []struct {
		Offset struct {
			Line   int `json:"line"`
			Column int `json:"column"`
		} `json:"offset"`
		Map json.RawMessage `json:"map"`
	} `json:"sections"`
}

func parseSourceMap(data []byte) (*sourceMap, error) {
	result := &sourceMap{}
	if err := result.add(data, 0, 0, 0); err != nil {
		return nil, err
	}
	return result, nil
}

func (m *sourceMap) add(data []byte, lineOffset, columnOffset, depth int) error {
	if depth > 4 {
		return fmt.Errorf("source map sections are nested too deeply")
	}
	var raw rawSourceMap
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("decode source map: %w", err)
	}
	for _, section := range raw.Sections {
		if err := m.add(section.Map, section.Offset.Line, section.Offset.Column, depth+1); err != nil {
			return err
		}
	}
	if len(raw.Sections) > 0 {
		return nil
	}
	sourceBase, nameBase := len(m.sources), len(m.names)
	for _, source := range raw.Sources {
		if raw.SourceRoot != "" && !strings.Contains(source, "://") && !strings.HasPrefix(source, "/") {
			source = strings.TrimSuffix(raw.SourceRoot, "/") + "/" + source
		}
		m.sources = append(m.sources, source)
		m.firstLine = append(m.firstLine, 0)
	}
	m.names = append(m.names, raw.Names...)
	line := lineOffset
	var column, source, originalLine, name int32
	for _, group := range strings.Split(raw.Mappings, ";") {
		column = 0
		if line == lineOffset {
			column = int32(columnOffset)
		}
		for _, encoded := range strings.Split(group, ",") {
			if encoded == "" {
				continue
			}
			values, ok := decodeVLQ(encoded)
			if !ok || len(values) == 0 {
				return fmt.Errorf("invalid source-map mapping %q", encoded)
			}
			column += values[0]
			if len(values) < 4 {
				continue
			}
			source += values[1]
			originalLine += values[2]
			segment := mapSegment{column: column, source: source + int32(sourceBase), line: originalLine, name: -1}
			if len(values) >= 5 {
				name += values[4]
				segment.name = name + int32(nameBase)
			}
			if segment.source < 0 || int(segment.source) >= len(m.sources) {
				continue
			}
			for len(m.lines) <= line {
				m.lines = append(m.lines, nil)
			}
			m.lines[line] = append(m.lines[line], segment)
			if m.firstLine[segment.source] == 0 {
				m.firstLine[segment.source] = line + 1
			}
		}
		line++
	}
	return nil
}

func decodeVLQ(value string) ([]int32, bool) {
	var result []int32
	var current, shift int32
	for i := 0; i < len(value); i++ {
		digit := strings.IndexByte("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/", value[i])
		if digit < 0 || shift > 30 {
			return nil, false
		}
		current += int32(digit&31) << shift
		if digit&32 != 0 {
			shift += 5
			continue
		}
		if current&1 != 0 {
			result = append(result, -(current >> 1))
		} else {
			result = append(result, current>>1)
		}
		current, shift = 0, 0
	}
	return result, shift == 0
}

// lookup returns the segment covering a one-based generated line and a
// zero-based UTF-16 column.
func (m *sourceMap) lookup(line, column int) (mapSegment, bool) {
	if line < 1 || line > len(m.lines) {
		return mapSegment{}, false
	}
	segments := m.lines[line-1]
	index := sort.Search(len(segments), func(i int) bool { return int(segments[i].column) > column }) - 1
	if index < 0 {
		return mapSegment{}, false
	}
	return segments[index], true
}

func (m *sourceMap) modules() []moduleSpan {
	spans := make([]moduleSpan, 0, len(m.sources))
	for i, source := range m.sources {
		spans = append(spans, moduleSpan{path: source, line: m.firstLine[i]})
	}
	return spans
}

// utf16Columns converts byte offsets into zero-based UTF-16 columns in one
// forward pass, because source maps count columns in UTF-16 code units.
func utf16Columns(code string, offsets []int) map[int]int {
	sorted := append([]int(nil), offsets...)
	sort.Ints(sorted)
	result := make(map[int]int, len(sorted))
	position, column := 0, 0
	for _, target := range sorted {
		for position < target && position < len(code) {
			c := code[position]
			switch {
			case c == '\n':
				column = 0
				position++
			case c < 0x80:
				column++
				position++
			default:
				size := 2
				switch {
				case c >= 0xf0:
					size, column = 4, column+2
				case c >= 0xe0:
					size, column = 3, column+1
				default:
					column++
				}
				position += size
			}
		}
		result[target] = column
	}
	return result
}

// origin returns the original file and line for one generated token.
func (m *sourceMap) origin(code string, t token) string {
	column := utf16Columns(code, []int{t.offset})[t.offset]
	segment, ok := m.lookup(t.line, column)
	if !ok {
		return ""
	}
	if path := normalizeModulePath(m.sources[segment.source]); path != "" {
		return fmt.Sprintf("%s:%d", path, segment.line+1)
	}
	return ""
}

// applySourceMap gives units their original names and locations.
func applySourceMap(m *sourceMap, code string, tokens []token, units []unitRange) {
	offsets := make([]int, 0, len(units)*2)
	for _, unit := range units {
		if unit.nameAt >= 0 && unit.nameAt < len(tokens) {
			offsets = append(offsets, tokens[unit.nameAt].offset)
			if unit.nameAt > 0 {
				offsets = append(offsets, tokens[unit.nameAt-1].offset)
			}
		}
	}
	columns := utf16Columns(code, offsets)
	for i := range units {
		at := units[i].nameAt
		if at < 0 || at >= len(tokens) {
			continue
		}
		name := tokens[at]
		segment, ok := m.lookup(name.line, columns[name.offset])
		if !ok {
			continue
		}
		if path := normalizeModulePath(m.sources[segment.source]); path != "" {
			units[i].Origin = fmt.Sprintf("%s:%d", path, segment.line+1)
		}
		original := ""
		if int(segment.column) == columns[name.offset] && segment.name >= 0 {
			original = m.names[segment.name]
		} else if at > 0 {
			if keyword, ok := m.lookup(tokens[at-1].line, columns[tokens[at-1].offset]); ok && int(keyword.column) == columns[tokens[at-1].offset] && keyword.name >= 0 {
				original = m.names[keyword.name]
			}
		}
		if original != "" && identifierLike(original) && original != lastNamePart(units[i].Name) {
			units[i].OriginalName = original
			if dot := strings.LastIndexByte(units[i].Name, '.'); dot >= 0 {
				units[i].OriginalName = units[i].Name[:dot+1] + original
			}
		}
	}
}

// useOriginalNames relabels flow nodes with source-map names. A name shared
// by several generated functions keeps its generated name for clarity.
func useOriginalNames(file *File, units []unitRange) {
	counts := make(map[string]int)
	for _, unit := range units {
		if unit.OriginalName != "" {
			counts[unit.OriginalName]++
		}
	}
	labels := make(map[string]string)
	for _, unit := range units {
		if unit.OriginalName == "" {
			continue
		}
		label := unit.OriginalName + "()"
		if counts[unit.OriginalName] > 1 {
			label = unit.OriginalName + "() [" + unit.Name + "]"
		}
		labels[unit.Name+"()"] = label
	}
	if len(labels) == 0 {
		return
	}
	rename := func(value string) string {
		if label, ok := labels[value]; ok {
			return label
		}
		return value
	}
	for i := range file.Links {
		file.Links[i].From, file.Links[i].To = rename(file.Links[i].From), rename(file.Links[i].To)
	}
	for i := range file.Dispatches {
		if file.Dispatches[i].In != "" {
			file.Dispatches[i].In = strings.TrimSuffix(rename(file.Dispatches[i].In+"()"), "()")
		}
	}
	for _, entries := range [][]Entry{file.EntryPoints, file.Registrations} {
		for i := range entries {
			entries[i].Action = rename(entries[i].Action)
			entries[i].RegisteredBy = rename(entries[i].RegisteredBy)
		}
	}
}

func lastNamePart(name string) string {
	if dot := strings.LastIndexByte(name, '.'); dot >= 0 {
		return name[dot+1:]
	}
	return name
}

func identifierLike(value string) bool {
	if value == "" || !identifierStart(value[0]) {
		return false
	}
	for i := 1; i < len(value); i++ {
		if !identifierPart(value[i]) {
			return false
		}
	}
	return true
}
