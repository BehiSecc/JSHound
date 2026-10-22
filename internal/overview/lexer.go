package overview

import "strings"

// token is intentionally small: overview needs source structure, not a full JS
// syntax tree. Strings, comments, templates, and regular expressions never
// contribute code tokens, so their contents cannot become false call edges.
type token struct {
	text   string
	kind   byte // 'i' identifier, 's' string, 't' template, 'n' number, 'p' punctuation
	line   int
	column int   // one-based source column; useful when a bundle occupies one line
	offset int   // byte offset in the analyzed JavaScript source
	depth  int16 // brace depth before this token
	paren  int16 // parenthesis depth before this token
	match  int   // matching bracket/brace/parenthesis, or -1
}

func lex(source string) []token {
	// Generated code averages roughly one token per four to six bytes.
	capacity := len(source) / 5
	tokens := make([]token, 0, capacity)
	stack := make([]int, 0, 32)
	var depth, paren int16
	line := 1
	lineStart := 0
	add := func(value string, kind byte, start int) {
		tokens = append(tokens, token{text: value, kind: kind, line: line, column: start - lineStart + 1, offset: start, depth: depth, paren: paren, match: -1})
		index := len(tokens) - 1
		switch value {
		case "(", "[", "{":
			stack = append(stack, index)
			if value == "{" {
				depth++
			} else if value == "(" {
				paren++
			}
		case ")", "]", "}":
			if value == "}" && depth > 0 {
				depth--
				tokens[index].depth = depth
			} else if value == ")" && paren > 0 {
				paren--
				tokens[index].paren = paren
			}
			if len(stack) > 0 {
				open := stack[len(stack)-1]
				if bracketsMatch(tokens[open].text, value) {
					tokens[open].match = index
					tokens[index].match = open
					stack = stack[:len(stack)-1]
				}
			}
		}
	}
	for i := 0; i < len(source); {
		c := source[i]
		if isSpace(c) {
			if c == '\n' {
				line++
				lineStart = i + 1
			}
			i++
			continue
		}
		if c == '/' && i+1 < len(source) {
			switch source[i+1] {
			case '/':
				i += 2
				for i < len(source) && source[i] != '\n' {
					i++
				}
				continue
			case '*':
				i += 2
				for i < len(source) {
					if source[i] == '*' && i+1 < len(source) && source[i+1] == '/' {
						i += 2
						break
					}
					if source[i] == '\n' {
						line++
						lineStart = i + 1
					}
					i++
				}
				continue
			}
			if regexAllowed(tokens) {
				end := skipRegex(source, i)
				line += strings.Count(source[i:end], "\n")
				if last := strings.LastIndexByte(source[i:end], '\n'); last >= 0 {
					lineStart = i + last + 1
				}
				i = end
				continue
			}
		}
		if c == '\'' || c == '"' || c == '`' {
			start := i
			if c == '`' {
				i = skipTemplate(source, i)
			} else {
				i = skipString(source, i)
			}
			value := source[start:i]
			if c == '`' {
				// Template expressions may contain arbitrary code. Treat the
				// complete template as data rather than inventing structure.
				add(value, 't', start)
			} else {
				add(value, 's', start)
			}
			line += strings.Count(value, "\n")
			if last := strings.LastIndexByte(value, '\n'); last >= 0 {
				lineStart = start + last + 1
			}
			continue
		}
		if identifierStart(c) {
			start := i
			i++
			for i < len(source) && identifierPart(source[i]) {
				i++
			}
			add(source[start:i], 'i', start)
			continue
		}
		if c >= '0' && c <= '9' {
			start := i
			i++
			for i < len(source) && (identifierPart(source[i]) || source[i] == '.') {
				i++
			}
			add(source[start:i], 'n', start)
			continue
		}
		if i+2 <= len(source) && (strings.HasPrefix(source[i:], "=>") || strings.HasPrefix(source[i:], "?.") || strings.HasPrefix(source[i:], "++") || strings.HasPrefix(source[i:], "--")) {
			add(source[i:i+2], 'p', i)
			i += 2
			continue
		}
		add(source[i:i+1], 'p', i)
		i++
	}
	return tokens
}

