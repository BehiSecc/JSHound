package overview

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// shortName gives A, B, ..., Z, AA, AB, ...: names a compiler hands out.
func shortName(i int) string {
	name := ""
	for ; i >= 0; i = i/26 - 1 {
		name = string(rune('A'+i%26)) + name
	}
	return name
}

// wrappedCode has short lines, as compiler output that wraps its lines does,
// and declares short functions followed by readable ones.
func wrappedCode(short, readable int) string {
	var b strings.Builder
	for i := 0; i < short; i++ {
		fmt.Fprintf(&b, "function %s(c){return c+%d}\n", shortName(i), i)
	}
	for i := 0; i < readable; i++ {
		fmt.Fprintf(&b, "function loadItem%d(c){return c+%d}\n", i, i)
	}
	b.WriteString("A();B();\n")
	return b.String()
}

func TestShortLinesWithMinifiedNamesCountAsMinified(t *testing.T) {
	code := wrappedCode(40, 0)
	if minified(code) {
		t.Fatal("fixture is minified by its lines; it must not be")
	}
	result := Analyze([]Source{{Name: "compiled.js", Data: []byte(code)}})
	file := result.Files[0]
	if !file.Minified {
		t.Fatal("a file whose declared names are all minified is not recognized as minified")
	}
	profile := fileProfile(file)
	if strings.Count(profile, "minified") != 1 || !strings.HasSuffix(profile, " · minified") {
		t.Errorf("header does not say the code is minified once: %q", profile)
	}
	verbose := renderText(t, result, TextOptions{Verbose: true})
	for _, hidden := range []string{"function A ", "### Main units", "module load → A()", "Starts with A()"} {
		if strings.Contains(verbose, hidden) {
			t.Errorf("minified detail %q is listed:\n%s", hidden, verbose)
		}
	}
	if !strings.Contains(verbose, "40 declarations") || !strings.Contains(verbose, "their names are minified, so they are not listed") {
		t.Errorf("counts line missing:\n%s", verbose)
	}
	var json bytes.Buffer
	if err := WriteJSON(&json, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(strings.Fields(json.String()), ""), `"minified":true`) || !strings.Contains(json.String(), `"name": "A"`) {
		t.Errorf("JSON lacks the minified field or keeps no names:\n%s", json.String())
	}
}

func TestMinifiedNamesNeedEnoughMostlyUnreadableDeclarations(t *testing.T) {
	for _, tc := range []struct {
		name            string
		short, readable int
		want            bool
	}{
		{"all short", 25, 0, true},
		{"nine in ten", 18, 2, true},
		{"just under nine in ten", 17, 3, false},
		{"readable with a few short names", 3, 30, false},
		{"one name short of the minimum", 19, 0, false},
		{"tiny file", 4, 0, false},
	} {
		file := Analyze([]Source{{Name: "app.js", Data: []byte(wrappedCode(tc.short, tc.readable))}}).Files[0]
		if file.Minified != tc.want {
			t.Errorf("%s: minified = %v, want %v", tc.name, file.Minified, tc.want)
		}
		if got := strings.Contains(fileProfile(file), "minified"); got != tc.want {
			t.Errorf("%s: header says minified = %v, want %v: %q", tc.name, got, tc.want, fileProfile(file))
		}
	}
}

func TestMethodNamesDoNotMakeAFileMinified(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 25; i++ {
		fmt.Fprintf(&b, "class Service%d {\n  run() {}\n  get() {}\n  add() {}\n}\n", i)
	}
	if file := Analyze([]Source{{Name: "services.js", Data: []byte(b.String())}}).Files[0]; file.Minified {
		t.Fatalf("readable classes with short method names are minified: %q", fileProfile(file))
	}
}

func TestLineMinifiedFilesKeepTheirBuildLabel(t *testing.T) {
	code := wrappedCode(40, 0) + minifiedPad()
	file := Analyze([]Source{{Name: "app.min.js", Data: []byte(code)}}).Files[0]
	if !file.Minified || file.Build != "minified" || fileProfile(file) != "script · "+humanBytes(file.Bytes)+" · minified" {
		t.Fatalf("line-minified file: minified=%v build=%q profile=%q", file.Minified, file.Build, fileProfile(file))
	}
}

