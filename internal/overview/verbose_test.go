package overview

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// minifiedPad makes an input count as minified: most of its bytes sit on one
// very long line.
func minifiedPad() string {
	return "var pad=\"" + strings.Repeat("x", 5000) + "\";"
}

func renderText(t *testing.T, result Result, opts TextOptions) string {
	t.Helper()
	var out bytes.Buffer
	if err := WriteText(&out, result, opts); err != nil {
		t.Fatal(err)
	}
	return out.String()
}

const defaultFooter = "\nUse --verbose for structure, entry paths, and call flow.\n"

func TestVerboseReportStartsWithTheDefaultSections(t *testing.T) {
	result := Analyze([]Source{{Name: "studio_compiled.js", Data: []byte(closureAngularApp)}})
	plain := renderText(t, result, TextOptions{})
	verbose := renderText(t, result, TextOptions{Verbose: true})
	if !strings.HasSuffix(plain, defaultFooter) {
		t.Fatalf("default report lost its footer:\n%s", plain)
	}
	summary := strings.TrimSuffix(plain, defaultFooter)
	if !strings.HasPrefix(verbose, summary) {
		t.Fatalf("verbose report does not start with the default report.\ndefault:\n%s\nverbose:\n%s", summary, verbose)
	}
	if strings.Contains(verbose, "Use --verbose") || strings.Contains(verbose, "### What this file does") {
		t.Errorf("verbose report keeps the default footer or the old paragraph:\n%s", verbose)
	}
	if !strings.Contains(verbose, "### Main units") && !strings.Contains(verbose, "### Observed links") {
		t.Errorf("verbose report has no detail sections:\n%s", verbose)
	}
}

func TestVerboseReportRepeatsEachFilesSummaryInOrder(t *testing.T) {
	result := Analyze([]Source{
		{Name: "studio_compiled.js", Data: []byte(closureAngularApp)},
		{Name: "tool.js", Data: []byte("function run(){fetch('/api/v1/users')}run();")},
	})
	verbose := renderText(t, result, TextOptions{Verbose: true})
	last := -1
	for _, file := range result.Files {
		var block strings.Builder
		writeFilePurpose(&block, file)
		index := strings.Index(verbose, block.String())
		if index < 0 || index < last {
			t.Fatalf("summary of %s missing or out of order:\n%s", file.Name, verbose)
		}
		last = index
	}
}

func TestVerboseDoesNotRepeatTheIdentitySentence(t *testing.T) {
	result := Analyze([]Source{{Name: "serverRegistry.js", Data: []byte("const registry = require('./registry');\nfunction ServerRegistry(){ return registry.load(); }\nServerRegistry();")}})
	verbose := renderText(t, result, TextOptions{Verbose: true})
	identity := result.Files[0].Purpose.Is
	if strings.Count(verbose, identity) != 1 {
		t.Errorf("identity sentence %q repeated:\n%s", identity, verbose)
	}
}

func TestFlowSentenceAppearsOnceUnderItsOwnHeading(t *testing.T) {
	code := "function bootApp(){loadSettings()}function loadSettings(){}\ndocument.addEventListener('click', bootApp);\nbootApp();"
	result := Analyze([]Source{{Name: "app.js", Data: []byte(code)}})
	verbose := renderText(t, result, TextOptions{Verbose: true})
	if got := strings.Count(verbose, "Starts with bootApp()"); got != 1 {
		t.Fatalf("flow sentence printed %d times:\n%s", got, verbose)
	}
}

const mixedMinified = "function a(){c();loadSettings()}function c(){}function loadSettings(){fetchUsers()}function fetchUsers(){}\n" +
	"exports.a=a;exports.createServer=loadSettings;\n" +
	"a();bootServer();function bootServer(){}\n"

