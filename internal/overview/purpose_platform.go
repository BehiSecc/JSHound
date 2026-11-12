package overview

import (
	"fmt"
	"sort"
	"strings"
)

// Platform is the host a file is written for, such as an Obsidian plugin or a
// browser extension, named only from what the file imports, requires, or calls.
type Platform struct {
	Name     string        `json:"name"`
	Role     string        `json:"role,omitempty"`
	Evidence []RunEvidence `json:"evidence,omitempty"`
}

// extensionNamespace is one browser extension API, such as chrome.tabs. The
// table is ordered from most to least sensitive, so a short list leads with
// what matters most. gloss says what the API lets a script do. backgroundOn
// lists events that only make sense in a background script: "*" means any
// onXxx event of the namespace.
type extensionNamespace struct {
	name, gloss, backgroundOn string
}

var extensionNamespaces = []extensionNamespace{
	{"debugger", "controls pages with the DevTools protocol", ""},
	{"nativeMessaging", "talks to a native app", ""},
	{"scripting", "runs code in pages", ""},
	{"webRequest", "observes or modifies requests", "*"},
	{"declarativeNetRequest", "blocks or rewrites requests", ""},
	{"cookies", "reads cookies", ""},
	{"proxy", "routes traffic through a proxy", ""},
	{"management", "manages other extensions", ""},
	{"history", "reads browsing history", ""},
	{"tabs", "reads and controls tabs", ""},
	{"webNavigation", "watches page navigation", "*"},
	{"downloads", "starts and reads downloads", ""},
	{"bookmarks", "reads and edits bookmarks", ""},
	{"identity", "signs in with browser accounts", ""},
	{"tabCapture", "captures tab audio and video", ""},
	{"desktopCapture", "captures the screen", ""},
	{"sessions", "", ""},
	{"topSites", "", ""},
	{"windows", "", ""},
	{"permissions", "", ""},
	{"contextMenus", "", "*"},
	{"commands", "", "*"},
	{"alarms", "", "*"},
	{"notifications", "", ""},
	{"offscreen", "", ""},
	{"sidePanel", "", ""},
	{"action", "", ""},
	{"browserAction", "", ""},
	{"storage", "", ""},
}

// extensionBackgroundEvents are runtime events that fire only in the
// background context.
var extensionBackgroundEvents = map[string]bool{
	"onInstalled": true, "onStartup": true, "onSuspend": true, "onUpdateAvailable": true,
}

const maxExtensionAPIs = 6

var extensionIndex = map[string]int{}

func init() {
	for i, namespace := range extensionNamespaces {
		extensionIndex[namespace.name] = i
	}
}

// hostModules are packages that only their host application provides. A file
// that imports one is written for that host.
var hostModules = map[string]string{
	"obsidian": "Obsidian plugin",
	"vscode":   "VS Code extension",
	"electron": "Electron app",
}

// platformSignals are the extra evidence behind the host platform. They are
// collected by the same single pass as the run signals.
type platformSignals struct {
	extensionAPIs                                   map[string]*sighting
	extensionRuntime, extensionBackground           sighting
	hostImports                                     map[string]*sighting
	electronMain, electronPreload, electronRenderer sighting
}

func newPlatformSignals() platformSignals {
	return platformSignals{extensionAPIs: map[string]*sighting{}, hostImports: map[string]*sighting{}}
}

// platformIdentifier records extension API use (chrome.tabs.query,
// browser.storage.local) and Electron markers.
func (s *runSignals) platformIdentifier(tokens []token, i int) {
	t := tokens[i]
	prev, next := at(tokens, i-1), at(tokens, i+1)
	prevDot := prev == "." || prev == "?."
	switch t.text {
	case "chrome", "browser":
		if prevDot || next != "." {
			return
		}
		namespace, member := at(tokens, i+2), at(tokens, i+4)
		if namespace == "runtime" {
			s.extensionRuntime.note(t, true)
			if extensionBackgroundEvents[member] {
				s.extensionBackground.note(t, true)
			}
			return
		}
		rank, ok := extensionIndex[namespace]
		if !ok {
			return
		}
		if s.extensionAPIs[namespace] == nil {
			s.extensionAPIs[namespace] = &sighting{}
		}
		s.extensionAPIs[namespace].note(t, true)
		switch on := extensionNamespaces[rank].backgroundOn; {
		case on == "*" && strings.HasPrefix(member, "on"):
			s.extensionBackground.note(t, true)
		case on != "" && on == member:
			s.extensionBackground.note(t, true)
		}
	case "BrowserWindow":
		if prev == "new" || prevDot {
			s.electronMain.note(t, true)
		}
	case "ipcMain", "whenReady":
		s.electronMain.note(t, true)
	case "exposeInMainWorld":
		if prevDot && next == "(" {
			s.electronPreload.note(t, true)
		}
	case "ipcRenderer":
		s.electronRenderer.note(t, true)
	}
}

