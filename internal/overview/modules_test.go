package overview

import (
	"bytes"
	"strings"
	"testing"
)

func TestEsbuildModuleMarkersBecomeAreasAndPackages(t *testing.T) {
	code := "\"use strict\";\nvar __commonJS = (cb, mod) => function __require() { return mod; };\n" +
		"// node_modules/@scope/lib/index.js\nvar require_lib = __commonJS({\n  \"node_modules/@scope/lib/index.js\"(exports, module) { module.exports = 1; }\n});\n" +
		"// node_modules/.pnpm/chalk@5/node_modules/chalk/index.js\nvar chalk = 1;\n" +
		"// src/server/api.ts\nfunction serve() { route(); }\n" +
		"// src/server/router.ts\nfunction route() {}\n" +
		"// src/client/main.ts\nfunction main() { serve(); }\nmain();\n"
	file := Analyze([]Source{{Name: "out.js", Data: []byte(code)}}).Files[0]
	modules := file.Modules
	if modules == nil || modules.Total != 5 || modules.Own != 3 || len(modules.Packages) != 2 {
		t.Fatalf("modules: %#v", modules)
	}
	if modules.Packages[0].Name != "@scope/lib" && modules.Packages[1].Name != "@scope/lib" || !strings.Contains(modules.Packages[0].Name+modules.Packages[1].Name, "chalk") {
		t.Fatalf("packages: %#v", modules.Packages)
	}
	if len(modules.Areas) != 2 || modules.Areas[0].Name != "src/server" || modules.Areas[0].Count != 2 || modules.Areas[1].Name != "src/client" {
		t.Fatalf("areas: %#v", modules.Areas)
	}
	var origin string
	for _, unit := range file.Units {
		if unit.Name == "route" {
			origin = unit.Origin
		}
	}
	if origin != "src/server/router.ts" {
		t.Fatalf("unit origin: %#v", file.Units)
	}
	var output bytes.Buffer
	if err := WriteText(&output, Result{Sources: 1, Files: []File{file}}, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"### Bundled modules", "5 modules — 3 from project sources, 2 from 2 packages", "| src/server | 2 |", "src/server/router.ts)"} {
		if !strings.Contains(output.String(), expected) {
			t.Errorf("missing %q:\n%s", expected, output.String())
		}
	}
}

func TestNormalizeModulePath(t *testing.T) {
	for input, expected := range map[string]string{
		"webpack://app/./src/index.tsx":           "src/index.tsx",
		"../../src/App.vue?vue&type=script":       "src/App.vue",
		"webpack/bootstrap":                       "",
		"\x00commonjsHelpers.js":                  "",
		"/home/u/app/node_modules/react/index.js": "home/u/app/node_modules/react/index.js",
	} {
		if got := normalizeModulePath(input); got != expected {
			t.Errorf("normalize %q = %q, want %q", input, got, expected)
		}
	}
}
