package overview

import "strings"

// token is intentionally small: overview needs source structure, not a full JS
// syntax tree. Strings, comments, templates, and regular expressions never
// contribute code tokens, so their contents cannot become false call edges.
type token struct {
	text   string
	kind   byte // 'i' identifier, 's' string or plain template, 't' template with interpolation or tag, 'n' number, 'p' punctuation
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
	var memo regexMemo
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
			if regexAllowed(tokens) && (i == 0 || source[i-1] != '<') { // "</" closes a JSX tag
				// An unterminated candidate is a division after all, so a
				// misjudged slash cannot swallow the rest of the line.
				if end := memo.skip(source, i); end >= 0 {
					i = end
					continue
				}
			}
		}
		if c == '\'' || c == '"' || c == '`' {
			start := i
			interpolated := false
			if c == '`' {
				i, interpolated = scanTemplate(source, i, &memo)
			} else {
				i = skipString(source, i)
			}
			value := source[start:i]
			if c == '`' && (interpolated || taggedTemplate(tokens) || !terminated(value)) {
				// Template expressions may contain arbitrary code. Treat the
				// complete template as data rather than inventing structure.
				add(value, 't', start)
			} else {
				// A template without interpolation is a plain string, which
				// minifiers often write instead of quotes.
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

func skipTemplate(source string, start int, memo *regexMemo) int {
	end, _ := scanTemplate(source, start, memo)
	return end
}

// scanTemplate returns the end of the template literal that starts at start
// and whether it contains a ${...} interpolation.
func scanTemplate(source string, start int, memo *regexMemo) (int, bool) {
	interpolated := false
	for i := start + 1; i < len(source); {
		switch source[i] {
		case '\\':
			i += 2
		case '`':
			return i + 1, interpolated
		case '$':
			if i+1 < len(source) && source[i+1] == '{' {
				interpolated = true
				i = skipTemplateExpression(source, i+2, memo)
			} else {
				i++
			}
		default:
			i++
		}
	}
	return len(source), interpolated
}

// terminated reports a literal that ends with its opening delimiter.
func terminated(value string) bool {
	return len(value) >= 2 && value[len(value)-1] == value[0]
}

// taggedTemplate reports whether a template literal that starts after the
// tokens so far is the tail of a tagged template, as in html`<p>` or a.b(c)`x`.
// Such a template is a call, not a string.
func taggedTemplate(tokens []token) bool {
	if len(tokens) == 0 {
		return false
	}
	last := tokens[len(tokens)-1]
	switch last.kind {
	case 'i':
		switch last.text {
		case "default", "of":
			return false
		}
		return !regexKeyword(last.text)
	case 'p':
		return last.text == ")" || last.text == "]"
	}
	return false
}

func skipTemplateExpression(source string, start int, memo *regexMemo) int {
	depth := 1
	for i := start; i < len(source); {
		switch source[i] {
		case '\'', '"':
			i = skipString(source, i)
		case '`':
			i = skipTemplate(source, i, memo)
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
			} else if end := regexEnd(source, i, memo); end >= 0 {
				i = end
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

// regexEnd returns the end of a regular expression literal starting at a slash
// inside a template expression, or -1 when the slash divides. Template
// expressions are skipped without tokens, so this looks at the preceding bytes
// instead of the previous token.
func regexEnd(source string, start int, memo *regexMemo) int {
	if !rawRegexAllowed(source, start) {
		return -1
	}
	return memo.skip(source, start)
}

func rawRegexAllowed(source string, start int) bool {
	i := start - 1
	for i >= 0 && isSpace(source[i]) {
		i--
	}
	if i < 0 {
		return true
	}
	c := source[i]
	switch c {
	case ')', ']', '}', '"', '\'', '`', '.':
		return false
	case '+', '-':
		// "a++ / b" divides; "a + /re/" does not.
		return i == 0 || source[i-1] != c
	}
	if identifierPart(c) {
		end := i + 1
		for i >= 0 && identifierPart(source[i]) {
			i--
		}
		return regexKeyword(source[i+1:end]) && (i < 0 || source[i] != '.')
	}
	return true
}

// regexKeyword reports words after which a slash begins a regular expression.
func regexKeyword(word string) bool {
	switch word {
	case "return", "throw", "case", "yield", "await", "typeof", "void", "delete", "in", "instanceof", "else", "do":
		return true
	}
	return false
}

// regexAllowed decides whether a slash after the last token starts a regular
// expression. After a value (identifier, literal, closing bracket) it divides;
// after any operator or opening punctuation it starts a regular expression.
func regexAllowed(tokens []token) bool {
	if len(tokens) == 0 {
		return true
	}
	last := &tokens[len(tokens)-1]
	switch last.kind {
	case 'p':
		switch last.text {
		case "]", "}", "++", "--":
			return false
		case ")":
			if open := last.match; open > 0 {
				switch tokens[open-1].text {
				case "if", "while", "for", "with", "catch":
					return true
				}
			}
			return false
		}
		return true
	case 'i':
		return regexKeyword(last.text) && (len(tokens) < 2 || tokens[len(tokens)-2].text != ".")
	}
	return false
}

// regexMemo keeps a failed regular expression scan from being repeated by the
// slashes inside it, which would make hostile input quadratic.
//
// A scan from a slash succeeds exactly when some later slash outside a
// character class ends the line first; whether a slash is outside a class
// depends only on the nearest preceding '[' or ']'. So if the scan from `from`
// failed at `end` (the line end), every slash it passed has a '[' as that
// nearest bracket, and a later scan starting in (from, end) can only succeed
// if it meets a slash before any bracket. It stops at the first bracket.
type regexMemo struct{ from, end int }

// skip returns the end of the regular expression literal that starts at start,
// or -1 when no closing slash appears before the line ends.
func (m *regexMemo) skip(source string, start int) int {
	covered := start > m.from && start < m.end
	inClass := false
	for i := start + 1; i < len(source); i++ {
		if source[i] == '\\' {
			i++
			continue
		}
		switch source[i] {
		case '[', ']':
			if covered {
				return -1
			}
			inClass = source[i] == '['
		case '/':
			if !inClass {
				i++
				for i < len(source) && identifierPart(source[i]) {
					i++
				}
				return i
			}
		case '\n', '\r':
			m.from, m.end = start, i
			return -1
		}
	}
	m.from, m.end = start, len(source)
	return -1
}

// lexNote describes a condition that makes the token stream unreliable, or
// returns "" when the source tokenized cleanly. Brackets that do not pair up
// mean the lexer lost sync with the source (or the file is not complete
// JavaScript); a literal or comment still open at the end of the input means
// everything after its start was consumed as data.
func lexNote(source string, tokens []token) string {
	unmatched := 0
	for i := range tokens {
		if tokens[i].match < 0 && tokens[i].kind == 'p' {
			switch tokens[i].text {
			case "(", ")", "[", "]", "{", "}":
				unmatched++
			}
		}
	}
	unterminated := false
	end := 0
	if n := len(tokens); n > 0 {
		last := tokens[n-1]
		end = last.offset + len(last.text)
		if last.kind == 's' || last.kind == 't' {
			unterminated = len(last.text) < 2 || last.text[len(last.text)-1] != last.text[0]
		}
	}
	if !unterminated {
		tail := strings.TrimSpace(source[end:])
		unterminated = strings.HasPrefix(tail, "/*") && !strings.Contains(tail[2:], "*/")
	}
	switch {
	case unterminated && unmatched > 0:
		return "Part of the file could not be tokenized (an unterminated string, template, or comment, and unpaired brackets); the overview may be incomplete."
	case unterminated:
		return "Part of the file could not be tokenized (an unterminated string, template, or comment); the overview may be incomplete."
	case unmatched > 0:
		return "Brackets in this file do not pair up, so the lexer may have lost sync with the source; the overview may be incomplete."
	}
	return ""
}

func at(tokens []token, index int) string {
	if index < 0 || index >= len(tokens) {
		return ""
	}
	return tokens[index].text
}

func stringValue(value string) string {
	if len(value) < 2 || value[0] != value[len(value)-1] || value[0] != '\'' && value[0] != '"' && value[0] != '`' {
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
