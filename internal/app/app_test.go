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
	code := Run(context.Background(), []string{"--no-secrets", "-p", "--wordlist", "--format", "json"}, Config{Version: "test", Stdin: bytes.NewBufferString(input), Stdout: &stdout, Stderr: &stderr})
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
	code := Run(context.Background(), []string{"--no-secrets", "https://example.test/app.js"}, Config{Version: "test", Stderr: &stderr})
	if code != 2 || !strings.Contains(stderr.String(), "require -u") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
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
	code := Run(context.Background(), []string{"-s", "--output", output, mapPath}, Config{Version: "test", Stdout: &stdout, Stderr: &stderr})
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
	code := Run(context.Background(), []string{"--sourcemap", "--output", output, jsPath}, Config{Version: "test", Stdout: &bytes.Buffer{}, Stderr: &stderr})
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
			code := Run(context.Background(), []string{"-s", "--output", output, "-u", target}, Config{Version: "test", Stdout: &bytes.Buffer{}, Stderr: &stderr})
			if code != 0 {
				t.Fatalf("exit=%d stderr=%s", code, stderr.String())
			}
			if _, err := os.Stat(filepath.Join(output, "src", "remote.ts")); err != nil {
				t.Fatal(err)
			}
		})
	}
}
