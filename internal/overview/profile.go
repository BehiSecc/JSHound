package overview

import (
	"fmt"
	"strings"
)

var nodeBuiltins = map[string]struct{}{
	"fs": {}, "path": {}, "os": {}, "child_process": {}, "net": {}, "http": {}, "https": {}, "http2": {},
	"crypto": {}, "readline": {}, "worker_threads": {}, "url": {}, "events": {}, "stream": {}, "zlib": {},
	"util": {}, "tls": {}, "dns": {}, "cluster": {}, "module": {}, "assert": {}, "buffer": {}, "vm": {},
	"async_hooks": {}, "perf_hooks": {}, "process": {}, "fs/promises": {}, "timers/promises": {},
}

type fileSignals struct {
	node, browser                     int
	serviceWorker, extension, worker  bool
	cli, server, stdinLines, mounts   bool
	umd, closure                      bool
	frameworks                        []string
	hasEntryPoints, hasExports, isLib bool // isLib: exports without startup
	runs                              runSignals
	platform                          *Platform
}

var signalTokens = map[string]struct{}{
	"process": {}, "__dirname": {}, "__filename": {}, "document": {}, "window": {}, "navigator": {}, "location": {},
	"importScripts": {}, "skipWaiting": {}, "clients": {}, "chrome": {}, "browser": {}, "createServer": {},
	"createRoot": {}, "hydrateRoot": {}, "mount": {}, "render": {}, "useState": {}, "useEffect": {},
	"ɵɵdefineComponent": {}, "__NEXT_DATA__": {}, "jQuery": {}, "addEventListener": {},
}

var packageFrameworks = map[string]string{
	"react": "React", "react-dom": "React", "vue": "Vue", "@vue/runtime-core": "Vue", "@angular/core": "Angular",
	"svelte": "Svelte", "preact": "Preact", "next": "Next.js", "jquery": "jQuery", "express": "Express", "electron": "Electron",
}

