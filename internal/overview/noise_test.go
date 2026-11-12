package overview

import (
	"strings"
	"testing"
)

func TestBuiltinAndDataCallsAreNotEntryPaths(t *testing.T) {
	code := "const cache = new Map();\nconst seen = new Set();\nif (process.argv.includes('--debug')) debug();\nObject.freeze(cache);\nstart();\nfunction start() {}\nfunction debug() {}\n"
	file := Analyze([]Source{{Name: "main.js", Data: []byte(code)}}).Files[0]
	var actions []string
	for _, entry := range file.EntryPoints {
		actions = append(actions, entry.Action)
	}
	if strings.Join(actions, ",") != "debug(),start()" {
		t.Fatalf("entry paths: %v", actions)
	}
}

func TestKeywordsBeforeParenthesesAreNotCalls(t *testing.T) {
	code := "const load = async (kind) => fetchKind(kind);\nconst save = async function (kind) { return (await (fetchKind(kind))); };\n" +
		"function* gen(kind) { yield (fetchKind(kind)); delete (cache.entry); throw (new Error(kind)); }\n" +
		"function fetchKind(kind) {}\n"
	var names []string
	for _, c := range callsIn(lex(code), 0, len(lex(code))) {
		names = append(names, c.name)
	}
	if got := strings.Join(names, ","); got != "fetchKind,fetchKind,fetchKind,Error" {
		t.Fatalf("calls: %s", got)
	}
	for _, kind := range []string{"async", "await", "yield", "delete", "throw"} {
		for _, c := range callsIn(lex(code), 0, len(lex(code))) {
			if c.name == kind {
				t.Fatalf("%s read as a call", kind)
			}
		}
	}
	file := Analyze([]Source{{Name: "main.js", Data: []byte(code + "load('a');\n")}}).Files[0]
	for _, entry := range file.EntryPoints {
		if strings.Contains(entry.Action, "async") {
			t.Fatalf("async arrow function reported as a call: %#v", file.EntryPoints)
		}
	}
}

func TestIdentifierNamedAsyncStaysACall(t *testing.T) {
	code := "function async(task) { task(); }\nasync(() => {});\nasync(run);\nfunction run() {}\n"
	file := Analyze([]Source{{Name: "main.js", Data: []byte(code)}}).Files[0]
	var actions []string
	for _, entry := range file.EntryPoints {
		actions = append(actions, entry.Action)
	}
	if got := strings.Join(actions, ","); got != "async()" {
		t.Fatalf("entry paths: %s", got)
	}
}

func TestEsbuildHelpersAreNotUnits(t *testing.T) {
	code := "var __defProp = Object.defineProperty;\nvar __commonJS = (cb, mod) => function __require() { return mod; };\nvar __export = (target, all) => { for (var name in all) __defProp(target, name, {}); };\n" +
		"var require_a = __commonJS({ \"node_modules/a/index.js\"(exports, module) { module.exports = 1; } });\n" +
		"function main() { run(); }\nfunction run() {}\n__export({}, {});\nmain();\n"
	file := Analyze([]Source{{Name: "out.js", Data: []byte(code)}}).Files[0]
	if file.Build != "esbuild bundle" {
		t.Fatalf("build: %q", file.Build)
	}
	for _, unit := range file.Units {
		if strings.HasPrefix(unit.Name, "__") {
			t.Fatalf("helper unit: %#v", file.Units)
		}
	}
	for _, entry := range file.EntryPoints {
		if strings.HasPrefix(entry.Action, "__") {
			t.Fatalf("helper entry: %#v", file.EntryPoints)
		}
	}
	if !hasLink(file.Links, Link{From: "main()", To: "run()", Line: 5}) {
		t.Fatalf("links: %#v", file.Links)
	}
}

func TestStorageRequiresGlobalUse(t *testing.T) {
	code := "function restore(sessionStorage = {}) { return { session_storage: sessionStorage }; }\nfunction save(v) { localStorage.setItem('k', v); }\nsave(restore());\n"
	file := Analyze([]Source{{Name: "a.js", Data: []byte(code)}}).Files[0]
	if !has(file.Interactions, "localStorage") || has(file.Interactions, "sessionStorage") {
		t.Fatalf("interactions: %v", file.Interactions)
	}
}

func TestPageEvaluatedCodeIsSeparated(t *testing.T) {
	code := "function observe(key) { window.addEventListener('message', () => {}); localStorage.clear(); }\n" +
		"async function run(page) { await page.evaluate(observe, 'k'); await page.evaluate(() => { document.body.innerHTML = ''; sessionStorage.clear(); }); }\nrun();\n"
	file := Analyze([]Source{{Name: "worker.js", Data: []byte(code)}}).Files[0]
	for _, unwanted := range []string{"localStorage", "sessionStorage", "DOM updates", "event listeners"} {
		if has(file.Interactions, unwanted) {
			t.Fatalf("page code leaked %q: %v", unwanted, file.Interactions)
		}
	}
	if !has(file.Interactions, "scripts evaluated in browser pages") || len(file.Registrations) != 0 {
		t.Fatalf("page scripts: %#v", file)
	}
}

func TestEventsWithSameNameStayDistinctByReceiver(t *testing.T) {
	code := "function a(){}\nfunction b(){}\npage.on('close', a);\nreadline.on('close', b);\n"
	file := Analyze([]Source{{Name: "a.js", Data: []byte(code)}}).Files[0]
	if !hasLink(file.Links, Link{From: "page close event", To: "a()", Line: 3}) || !hasLink(file.Links, Link{From: "readline close event", To: "b()", Line: 4}) {
		t.Fatalf("links: %#v", file.Links)
	}
}

func TestLeadingPolyfillIsNotAnEnclosingIIFE(t *testing.T) {
	code := strings.Repeat(" ", 0) + "(function(){const r=document.createElement('link').relList;if(r&&r.supports)return})();import{a as b}from'./x.js';function c(){b()}c();export{c as g};" + strings.Repeat("/*pad*/", 700)
	file := Analyze([]Source{{Name: "index.js", Data: []byte(code)}}).Files[0]
	if file.Format != "ES module" {
		t.Fatalf("format: %q", file.Format)
	}
	for _, entry := range file.EntryPoints {
		if strings.Contains(entry.Action, "enclosing IIFE") {
			t.Fatalf("leading polyfill reported as enclosing IIFE: %#v", file.EntryPoints)
		}
	}
	wrapped := Analyze([]Source{{Name: "lib.js", Data: []byte("(function(){var a=1;function b(){}b()})();" + strings.Repeat("/*pad*/", 700))}}).Files[0]
	if !enclosingIIFE(lex(string("(function(){var a=1;function b(){}b()})();"))) {
		t.Fatalf("enclosing IIFE not detected: %#v", wrapped)
	}
}
