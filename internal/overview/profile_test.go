package overview

import (
	"strings"
	"testing"
)

func summaryOf(t *testing.T, name, code string) string {
	t.Helper()
	return Analyze([]Source{{Name: name, Data: []byte(code)}}).Files[0].Summary
}

func TestSummaryDescribesRuntimeKindAndFlow(t *testing.T) {
	for _, tc := range []struct {
		name, code, want string
	}{
		{"worker.js", `import { createInterface } from "node:readline";
const rl = createInterface({ input: process.stdin });
function handle(req) { switch (req.method) { case "a": return one(); case "b": return two(); case "c": return three(); } }
rl.on("line", handle);`, "Node.js process that reads line-delimited input from stdin. Starts with createInterface(), handles line event, and dispatches 3 cases on req.method in handle()."},
		{"main.jsx", `import { createRoot } from "react-dom/client";
function App() { const [v] = useState(0); useEffect(() => {}); return v; }
createRoot(document.getElementById("root")).render(App());`, "Browser app using React."},
		{"lib.js", `import fs from "node:fs"; export function read(p) { return fs.readFileSync(p); }`, "Node.js library."},
		{"sw.js", `self.addEventListener("fetch", (e) => e.respondWith(fetch(e.request)));`, "Service worker."},
		{"cli.js", "#!/usr/bin/env node\nconst args = process.argv.slice(2);\nmain(args);\nfunction main() {}", "Node.js command-line program. Starts with main()."},
	} {
		if got := summaryOf(t, tc.name, tc.code); !strings.HasPrefix(got, tc.want) {
			t.Errorf("%s: got %q, want prefix %q", tc.name, got, tc.want)
		}
	}
}

func TestKeywordRolesNeedWholeWordsInAFocusedScope(t *testing.T) {
	// Substrings such as decode, keyCode, isMobile, and verifyHost used to
	// label large non-UI scripts as account verification UI.
	var code strings.Builder
	code.WriteString("document.createElement('div');\n")
	for i := 0; i < 3000; i++ {
		code.WriteString("function decode(keyCode){ return verifyHostname(keyCode) || isMobileDevice || emailRegex; }\n")
	}
	file := Analyze([]Source{{Name: "big.js", Data: []byte(code.String())}}).Files[0]
	if file.Insights != nil {
		for _, component := range file.Insights.Components {
			if component.Role == "account verification UI" {
				t.Fatalf("false verification role: %#v", component)
			}
		}
	}
	if got := identifierWords("sendSMSCode_retry-count.v2"); strings.Join(got, ",") != "send,sms,code,retry,count,v2" {
		t.Fatalf("words: %v", got)
	}
}

func TestBundledPackageCodeDoesNotShapeTheSummary(t *testing.T) {
	code := "var __commonJS = (cb, mod) => function __require() { return mod; };\n" +
		"// node_modules/yargs/index.js\nvar argv = process.argv.slice(2); var el = \"react.element\";\n" +
		"// src/lib.ts\nexport function api() { return 1; }\n"
	got := summaryOf(t, "out.js", code)
	if strings.Contains(got, "command-line") || strings.Contains(got, "React") {
		t.Fatalf("package signals leaked into summary: %q", got)
	}
}