// collectSignals reads runtime and framework evidence. fromPackage reports
// whether a token belongs to bundled third-party code, whose incidental use
// of process.argv or React internals says nothing about this file.
func collectSignals(file *File, tokens []token, raw string, fromPackage func(int) bool) fileSignals {
	var s fileSignals
	pageMask, _, _ := pageScriptMask(tokens)
	framework := func(name string) { s.frameworks = appendUnique(s.frameworks, name) }
	var hooks [2]bool
	for _, value := range file.Imports {
		name := strings.TrimPrefix(value, "node:")
		if _, ok := nodeBuiltins[name]; ok || strings.HasPrefix(value, "node:") {
			s.node += 5
		}
		switch {
		case value == "react" || value == "react-dom" || strings.HasPrefix(value, "react/") || strings.HasPrefix(value, "react-dom/"):
			framework("React")
		case value == "vue" || strings.HasPrefix(value, "@vue/"):
			framework("Vue")
		case strings.HasPrefix(value, "@angular/"):
			framework("Angular")
		case value == "svelte" || strings.HasPrefix(value, "svelte/"):
			framework("Svelte")
		case value == "preact" || strings.HasPrefix(value, "preact/"):
			framework("Preact")
		case value == "express":
			framework("Express")
			s.server = true
		case value == "electron":
			framework("Electron")
		case value == "commander" || value == "yargs" || value == "minimist":
			s.cli = true
		}
	}
	for i, t := range tokens {
		if isMasked(pageMask, i) {
			continue
		}
		next := at(tokens, i+1)
		switch t.kind {
		case 's':
			if file.Modules != nil {
				continue
			}
			value := stringValue(t.text)
			switch value {
			case "react.element", "react.transitional.element", "react.fragment":
				framework("React")
			case "__v_isRef", "__vccOpts":
				framework("Vue")
			case "ng-version":
				// Angular sets the attribute on its root; a detector reads it.
				if name := at(tokens, i-2); at(tokens, i-1) != "(" || name != "getAttribute" && name != "hasAttribute" {
					framework("Angular")
				}
			case "Cannot find global object":
				// Closure Compiler's $jscomp.getGlobal polyfill.
				s.closure = true
			}
			// Compiled Angular components keep emulated style encapsulation
			// markers even when every identifier is renamed.
			if len(value) > 32 && strings.Contains(value, "[_ngcontent-%COMP%]") {
				framework("Angular")
			}
			continue
		case 'i':
			if _, ok := signalTokens[t.text]; !ok || fromPackage != nil && fromPackage(i) {
				continue
			}
		default:
			continue
		}
		prevDot := at(tokens, i-1) == "."
		switch t.text {
		case "process":
			if next == "." && !prevDot {
				switch at(tokens, i+2) {
				case "argv":
					s.node++
					s.cli = true
				case "env", "exit", "cwd", "stdout", "stderr", "platform", "nextTick", "pid", "on":
					s.node++
				case "stdin":
					s.node++
					s.stdinLines = s.stdinLines || nearToken(tokens, i, "createInterface", 12)
				}
			}
		case "__dirname", "__filename":
			s.node++
		case "document", "window", "navigator", "location":
			if !prevDot && (next == "." || next == "[") {
				s.browser++
			}
		case "importScripts":
			s.worker = true
		case "skipWaiting", "clients":
			if prevDot && at(tokens, i-2) == "self" {
				s.serviceWorker = true
			}
		case "chrome", "browser":
			if !prevDot && next == "." && at(tokens, i+2) == "runtime" {
				s.extension = true
			}
		case "createServer":
			s.server = s.server || next == "("
		case "createRoot", "hydrateRoot":
			if next == "(" {
				s.mounts = true
			}
		case "mount", "render":
			if next == "(" && prevDot {
				s.mounts = true
			}
		case "useState":
			hooks[0] = hooks[0] || next == "("
		case "useEffect":
			hooks[1] = hooks[1] || next == "("
		case "ɵɵdefineComponent":
			if next == "(" || next == ":" || at(tokens, i-1) == "function" || assigned(tokens, i) {
				framework("Angular")
			}
		case "__NEXT_DATA__":
			// Next.js stores the page data in it; analytics and detection
			// scripts only read it.
			if assigned(tokens, i) {
				framework("Next.js")
			}
		case "jQuery":
			// A read such as "o.jQuery || o.Zepto" or "typeof jQuery" only
			// checks whether the page has jQuery.
			if at(tokens, i-1) != "typeof" && (next == "(" || assigned(tokens, i) || next == "." && at(tokens, i+2) != "" && tokens[i+2].kind == 'i') {
				framework("jQuery")
			}
		}
		if t.text == "addEventListener" && at(tokens, i-2) == "self" && next == "(" {
			switch stringValue(at(tokens, i+2)) {
			case "fetch", "install", "activate":
				s.serviceWorker = true
			case "message":
				s.worker = true
			}
		}
	}
	if hooks[0] && hooks[1] {
		framework("React")
	}
	if file.Modules != nil {
		for _, pkg := range file.Modules.Packages {
			if name, ok := packageFrameworks[pkg.Name]; ok {
				framework(name)
			}
			switch pkg.Name {
			case "express", "fastify", "koa":
				s.server = true
			}
		}
	}
	if strings.HasPrefix(raw, "#!") {
		s.cli = true
		s.node += 5
	}
	for _, entry := range file.EntryPoints {
		if entry.Trigger != "module load" || !strings.Contains(entry.Action, "IIFE") {
			s.hasEntryPoints = true
		}
	}
	s.hasExports = len(file.Exports) > 0
	s.umd = isUMD(tokens)
	// A UMD wrapper marks a distributed library only when it is the whole
	// file; app bundles often begin with a UMD helper.
	s.isLib = s.hasExports && !s.hasEntryPoints || s.umd && !s.mounts && file.Bundle == nil
	return s
}

