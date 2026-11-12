package overview

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestExtensionAPIsAreListedMostSensitiveFirst(t *testing.T) {
	source := `chrome.storage.local.get("k");chrome.tabs.query({},f);chrome.tabs.create({});
chrome.scripting.executeScript({target:{tabId:1},files:["a.js"]});chrome.cookies.getAll({});
chrome.runtime.sendMessage({});`
	facts := factsOf(source, contextPage)
	fact, ok := factOf(facts, "Extension APIs")
	if !ok {
		t.Fatalf("no extension APIs: %s", factDetails(facts))
	}
	want := "scripting (runs code in pages), cookies (reads cookies), tabs (reads and controls tabs), storage"
	if fact.Detail != want {
		t.Errorf("detail = %q, want %q", fact.Detail, want)
	}
	var signals []string
	for _, item := range fact.Evidence {
		signals = append(signals, item.Signal)
		if item.Signal == "chrome.tabs" && (item.Uses != 2 || item.Line != 1) {
			t.Errorf("tabs evidence = %+v", item)
		}
	}
	if strings.Join(signals, ",") != "chrome.scripting,chrome.cookies,chrome.tabs,chrome.storage" {
		t.Errorf("evidence order = %v", signals)
	}
	if storage, _ := factOf(facts, "Storage"); storage.Detail != "extension storage" {
		t.Errorf("storage = %q", storage.Detail)
	}
}

func TestExtensionAPIListIsCapped(t *testing.T) {
	var source strings.Builder
	for _, name := range []string{"debugger", "scripting", "webRequest", "cookies", "history", "tabs", "bookmarks", "alarms"} {
		source.WriteString("chrome." + name + ".x();")
	}
	fact, _ := factOf(factsOf(source.String(), contextPage), "Extension APIs")
	if !strings.HasSuffix(fact.Detail, ", and 2 more") || strings.Contains(fact.Detail, "alarms") {
		t.Errorf("detail = %q", fact.Detail)
	}
}

func TestExtensionNeedsRuntimeOrTwoNamespaces(t *testing.T) {
	if p := analyzePurpose(t, "a.js", `if(chrome.storage){document.title="x"}`); p.Platform != nil || strings.Contains(p.Is, "extension") {
		t.Errorf("one namespace made an extension: %q %+v", p.Is, p.Platform)
	}
	if p := analyzePurpose(t, "a.js", `var r=browser.runtime.getURL("a");document.title=r;`); p.Platform == nil || p.Platform.Name != "Browser extension" {
		t.Errorf("browser.runtime: %+v", p.Platform)
	}
	if p := analyzePurpose(t, "a.js", `var chrome={tabs:1,cookies:2};use(chrome.name)`); p.Platform != nil {
		t.Errorf("local variable made an extension: %+v", p.Platform)
	}
}

func TestExtensionBackgroundIsClaimedOnlyFromBackgroundEvents(t *testing.T) {
	for _, tc := range []struct {
		name, code, is, role string
	}{
		{"install event", `chrome.runtime.onInstalled.addListener(function(){});chrome.tabs.query({},f);`, "Browser extension background script.", "background script"},
		{"alarm event", `chrome.alarms.onAlarm.addListener(f);chrome.runtime.getURL("a");`, "Browser extension background script.", "background script"},
		{"request event", `chrome.webRequest.onBeforeRequest.addListener(f,{});chrome.runtime.id;`, "Browser extension background script.", "background script"},
		{"service worker", `self.addEventListener("install",f);chrome.runtime.id;`, "Browser extension service worker.", "service worker"},
		// A popup calls the same APIs and has a DOM; nothing says background.
		{"popup", `document.getElementById("a").onclick=function(){chrome.tabs.query({},f);chrome.runtime.sendMessage({})};`, "Browser extension script.", ""},
		{"messaging only", `document.body.append("x");chrome.runtime.sendMessage({});chrome.runtime.onMessage.addListener(f);`, "Browser extension script.", ""},
	} {
		p := analyzePurpose(t, "ext.js", tc.code)
		if p.Platform == nil || p.Platform.Name != "Browser extension" || p.Platform.Role != tc.role || p.Is != tc.is {
			t.Errorf("%s: is %q platform %+v", tc.name, p.Is, p.Platform)
		}
	}
}

