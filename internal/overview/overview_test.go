package overview

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestOverviewShowsExportAndEventWithoutInventingStartupCall(t *testing.T) {
	result := Analyze([]Source{{Name: "app.js", Data: []byte("export function greet(name) { return name; }\ndocument.addEventListener('DOMContentLoaded', () => greet('Ada'));\n")}})
	file := result.Files[0]
	if file.Format != "ES module" || !has(file.Exports, "greet") {
		t.Fatalf("module structure: %#v", file)
	}
	if len(file.Units) != 1 || file.Units[0].Name != "greet" || file.Units[0].Line != 1 {
		t.Fatalf("unit: %#v", file.Units)
	}
	if len(file.EntryPoints) != 1 || file.EntryPoints[0].Trigger != "DOMContentLoaded event" || file.EntryPoints[0].Action != "greet()" {
		t.Fatalf("entry: %#v", file.EntryPoints)
	}
	if file.Diagram != "" {
		t.Fatalf("trivial file should not get a diagram: %s", file.Diagram)
	}
}

func TestOverviewShowsObservedLifecycleAndMermaid(t *testing.T) {
	code := `import { formatUser } from './format.js';
export async function loadUsers() {
  const response = await fetch('/api/users');
  render(await response.json());
}
function render(users) { document.querySelector('#app').textContent = formatUser(users); }
function start() { loadUsers(); }
document.addEventListener('DOMContentLoaded', start);
`
	file := Analyze([]Source{{Name: "app.js", Data: []byte(code)}}).Files[0]
	if !has(file.Imports, "./format.js") || !has(file.Exports, "loadUsers") {
		t.Fatalf("imports/exports: %#v", file)
	}
	for _, expected := range []Link{
		{From: "DOMContentLoaded event", To: "start()", Line: 8},
		{From: "start()", To: "loadUsers()", Line: 7},
		{From: "loadUsers()", To: "render()", Line: 4},
	} {
		if !hasLink(file.Links, expected) {
			t.Errorf("missing link %#v in %#v", expected, file.Links)
		}
	}
	if !has(file.Interactions, "HTTP via fetch()") || file.Diagram == "" || !strings.Contains(file.Diagram, "flowchart LR") {
		t.Fatalf("flow/interactions: %#v", file)
	}
	var output bytes.Buffer
	if err := WriteText(&output, Result{Sources: 1, Files: []File{file}}, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"### Entry paths", "### Main units", "### Observed links", "```mermaid", "fetch()"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("missing %q:\n%s", expected, output.String())
		}
	}
}

func TestOverviewKeepsSourcesSeparate(t *testing.T) {
	result := Analyze([]Source{
		{Name: "main.js", Data: []byte("import { render } from './view.js'; function start() { render(); } start();")},
		{Name: "view.js", Data: []byte("export function render() { document.createElement('div'); }")},
	})
	if result.Sources != 2 || len(result.Files) != 2 {
		t.Fatalf("files: %#v", result)
	}
	if !has(result.Files[0].Imports, "./view.js") || !has(result.Files[1].Exports, "render") {
		t.Fatalf("module facts: %#v", result.Files)
	}
	for _, link := range result.Files[0].Links {
		if link.To == "render()" {
			t.Fatalf("cross-file call was treated as locally resolved: %#v", link)
		}
	}
}

func TestOverviewIgnoresCommentsStringsRegexAndTemplateText(t *testing.T) {
	code := "// fetch('/api/users'); function fake() {}\n" +
		"/* const hidden = () => render(); */\n" +
		"const prose = \"function madeUp() { fetch('/api') }\";\n" +
		"const regex = /function\\s+ghost\\(\\)/;\n" +
		"const template = " + "`fetch('/api/other')`" + ";\n" +
		"export function real() { return prose; }\n"
	file := Analyze([]Source{{Name: "library.js", Data: []byte(code)}}).Files[0]
	if len(file.Units) != 1 || file.Units[0].Name != "real" || len(file.Interactions) != 0 {
		t.Fatalf("decoy content became structure: %#v", file)
	}
}

