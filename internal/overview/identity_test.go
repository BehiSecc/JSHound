package overview

import (
	"fmt"
	"strings"
	"testing"
)

// typeOf is the identity sentence, which opens the summary.
func typeOf(t *testing.T, code string) string {
	t.Helper()
	summary := summaryOf(t, "a.js", code)
	if i := strings.Index(summary, ". "); i >= 0 {
		return summary[:i+1]
	}
	return summary
}

func TestPageMessagingAnExtensionIsNotAnExtension(t *testing.T) {
	page := `document.title="x";
chrome.runtime.sendMessage("abcdefghijklmnopabcdefghijklmnop",{a:1},function(r){if(chrome.runtime.lastError){return}});
chrome.runtime.connect("abcdefghijklmnopabcdefghijklmnop");`
	if got := typeOf(t, page); got != "Browser script." {
		t.Errorf("page that messages an extension by id: %q", got)
	}
	for _, code := range []string{
		`document.title="x";chrome.runtime.onMessage.addListener(f);`,
		`document.title="x";chrome.runtime.sendMessage({type:"ping"});`,
		`document.title="x";var u=browser.runtime.getURL("a.png");`,
		`document.title="x";var i=chrome.runtime.id;`,
	} {
		if got := typeOf(t, code); !strings.HasPrefix(got, "Browser extension script") {
			t.Errorf("%s: %q", code, got)
		}
	}
	// The second route stays: two other namespaces make an extension.
	if p := analyzePurpose(t, "a.js", `chrome.storage.local.get("k");chrome.tabs.query({},f);`); p.Platform == nil {
		t.Error("two namespaces no longer make an extension")
	}
}

func TestReactHooksAreCalledNotMentioned(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		react      bool
	}{
		{"indirect calls", `document.title="x";var[a,b]=(0,r.useState)(0);(0,r.useEffect)(f,[]);`, true},
		{"other core hooks", `document.title="x";var a=(0,r.useRef)(null);var b=(0,r.useMemo)(f,[]);`, true},
		{"direct calls", `document.title="x";useState(0);useReducer(f,0);`, true},
		{"one hook only", `document.title="x";var a=(0,r.useState)(0);`, false},
		{"same hook twice", `document.title="x";(0,r.useState)(0);(0,r.useState)(1);`, false},
		{"property and string", `document.title="x";var o={useState:1,useEffect:2,useRef:3};x.useMemo="useCallback";`, false},
		{"references without a call", `document.title="x";var a=r.useState,b=(r.useEffect);`, false},
	} {
		got := typeOf(t, tc.code)
		if has := strings.Contains(got, "React"); has != tc.react {
			t.Errorf("%s: %q, React = %v, want %v", tc.name, got, has, tc.react)
		}
	}
}

func TestNextChunkGlobalNeedsAPushOrADefinition(t *testing.T) {
	push := `(self.webpackChunk_N_E=self.webpackChunk_N_E||[]).push([[7],{1:function(e){e.exports=1}}]);`
	if got := typeOf(t, push); !strings.Contains(got, "Next.js") {
		t.Errorf("chunk push: %q", got)
	}
	for _, code := range []string{
		`document.title="x";var chunks=self.webpackChunk_N_E;`,
		`document.title="x";var name="webpackChunk_N_E";`,
		`document.title="x";if(self.webpackChunk_N_E==null){f()}`,
	} {
		if got := typeOf(t, code); strings.Contains(got, "Next.js") {
			t.Errorf("%s: %q", code, got)
		}
	}
}

func TestTypeAndBehaviorAgreeAboutAUIMount(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		app        bool
	}{
		{"root mount", `import{createRoot}from"react-dom/client";createRoot(document.getElementById("root")).render(App());`, true},
		{"vue mount", `createApp(App).mount("#app");document.title="x";`, true},
		{"view render", `document.body.appendChild(v.render().el);`, false},
		{"router mount", `document.title="x";router.mount(routes,"/");`, false},
		{"render into a variable", `document.title="x";function f(t,e){return t.render(e)}`, false},
		{"angular bootstrap", `document.title="x";bootstrapApplication(App,cfg);`, true},
		{"root into a variable", `document.title="x";var el=find();createRoot(el).render(App());`, true},
		{"member call into an expression", `document.title="x";ReactDOMClient.createRoot(host.firstChild).render(a);`, true},
		{"indirect call", `document.title="x";(0,r.createRoot)(e).render(a);`, true},
		{"hydrate root", `document.title="x";hydrateRoot(el,node);`, true},
		{"hydrate", `document.title="x";L.hydrate(tree,el);`, true},
		{"legacy render by name", `document.title="x";ReactDOM.render(App,el);`, true},
		{"vue chain", `document.title="x";createApp(App).use(router).mount(el);`, true},
		{"vue 2", `document.title="x";new Vue({render:h=>h(App)}).$mount(el);`, true},
		{"svelte mount", `document.title="x";mount(App,{target:el,props:{}});`, true},
		{"minified two-argument render", `document.title="x";function f(t,e){return s.render(t,e)}`, false},
		{"one-argument hydrate", `document.title="x";model.hydrate(data);this.hydrate(a,b);`, false},
		{"mount of an unrelated receiver", `document.title="x";app.mount(el);mount(a,b);`, false},
		{"createApp without a mount", `document.title="x";createApp(App).use(router);`, false},
	} {
		file := Analyze([]Source{{Name: "a.js", Data: []byte(tc.code)}}).Files[0]
		type_ := strings.Contains(typeOf(t, tc.code), " app")
		fact, _ := factOf(file.Purpose.Runs, "Starts")
		starts := strings.HasPrefix(fact.Detail, "mounts") || strings.HasPrefix(fact.Detail, "bootstraps")
		if type_ != tc.app || starts != tc.app {
			t.Errorf("%s: Type %q, Starts %q, want app = %v", tc.name, file.Summary, fact.Detail, tc.app)
		}
	}
}

