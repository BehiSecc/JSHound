package overview

import (
	"strings"
	"testing"
)

func tokenTexts(tokens []token) []string {
	texts := make([]string, len(tokens))
	for i, tk := range tokens {
		texts[i] = tk.text
	}
	return texts
}

// requireTail fails unless the tokens end with the wanted texts, so code after
// a construct is proven to be lexed rather than swallowed.
func requireTail(t *testing.T, source string, want ...string) []token {
	t.Helper()
	tokens := lex(source)
	got := tokenTexts(tokens)
	if len(got) < len(want) || strings.Join(got[len(got)-len(want):], " ") != strings.Join(want, " ") {
		t.Fatalf("tokens of %q: %q, want tail %q", source, got, want)
	}
	return tokens
}

func requireBalanced(t *testing.T, tokens []token) {
	t.Helper()
	for i, tk := range tokens {
		switch tk.text {
		case "(", ")", "[", "]", "{", "}":
			if tk.match < 0 {
				t.Fatalf("unmatched %q at token %d (offset %d)", tk.text, i, tk.offset)
			}
		}
	}
}

func TestRegexAfterOperatorKeepsLexerInSync(t *testing.T) {
	// The regular expression contains an escaped slash pair, which reads as a
	// line comment if the opening slash is mistaken for division.
	for _, operator := range []string{"&&", "||", "&", "|", "+", "-", "*", "%", "<", ">", "^", "~", "==", "!=", "===", "?", "??", ",", ":", "=", "+=", "&&=", "!", "=>", "(", "[", "{", ";"} {
		prefix, gap := "return a", ""
		if strings.ContainsAny(operator, "([{;") {
			prefix = "a"
		}
		if operator == "<" {
			gap = " " // "</" is a JSX closing tag, covered separately
		}
		source := "function f(t){" + prefix + operator + gap + `/^(?:http|ftp):\/\//.test(t)}` + "\nlater(1);"
		tokens := lex(source)
		got := strings.Join(tokenTexts(tokens), " ")
		if !strings.HasSuffix(got, "later ( 1 ) ;") {
			t.Fatalf("operator %q lost the code after the regular expression: %s", operator, got)
		}
		for _, tk := range tokens {
			if strings.Contains(tk.text, "http") {
				t.Fatalf("operator %q leaked regular expression text: %q", operator, tk.text)
			}
		}
	}
}

func TestRegexAfterKeywordKeepsLexerInSync(t *testing.T) {
	for _, keyword := range []string{"return", "typeof", "in", "instanceof", "else", "do", "case", "yield", "await", "void", "delete", "throw"} {
		source := "x=1;" + keyword + ` /a\/\/b/g.test(s);` + "\nlater();"
		tokens := requireTail(t, source, "later", "(", ")", ";")
		for _, tk := range tokens {
			if tk.text == "b" {
				t.Fatalf("keyword %q leaked regular expression text", keyword)
			}
		}
	}
}

func TestDivisionIsNotARegex(t *testing.T) {
	for _, source := range []string{
		"x=a/b/c;later();",
		"x=a++ / 2 / c;later();",
		"x=a-- / 2 / c;later();",
		"x=a[0] / 2 / c;later();",
		"x=f(1) / 2 / g(2);later();",
		"x=(a+b) / 2 / c;later();",
		"x={}.n / 2 / c;later();",
		"x=1 / 2 / 3;later();",
		"x='s' / 2 / c;later();",
		"x=a.return / 2 / c;later();",
		"x=a.in / 2 / c;later();",
		"x=`t` / 2 / c;later();",
		"x=a<b/2/c;later();",
	} {
		tokens := requireTail(t, source, "later", "(", ")", ";")
		slashes := 0
		for _, tk := range tokens {
			if tk.text == "/" {
				slashes++
			}
		}
		if slashes != 2 {
			t.Fatalf("%q: %d division tokens, want 2: %q", source, slashes, tokenTexts(tokens))
		}
	}
}

func TestUnterminatedRegexCandidateIsDivision(t *testing.T) {
	// A slash after an operator with no closing slash on the line cannot be a
	// regular expression, so the rest of the line must not be swallowed.
	tokens := requireTail(t, "x=a& /2;later();\nmore();", "more", "(", ")", ";")
	if got := strings.Join(tokenTexts(tokens), " "); !strings.Contains(got, "later ( ) ;") {
		t.Fatalf("tokens: %s", got)
	}
}