func TestOverviewHTMLInlineScriptKeepsSourceLines(t *testing.T) {
	file := Analyze([]Source{{Name: "index.html", Data: []byte("<html>\n<body>\n<script>\nfunction boot() { fetch('/api'); }\nboot();\n</script>\n</body></html>")}}).Files[0]
	if file.Format != "HTML inline scripts" || len(file.Units) != 1 || file.Units[0].Line != 4 {
		t.Fatalf("HTML structure: %#v", file)
	}
	if len(file.EntryPoints) != 1 || file.EntryPoints[0].Line != 5 {
		t.Fatalf("HTML entry: %#v", file.EntryPoints)
	}
}

func TestOverviewCommonJSAndEmptyInput(t *testing.T) {
	file := Analyze([]Source{{Name: "sdk.js", Data: []byte("const send = (x) => fetch(x); module.exports = { send };")}}).Files[0]
	if file.Format != "CommonJS module" || !has(file.Exports, "module.exports") || len(file.Units) != 1 || file.Units[0].Name != "send" {
		t.Fatalf("CommonJS structure: %#v", file)
	}
	if len(file.EntryPoints) != 0 || len(file.Notes) == 0 {
		t.Fatalf("library should not imply startup: %#v", file)
	}
	empty := Analyze([]Source{{Name: "empty.js"}}).Files[0]
	if len(empty.Units) != 0 || len(empty.Notes) == 0 || empty.Notes[0] != "No JavaScript code found." {
		t.Fatalf("empty input: %#v", empty)
	}
}

func TestOverviewCompiledChunkStaysConcise(t *testing.T) {
	code := "self.webpackChunkdemo=self.webpackChunkdemo||[];" +
		"(self.webpackChunkdemo).push([[42],{101:(e,t,r)=>{fetch('/api')},202:function(e,t,r){}}]);"
	file := Analyze([]Source{{Name: "chunk.js", Data: []byte(code)}}).Files[0]
	if file.Format != "loader chunk" || !strings.Contains(strings.Join(file.Shape, " "), "2 module factories in chunk 42") {
		t.Fatalf("chunk structure: %#v", file)
	}
	if len(file.Units) != 0 || len(file.Links) != 0 || len(file.EntryPoints) != 1 {
		t.Fatalf("chunk report is noisy or lacks registration: %#v", file)
	}
}

func TestOverviewUMDWrapper(t *testing.T) {
	code := "!function(g,f){typeof module==='object'&&module.exports?module.exports=f():typeof define==='function'&&define.amd?define([],f):g.sdk=f()}(this,function(){return {}});"
	file := Analyze([]Source{{Name: "sdk.js", Data: []byte(code)}}).Files[0]
	if file.Format != "UMD bundle" || len(file.EntryPoints) != 1 || file.EntryPoints[0].Action != "runs the UMD wrapper" {
		t.Fatalf("UMD wrapper: %#v", file)
	}
}

func TestNestedEventIsRegistrationNotStartup(t *testing.T) {
	code := "function setup(){button.addEventListener('click', handleClick)} function handleClick(){}"
	file := Analyze([]Source{{Name: "app.js", Data: []byte(code)}}).Files[0]
	if len(file.EntryPoints) != 0 || len(file.Registrations) != 1 || file.Registrations[0].RegisteredBy != "setup()" {
		t.Fatalf("nested listener was called startup: %#v", file)
	}
}

func TestOverviewJSONAndJSONL(t *testing.T) {
	result := Analyze([]Source{{Name: "app.js", Data: []byte("function start() {} start();")}})
	result.Version = "test"
	var jsonOutput, jsonlOutput bytes.Buffer
	if err := WriteJSON(&jsonOutput, result); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSONL(&jsonlOutput, result); err != nil {
		t.Fatal(err)
	}
	var decoded Result
	if err := json.Unmarshal(jsonOutput.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Version != "test" || decoded.Files[0].Units[0].Name != "start" {
		t.Fatalf("JSON: %#v", decoded)
	}
	var line map[string]any
	if err := json.Unmarshal(jsonlOutput.Bytes(), &line); err != nil {
		t.Fatal(err)
	}
	if line["kind"] != "overview" || line["name"] != "app.js" || line["version"] != "test" {
		t.Fatalf("JSONL: %#v", line)
	}
}

func BenchmarkOverviewMinifiedBundle(b *testing.B) {
	code := strings.Repeat("function a(){return fetch('/api')};a();", 40000)
	source := []Source{{Name: "bundle.js", Data: []byte(code)}}
	b.SetBytes(int64(len(code)))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		Analyze(source)
	}
}

func has(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func hasLink(values []Link, wanted Link) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}
