package overview

import (
	"fmt"
	"strings"
	"testing"
)

func evidenceLines(text string) []string {
	var lines []string
	for _, line := range strings.Split(text, "\n") {
		if runEvidenceLine.MatchString(line) {
			lines = append(lines, line)
		}
	}
	return lines
}

func TestVerboseShowsRunEvidenceWithCountsAndLocations(t *testing.T) {
	// fetch is used three times, first at line 2 column 5; the WebSocket once.
	code := "var a=1;\nvar b=fetch(\"/a\");fetch(\"/b\");\nvar w=new WebSocket(\"wss://x.test\");fetch(\"/c\");\n"
	result := Analyze([]Source{src("tool.js", code)})
	verbose := renderText(t, result, TextOptions{Verbose: true})
	want := "- Server calls: fetch and WebSocket\n    fetch: 3 uses, first at line 2:7 · WebSocket: 1 use, first at line 3:11\n"
	if !strings.Contains(verbose, want) {
		t.Errorf("evidence line missing or wrong:\n%s", verbose)
	}
	plain := renderText(t, result, TextOptions{})
	if len(evidenceLines(plain)) != 0 || strings.Contains(plain, "first at") {
		t.Errorf("default output shows evidence:\n%s", plain)
	}
	if without := withoutRunEvidence(verbose); !strings.Contains(without, "- Server calls: fetch and WebSocket\n\n") && !strings.Contains(without, "- Server calls: fetch and WebSocket\n") {
		t.Errorf("fact line changed:\n%s", without)
	}
}

func TestVerboseRollUpNamesTheFileOfFirstUse(t *testing.T) {
	a := "window.name=1;\nfetch(\"/a\");fetch(\"/b\");"
	b := "document.title=\"x\";fetch(\"/c\");"
	result := Analyze([]Source{src("dist/a.js", a), src("dist/b.js", b)})
	verbose := renderText(t, result, TextOptions{Verbose: true})
	rollup := verbose[:strings.Index(verbose, "\n## dist/a.js\n")]
	if want := "    fetch: 3 uses, first at a.js line 2:1\n"; !strings.Contains(rollup, want) {
		t.Errorf("roll-up evidence lacks the file:\n%s", rollup)
	}
	// The file sections beneath it do not name files.
	for _, line := range evidenceLines(verbose[len(rollup):]) {
		if strings.Contains(line, ".js") {
			t.Errorf("a file's own evidence names a file: %s", line)
		}
	}
	if def := renderText(t, result, TextOptions{}); len(evidenceLines(def)) != 0 {
		t.Errorf("default roll-up shows evidence:\n%s", def)
	}
}

func TestRunEvidenceLineCapsLongListsAndHandlesPlurals(t *testing.T) {
	var evidence []RunEvidence
	for i := 1; i <= 7; i++ {
		evidence = append(evidence, RunEvidence{Signal: fmt.Sprintf("chrome.api%d", i), Uses: i, Line: i, Column: 1})
	}
	got := runEvidenceText(evidence, shortNames{})
	want := "chrome.api1: 1 use, first at line 1:1 · chrome.api2: 2 uses, first at line 2:1 · chrome.api3: 3 uses, first at line 3:1 · chrome.api4: 4 uses, first at line 4:1 · +3 more"
	if got != want {
		t.Errorf("line = %q", got)
	}
	if got := runEvidenceText([]RunEvidence{{Signal: "x", Uses: 2}}, shortNames{}); got != "x: 2 uses" {
		t.Errorf("no location = %q", got)
	}
	if runEvidenceText(nil, shortNames{}) != "" {
		t.Error("empty evidence has a line")
	}
}

func TestVerboseRunEvidenceInEveryFileKind(t *testing.T) {
	// Plain, compiled (Closure/Angular), and Webpack-bundle files share the
	// purpose writer, so each shows evidence under its run facts.
	files := map[string]string{
		"plain.js":           "function go(){fetch('/api/v1/users')}go();",
		"studio_compiled.js": closureAngularApp + "\nfetch(\"/api/v1/c\");",
		"webpack.js":         `(self.webpackChunkapp=self.webpackChunkapp||[]).push([[1],{1:function(e,t,n){fetch("/api/v1/x")}}]);`,
	}
	for name, code := range files {
		result := Analyze([]Source{src(name, code)})
		if result.Files[0].Purpose == nil || len(result.Files[0].Purpose.Runs) == 0 {
			t.Fatalf("%s has no run facts", name)
		}
		file := result.Files[0]
		switch name {
		case "studio_compiled.js":
			if !strings.Contains(file.Purpose.Is, "Closure Compiler") {
				t.Fatalf("not a Closure-compiled file: %q", file.Purpose.Is)
			}
		case "webpack.js":
			if file.Bundle == nil {
				t.Fatalf("webpack.js is not a bundle")
			}
		}
		verbose := renderText(t, result, TextOptions{Verbose: true})
		if len(evidenceLines(verbose)) == 0 {
			t.Errorf("%s: no evidence in verbose output:\n%s", name, verbose)
		}
		if plain := renderText(t, result, TextOptions{}); len(evidenceLines(plain)) != 0 {
			t.Errorf("%s: evidence in default output", name)
		}
	}
}

func TestRunEvidenceLineIsDimInTerminals(t *testing.T) {
	text := "### Behavior\n\n- Server calls: fetch\n    fetch: 3 uses, first at line 2:7\n- Storage: IndexedDB\n    indexedDB: 1 use, first at line 4:2 · caches.open: 1 use, first at line 5:1\n    1 API call (/api/x)\n"
	out := colorizeOverview(text)
	for _, line := range []string{"    fetch: 3 uses, first at line 2:7", "    indexedDB: 1 use, first at line 4:2 · caches.open: 1 use, first at line 5:1"} {
		if !strings.Contains(out, "\x1b[2m"+line+"\x1b[0m") {
			t.Errorf("evidence line is not dim: %q\n%q", line, out)
		}
	}
	if strings.Contains(out, "\x1b[36m•\x1b[0m     ") {
		t.Errorf("evidence line was bulleted: %q", out)
	}
}
