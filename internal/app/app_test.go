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

func TestOverviewWritesReconBriefWithoutStartingKingfisher(t *testing.T) {
	input := `window.__NEXT_DATA__={}; fetch('/api/login',{method:'POST',headers:{Authorization:'Bearer token'}}); localStorage.setItem('access_token',token);`
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--overview"}, Config{Version: "test", Stdin: bytes.NewBufferString(input), Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	for _, expected := range []string{"APPLICATION OVERVIEW", "Framework: Next.js", "POST /api/login", "Authorization header handling", "localStorage[access_token] (write)"} {
		if !strings.Contains(stdout.String(), expected) {
			t.Errorf("missing %q:\n%s", expected, stdout.String())
		}
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
