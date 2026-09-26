package overview

import (
	"fmt"
	"strings"
	"testing"
)

func TestMinificationPreservesObservedCallsAndCommonJS(t *testing.T) {
	statements := make([]string, 360)
	for i := range statements {
		statements[i] = fmt.Sprintf("var value%d=%d;", i, i)
	}
	statements = append(statements,
		"function finish(){fetch('/api/items')}",
		"function work(){finish()}",
		"function start(){work()}",
		"module.exports={start};",
		"start();",
	)
	for _, separator := range []string{"", "\n"} {
		code := strings.Join(statements, separator)
		file := Analyze([]Source{{Name: "module.js", Data: []byte(code)}}).Files[0]
		if file.Format != "CommonJS module" || !has(file.Exports, "module.exports") ||
			!hasLink(file.Links, Link{From: "start()", To: "work()", Line: lineOf(code, "function start")}) ||
			len(file.EntryPoints) == 0 || file.Diagram == "" {
			t.Fatalf("overview lost semantic facts with separator %q: %#v", separator, file)
		}
		if separator == "" && file.Build != "minified" {
			t.Fatalf("expected descriptive minification label, got %q", file.Build)
		}
	}
}

func lineOf(code, fragment string) int {
	at := strings.Index(code, fragment)
	if at < 0 {
		return 0
	}
	return strings.Count(code[:at], "\n") + 1
}

func TestMultipleWebpackRegistrationsRespectRuntimeBoundaries(t *testing.T) {
	code := `(self.webpackChunkA=self.webpackChunkA||[]).push([[1],{
2:(m,e,r)=>{r(3);r(99)},
5:(m,e,r)=>{function same(){return 1}}
},r=>{r.O(0,[9],()=>r(3));r.O()}]);
(self.webpackChunkA=self.webpackChunkA||[]).push([[2],{
3:(m,e,r)=>{function same(){return 2}}
}]);
(self.webpackChunkB=self.webpackChunkB||[]).push([[4],{
99:(m,e,r)=>{function same(){return 3}}
}]);`
	file := Analyze([]Source{{Name: "combined.js", Data: []byte(code)}}).Files[0]
	if file.Bundle == nil || len(file.Bundle.Chunks) != 3 || file.Bundle.Chunks[0].ID != "1" ||
		file.Bundle.Chunks[1].ID != "2" || file.Bundle.Chunks[2].ID != "4" {
		t.Fatalf("registrations were not kept separate: %#v", file.Bundle)
	}
	startup := file.Bundle.Chunks[0].Startup
	if startup == nil || len(startup.Modules) != 1 || !startup.Modules[0].DefinedHere || startup.Modules[0].DefinedInChunk != "2" {
		t.Fatalf("same-runtime startup target should resolve across registrations: %#v", startup)
	}
	var crossChunk, unresolved bool
	for _, dep := range file.Bundle.Dependencies {
		if dep.From == "2" && dep.To == "3" {
			crossChunk = dep.DefinedHere && dep.ToChunk == "2"
		}
		if dep.From == "2" && dep.To == "99" {
			unresolved = !dep.DefinedHere && dep.ToChunk == ""
		}
	}
	if !crossChunk || !unresolved || strings.Contains(file.Diagram, "m0 -->|requires| m0") {
		t.Fatalf("dependency crosses wrong runtime or module scope: %#v / %s", file.Bundle.Dependencies, file.Diagram)
	}
	if len(file.Links) > 0 {
		t.Fatalf("same local function names in separate factories became file-level call links: %#v", file.Links)
	}
}

func TestBundleKeepsMultipleMeaningfulModulesWithSameRole(t *testing.T) {
	paths := func(prefix string) string {
		values := make([]string, 20)
		for i := range values {
			values[i] = fmt.Sprintf("%q", fmt.Sprintf("/%s/page%d", prefix, i))
		}
		return "[" + strings.Join(values, ",") + "]"
	}
	code := `(self.webpackChunkapp=self.webpackChunkapp||[]).push([[7],{` +
		`11:(m,e,r)=>{const a=` + paths("alpha") + `},` +
		`12:(m,e,r)=>{const b=` + paths("beta") + `},` +
		`13:(m,e,r)=>{const c=` + paths("gamma") + `}}]);`
	file := Analyze([]Source{{Name: "routes.js", Data: []byte(code)}}).Files[0]
	if file.Bundle == nil || len(file.Bundle.Modules) < 3 {
		t.Fatalf("same-role route modules were collapsed: %#v", file.Bundle)
	}
	seen := make(map[string]bool)
	for _, module := range file.Bundle.Modules {
		seen[module.ID] = true
	}
	if !seen["11"] || !seen["12"] || !seen["13"] {
		t.Fatalf("important route modules were dropped: %#v", file.Bundle.Modules)
	}
}

