package sourcemap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractEmbeddedSourcesSafely(t *testing.T) {
	output := filepath.Join(t.TempDir(), "out")
	data := []byte(`{"version":3,"sourceRoot":"webpack:///./","sources":["src/app.ts","../../escape.js"],"sourcesContent":["export const app=1","no escape"],"mappings":""}`)
	result, err := Extract(context.Background(), "app.js.map", data, ExtractOptions{Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Written) != 2 {
		t.Fatalf("result=%+v", result)
	}
	for _, want := range []string{"src/app.ts", "escape.js"} {
		if _, err := os.Stat(filepath.Join(output, filepath.FromSlash(want))); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(output), "escape.js")); !os.IsNotExist(err) {
		t.Fatalf("source escaped output")
	}
}

func TestExtractIndexedMapAndSkipNullContent(t *testing.T) {
	output := filepath.Join(t.TempDir(), "out")
	data := []byte(`{"version":3,"sections":[{"map":{"version":3,"sources":["a.ts","b.ts"],"sourcesContent":["a",null],"mappings":""}}]}`)
	result, err := Extract(context.Background(), "bundle.map", data, ExtractOptions{Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Written) != 1 || len(result.Skipped) != 1 {
		t.Fatalf("result=%+v", result)
	}
}

func TestExtractRequiresEmptyOutput(t *testing.T) {
	output := t.TempDir()
	if err := os.WriteFile(filepath.Join(output, "existing"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Extract(context.Background(), "a.map", []byte(`{"version":3,"sources":["a.js"],"sourcesContent":["a"],"mappings":""}`), ExtractOptions{Output: output})
	if err == nil || !strings.Contains(err.Error(), "must be empty") {
		t.Fatalf("err=%v", err)
	}
}
