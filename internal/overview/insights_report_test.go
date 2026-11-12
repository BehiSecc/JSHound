package overview

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestInsightsReportOmitsAPIRequestsAcrossFormats(t *testing.T) {
	routes := []InsightRoute{{Path: "/login", Module: "16206", Line: 1, Column: 420}}
	for i := 0; i < 12; i++ {
		routes = append(routes, InsightRoute{Path: "/page/" + string(rune('a'+i)), Module: "16206", Line: 1, Column: 500 + i})
	}
	api := []InsightAPI{
		{Name: "read config", Method: "GET", Path: "/api/open/config", Kind: "request wrapper", Module: "37452", Line: 1, Column: 1030},
		{Name: "create registration", Method: "POST", Path: "/api/publishing/registration", Kind: "request wrapper", Module: "37452", Line: 1, Column: 1130, Parameters: []string{"artist_id", "release_id"}},
	}
	file := File{
		Name: "main.js", Bytes: 855000, Format: "loader chunk", Build: "Webpack-style bundle",
		Bundle: &Bundle{Startup: &BundleStartup{WaitFor: []string{"594", "628"}, Modules: []StartupModule{
			{ID: "82529", DefinedHere: true, AssetPrefix: "https://cdn.example/assets"},
			{ID: "45009", DefinedHere: false},
		}}},
		Insights: &Insights{
			Summary: "Provides the home application, account access, and API wrappers.",
			Components: []InsightComponent{
				{Name: "App shell", Role: "React routes and lazy pages", Evidence: "route declarations", Module: "16206", Line: 1, Column: 200},
				{Name: "API client", Role: "Request definitions", Evidence: "a.x3 wrappers", Module: "37452", Line: 1, Column: 900},
			},
			Routes: routes,
			API:    api,
			Behaviors: []InsightBehavior{
				{Action: "Reports Visit analytics", Module: "16206", Line: 1, Column: 1500},
			},
			Notes: []string{"Module 45009 is outside this input; its effects cannot be determined here."},
		},
		Diagram: "flowchart LR\n  chunk[\"chunk 792\"] -->|defines| app[\"app shell\"]\n  chunk -->|defines| api[\"API client\"]\n",
	}
	result := Result{Sources: 1, Bytes: file.Bytes, Files: []File{file}}
	var plain, terminal, jsonOutput, jsonlOutput bytes.Buffer
	for _, output := range []struct {
		buf *bytes.Buffer
		fn  func() error
	}{
		{&plain, func() error { return WriteText(&plain, result, TextOptions{Verbose: true}) }},
		{&terminal, func() error { return WriteText(&terminal, result, TextOptions{Terminal: true, Verbose: true}) }},
		{&jsonOutput, func() error { return WriteJSON(&jsonOutput, result) }},
		{&jsonlOutput, func() error { return WriteJSONL(&jsonlOutput, result) }},
	} {
		if err := output.fn(); err != nil {
			t.Fatal(err)
		}
	}
	got := plain.String()
	for _, want := range []string{
		"### What this file does", "home application", "### Main components", "React routes and lazy pages",
		"module 16206 · line 1:200", "### Architecture", "```mermaid", "### UI routes", "/login",
		"+3 more routes", "### Notable behavior", "Visit analytics", "### Where to read first",
		"### Loading and visibility", "waits for chunks 594, 628", "module 45009 (outside this input)",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in report:\n%s", want, got)
		}
	}
	if strings.Contains(got, "/page/k") || strings.Contains(got, "### API requests") ||
		strings.Contains(got, "/api/open/config") || strings.Contains(got, "/api/publishing/registration") {
		t.Fatalf("unbounded or unsupported claim in report:\n%s", got)
	}
	for name, content := range map[string]string{
		"terminal": terminal.String(), "json": jsonOutput.String(), "jsonl": jsonlOutput.String(),
	} {
		if strings.Contains(content, "api_definitions") || strings.Contains(content, "/api/open/config") ||
			strings.Contains(content, "/api/publishing/registration") || strings.Contains(content, "API requests") {
			t.Fatalf("API inventory reached %s output:\n%s", name, content)
		}
	}
	var decoded Result
	if err := json.Unmarshal(jsonOutput.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Files[0].Insights == nil || len(decoded.Files[0].Insights.Routes) != len(routes) || len(decoded.Files[0].Insights.API) != 0 {
		t.Fatalf("JSON lost UI routes or exposed API entries: %#v", decoded.Files[0].Insights)
	}
	var line struct {
		Kind     string    `json:"kind"`
		Insights *Insights `json:"insights"`
	}
	if err := json.Unmarshal(jsonlOutput.Bytes(), &line); err != nil {
		t.Fatal(err)
	}
	if line.Kind != "overview" || line.Insights == nil || len(line.Insights.Routes) != len(routes) || len(line.Insights.API) != 0 {
		t.Fatalf("JSONL lost UI routes or exposed API entries: %#v", line)
	}
}