// assigned reports whether the token at index is the target of "=", not of
// "==" or "===".
func assigned(tokens []token, index int) bool {
	return at(tokens, index+1) == "=" && at(tokens, index+2) != "="
}

func uiFramework(frameworks []string) bool {
	for _, name := range frameworks {
		switch name {
		case "Angular", "React", "Vue", "Svelte", "Preact", "Next.js":
			return true
		}
	}
	return false
}

func nearToken(tokens []token, index int, value string, distance int) bool {
	for i := max(0, index-distance); i < min(len(tokens), index+distance); i++ {
		if tokens[i].text == value {
			return true
		}
	}
	return false
}

// describeFile writes a short, evidence-based description: what kind of file
// this is, where it runs, and how control enters it. It also sets the plain
// purpose summary, which reuses the same signals.
func describeFile(file *File, tokens []token, raw string, fromPackage func(int) bool, sink *rollupSink) string {
	if len(tokens) == 0 {
		return ""
	}
	s := collectSignals(file, tokens, raw, fromPackage)
	pageMask, _, _ := pageScriptMask(tokens)
	s.runs = scanRunSignals(tokens, pageMask)
	s.platform = detectPlatform(s.runs, s)
	identity, runtime := identitySentence(file, s)
	file.Purpose = describePurpose(file, tokens, raw, fromPackage, s, identity, runtime, sink)
	file.kind = fileKind(file, s)
	if sink != nil {
		sink.record(file, s, runtime)
	}
	sentences := []string{identity}
	if flow := describeFlow(file); flow != "" {
		sentences = append(sentences, flow)
	}
	return strings.Join(sentences, " ")
}

// identitySentence names the runtime, kind, frameworks, and build tool, such
// as "Browser app using Angular, compiled by Closure Compiler."
func identitySentence(file *File, s fileSignals) (string, string) {
	runtime := ""
	switch {
	case s.serviceWorker:
		runtime = "service worker"
	case s.extension:
		runtime = "browser extension"
	case s.node > 0 && s.node >= s.browser:
		runtime = "Node.js"
	case s.worker && s.browser == 0:
		runtime = "web worker"
	case s.browser > 0:
		runtime = "browser"
	}
	kind := "script"
	switch {
	case runtime == "service worker" || runtime == "web worker":
		kind = ""
	case s.stdinLines:
		kind = "process that reads line-delimited input from stdin"
	case s.isLib:
		kind = "library"
	case s.server && runtime == "Node.js":
		kind = "server"
	case s.cli && runtime == "Node.js":
		kind = "command-line program"
	case s.mounts && runtime == "browser":
		kind = "app"
	case runtime == "browser" && file.Bundle == nil && uiFramework(s.frameworks):
		kind = "app"
	case file.Bundle != nil:
		kind = "bundle chunk"
	case strings.HasSuffix(file.Format, "module") || file.Format == "IIFE script":
		kind = file.Format
	}
	var first strings.Builder
	phrase, hosted := platformPhrase(s.platform)
	switch {
	case hosted:
		first.WriteString(phrase)
	case runtime == "":
		first.WriteString(capitalize(kind))
	case kind == "":
		first.WriteString(capitalize(runtime))
	default:
		first.WriteString(capitalize(runtime + " " + kind))
	}
	if frameworks := hostFrameworks(s); len(frameworks) > 0 {
		first.WriteString(" using " + strings.Join(frameworks, ", "))
	}
	if strings.Contains(file.Build, "bundle") {
		builder := strings.TrimSuffix(strings.TrimSuffix(file.Build, " bundle"), "-style")
		fmt.Fprintf(&first, ", bundled by %s", builder)
	} else if s.closure {
		first.WriteString(", compiled by Closure Compiler")
	}
	if file.Modules != nil {
		fmt.Fprintf(&first, " (%d %s", file.Modules.Total, plural(file.Modules.Total, "module"))
		if len(file.Modules.Packages) > 0 {
			fmt.Fprintf(&first, ", %d from packages", file.Modules.Total-file.Modules.Own)
		}
		first.WriteString(")")
	}
	first.WriteString(".")
	return first.String(), runtime
}

