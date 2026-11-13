package app

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BehiSecc/JSHound/internal/report"
)

func TestRunStdinWithoutSecrets(t *testing.T) {
	input := `<form action="/api/v1/users?role=admin"><input name="email"></form><script>query CurrentUser { viewer { id } }</script>`
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"-p", "--wordlist", "--format", "json"}, Config{Version: "test", Stdin: bytes.NewBufferString(input), Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	var result report.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"endpoint", "parameter", "graphql_operation", "word"} {
		if result.Summary[kind] == 0 {
			t.Errorf("missing %s: %#v", kind, result)
		}
	}
}

func TestScanReportsRPCMethodsInEveryFormat(t *testing.T) {
	input := `function A(){this.i="OrderService";this.a=reg(this,"listOrders");this.b=reg(this,"cancelOrder");this.c=reg(this,"getOrder")}x("BillingService.PayInvoice")`
	run := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), args, Config{Version: "test", Stdin: bytes.NewBufferString(input), Stdout: &stdout, Stderr: &stderr}); code != 0 {
			t.Fatalf("exit %d: %s", code, stderr.String())
		}
		return stdout.String()
	}
	var result report.Result
	if err := json.Unmarshal([]byte(run("--format", "json", "--wordlist")), &result); err != nil {
		t.Fatal(err)
	}
	var methods, words []string
	for _, finding := range result.Findings {
		switch finding.Kind {
		case "rpc_method":
			methods = append(methods, finding.Value)
		case "word":
			words = append(words, finding.Value)
		}
	}
	if strings.Join(methods, " ") != "BillingService.PayInvoice OrderService.cancelOrder OrderService.getOrder OrderService.listOrders" || result.Summary["rpc_method"] != 4 {
		t.Errorf("methods = %v, summary = %v", methods, result.Summary)
	}
	for _, want := range []string{"orderservice", "listorders", "payinvoice"} {
		if !strings.Contains(" "+strings.Join(words, " ")+" ", " "+want+" ") {
			t.Errorf("wordlist lacks %q: %v", want, words)
		}
	}
	if lines := run("--format", "jsonl"); strings.Count(lines, `"kind":"rpc_method"`) != 4 {
		t.Errorf("jsonl:\n%s", lines)
	}
	text := run()
	if !strings.Contains(text, "4 RPC methods") || !strings.Contains(text, "RPC METHODS (4)") || !strings.Contains(text, "OrderService.listOrders") {
		t.Errorf("text:\n%s", text)
	}
}

func TestScanWithoutRPCMethodsHasNoRPCOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--format", "json"}, Config{Version: "test", Stdin: bytes.NewBufferString(`fetch("/api/v1/users")`), Stdout: &stdout, Stderr: &stderr})
	if code != 0 || strings.Contains(stdout.String(), "rpc") {
		t.Fatalf("exit %d: %s", code, stdout.String())
	}
}

func TestRejectsInvalidFormat(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run(context.Background(), []string{"--format", "xml"}, Config{Version: "test", Stderr: &stderr}); code != 2 {
		t.Fatalf("exit %d", code)
	}
}

func TestPositionalURLRequiresU(t *testing.T) {
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"https://example.test/app.js"}, Config{Version: "test", Stderr: &stderr})
	if code != 2 || !strings.Contains(stderr.String(), "require -u") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestValidateSecretsRequiresExplicitKingfisher(t *testing.T) {
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"--validate-secrets"}, Config{Version: "test", Stdin: bytes.NewBuffer(nil), Stdout: &bytes.Buffer{}, Stderr: &stderr})
	if code != 2 || !strings.Contains(stderr.String(), "requires -kingfisher") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestDefaultScanNeverStartsKingfisherFromPath(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "started")
	binary := filepath.Join(dir, "kingfisher")
	script := "#!/bin/sh\ntouch '" + marker + "'\nexit 99\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--format", "json"}, Config{Version: "test", Stdin: bytes.NewBufferString(`const api="/api/v1/demo"`), Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("Kingfisher was started implicitly")
	}
}