func TestMinifiedNamesAreHiddenAndCountedOnce(t *testing.T) {
	code := "function a(){b();c()}function b(){c()}function c(){}function d(){a()}\nexports.a=a;exports.b=b;\na();d();\n" + minifiedPad()
	result := Analyze([]Source{{Name: "app.min.js", Data: []byte(code)}})
	if !result.Files[0].Minified {
		t.Fatal("fixture is not recognized as minified")
	}
	verbose := renderText(t, result, TextOptions{Verbose: true})
	for _, hidden := range []string{"### Main units", "### Observed links", "### Exports", "### Entry paths", "### Loading", "function a", "a() → b()", "mermaid", "Starts with a()"} {
		if strings.Contains(verbose, hidden) {
			t.Errorf("minified detail %q is listed:\n%s", hidden, verbose)
		}
	}
	if got := strings.Count(verbose, "their names are minified, so they are not listed"); got != 1 {
		t.Fatalf("hidden counts stated %d times:\n%s", got, verbose)
	}
	for _, counted := range []string{"4 declarations", "call links", "2 exports", "entry paths"} {
		if !strings.Contains(verbose, counted) {
			t.Errorf("counts line lacks %q:\n%s", counted, verbose)
		}
	}
}

func TestMixedSectionsKeepReadableNamesAndCountTheRest(t *testing.T) {
	code := mixedMinified + minifiedPad()
	result := Analyze([]Source{{Name: "app.min.js", Data: []byte(code)}})
	file := result.Files[0]
	if !file.Minified {
		t.Fatal("fixture is not recognized as minified")
	}
	verbose := renderText(t, result, TextOptions{Verbose: true})
	for _, want := range []string{"function loadSettings", "function fetchUsers", "loadSettings() → fetchUsers()", "- createServer", "module load → bootServer()"} {
		if !strings.Contains(verbose, want) {
			t.Errorf("readable entry %q missing:\n%s", want, verbose)
		}
	}
	for _, hidden := range []string{"function a ", "function c ", "a() → c()", "- a\n", "→ a()", "Starts with"} {
		if strings.Contains(verbose, hidden) {
			t.Errorf("minified entry %q listed:\n%s", hidden, verbose)
		}
	}
	if got := strings.Count(verbose, "their names are minified"); got != 1 {
		t.Fatalf("hidden counts stated %d times:\n%s", got, verbose)
	}
	if !strings.Contains(verbose, "2 declarations") || !strings.Contains(verbose, "1 export") {
		t.Errorf("counts line wrong:\n%s", verbose)
	}
}

func TestStartsClauseNeedsReadableLeadingCalls(t *testing.T) {
	setup := "function a(){}function bootServer(){}function loadSettings(){}\n"
	for name, c := range map[string]struct {
		calls  string
		starts bool
	}{
		"minified call first":  {"a();bootServer();loadSettings();", false},
		"minified call second": {"bootServer();a();loadSettings();", false},
		"readable leading":     {"bootServer();loadSettings();a();", true},
	} {
		t.Run(name, func(t *testing.T) {
			result := Analyze([]Source{{Name: "app.min.js", Data: []byte(setup + c.calls + "\n" + minifiedPad())}})
			if !result.Files[0].Minified {
				t.Fatal("fixture is not recognized as minified")
			}
			verbose := renderText(t, result, TextOptions{Verbose: true})
			if got := strings.Contains(verbose, "Starts with bootServer() and loadSettings()"); got != c.starts {
				t.Errorf("starts clause = %v, want %v:\n%s", got, c.starts, verbose)
			}
			if got := readableStarts(result.Files[0]); got != c.starts {
				t.Errorf("readableStarts = %v, want %v", got, c.starts)
			}
		})
	}
}

func TestLoadingLineNeedsAReadableFirstEntry(t *testing.T) {
	base := File{
		Name: "app.min.js", Format: "script", Minified: true, Compiled: &Compiled{Boot: "Boots through the loader."},
		EntryPoints: []Entry{
			{Trigger: "module load", Action: "a()", Line: 1},
			{Trigger: "module load", Action: "bootServer()", Line: 2},
		},
	}
	out := renderText(t, Result{Sources: 1, Files: []File{base}}, TextOptions{Verbose: true})
	if strings.Contains(out, "bootServer()") || !strings.Contains(out, "Boots through the loader.") {
		t.Errorf("a later readable entry is presented as the first:\n%s", out)
	}
	readable := base
	readable.EntryPoints = []Entry{base.EntryPoints[1], base.EntryPoints[0]}
	out = renderText(t, Result{Sources: 1, Files: []File{readable}}, TextOptions{Verbose: true})
	if !strings.Contains(out, "- module load → bootServer()") {
		t.Errorf("a readable first entry is missing:\n%s", out)
	}
	base.Compiled.Boot = ""
	out = renderText(t, Result{Sources: 1, Files: []File{base}}, TextOptions{Verbose: true})
	if strings.Contains(out, "### Loading") {
		t.Errorf("empty Loading section:\n%s", out)
	}
}