// platformString records import or require of a host module and reports
// whether the string was a module specifier.
func (s *runSignals) platformString(tokens []token, i int, value string) bool {
	if _, ok := hostModules[value]; !ok {
		return false
	}
	prev := at(tokens, i-1)
	if prev != "from" && prev != "import" && !(prev == "(" && (at(tokens, i-2) == "require" || at(tokens, i-2) == "__require" || at(tokens, i-2) == "import")) {
		return false
	}
	if s.hostImports[value] == nil {
		s.hostImports[value] = &sighting{}
	}
	s.hostImports[value].note(tokens[i], true)
	return true
}

// extensionStorage is chrome.storage use, which needs no page.
func (s runSignals) extensionStorage() sighting {
	if h := s.extensionAPIs["storage"]; h != nil {
		return *h
	}
	return sighting{}
}

func evidenceOf(signal string, h sighting) RunEvidence {
	return RunEvidence{Signal: signal, Uses: h.uses, Line: h.line, Column: h.column}
}

// extensionNamespacesUsed returns the namespaces in the table that the file
// uses, most sensitive first.
func (s runSignals) extensionNamespacesUsed() []extensionNamespace {
	var used []extensionNamespace
	for name, h := range s.extensionAPIs {
		if h.stands() {
			used = append(used, extensionNamespaces[extensionIndex[name]])
		}
	}
	sort.Slice(used, func(i, j int) bool { return extensionIndex[used[i].name] < extensionIndex[used[j].name] })
	return used
}

// extensionFact lists the extension APIs a script uses, such as
// "scripting (runs code in pages), tabs (reads and controls tabs), storage".
func (s runSignals) extensionFact() (string, []RunEvidence) {
	used := s.extensionNamespacesUsed()
	if len(used) == 0 {
		return "", nil
	}
	var parts []string
	var evidence []RunEvidence
	for _, namespace := range used[:min(len(used), maxExtensionAPIs)] {
		part := namespace.name
		if namespace.gloss != "" {
			part += " (" + namespace.gloss + ")"
		}
		parts = append(parts, part)
		evidence = append(evidence, evidenceOf("chrome."+namespace.name, *s.extensionAPIs[namespace.name]))
	}
	detail := strings.Join(parts, ", ")
	if len(used) > len(parts) {
		detail += fmt.Sprintf(", and %d more", len(used)-len(parts))
	}
	return detail, evidence
}

// isExtension reports extension API use: chrome.runtime, or two different
// namespaces. One lone namespace is feature detection on an ordinary page.
func (s runSignals) isExtension() bool {
	return s.extensionRuntime.stands() || len(s.extensionNamespacesUsed()) >= 2
}