func bracketsMatch(open, close string) bool {
	return open == "(" && close == ")" || open == "[" && close == "]" || open == "{" && close == "}"
}

func isSpace(c byte) bool { return c == ' ' || c == '\t' || c == '\n' || c == '\r' }
func identifierStart(c byte) bool {
	return c == '_' || c == '$' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= 128
}
func identifierPart(c byte) bool { return identifierStart(c) || c >= '0' && c <= '9' }

func skipString(source string, start int) int {
	quote := source[start]
	for i := start + 1; i < len(source); i++ {
		if source[i] == '\\' {
			i++
			continue
		}
		if source[i] == quote {
			return i + 1
		}
	}
	return len(source)
}

func skipTemplate(source string, start int) int {
	for i := start + 1; i < len(source); {
		switch source[i] {
		case '\\':
			i += 2
		case '`':
			return i + 1
		case '$':
			if i+1 < len(source) && source[i+1] == '{' {
				i = skipTemplateExpression(source, i+2)
			} else {
				i++
			}
		default:
			i++
		}
	}
	return len(source)
}

func skipTemplateExpression(source string, start int) int {
	depth := 1
	for i := start; i < len(source); {
		switch source[i] {
		case '\'', '"':
			i = skipString(source, i)
		case '`':
			i = skipTemplate(source, i)
		case '/':
			if i+1 < len(source) && source[i+1] == '/' {
				i += 2
				for i < len(source) && source[i] != '\n' {
					i++
				}
			} else if i+1 < len(source) && source[i+1] == '*' {
				i += 2
				for i+1 < len(source) && (source[i] != '*' || source[i+1] != '/') {
					i++
				}
				i = min(len(source), i+2)
			} else if rawRegexAllowed(source, i) {
				i = skipRegex(source, i)
			} else {
				i++
			}
		case '{':
			depth++
			i++
		case '}':
			depth--
			i++
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return len(source)
}

func rawRegexAllowed(source string, start int) bool {
	for i := start - 1; i >= 0; i-- {
		if isSpace(source[i]) {
			continue
		}
		return strings.ContainsRune("(=:[,!&|?{;", rune(source[i]))
	}
	return true
}

func regexAllowed(tokens []token) bool {
	if len(tokens) == 0 {
		return true
	}
	last := tokens[len(tokens)-1].text
	switch last {
	case "(", "[", "{", "=", ":", ",", ";", "!", "?", "=>", "return", "throw", "case", "yield", "await", "typeof", "void", "delete":
		return true
	}
	if last == ")" {
		close := len(tokens) - 1
		open := tokens[close].match
		if open > 0 {
			switch tokens[open-1].text {
			case "if", "while", "for", "with", "catch":
				return true
			}
		}
	}
	return false
}

func skipRegex(source string, start int) int {
	inClass := false
	for i := start + 1; i < len(source); i++ {
		if source[i] == '\\' {
			i++
			continue
		}
		switch source[i] {
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				i++
				for i < len(source) && identifierPart(source[i]) {
					i++
				}
				return i
			}
		case '\n', '\r':
			return i
		}
	}
	return len(source)
}

func at(tokens []token, index int) string {
	if index < 0 || index >= len(tokens) {
		return ""
	}
	return tokens[index].text
}

func stringValue(value string) string {
	if len(value) < 2 || value[0] != value[len(value)-1] || value[0] != '\'' && value[0] != '"' {
		return ""
	}
	return value[1 : len(value)-1]
}

// argumentRanges returns top-level argument spans within a matched call.
func argumentRanges(tokens []token, open int) [][2]int {
	if open < 0 || open >= len(tokens) || tokens[open].text != "(" || tokens[open].match < 0 {
		return nil
	}
	end := tokens[open].match
	start := open + 1
	var ranges [][2]int
	for i := start; i < end; i++ {
		if tokens[i].text == "," {
			ranges = append(ranges, [2]int{start, i})
			start = i + 1
		} else if tokens[i].match > i {
			i = tokens[i].match
		}
	}
	if start < end {
		ranges = append(ranges, [2]int{start, end})
	}
	return ranges
}