func TestSourceMapNamesCountAsReadable(t *testing.T) {
	// "a" sits at column 9 and maps to "startApp"; "b" at column 17 to "loadUsers".
	code := "function a(){b()}function b(){}a();\n" + minifiedPad()
	mappings := encodeVLQ(9, 0, 0, 0, 0) + "," + encodeVLQ(8, 0, 0, 0, 1)
	data, _ := json.Marshal(map[string]any{"version": 3, "sources": []string{"src/main.ts"}, "names": []string{"fn", "go"}, "mappings": mappings})
	withMap := Analyze([]Source{{Name: "app.min.js", Data: []byte(code), Map: data, MapName: "inline source map"}})
	file := withMap.Files[0]
	if file.SourceMapUsed == "" || len(file.Units) == 0 || file.Units[0].OriginalName == "" {
		t.Fatalf("map not applied: %+v", file.Units)
	}
	verbose := renderText(t, withMap, TextOptions{Verbose: true})
	if !strings.Contains(verbose, "### Main units") || !strings.Contains(verbose, "(fn)") || !strings.Contains(verbose, "fn() → go()") {
		t.Errorf("restored names not listed:\n%s", verbose)
	}
	if strings.Contains(verbose, "not listed") || strings.Contains(verbose, "Source map not applied") {
		t.Errorf("restored names reported as hidden:\n%s", verbose)
	}

	without := Analyze([]Source{{Name: "app.min.js", Data: []byte(code)}})
	plain := renderText(t, without, TextOptions{Verbose: true})
	if strings.Contains(plain, "### Main units") || !strings.Contains(plain, "not listed") {
		t.Errorf("same code without the map still lists names:\n%s", plain)
	}
}

func TestReadableFilesKeepEveryDetailSection(t *testing.T) {
	code := "function run(){go()}function go(){fetch('/api/v1/users')}\nexports.run=run;\nrun();\n"
	result := Analyze([]Source{{Name: "tool.js", Data: []byte(code)}})
	verbose := renderText(t, result, TextOptions{Verbose: true})
	for _, want := range []string{"### Exports", "- run", "### Entry paths", "module load → run()", "### Main units", "function run", "### Observed links", "run() → go()", "### Startup and dispatch", "Starts with run()"} {
		if !strings.Contains(verbose, want) {
			t.Errorf("readable file lost %q:\n%s", want, verbose)
		}
	}
	if strings.Contains(verbose, "not listed") {
		t.Errorf("readable file reports hidden names:\n%s", verbose)
	}
}

func TestMinifiedDetailIsHiddenOnTheCompiledAndBundlePaths(t *testing.T) {
	diagram := "flowchart LR\n  n0[\"a()\"] --> n1[\"b()\"]\n  n0[\"a()\"] --> n2[\"c()\"]\n  n2[\"c()\"] --> n3[\"d()\"]\n  n3[\"d()\"] --> n4[\"e()\"]\n"
	base := File{
		Name: "app.min.js", Format: "script", Minified: true, Diagram: diagram,
		Units:   []Unit{{Name: "a", Kind: "function", Line: 1, Calls: []string{"b", "c"}}},
		Links:   []Link{{From: "a()", To: "b()", Line: 1}},
		Exports: []string{"a", "b"},
		EntryPoints: []Entry{
			{Trigger: "module load", Action: "a()", Line: 1},
		},
		MoreUnits: 40,
	}
	compiled := base
	compiled.Compiled = &Compiled{}
	bundle := base
	bundle.Bundle = &Bundle{}
	for name, file := range map[string]File{"plain": base, "compiled": compiled, "bundle": bundle} {
		t.Run(name, func(t *testing.T) {
			out := renderText(t, Result{Sources: 1, Files: []File{file}}, TextOptions{Verbose: true})
			for _, hidden := range []string{"function a", "a() → b()", "mermaid", "### Exports", "module load → a()"} {
				if strings.Contains(out, hidden) {
					t.Errorf("%s path lists %q:\n%s", name, hidden, out)
				}
			}
			if !strings.Contains(out, "41 declarations, 1 call link, 2 exports, and 1 entry path found; their names are minified, so they are not listed.") {
				t.Errorf("%s path lacks the counts line:\n%s", name, out)
			}
		})
	}
}