const dispatchBody = "{switch(%s){case \"alpha\":run();break;case \"beta\":stop();break;case \"gamma\":wait();break}}\n"

func dispatchRows(t *testing.T, code string) (string, bool) {
	t.Helper()
	result := Analyze([]Source{{Name: "app.js", Data: []byte(code)}})
	return renderText(t, result, TextOptions{Verbose: true}), result.Files[0].Minified
}

func TestDispatchRowsInMinifiedFilesHideMinifiedNames(t *testing.T) {
	code := wrappedCode(40, 0) +
		"function Z0(a)" + fmt.Sprintf(dispatchBody, "a") +
		"function handleMessage(a)" + fmt.Sprintf(dispatchBody, "request.kind") +
		"function routeEvent(e)" + fmt.Sprintf(dispatchBody, "e.type")
	verbose, minifiedFile := dispatchRows(t, code)
	if !minifiedFile {
		t.Fatal("fixture is not recognized as minified")
	}
	for _, want := range []string{
		"- switch — 3 cases: alpha, beta, gamma (line 42)",
		"- handleMessage() · switch on request.kind — 3 cases: alpha, beta, gamma (line 43)",
		"- routeEvent() · switch — 3 cases: alpha, beta, gamma (line 44)",
	} {
		if !strings.Contains(verbose, want) {
			t.Errorf("dispatch row %q missing:\n%s", want, verbose)
		}
	}
	for _, hidden := range []string{"Z0()", "switch on a ", "switch on e.type"} {
		if strings.Contains(verbose, hidden) {
			t.Errorf("minified name %q printed in a dispatch row:\n%s", hidden, verbose)
		}
	}
}

func TestDispatchRowsKeepEveryNameInReadableFiles(t *testing.T) {
	code := wrappedCode(3, 40) + "function Z0(a)" + fmt.Sprintf(dispatchBody, "a")
	verbose, minifiedFile := dispatchRows(t, code)
	if minifiedFile {
		t.Fatal("fixture is recognized as minified")
	}
	if want := "- Z0() · switch on a — 3 cases: alpha, beta, gamma (line 45)"; !strings.Contains(verbose, want) {
		t.Errorf("dispatch row %q missing:\n%s", want, verbose)
	}
}

func TestRoutesAndHandlerMapRowsHideOnlyMinifiedNames(t *testing.T) {
	strict := nameJudge{strict: true}
	cases := []string{"x", "y", "z"}
	for _, tc := range []struct {
		dispatch Dispatch
		want     string
	}{
		{Dispatch{Kind: "routes", On: "app", Cases: cases, Count: 3, Line: 1}, "route registrations — 3 routes: x, y, z (line 1)"},
		{Dispatch{Kind: "routes", On: "server", Cases: cases, Count: 3, Line: 1}, "server route registrations — 3 routes: x, y, z (line 1)"},
		{Dispatch{Kind: "handler map h", On: "k", Cases: cases, Count: 3, Line: 1}, "handler map — 3 handlers: x, y, z (line 1)"},
		{Dispatch{Kind: "handler map handlers", On: "k", Cases: cases, Count: 3, Line: 1}, "handler map handlers — 3 handlers: x, y, z (line 1)"},
		{Dispatch{Kind: "handler map handlers", On: "request.kind", Cases: cases, Count: 3, Line: 1}, "handlers[request.kind]() handler map — 3 handlers: x, y, z (line 1)"},
	} {
		if got := describeDispatch(strict.dispatchRow(tc.dispatch)); got != tc.want {
			t.Errorf("%s %s: got %q, want %q", tc.dispatch.Kind, tc.dispatch.On, got, tc.want)
		}
	}
	lax := nameJudge{}
	short := Dispatch{Kind: "handler map h", In: "f", On: "k", Cases: cases, Count: 3, Line: 1}
	if got := describeDispatch(lax.dispatchRow(short)); got != "f() · h[k]() handler map — 3 handlers: x, y, z (line 1)" {
		t.Errorf("a file that is not minified lost names: %q", got)
	}
}