func TestInsightsKeepDuplicateModuleIDsSeparateAcrossRuntimes(t *testing.T) {
	code := `(window.webpackChunkA=window.webpackChunkA||[]).push([[1],{10:(e,t,r)=>{const C=()=>null;const a=(0,j.jsx)(Route,{path:'/alpha',Component:C})}}]);` +
		`(window.webpackChunkB=window.webpackChunkB||[]).push([[2],{10:(e,t,r)=>{const D=()=>null;const b=(0,j.jsx)(Route,{path:'/beta',Component:D})}}]);`
	file := Analyze([]Source{{Name: "parallel.js", Data: []byte(code)}}).Files[0]
	if file.Insights == nil {
		t.Fatal("expected route facts in both runtime registrations")
	}
	seen := map[string]bool{}
	for _, route := range file.Insights.Routes {
		if route.Module != "10" {
			continue
		}
		seen[route.Runtime+":"+route.Path] = true
	}
	if !seen["webpackChunkA:/alpha"] || !seen["webpackChunkB:/beta"] {
		t.Fatalf("same module ID lost runtime ownership: %#v", file.Insights.Routes)
	}
	if file.Bundle == nil || len(file.Bundle.Chunks) != 2 || file.Bundle.Chunks[0].Runtime == file.Bundle.Chunks[1].Runtime {
		t.Fatalf("distinct runtime registrations were merged: %#v", file.Bundle)
	}
}

func TestDuplicateModuleIDInOneRuntimeIsAmbiguous(t *testing.T) {
	code := `(self.webpackChunkA=self.webpackChunkA||[]).push([[1],{2:(m,e,r)=>{r(3)},3:(m,e,r)=>{}},r=>r(3)]);` +
		`(self.webpackChunkA=self.webpackChunkA||[]).push([[2],{3:(m,e,r)=>{}}]);`
	file := Analyze([]Source{{Name: "redefined.js", Data: []byte(code)}}).Files[0]
	if file.Bundle == nil || len(file.Bundle.Chunks) != 2 || file.Bundle.Chunks[0].Startup == nil ||
		!file.Bundle.Chunks[0].Startup.Modules[0].Ambiguous || file.Bundle.Chunks[0].Startup.Modules[0].DefinedHere {
		t.Fatalf("duplicate startup module was resolved to one definition: %#v", file.Bundle)
	}
	for _, dep := range file.Bundle.Dependencies {
		if dep.From == "2" && dep.To == "3" {
			if !dep.Ambiguous || dep.DefinedHere || dep.ToChunk != "" {
				t.Fatalf("duplicate dependency had false target: %#v", dep)
			}
			return
		}
	}
	t.Fatal("expected direct dependency to ambiguous module")
}

func TestConciseFactoryDependencyHasModuleOwnership(t *testing.T) {
	code := `(self.webpackChunkapp=self.webpackChunkapp||[]).push([[1],{2:(m,e,r)=>r(3),3:(m,e,r)=>{}}]);`
	file := Analyze([]Source{{Name: "concise.js", Data: []byte(code)}}).Files[0]
	if file.Bundle == nil || len(file.Bundle.Chunks) != 1 || file.Bundle.Chunks[0].ModuleCount != 2 {
		t.Fatalf("concise factory count was lost: %#v", file.Bundle)
	}
	for _, dep := range file.Bundle.Dependencies {
		if dep.From == "2" && dep.To == "3" && dep.DefinedHere && dep.ToChunk == "1" {
			return
		}
	}
	t.Fatalf("concise factory's direct require was lost: %#v", file.Bundle.Dependencies)
}

func TestBundleFactsKeepDirectCallsSeparateFromRequestWrappers(t *testing.T) {
	code := `(self.webpackChunkapp=self.webpackChunkapp||[]).push([[1],{` +
		`10:(m,e,r)=>{window.__assetPrefix__="/assets";function show(){fetch("/api/config")}},` +
		`11:(m,e,r)=>{function read(){return request({method:"GET",url:"/api/items"})}` +
		`function save(){return request({method:"POST",url:"/api/items"})}},` +
		`12:(m,e,r)=>{const Page=()=>null;const view=(0,j.jsx)(Route,{path:"/home",Component:Page});navigate("/account")}` +
		`}]);`
	file := Analyze([]Source{{Name: "mixed.js", Data: []byte(code)}}).Files[0]
	if file.Insights == nil || file.Bundle == nil {
		t.Fatalf("expected owned bundle facts: %#v", file)
	}
	roles := make(map[string]string)
	for _, component := range file.Insights.Components {
		roles[component.Module] = component.Role
	}
	if roles["10"] == "API client" || roles["11"] != "API client" {
		t.Fatalf("direct fetch was conflated with request wrappers: %#v", file.Insights.Components)
	}
	for _, module := range file.Bundle.Modules {
		if module.ID == "10" && module.Role == "API client" {
			t.Fatalf("direct-call module was ranked as an API client: %#v", module)
		}
	}
	if !strings.Contains(file.Diagram, "direct HTTP call expressions") ||
		!strings.Contains(file.Diagram, "HTTP request wrappers") ||
		!strings.Contains(file.Diagram, "UI route declarations") ||
		!strings.Contains(file.Diagram, "navigation targets") {
		t.Fatalf("diagram obscured fact kinds: %s", file.Diagram)
	}
}