func TestDiagramKeepsOnlyReadableEdges(t *testing.T) {
	judge := nameJudge{strict: true}
	mixed := "flowchart LR\n" +
		"  n0[\"parseInput()\"] --> n1[\"readConfig()\"]\n" +
		"  n0[\"parseInput()\"] --> n2[\"a()\"]\n" +
		"  n1[\"readConfig()\"] -->|calls| n3[\"loadFile()\"]\n" +
		"  n3[\"loadFile()\"] --> n4[\"writeLog()\"]\n"
	got := readableDiagram(mixed, judge)
	if strings.Contains(got, "a()") || !strings.Contains(got, `n0["parseInput()"] --> n1["readConfig()"]`) || !strings.Contains(got, "|calls|") {
		t.Errorf("mixed diagram:\n%s", got)
	}
	if got := readableDiagram("flowchart LR\n  n0[\"a()\"] --> n1[\"b()\"]\n  n1[\"b()\"] --> n2[\"c()\"]\n  n2[\"c()\"] --> n3[\"d()\"]\n", judge); got != "" {
		t.Errorf("minified diagram kept:\n%s", got)
	}
	// Two readable edges are too little to draw.
	sparse := "flowchart LR\n  n0[\"parseInput()\"] --> n1[\"readConfig()\"]\n  n1[\"readConfig()\"] --> n2[\"loadFile()\"]\n  n0[\"parseInput()\"] --> n3[\"a()\"]\n"
	if got := readableDiagram(sparse, judge); got != "" {
		t.Errorf("sparse diagram kept:\n%s", got)
	}
	// A diagram with nothing minified is returned untouched.
	readable := "flowchart LR\n  n0[\"parseInput()\"] --> n1[\"readConfig()\"]\n"
	if got := readableDiagram(readable, judge); got != readable {
		t.Errorf("readable diagram changed:\n%s", got)
	}
}