// hostFrameworks drops Electron from the framework list when the platform
// already says it: an Obsidian plugin only requires it as the host's shell.
func hostFrameworks(s fileSignals) []string {
	if s.platform == nil || s.platform.Name == "Browser extension" {
		return s.frameworks
	}
	var kept []string
	for _, name := range s.frameworks {
		if name != "Electron" {
			kept = append(kept, name)
		}
	}
	return kept
}

// startActions are the top-level calls describeFlow names as how the file
// starts: the first two, leaving out the wrappers that only enclose the code.
func startActions(file *File) []string {
	var starts []string
	for _, entry := range file.EntryPoints {
		if entry.Trigger == "module load" && !strings.Contains(entry.Action, "IIFE") && !strings.Contains(entry.Action, "registers chunk") && len(starts) < 2 {
			starts = appendUnique(starts, entry.Action)
		}
	}
	return starts
}

func describeFlow(file *File) string {
	starts := startActions(file)
	var events []string
	for _, entry := range file.EntryPoints {
		if entry.Trigger != "module load" && len(events) < 3 {
			events = appendUnique(events, strings.TrimSuffix(entry.Trigger, " event"))
		}
	}
	var parts []string
	if len(starts) > 0 {
		parts = append(parts, "starts with "+strings.Join(starts, " and "))
	}
	if len(events) > 0 {
		parts = append(parts, "handles "+strings.Join(events, ", ")+" "+plural(len(events), "event"))
	}
	if d, ok := leadingDispatch(file); ok {
		{
			where := ""
			if d.In != "" {
				where = " in " + d.In + "()"
			}
			switch {
			case d.Kind == "routes":
				parts = append(parts, fmt.Sprintf("registers %d routes on %s", d.Count, d.On))
			case strings.HasPrefix(d.Kind, "handler map "):
				parts = append(parts, fmt.Sprintf("dispatches %d handlers by %s%s", d.Count, d.On, where))
			default:
				parts = append(parts, fmt.Sprintf("dispatches %d cases on %s%s", d.Count, d.On, where))
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return capitalize(joinClauses(parts)) + "."
}

// leadingDispatch prefers the dispatch inside the most central unit, and only
// names one whose function or discriminator is readable.
func leadingDispatch(file *File) (Dispatch, bool) {
	rank := make(map[string]int, len(file.Units))
	for i, unit := range file.Units {
		rank[unit.Name] = i + 1
		if unit.OriginalName != "" {
			rank[unit.OriginalName] = i + 1
		}
	}
	best, bestRank := -1, 0
	for i, d := range file.Dispatches {
		if strings.Contains(d.Origin, "node_modules/") || !memberExpression(d.On) || len(d.In) < 3 && len(d.On) < 3 {
			continue
		}
		r := rank[d.In]
		if r == 0 {
			r = len(file.Units) + 1 + i
		}
		if best < 0 || r < bestRank {
			best, bestRank = i, r
		}
	}
	if best < 0 {
		return Dispatch{}, false
	}
	return file.Dispatches[best], true
}

func memberExpression(value string) bool {
	for _, part := range strings.Split(value, ".") {
		if !identifierLike(part) {
			return false
		}
	}
	return value != ""
}

func joinClauses(parts []string) string {
	if len(parts) <= 1 {
		return strings.Join(parts, "")
	}
	return strings.Join(parts[:len(parts)-1], ", ") + ", and " + parts[len(parts)-1]
}

func capitalize(value string) string {
	if value == "" {
		return value
	}
	if value[0] >= 'a' && value[0] <= 'z' {
		return string(value[0]-32) + value[1:]
	}
	return value
}