// detectPlatform names the host from imports first, then extension APIs.
// Roles are stated only when the code shows them:
//
//   - Electron: exposeInMainWorld is a preload script; BrowserWindow, ipcMain,
//     or app.whenReady is the main process; ipcRenderer alone is a renderer.
//   - Browser extension: an onInstalled-style event, or an on* event of an API
//     only a background script needs (webRequest, alarms, contextMenus), is a
//     background script; self.skipWaiting or a service worker event makes it a
//     service worker. Anything else is a plain extension script. Popup,
//     options, offscreen, and content scripts all call the same APIs and have
//     a DOM, so none of them is told apart from the others, and none is
//     called a background script.
func detectPlatform(s runSignals, f fileSignals) *Platform {
	for _, module := range []string{"obsidian", "vscode", "electron"} {
		h := s.hostImports[module]
		if h == nil {
			continue
		}
		p := &Platform{Name: hostModules[module], Evidence: []RunEvidence{evidenceOf(`require("`+module+`")`, *h)}}
		if module == "electron" {
			for _, role := range []struct {
				name string
				h    sighting
				what string
			}{
				{"preload script", s.electronPreload, "contextBridge.exposeInMainWorld"},
				{"main process", s.electronMain, "BrowserWindow, ipcMain, or app.whenReady"},
				{"renderer", s.electronRenderer, "ipcRenderer"},
			} {
				if role.h.uses > 0 {
					p.Role = role.name
					p.Evidence = append(p.Evidence, evidenceOf(role.what, role.h))
					break
				}
			}
		}
		return p
	}
	if !f.extension && !s.isExtension() {
		return nil
	}
	p := &Platform{Name: "Browser extension"}
	switch {
	case f.serviceWorker:
		p.Role = "service worker"
	case s.extensionBackground.uses > 0:
		p.Role = "background script"
		p.Evidence = append(p.Evidence, evidenceOf("background-only event", s.extensionBackground))
	}
	if s.extensionRuntime.uses > 0 {
		p.Evidence = append(p.Evidence, evidenceOf("chrome.runtime", s.extensionRuntime))
	}
	return p
}

// pageContext is where a file's code runs, as far as the evidence shows.
// Unknown counts as a page: facts are dropped only when the context is
// established.
type pageContext int

const (
	contextPage       pageContext = iota // a page, or not known
	contextNoDocument                    // worker or service worker: no window, document, or Web Storage
	contextNode                          // Node.js host: no browser APIs at all
	contextMixed                         // the identity names Node.js or a background script, yet page globals appear
)

// bootsInPage reports whether "Starts" and "Navigation" may be stated: how a
// file boots and navigates is part of its identity, so a file that identifies
// as a Node.js program or a background script does not mount a UI or route
// pages, even when it also contains code meant to run in pages.
func bootsInPage(ctx pageContext) bool {
	return ctx == contextPage
}

// hasPageAPIs reports whether storage, audio, and similar page facts may be
// stated. It is conservative: only an established context without a page
// drops them.
func hasPageAPIs(ctx pageContext) bool {
	return ctx == contextPage || ctx == contextMixed
}

// runContext decides where the file runs. A service worker, a web worker, and
// an Electron main process have no page. A Node.js file or VS Code extension
// is a Node host when it touches no page globals at all. When page globals
// appear next to a Node.js or background-script identity, the file is mixed:
// the page facts stay conservative, but it is not said to boot in a page.
func runContext(f fileSignals, runtime string, p *Platform) pageContext {
	switch {
	case p != nil && p.Name == "VS Code extension", p != nil && p.Name == "Electron app" && p.Role == "main process":
		return contextNode
	case f.serviceWorker, runtime == "web worker":
		return contextNoDocument
	case p != nil && p.Name == "Browser extension" && p.Role == "background script":
		if f.browser == 0 {
			return contextNoDocument
		}
		return contextMixed
	case runtime == "Node.js" && f.browser == 0 && p == nil:
		return contextNode
	case runtime == "Node.js":
		// Only when the identity sentence itself says Node.js, which a
		// platform phrase replaces.
		if _, hosted := platformPhrase(p); !hosted {
			return contextMixed
		}
	}
	return contextPage
}

// platformPhrase is the head of the identity sentence for a platform. It
// returns false when the ordinary runtime-and-kind wording should be kept.
func platformPhrase(p *Platform) (string, bool) {
	if p == nil {
		return "", false
	}
	switch p.Name {
	case "Browser extension":
		if p.Role == "" {
			return "", false
		}
		return p.Name + " " + p.Role, true
	case "Electron app":
		switch p.Role {
		case "main process":
			return "Electron main process", true
		case "preload script":
			return "Electron preload script", true
		case "renderer":
			return "Electron renderer script", true
		}
		return "Electron app script", true
	}
	return p.Name, true
}