func TestEsbuildChunksAreNamedByTheirImportShape(t *testing.T) {
	chunk := `import{a as r}from"./chunk-AB12CD34.js";import"./chunk-ZZ99YY88.js";export{r as Widget};`
	if got := typeOf(t, chunk); got != "Bundle chunk, bundled by esbuild." {
		t.Errorf("chunk: %q", got)
	}
	// Only a bootstrap or a mount makes it an app.
	entry := `import{a as r}from"./chunk-AB12CD34.js";document.title="x";bootstrapApplication(r,{});`
	if got := typeOf(t, entry); got != "Browser app, bundled by esbuild." {
		t.Errorf("entry: %q", got)
	}
	for _, code := range []string{
		`import{a}from"./chunk-ab12cd34.js";export{a as W};`,
		`import{a}from"./chunk-AB12CD3.js";export{a as W};`,
		`import{a}from"./chunk-AB12CD345.js";export{a as W};`,
		`import{a}from"./widgets.js";export{a as W};`,
		`import{a}from"lib/chunk-AB12CD34.js";export{a as W};`,
	} {
		if got := typeOf(t, code); strings.Contains(got, "esbuild") || strings.Contains(got, "chunk") {
			t.Errorf("%s: %q", code, got)
		}
	}
}

func TestModuleFederationNeedsBothSharedKeys(t *testing.T) {
	entry := `document.title="x";var d={1:{shareScope:"default",shareKey:"lib",import:"lib",requiredVersion:"^1.0.0",singleton:!0}};`
	if got := typeOf(t, entry); got != "Browser Module Federation container entry." {
		t.Errorf("entry: %q", got)
	}
	for _, code := range []string{
		`document.title="x";var d={shareKey:"lib"};`,
		`document.title="x";var d={requiredVersion:"^1.0.0"};`,
		`document.title="x";var s="shareKey requiredVersion";var shareKey=1,requiredVersion=2;`,
	} {
		if got := typeOf(t, code); strings.Contains(got, "Federation") {
			t.Errorf("%s: %q", code, got)
		}
	}
}

func TestClosurePrologueMarksCompiledMinifiedOutput(t *testing.T) {
	code := `"use strict";this.default_Mod=this.default_Mod||{};(function(_){var window=this;_.a=1;})(this.default_Mod);`
	file := Analyze([]Source{{Name: "a.js", Data: []byte(code)}}).Files[0]
	if !file.Minified || !strings.HasSuffix(fileProfile(file), " · minified") {
		t.Errorf("header: %q", fileProfile(file))
	}
	if !strings.Contains(file.Summary, "compiled by Closure Compiler") {
		t.Errorf("summary: %q", file.Summary)
	}
	// A module name that differs, or no window alias, is not the prologue.
	for _, other := range []string{
		`this.a=this.b||{};(function(_){var window=this;_.a=1;})(this.a);`,
		`this.default_Mod=this.default_Mod||{};(function(_){_.a=1;})(this.default_Mod);`,
	} {
		file := Analyze([]Source{{Name: "a.js", Data: []byte(other)}}).Files[0]
		if file.Minified || strings.Contains(file.Summary, "Closure") {
			t.Errorf("%s: minified = %v, %q", other, file.Minified, file.Summary)
		}
	}
}