func TestHostApplicationsAreNamedFromTheirImports(t *testing.T) {
	for _, tc := range []struct {
		name, code, is, platform, role string
	}{
		{"obsidian require", `var o=require("obsidian");var e=require("electron");class P extends o.Plugin{}module.exports=P;`, "Obsidian plugin.", "Obsidian plugin", ""},
		{"obsidian import", `import {Plugin} from "obsidian";export default class P extends Plugin{}`, "Obsidian plugin.", "Obsidian plugin", ""},
		{"obsidian with React", `var o=require("obsidian");var r=require("react");document.title="x";`, "Obsidian plugin using React.", "Obsidian plugin", ""},
		{"vscode", `const v=require("vscode");exports.activate=function(c){v.window.showInformationMessage("x")};`, "VS Code extension.", "VS Code extension", ""},
		{"electron main", `const {app,BrowserWindow}=require("electron");app.whenReady().then(()=>new BrowserWindow({}));`, "Electron main process.", "Electron app", "main process"},
		{"electron preload", `const {contextBridge,ipcRenderer}=require("electron");contextBridge.exposeInMainWorld("api",{});`, "Electron preload script.", "Electron app", "preload script"},
		{"electron renderer", `const {ipcRenderer}=require("electron");ipcRenderer.send("a");document.title="x";`, "Electron renderer script.", "Electron app", "renderer"},
		{"electron only imported", `import {shell} from "electron";shell.openExternal("x");`, "Electron app script.", "Electron app", ""},
	} {
		p := analyzePurpose(t, "host.js", tc.code)
		if p.Platform == nil || p.Platform.Name != tc.platform || p.Platform.Role != tc.role || !strings.HasPrefix(p.Is, tc.is) {
			t.Errorf("%s: is %q platform %+v", tc.name, p.Is, p.Platform)
		}
	}
}

func TestHostModuleNeedsAnImportNotAString(t *testing.T) {
	p := analyzePurpose(t, "a.js", `var names=["obsidian","vscode","electron"];document.title=names.join();`)
	if p.Platform != nil {
		t.Errorf("strings made a platform: %+v", p.Platform)
	}
}

func TestPlatformIsPartOfTheJSON(t *testing.T) {
	p := analyzePurpose(t, "host.js", "var o=require(\"obsidian\");\nvar p=require(\"obsidian\");")
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	want := `"platform":{"name":"Obsidian plugin","evidence":[{"signal":"require(\"obsidian\")","uses":2,"line":1,"column":15}]}`
	if !strings.Contains(string(data), want) {
		t.Errorf("json = %s", data)
	}
	plain := analyzePurpose(t, "a.js", `document.title="x";`)
	data, _ = json.Marshal(plain)
	if strings.Contains(string(data), "platform") {
		t.Errorf("plain file has a platform: %s", data)
	}
}

func TestPageScriptsPassedToTheBrowserAreNotThisFilesBehaviour(t *testing.T) {
	code := `chrome.scripting.executeScript({target:{tabId:1},func:function(){document.cookie="a=1";fetch("/in-page")}});`
	facts := factsOf(code, contextPage)
	masked := runFacts(scanRunSignals(lex(code), func() []bool { m, _, _ := pageScriptMask(lex(code)); return m }()), nil, false, contextPage)
	if _, ok := factOf(facts, "Storage"); !ok {
		t.Fatalf("unmasked scan should see the cookie: %s", factDetails(facts))
	}
	if _, ok := factOf(masked, "Storage"); ok {
		t.Errorf("in-page code counted: %s", factDetails(masked))
	}
	if _, ok := factOf(masked, "Server calls"); ok {
		t.Errorf("in-page fetch counted: %s", factDetails(masked))
	}
}
