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

	"github.com/jshound/jshound/internal/report"
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

func TestOverviewDefaultsToWhatItIsAndDoes(t *testing.T) {
	input := `export function load() { return fetch('/api/login'); }
function start() { load(); }
document.addEventListener('DOMContentLoaded', start);`
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--overview"}, Config{Version: "test", Stdin: bytes.NewBufferString(input), Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	output := stdout.String()
	for _, expected := range []string{"### What it is", "Browser ES module.", "### How it runs", "- Server calls: fetch", "--verbose"} {
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