func TestAssignedFunctionNamesJudgeMinification(t *testing.T) {
	build := func(format string) string {
		var b strings.Builder
		b.WriteString("(function(_){var window=this;\ntry{\n")
		for i := 0; i < 30; i++ {
			fmt.Fprintf(&b, format, shortName(i), i)
		}
		b.WriteString("}catch(e){}})(this);\n")
		return b.String()
	}
	for _, tc := range []struct {
		name, format string
		want         bool
	}{
		{"namespace members", "_.%s=function(a){return a+%d};\n", true},
		{"bare names", "%s=function(a){return a+%d};\n", true},
		{"classes", "_.%s=class{constructor(){this.v=%d}};\n", true},
		{"members of a named object", "exports.%s=function(a){return a+%d};\n", false},
	} {
		file := Analyze([]Source{{Name: "a.js", Data: []byte(build(tc.format))}}).Files[0]
		if file.Minified != tc.want {
			t.Errorf("%s: minified = %v, want %v", tc.name, file.Minified, tc.want)
		}
	}
	var readable strings.Builder
	readable.WriteString("(function(_){var window=this;\n")
	for i := 0; i < 30; i++ {
		fmt.Fprintf(&readable, "_.loadItem%d=function(a){return a+%d};\nrenderPanel%d=function(a){return a};\n", i, i, i)
	}
	readable.WriteString("})(this);\n")
	if file := Analyze([]Source{{Name: "a.js", Data: []byte(readable.String())}}).Files[0]; file.Minified {
		t.Errorf("readable assigned functions flagged as minified")
	}
}

func TestScriptsThatOnlyDefineCustomElementsSayso(t *testing.T) {
	define := `document.title="x";customElements.define("x-card",class extends HTMLElement{});`
	if got := typeOf(t, define); got != "Browser script that defines custom elements." {
		t.Errorf("define: %q", got)
	}
	for _, code := range []string{
		`document.title="x";var c=window.customElements.get("x-card");`,
		`document.title="x";var s="customElements.define(";`,
	} {
		if got := typeOf(t, code); got != "Browser script." {
			t.Errorf("%s: %q", code, got)
		}
	}
}

func TestMountStartsNameTheContainerOnlyWhenItIsALookup(t *testing.T) {
	for _, tc := range []struct{ code, want string }{
		{`import{createRoot}from"react-dom/client";createRoot(document.getElementById("root")).render(App());`, "mounts a React UI into #root"},
		{`import{createRoot}from"react-dom/client";var el=find();createRoot(el).render(App());`, "mounts a React UI"},
		{`import{createApp}from"vue";createApp(App).mount(el);`, "mounts a Vue UI"},
	} {
		file := Analyze([]Source{{Name: "a.js", Data: []byte(tc.code)}}).Files[0]
		if fact, _ := factOf(file.Purpose.Runs, "Starts"); !strings.HasPrefix(fact.Detail, tc.want) || strings.Contains(strings.TrimPrefix(fact.Detail, tc.want), " into ") {
			t.Errorf("%s: Starts = %q, want %q", tc.code, fact.Detail, tc.want)
		}
	}
}

func TestAFrameworkRuntimeThatDefinesItsEntryPointsIsNotAnApp(t *testing.T) {
	for _, code := range []string{
		`document.title="x";t.createRoot=function(e,n){return build(createRoot(e),n)};t.hydrateRoot=function(e,n){return this.createRoot(e)};`,
		`document.title="x";function createRoot(e){return init(createRoot(e))}function hydrate(a,b){return createRoot(b)}`,
		`document.title="x";var api={createRoot:function(e){return make(e)},hydrateRoot:function(e,n){return api.createRoot(e,n)},hydrate:function(a,b){return x.hydrate(a,b)}};`,
	} {
		if got := typeOf(t, code); strings.Contains(got, " app") {
			t.Errorf("%s: %q", code, got)
		}
		file := Analyze([]Source{{Name: "a.js", Data: []byte(code)}}).Files[0]
		if fact, ok := factOf(file.Purpose.Runs, "Starts"); ok && strings.HasPrefix(fact.Detail, "mounts") {
			t.Errorf("%s: Starts = %q", code, fact.Detail)
		}
	}
	// The same call outside a definition is a use.
	use := `document.title="x";t.createRoot=function(e){return make(e)};createRoot(host);`
	if got := typeOf(t, use); !strings.Contains(got, " app") {
		t.Errorf("use after a definition: %q", got)
	}
}

func TestACompatibilityWrapperAroundCreateRootIsNotAnApp(t *testing.T) {
	for _, code := range []string{
		`document.title="x";function f(el){return typeof c.createRoot=="function"?c.createRoot(el):c.render(a,el)}`,
		`document.title="x";function f(el){if(!n.createRoot)throw Error("old");n.createRoot(el).render(a)}`,
		`document.title="x";function f(el){return n.createRoot&&n.createRoot(el)}`,
	} {
		if got := typeOf(t, code); strings.Contains(got, " app") {
			t.Errorf("%s: %q", code, got)
		}
	}
}