func TestSourceMapModeExtractsDirectMap(t *testing.T) {
	dir := t.TempDir()
	mapPath := filepath.Join(dir, "app.js.map")
	output := filepath.Join(dir, "src")
	data := `{"version":3,"sources":["src/app.ts"],"sourcesContent":["export const x=1"],"mappings":""}`
	if err := os.WriteFile(mapPath, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"-s", mapPath, "--output", output}, Config{Version: "test", Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	content, err := os.ReadFile(filepath.Join(output, "src", "app.ts"))
	if err != nil || string(content) != "export const x=1" {
		t.Fatalf("content=%q err=%v", content, err)
	}
}

func TestSourceMapModeDiscoversMapFromJS(t *testing.T) {
	dir := t.TempDir()
	jsPath := filepath.Join(dir, "app.js")
	output := filepath.Join(dir, "src")
	if err := os.WriteFile(jsPath, []byte("//# sourceMappingURL=app.js.map"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.js.map"), []byte(`{"version":3,"sources":["original.js"],"sourcesContent":["alert(1)"],"mappings":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"--sourcemap", jsPath, "--output", output}, Config{Version: "test", Stdout: &bytes.Buffer{}, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(output, "original.js")); err != nil {
		t.Fatal(err)
	}
}

func TestSourceMapModeSupportsRemoteJSAndDirectMap(t *testing.T) {
	mapData := `{"version":3,"sources":["src/remote.ts"],"sourcesContent":["export const remote=1"],"mappings":""}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/app.js":
			w.Header().Set("SourceMap", "/app.js.map")
			_, _ = w.Write([]byte("compiled"))
		case "/app.js.map":
			_, _ = w.Write([]byte(mapData))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	for name, target := range map[string]string{"js": server.URL + "/app.js", "map": server.URL + "/app.js.map"} {
		t.Run(name, func(t *testing.T) {
			output := filepath.Join(t.TempDir(), "src")
			var stderr bytes.Buffer
			code := Run(context.Background(), []string{"-s", target, "--output", output}, Config{Version: "test", Stdout: &bytes.Buffer{}, Stderr: &stderr})
			if code != 0 {
				t.Fatalf("exit=%d stderr=%s", code, stderr.String())
			}
			if _, err := os.Stat(filepath.Join(output, "src", "remote.ts")); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestSourceMapModeRejectsRedundantU(t *testing.T) {
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"-s", "https://example.test/app.js", "--output", filepath.Join(t.TempDir(), "out"), "-u", "https://example.test/other.js"}, Config{Version: "test", Stdout: &bytes.Buffer{}, Stderr: &stderr})
	if code != 2 || !strings.Contains(stderr.String(), "do not use -u") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestOverviewShowsStructureWithoutStartingKingfisher(t *testing.T) {
	input := `export function load() { return fetch('/api/login'); }
function start() { load(); }
document.addEventListener('DOMContentLoaded', start);`
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--overview", "--verbose"}, Config{Version: "test", Stdin: bytes.NewBufferString(input), Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	for _, expected := range []string{"# JavaScript overview", "### Exports", "load", "DOMContentLoaded event → start()", "function start", "start() → load()", "fetch() calls"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("missing %q:\n%s", expected, stdout.String())
		}
	}
}

func TestOverviewDefaultsToTypeFeaturesAndBehavior(t *testing.T) {
	input := `export function load() { return fetch('/api/login'); }
function start() { load(); }
document.addEventListener('DOMContentLoaded', start);`
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--overview"}, Config{Version: "test", Stdin: bytes.NewBufferString(input), Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	output := stdout.String()
	for _, expected := range []string{"### Type", "Browser ES module.", "### Features", "No feature areas were identified.", "### Behavior", "- Server calls: fetch", "- API: 1 call (/login 1)", "- Starts: handles DOMContentLoaded events", "--verbose"} {
		if !strings.Contains(output, expected) {
			t.Errorf("missing %q:\n%s", expected, output)
		}
	}
	for _, detail := range []string{"### Exports", "### Observed links", "start() → load()"} {
		if strings.Contains(output, detail) {
			t.Errorf("default overview includes verbose detail %q:\n%s", detail, output)
		}
	}
}

func TestOverviewIgnoresScanOnlyFlags(t *testing.T) {
	input := `export function load() { return fetch('/api/v1/users'); }`
	run := func(args []string) (int, string, string) {
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), args, Config{Version: "test", Stdin: bytes.NewBufferString(input), Stdout: &stdout, Stderr: &stderr})
		return code, stdout.String(), stderr.String()
	}
	code, expected, stderr := run([]string{"--overview", "--format", "json"})
	if code != 0 {
		t.Fatalf("baseline exit=%d stderr=%s", code, stderr)
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(expected), &result); err != nil {
		t.Fatal(err)
	}
	files, ok := result["files"].([]any)
	if result["sources"] != float64(1) || !ok || len(files) != 1 {
		t.Fatalf("baseline did not return overview data: %#v", result)
	}

	for _, tc := range []struct {
		name string
		args []string
	}{
		{"all scan flags", []string{"-p", "--wordlist", "--validate-secrets", "--kingfisher", filepath.Join(t.TempDir(), "missing-kingfisher"), "-overview", "--format", "json"}},
		{"validation without Kingfisher", []string{"--overview", "--validate-secrets", "--format", "json"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			code, output, stderr := run(tc.args)
			if code != 0 || stderr != "" {
				t.Fatalf("exit=%d stderr=%q", code, stderr)
			}
			if output != expected {
				t.Fatalf("scan flags changed overview output:\n%s\nwant:\n%s", output, expected)
			}
		})
	}
}

func TestOverviewJSONAndSourceMapConflict(t *testing.T) {
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--overview", "--format", "json"}, Config{Version: "test", Stdin: bytes.NewBufferString(`fetch('/api/v1/users')`), Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var result map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if result["sources"] != float64(1) {
		t.Fatalf("unexpected overview JSON: %#v", result)
	}
	if result["version"] != "test" {
		t.Fatalf("overview version missing: %#v", result)
	}

	stderr.Reset()
	code = Run(context.Background(), []string{"--overview", "-s", "missing.js", "--output", filepath.Join(t.TempDir(), "out")}, Config{Version: "test", Stdout: &bytes.Buffer{}, Stderr: &stderr})
	if code != 2 || !strings.Contains(stderr.String(), "scan-only flags") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestOverviewAppliesLocalSourceMapButNeverFetchesOne(t *testing.T) {
	// "a" sits at column 9 and maps to the original name "startApp".
	sourceMap := `{"version":3,"sources":["src/main.ts","src/load.ts"],"names":["startApp"],"mappings":"SAAAA"}`
	code := "function a(){b()}function b(){}a();\n//# sourceMappingURL=app.min.js.map\n"
	dir := t.TempDir()
	path := filepath.Join(dir, "app.min.js")
	if err := os.WriteFile(path, []byte(code), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".map", []byte(sourceMap), 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if exit := Run(context.Background(), []string{"--overview", "--format", "json", path}, Config{Version: "test", Stdout: &stdout, Stderr: &stderr}); exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	if !strings.Contains(stdout.String(), `"source_map_applied"`) || !strings.Contains(stdout.String(), `"original_name": "startApp"`) {
		t.Fatalf("local map not applied:\n%s", stdout.String())
	}

	mapRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".map") {
			mapRequests++
			_, _ = w.Write([]byte(sourceMap))
			return
		}
		_, _ = w.Write([]byte(code))
	}))
	defer server.Close()
	stdout.Reset()
	if exit := Run(context.Background(), []string{"--overview", "--format", "json", "-u", server.URL + "/app.min.js"}, Config{Version: "test", Stdout: &stdout, Stderr: &stderr}); exit != 0 {
		t.Fatalf("exit=%d stderr=%s", exit, stderr.String())
	}
	if mapRequests != 0 || strings.Contains(stdout.String(), `"source_map_applied"`) {
		t.Fatalf("overview fetched a remote source map (%d requests)", mapRequests)
	}
}

func TestOverviewSourceMapStaysInInputDirectory(t *testing.T) {
	sourceMap := `{"version":3,"sources":["src/main.ts"],"names":["startApp"],"mappings":"SAAAA"}`
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "outside.map"), []byte(sourceMap), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "app")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, directive := range map[string]string{
		"parent.js":   "//# sourceMappingURL=../outside.map",
		"absolute.js": "//# sourceMappingURL=" + filepath.Join(root, "outside.map"),
		"device.js":   "//# sourceMappingURL=/dev/zero",
		"string.js":   "//# sourceMappingURL=../outside.map\nconst note = 'sourceMappingURL';",
	} {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte("function a(){}a();\n"+directive+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		var stdout, stderr bytes.Buffer
		if exit := Run(context.Background(), []string{"--overview", "--format", "json", path}, Config{Version: "test", Stdout: &stdout, Stderr: &stderr}); exit != 0 {
			t.Fatalf("%s: exit=%d stderr=%s", name, exit, stderr.String())
		}
		if strings.Contains(stdout.String(), `"source_map_applied"`) {
			t.Errorf("%s: map outside the input directory was applied", name)
		}
	}
}

func TestOverviewSourceMapHintCommandsWork(t *testing.T) {
	sourceMap := `{"version":3,"sources":["src/main.ts"],"sourcesContent":["export function startApp(){ loadUsers() }\nexport function loadUsers(){}\n"],"names":["startApp"],"mappings":"SAAAA"}`
	mapRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".map") {
			mapRequests++
			_, _ = w.Write([]byte(sourceMap))
			return
		}
		_, _ = w.Write([]byte("function a(){b()}function b(){}a();\n//# sourceMappingURL=app.min.js.map\n"))
	}))
	defer server.Close()
	run := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if exit := Run(context.Background(), args, Config{Version: "test", Stdout: &stdout, Stderr: &stderr}); exit != 0 {
			t.Fatalf("%v: exit=%d stderr=%s", args, exit, stderr.String())
		}
		return stdout.String()
	}

	// URL input: the hint names the extract command, and it works.
	url := server.URL + "/app.min.js"
	out := run("--overview", "-u", url)
	if want := "Source map not applied. Overview does not fetch maps; run jshound -s " + url + " --output ./source, then jshound --overview ./source"; !strings.Contains(out, want) || mapRequests != 0 {
		t.Fatalf("URL input hint missing (%d map requests):\n%s", mapRequests, out)
	}
	recovered := filepath.Join(t.TempDir(), "source")
	run("-s", url, "--output", recovered)
	if out := run("--overview", "--verbose", recovered); !strings.Contains(out, "function startApp") {
		t.Fatalf("recovered sources not read:\n%s", out)
	}

	// Local file whose map is missing: put the map next to it and the hint goes.
	dir := t.TempDir()
	path := filepath.Join(dir, "app.min.js")
	if err := os.WriteFile(path, []byte("function a(){b()}function b(){}a();\n//# sourceMappingURL=app.min.js.map\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := run("--overview", path); !strings.Contains(out, "Source map not applied. app.min.js.map was not found in this file's folder") {
		t.Fatalf("missing-map hint absent:\n%s", out)
	}
	if err := os.WriteFile(path+".map", []byte(sourceMap), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := run("--overview", path); strings.Contains(out, "Source map not applied") || !strings.Contains(out, "source map applied") {
		t.Fatalf("map next to the file was not applied:\n%s", out)
	}

	// Local file whose map is a URL: the hint names jshound -s FILE, and it works.
	remote := filepath.Join(dir, "remote.js")
	if err := os.WriteFile(remote, []byte("function a(){}a();\n//# sourceMappingURL="+server.URL+"/remote.js.map\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if out := run("--overview", remote); !strings.Contains(out, "jshound -s "+remote+" --output ./source") {
		t.Fatalf("URL-map hint absent:\n%s", out)
	}
	extracted := filepath.Join(t.TempDir(), "source")
	run("-s", remote, "--output", extracted)
	if _, err := os.Stat(filepath.Join(extracted, "src", "main.ts")); err != nil {
		t.Fatalf("suggested command did not recover the sources: %v", err)
	}
}

// writeApp writes a small built application: an entry file that mounts a UI
// and loads its chunks, and chunks that name nothing back.
func writeApp(t *testing.T, chunks int) string {
	t.Helper()
	dir := t.TempDir()
	entry := "import {useState,useEffect} from \"react\";useState(1);useEffect(f);ReactDOM.createRoot(document.getElementById(\"root\")).render(App);var u=[\"/api/v1/invoices\"];\n"
	for i := 0; i < chunks; i++ {
		name := "chunk" + string(rune('a'+i)) + "-Ab12Cd34.js"
		entry += "import(\"./" + name + "\");\n"
		code := "export const v" + string(rune('a'+i)) + "=1;\n"
		if i == 0 {
			code = "var n=[\"invoiceTable\",\"invoiceList\",\"sendInvoice\",\"payInvoice\",\"invoiceReport\",\"invoiceDetails\"];fetch(\"/api/v1/invoices\");\n"
		}
		if err := os.WriteFile(filepath.Join(dir, name), []byte(code), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "entry.js"), []byte(entry), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestOverviewOfADirectoryRollsUpTheApplication(t *testing.T) {
	run := func(args ...string) string {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := Run(context.Background(), args, Config{Version: "test", Stdout: &stdout, Stderr: &stderr}); code != 0 {
			t.Fatalf("exit %d: %s", code, stderr.String())
		}
		return stdout.String()
	}

	// Seven files: the roll-up, then a compact index instead of seven sections.
	big := writeApp(t, 6)
	text := run("--overview", big)
	for _, want := range []string{"7 sources", "## All 7 files", "Browser app using React", "Entry file: entry.js (", "- invoice:", "## Files", "Run --overview on a single file, or add --verbose"} {
		if !strings.Contains(text, want) {
			t.Errorf("default output lacks %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "\n## "+filepath.Join(big, "entry.js")) {
		t.Errorf("default output printed a full section per file:\n%s", text)
	}
	if verbose := run("--overview", "--verbose", big); !strings.Contains(verbose, "\n## "+filepath.Join(big, "entry.js")+"\n") || !strings.Contains(verbose, "\n## All 7 files\n") {
		t.Errorf("verbose output lacks the roll-up or the file sections:\n%s", verbose)
	}

	// JSON: the roll-up is added; the files stay complete.
	var result struct {
		Sources int `json:"sources"`
		Overall *struct {
			Is      string `json:"is"`
			Related bool   `json:"related"`
			Entries []struct {
				File string `json:"file"`
			} `json:"entry_files"`
		} `json:"overall"`
		Files []struct {
			Name  string   `json:"name"`
			Loads []string `json:"loads"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(run("--overview", "--format", "json", big)), &result); err != nil {
		t.Fatal(err)
	}
	if result.Overall == nil || !result.Overall.Related || len(result.Files) != 7 || len(result.Overall.Entries) != 1 || filepath.Base(result.Overall.Entries[0].File) != "entry.js" {
		t.Fatalf("overall = %+v files=%d", result.Overall, len(result.Files))
	}
	for _, file := range result.Files {
		if filepath.Base(file.Name) == "entry.js" && len(file.Loads) != 6 {
			t.Errorf("entry loads %v", file.Loads)
		}
	}

	// JSONL: one record per file as before, then a record of its own kind.
	lines := strings.Split(strings.TrimSpace(run("--overview", "--format", "jsonl", big)), "\n")
	if len(lines) != 8 || !strings.HasPrefix(lines[0], `{"kind":"overview",`) || !strings.HasPrefix(lines[7], `{"kind":"overall",`) {
		t.Errorf("jsonl has %d lines, first %.40s, last %.40s", len(lines), lines[0], lines[len(lines)-1])
	}

	// Three files stay a small set: the roll-up and every file in full.
	small := run("--overview", writeApp(t, 2))
	if !strings.Contains(small, "## All 3 files") || strings.Contains(small, "\n## Files\n") || strings.Count(small, "### Type") != 4 {
		t.Errorf("small set layout:\n%s", small)
	}

	// One file: exactly the single-file output, with no roll-up.
	entry := filepath.Join(big, "entry.js")
	if one := run("--overview", "--format", "json", entry); strings.Contains(one, `"overall"`) || !strings.Contains(run("--overview", entry), "\n## "+entry+"\n") {
		t.Errorf("single-file output changed:\n%s", one)
	}
}