func TestInsightsReportEscapesUntrustedMarkdownAndANSI(t *testing.T) {
	file := File{
		Name: "odd.js", Format: "script", Build: "Webpack-style bundle",
		Bundle: &Bundle{},
		Insights: &Insights{
			Summary:    "One line\n### injected\x1b[31m",
			Components: []InsightComponent{{Name: "<script>|x", Role: "*route* `reader`", Module: "10", Line: 1, Column: 2}},
			Routes:     []InsightRoute{{Path: "/a|b\n### fake", Module: "10", Line: 1, Column: 3}},
			API:        []InsightAPI{{Name: "<bad>", Method: "GET", Path: "/api/a|b", Module: "10", Line: 1, Column: 4}},
		},
	}
	var output bytes.Buffer
	if err := WriteText(&output, Result{Sources: 1, Files: []File{file}}, TextOptions{Verbose: true}); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{"&lt;script&gt;\\|x", "/a\\|b ### fake"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing escaped %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "\x1b[") || strings.Contains(got, "\n### injected") || strings.Contains(got, "/api/a") || strings.Contains(got, "&lt;bad&gt;") {
		t.Fatalf("control or heading injection reached report:\n%s", got)
	}
}

func TestEmptyAndSmallOverviewStayConcise(t *testing.T) {
	for _, code := range []string{"", "export const version = '1';"} {
		result := Analyze([]Source{{Name: "small.js", Data: []byte(code)}})
		var output bytes.Buffer
		if err := WriteText(&output, result, TextOptions{Verbose: true}); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(output.String(), "### Main components") || strings.Contains(output.String(), "```mermaid") {
			t.Fatalf("small input received inflated report:\n%s", output.String())
		}
	}
}

func TestMinimalChunkWithoutInsightsStillRenders(t *testing.T) {
	code := `(self.webpackChunkapp=self.webpackChunkapp||[]).push([[1],{2:(m,e,r)=>{}}]);`
	result := Analyze([]Source{{Name: "small-chunk.js", Data: []byte(code)}})
	if result.Files[0].Bundle == nil {
		t.Fatalf("minimal chunk was not recognized: %#v", result.Files[0])
	}
	var output bytes.Buffer
	if err := WriteText(&output, result, TextOptions{Verbose: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "### Loading and visibility") || strings.Contains(output.String(), "### Main components") {
		t.Fatalf("minimal chunk report should use concise fallback:\n%s", output.String())
	}
}

func TestDuplicateModuleDefinitionsAreNotCountedAsExternal(t *testing.T) {
	code := `(self.webpackChunkapp=self.webpackChunkapp||[]).push([[1],{
101:(m,e,r)=>{r(202);r(999)},202:(m,e,r)=>{}
},r=>r(202)]);
(self.webpackChunkapp=self.webpackChunkapp||[]).push([[2],{
202:(m,e,r)=>{}
}]);`
	result := Analyze([]Source{{Name: "duplicate.js", Data: []byte(code)}})
	file := result.Files[0]
	if file.Bundle == nil || len(file.Bundle.Chunks) != 2 {
		t.Fatalf("multiple registrations not recognized: %#v", file.Bundle)
	}
	var output bytes.Buffer
	if err := WriteText(&output, result, TextOptions{Verbose: true}); err != nil {
		t.Fatal(err)
	}
	got := output.String()
	for _, want := range []string{
		"Startup requires module 202 (ambiguous: the ID has multiple definitions in this runtime)",
		"1 required module ID outside this input",
		"1 required module ID has multiple definitions in this input",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in duplicate-module report:\n%s", want, got)
		}
	}
	if strings.Contains(got, "2 required module IDs outside this input") {
		t.Fatalf("ambiguous in-file module counted as absent:\n%s", got)
	}
}