func TestRegexClassAndEscapesAfterOperator(t *testing.T) {
	requireTail(t, `ok=a&&/[/]\/\/[^/]+/i.test(s)||b;later();`, "later", "(", ")", ";")
}

func TestJSXClosingTagIsNotARegex(t *testing.T) {
	requireTail(t, "x=<a>t</a><b>u</b>;later();", "later", "(", ")", ";")
}

func TestTemplateExpressionRegexAfterOperator(t *testing.T) {
	source := "const s=`v:${a&&/\\/\\//.test(b)?`n${c}`:/[}`]/.test(d)}`;later();"
	tokens := requireTail(t, source, "later", "(", ")", ";")
	requireBalanced(t, tokens)
	templates := 0
	for _, tk := range tokens {
		if tk.kind == 't' {
			templates++
			if !strings.HasSuffix(tk.text, "}`") || !strings.HasPrefix(tk.text, "`v:") {
				t.Fatalf("template token: %q", tk.text)
			}
		}
	}
	if templates != 1 {
		t.Fatalf("templates: %d", templates)
	}
	// Division and postfix operators inside a template expression stay division.
	tokens = requireTail(t, "s=`${a++ / 2 / b}${c / 2 / d}${e.in / 2 / f}`;later();", "later", "(", ")", ";")
	if len(tokens) != 8 || tokens[2].kind != 't' {
		t.Fatalf("tokens: %q", tokenTexts(tokens))
	}
}

func TestLexNoteOnlyForRealProblems(t *testing.T) {
	clean := []string{
		"function a(){return [1,2].map(x=>x/2)}\nb();\n//# sourceMappingURL=a.js.map\n",
		"a();\n/* trailing license */\n",
		"const s = `${a}/${b}`;\nconst r = /[(]\\)/;\n",
		"",
	}
	for _, code := range clean {
		if note := lexNote(code, lex(code)); note != "" {
			t.Fatalf("unexpected note %q for %q", note, code)
		}
	}
	broken := map[string]string{
		"function a(){ b(1;\n":          "Brackets",
		"a();\nvar s = 'never closed":   "unterminated",
		"a();\n/* never closed\nb();\n": "unterminated",
		"a(`never closed ${x}\nb();":    "unterminated",
		"a(); var s = 'x;\nb({);\n":     "unterminated",
	}
	for code, want := range broken {
		note := lexNote(code, lex(code))
		if !strings.Contains(note, want) {
			t.Fatalf("note %q for %q, want it to mention %q", note, code, want)
		}
	}
	file := Analyze([]Source{{Name: "cut.js", Data: []byte("function a(){ b(1;\n")}}).Files[0]
	if len(file.Notes) == 0 || !strings.Contains(file.Notes[0], "Brackets") {
		t.Fatalf("notes: %v", file.Notes)
	}
	ok := Analyze([]Source{{Name: "ok.js", Data: []byte("function a(){ b(1); }\na();\n")}}).Files[0]
	for _, note := range ok.Notes {
		if strings.Contains(note, "lost sync") || strings.Contains(note, "tokenized") {
			t.Fatalf("clean file got a lexer note: %v", ok.Notes)
		}
	}
}

func TestRegexWithSlashesDoesNotHideLaterRegistrations(t *testing.T) {
	code := "function ok(t){return!(top===self&&/^(?:chrome-extension|moz-extension):\\/\\//.test(t))}\n" +
		"self.addEventListener('activate',function(){clients.claim()});\n" +
		"chrome.runtime.onInstalled.addListener(function(){});\n"
	tokens := lex(code)
	requireBalanced(t, tokens)
	got := strings.Join(tokenTexts(tokens), " ")
	for _, want := range []string{"self . addEventListener ( 'activate'", "chrome . runtime . onInstalled . addListener ("} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in %s", want, got)
		}
	}
	file := Analyze([]Source{{Name: "worker.js", Data: []byte(code)}}).Files[0]
	var triggers []string
	for _, entry := range file.EntryPoints {
		triggers = append(triggers, entry.Trigger)
	}
	if !has(triggers, "activate event") || !has(file.Interactions, "event listeners") {
		t.Fatalf("code after the regular expression was not analysed: %v %v", triggers, file.Interactions)
	}
}
