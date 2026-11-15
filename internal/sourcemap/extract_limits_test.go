package sourcemap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractSkipsSourcesTheFileSystemRejects(t *testing.T) {
	output := filepath.Join(t.TempDir(), "out")
	long := strings.Repeat("n", 300) + ".js"
	data := []byte(`{"version":3,"sources":["a","a/b","bad\u0000name.js","` + long + `","ok.js"],"sourcesContent":["1","2","3","4","5"],"mappings":""}`)
	result, err := Extract(context.Background(), "app.js.map", data, ExtractOptions{Output: output})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Written) != 2 || result.Failed != 3 || result.FirstError == nil || len(result.Skipped) != 3 {
		t.Fatalf("result=%+v", result)
	}
	if _, err := os.Stat(filepath.Join(output, "ok.js")); err != nil {
		t.Fatalf("a later valid source was not written: %v", err)
	}
}

func TestExtractStopsAtTheFileLimit(t *testing.T) {
	output := filepath.Join(t.TempDir(), "out")
	data := []byte(`{"version":3,"sources":["a.js","b.js","c.js","d.js"],"sourcesContent":["1","2","3","4"],"mappings":""}`)
	result, err := Extract(context.Background(), "app.js.map", data, ExtractOptions{Output: output, MaxFiles: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Written) != 2 || len(result.Skipped) != 2 || !result.Truncated {
		t.Fatalf("result=%+v", result)
	}
	entries, _ := os.ReadDir(output)
	if len(entries) != 2 {
		t.Fatalf("wrote %d files past the limit", len(entries))
	}
}

func TestExtractKeepsWindowsSourceNames(t *testing.T) {
	for name, want := range map[string]string{
		`C:\\work\\src\\app.ts`:          "work/src/app.ts",
		`webpack:///.\\src\\util.ts`:     "src/util.ts",
		`webpack://shop/./src/index.tsx`: "shop/src/index.tsx",
	} {
		if got := filepath.ToSlash(safeSourcePath("", strings.ReplaceAll(name, `\\`, `\`), 0)); got != want {
			t.Errorf("safeSourcePath(%s) = %s, want %s", name, got, want)
		}
	}
}