func TestRepresentativeRoutesCoversDeclaredPathFamilies(t *testing.T) {
	items := []InsightRoute{
		{Path: "/", Kind: "declared route"},
		{Path: "/login/oauth/middle", Kind: "declared route"},
		{Path: "/login/region-select", Kind: "declared route"},
		{Path: "/login/:type", Kind: "declared route"},
		{Path: "/login", Kind: "declared route"},
		{Path: "/home", Kind: "declared route"},
		{Path: "/content-supply/join-us", Kind: "declared route"},
		{Path: "/artist-story", Kind: "declared route"},
		{Path: "/funding", Kind: "declared route"},
		{Path: "/legal/privacy", Kind: "navigation target"},
	}
	selected := representativeRoutes(items, 6)
	got := make(map[string]bool)
	for _, route := range selected {
		got[route.Path] = true
	}
	for _, want := range []string{"/", "/login", "/home", "/content-supply/join-us", "/artist-story", "/funding"} {
		if !got[want] {
			t.Fatalf("route family %s omitted: %#v", want, selected)
		}
	}
	if got["/legal/privacy"] || got["/login/oauth/middle"] {
		t.Fatalf("navigation or duplicate family displaced declared route: %#v", selected)
	}
	withRoom := representativeRoutes(items, 7)
	for _, route := range withRoom {
		if route.Kind == "navigation target" {
			t.Fatalf("navigation target displaced an additional declared route: %#v", withRoom)
		}
	}
}

func TestCompiledReportRetainsBaselineAndCompletenessNote(t *testing.T) {
	file := File{
		Name: "compiled.js", Format: "CommonJS module", Build: "minified", Minified: true,
		Compiled:     &Compiled{Paths: []PathGroup{{Prefix: "/api/v1", Count: 2, Examples: []string{"/api/v1/users"}}}},
		Exports:      []string{"module.exports"},
		Units:        []Unit{{Name: "load", Kind: "function", Line: 1, Calls: []string{"render"}}},
		Links:        []Link{{From: "load()", To: "render()", Line: 1}},
		Interactions: []string{"fetch() calls"},
		Notes:        []string{"Local call analysis stopped at a complete top-level boundary in this large script."},
	}
	var output bytes.Buffer
	if err := WriteText(&output, Result{Sources: 1, Files: []File{file}}, TextOptions{Verbose: true}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"### Exports", "module.exports", "### Main units", "function load", "### Observed links", "load() → render()", "### Code interactions", "fetch() calls", "### Visibility limits", "Local call analysis stopped"} {
		if !strings.Contains(output.String(), want) {
			t.Errorf("compiled report lost %q:\n%s", want, output.String())
		}
	}
	var jsonOutput bytes.Buffer
	if err := WriteJSON(&jsonOutput, Result{Sources: 1, Files: []File{file}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "/api/v1") || strings.Contains(jsonOutput.String(), "/api/v1") || strings.Contains(jsonOutput.String(), "path_families") {
		t.Fatalf("compiled path inventory reached overview output:\n%s\n%s", output.String(), jsonOutput.String())
	}
}

func TestDirectHTTPCallRetainsHighLevelSummaryWithoutPath(t *testing.T) {
	file := Analyze([]Source{{Name: "app.js", Data: []byte(`function load(){return fetch("/api/items")}`)}}).Files[0]
	var output bytes.Buffer
	if err := WriteText(&output, Result{Sources: 1, Files: []File{file}}, TextOptions{Verbose: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Contains direct HTTP call expressions.") || strings.Contains(output.String(), "/api/items") {
		t.Fatalf("direct call overview exposed its request path:\n%s", output.String())
	}
	var encoded bytes.Buffer
	if err := WriteJSON(&encoded, Result{Sources: 1, Files: []File{file}}); err != nil {
		t.Fatal(err)
	}
	var decoded Result
	if err := json.Unmarshal(encoded.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if err := WriteText(&output, decoded, TextOptions{Verbose: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Contains direct HTTP call expressions.") || strings.Contains(output.String(), "/api/items") {
		t.Fatalf("API-only overview changed after JSON roundtrip:\n%s", output.String())
	}
}