func TestReadableNamePredicate(t *testing.T) {
	for name, want := range map[string]bool{
		"parseCSS": true, "run": false, "load": true, "ZK": false, "e.C": false, "Object.keys": true,
		"matchesId.get": false, "default.createElement()": true, "a_b": false, "snake_case": true, "o()": false,
	} {
		if got := readableName(name); got != want {
			t.Errorf("readableName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestHiddenNamesLine(t *testing.T) {
	for _, c := range []struct {
		hidden hiddenNames
		want   string
	}{
		{hiddenNames{}, ""},
		{hiddenNames{declarations: 1911, links: 1098}, "1,911 declarations and 1,098 call links found; their names are minified, so they are not listed."},
		{hiddenNames{exports: 1}, "1 export found; its name is minified, so it is not listed."},
		{hiddenNames{links: 12}, "12 call links found; their names are minified, so they are not listed."},
		{hiddenNames{declarations: 2, links: 3, exports: 4, entries: 5, registrations: 6}, "2 declarations, 3 call links, 4 exports, 5 entry paths, and 6 event registrations found; their names are minified, so they are not listed."},
	} {
		if got := c.hidden.line(); got != c.want {
			t.Errorf("line() = %q, want %q", got, c.want)
		}
	}
}

func TestJSONKeepsEveryNameInMinifiedFiles(t *testing.T) {
	code := mixedMinified + minifiedPad()
	result := Analyze([]Source{{Name: "app.min.js", Data: []byte(code + "\n//# sourceMappingURL=app.min.js.map\n")}})
	before, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	renderText(t, result, TextOptions{Verbose: true})
	renderText(t, result, TextOptions{Terminal: true, Color: true, Verbose: true})
	after, _ := json.Marshal(result)
	if !bytes.Equal(before, after) {
		t.Fatal("rendering text changed the result")
	}
	var jsonOut, jsonlOut bytes.Buffer
	if err := WriteJSON(&jsonOut, result); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSONL(&jsonlOut, result); err != nil {
		t.Fatal(err)
	}
	for _, output := range []string{jsonOut.String(), jsonlOut.String()} {
		compact := strings.Join(strings.Fields(output), "")
		if !strings.Contains(compact, `"name":"a"`) || !strings.Contains(compact, `"name":"c"`) || !strings.Contains(compact, `"exports":["a","createServer"]`) {
			t.Errorf("JSON lost a minified name:\n%s", output)
		}
		if strings.Contains(output, "Source map not applied") || strings.Contains(output, "not listed") || strings.Contains(output, "mapHint") {
			t.Errorf("text-only wording leaked into JSON:\n%s", output)
		}
		if !strings.Contains(output, `"source_map_reference"`) {
			t.Errorf("source map reference missing from JSON:\n%s", output)
		}
	}
}

func TestTerminalVerboseRendersNewLines(t *testing.T) {
	code := mixedMinified + minifiedPad() + "\n//# sourceMappingURL=app.min.js.map\n"
	result := Analyze([]Source{{Name: "app.min.js", Data: []byte(code)}})
	terminal := renderText(t, result, TextOptions{Terminal: true, Verbose: true})
	colored := renderText(t, result, TextOptions{Terminal: true, Color: true, Verbose: true})
	for _, want := range []string{"Source map not applied.", "not listed"} {
		if !strings.Contains(terminal, want) {
			t.Errorf("terminal output lacks %q:\n%s", want, terminal)
		}
	}
	if strings.Contains(terminal, "```") || strings.Contains(terminal, "\x1b[") || !strings.Contains(colored, "\x1b[") {
		t.Errorf("terminal or color handling wrong:\n%s", colored)
	}
	if !strings.Contains(colored, "Source map not applied.") || !strings.Contains(colored, "not listed") {
		t.Errorf("colored output lost the new lines:\n%s", colored)
	}
}

func TestSourceMapHintLine(t *testing.T) {
	const reference = "\n//# sourceMappingURL="
	for _, c := range []struct {
		name, header, raw string
		want              []string
		wantNone          bool
	}{
		{name: "https://cdn.example/js/app.min.js", raw: "a();" + reference + "app.min.js.map",
			want: []string{"Source map not applied.", "jshound -s https://cdn.example/js/app.min.js --output ./source, then jshound --overview ./source"}},
		{name: "https://cdn.example/app.js", header: "/maps/app.js.map", raw: "a();",
			want: []string{"jshound -s https://cdn.example/app.js --output ./source"}},
		{name: "dist/app.js", raw: "a();" + reference + "app.js.map",
			want: []string{"app.js.map was not found in this file's folder", "put it there and rerun"}},
		{name: "dist/app.js", raw: "a();" + reference + "maps/app.js.map?v=2",
			want: []string{"maps/app.js.map was not found"}},
		{name: "dist/app.js", raw: "a();" + reference + "https://maps.example/app.js.map",
			want: []string{"jshound -s dist/app.js --output ./source, then jshound --overview ./source", "to fetch the map"}},
		{name: "dist/app.js", raw: "a();" + reference + "../maps/app.js.map",
			want: []string{"reads maps only from this file's folder", "jshound -s dist/app.js --output ./source"}},
		{name: "dist/app.js", raw: "a();" + reference + "/srv/maps/app.js.map",
			want: []string{"jshound -s dist/app.js --output ./source"}},
		{name: "my app/app.js", raw: "a();" + reference + "/srv/app.js.map",
			want: []string{"jshound -s 'my app/app.js' --output"}},
		{name: "stdin", raw: "a();" + reference + "https://maps.example/app.js.map",
			want: []string{"jshound -s https://maps.example/app.js.map --output ./source, then jshound --overview ./source"}},
		{name: "stdin", raw: "a();" + reference + "app.js.map",
			want: []string{"Piped input has no folder", "save the map as app.js.map", "jshound -s app.js.map --output ./source"}},
		{name: "dist/app.js", raw: "a();" + reference + "data:application/json;base64,e30=", wantNone: true},
		{name: "dist/app.js", raw: "a();" + reference + "file:///tmp/app.js.map", wantNone: true},
		{name: "dist/app.js", raw: "a();", wantNone: true},
	} {
		got := sourceMapHint(c.name, c.header, c.raw)
		if c.wantNone {
			if got != "" {
				t.Errorf("%s %q: hint %q, want none", c.name, c.raw, got)
			}
			continue
		}
		if strings.Contains(got, "\n") {
			t.Errorf("hint spans lines: %q", got)
		}
		for _, want := range c.want {
			if !strings.Contains(got, want) {
				t.Errorf("%s %q: hint %q lacks %q", c.name, c.raw, got, want)
			}
		}
	}
}

func TestSourceMapHintSitsUnderTheHeaderInBothReports(t *testing.T) {
	code := "function a(){b()}function b(){}a();\n//# sourceMappingURL=app.js.map\n"
	result := Analyze([]Source{{Name: "dist/app.js", Data: []byte(code)}})
	for name, opts := range map[string]TextOptions{"default": {}, "verbose": {Verbose: true}} {
		out := renderText(t, result, opts)
		lines := strings.Split(out, "\n")
		found := false
		for i, line := range lines {
			if strings.HasSuffix(line, "source map referenced") {
				found = i+1 < len(lines) && strings.HasPrefix(lines[i+1], "Source map not applied. app.js.map was not found")
			}
		}
		if !found {
			t.Errorf("%s report lacks the hint under the header:\n%s", name, out)
		}
	}

	// Applied, absent, or undecodable maps get no hint.
	mapData := []byte(`{"version":3,"sources":["src/a.ts"],"names":["start"],"mappings":"SAAAA"}`)
	applied := Analyze([]Source{{Name: "dist/app.js", Data: []byte(code), Map: mapData, MapName: "app.js.map"}})
	plain := Analyze([]Source{{Name: "dist/app.js", Data: []byte("function a(){}a();")}})
	broken := Analyze([]Source{{Name: "dist/app.js", Data: []byte(code), Map: []byte("{"), MapName: "app.js.map"}})
	for name, r := range map[string]Result{"applied": applied, "no reference": plain, "undecodable": broken} {
		if out := renderText(t, r, TextOptions{Verbose: true}); strings.Contains(out, "Source map not applied") {
			t.Errorf("%s: unexpected hint:\n%s", name, out)
		}
	}
	if out := renderText(t, applied, TextOptions{}); !strings.Contains(out, "source map applied") {
		t.Errorf("applied map not reported:\n%s", out)
	}
}

func TestTerminalGraphDrawsOnlyReadableEdges(t *testing.T) {
	file := File{
		Name: "app.min.js", Format: "script", Minified: true,
		Diagram: "flowchart LR\n" +
			"  n0[\"parseInput()\"] --> n1[\"readConfig()\"]\n" +
			"  n0[\"parseInput()\"] --> n2[\"a()\"]\n" +
			"  n1[\"readConfig()\"] --> n3[\"loadFile()\"]\n" +
			"  n3[\"loadFile()\"] --> n4[\"writeLog()\"]\n",
	}
	out := renderText(t, Result{Sources: 1, Files: []File{file}}, TextOptions{Terminal: true, Verbose: true})
	for _, want := range []string{"● parseInput()", "→ readConfig()", "→ loadFile()", "→ writeLog()"} {
		if !strings.Contains(out, want) {
			t.Errorf("terminal graph lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "a()") || strings.Contains(out, "```") {
		t.Errorf("terminal graph kept a minified edge or the Mermaid source:\n%s", out)
	}
}
