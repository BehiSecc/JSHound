package overview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func src(name, code string) Source { return Source{Name: name, Data: []byte(code)} }

func hasLoad(file File, name string) bool {
	for _, load := range file.Loads {
		if load == name {
			return true
		}
	}
	return false
}

func featureAreas(features []Feature) []string {
	var areas []string
	for _, feature := range features {
		areas = append(areas, feature.Area)
	}
	return areas
}

func TestSingleSourceHasNoOverall(t *testing.T) {
	result := Analyze([]Source{src("a.js", billingApp)})
	if result.Overall != nil {
		t.Fatalf("single source has an overall: %+v", result.Overall)
	}
	var out bytes.Buffer
	if err := WriteJSON(&out, result); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out.String(), `"overall"`) || strings.Contains(out.String(), `"loads"`) || strings.Contains(out.String(), `"file":`) {
		t.Errorf("single-source JSON gained fields:\n%s", out.String())
	}
	out.Reset()
	if err := WriteJSONL(&out, result); err != nil {
		t.Fatal(err)
	}
	if lines := strings.Split(strings.TrimSpace(out.String()), "\n"); len(lines) != 1 || !strings.HasPrefix(lines[0], `{"kind":"overview",`) {
		t.Errorf("single-source JSONL = %s", out.String())
	}
	var text bytes.Buffer
	if err := WriteText(&text, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(text.String(), "All 1 files") || !strings.Contains(text.String(), "\n## a.js\n") || !strings.HasSuffix(text.String(), "\nUse --verbose for structure, entry paths, and call flow.\n") {
		t.Errorf("single-source text changed:\n%s", text.String())
	}
}

func TestMergedFeaturesCountSharedNamesAndEvidenceOnce(t *testing.T) {
	// Two names and one API path appear in both files.
	a := `import "./b.js";var n=["invoiceTable","invoiceList","sendInvoice","invoiceDetails"];var u=["/api/v1/invoices"];`
	b := `var n=["invoiceTable","invoiceList","payInvoice","invoiceReport"];var u=["/api/v1/invoices","/api/v1/invoices/export"];`
	result := Analyze([]Source{src("dir/a.js", a), src("dir/b.js", b)})
	o := result.Overall
	if o == nil {
		t.Fatal("no overall")
	}
	if len(o.Features) != 1 || o.Features[0].Area != "invoice" {
		t.Fatalf("features = %v", featureAreas(o.Features))
	}
	if got := o.Features[0].Names; got != 6 {
		t.Errorf("invoice names = %d, want 6 distinct names", got)
	}
	if got := o.Features[0].APICalls; got != 2 {
		t.Errorf("invoice API calls = %d, want 2 distinct paths", got)
	}
	if o.Evidence == nil || o.Evidence.APICalls != 2 {
		t.Errorf("evidence = %+v", o.Evidence)
	}
}

func TestMergedFeaturesNeedASecondKindOfEvidence(t *testing.T) {
	var names []string
	for _, suffix := range []string{"Alpha", "Beta", "Gamma", "Delta", "Omega", "Sigma", "Kappa", "Theta", "Lambda", "Zeta", "Iota", "Rho"} {
		names = append(names, `"sendWidget`+suffix+`"`, `"editWidget`+suffix+`"`)
	}
	half := func(from, to int) string { return `var n=[` + strings.Join(names[from:to], ",") + `];` }
	linked := func(code string) string { return `import "./two.js";` + code }
	single := Analyze([]Source{src("one.js", half(0, 24))})
	if _, ok := featureByArea(single.Files[0].Purpose, "widget"); !ok {
		t.Fatalf("a single file with many acted-on names has no widget area: %+v", single.Files[0].Purpose.Features)
	}
	pair := Analyze([]Source{src("d/one.js", linked(half(0, 12))), src("d/two.js", half(12, 24))})
	if pair.Overall == nil || len(pair.Overall.Features) != 0 {
		t.Errorf("names alone made set-level features: %+v", pair.Overall)
	}
	// With one API call anywhere in the set, the area stands.
	withAPI := Analyze([]Source{src("d/one.js", linked(half(0, 12))), src("d/two.js", half(12, 24)+`var u=["/api/v1/widgets"];`)})
	if areas := featureAreas(withAPI.Overall.Features); len(areas) != 1 || areas[0] != "widget" {
		t.Errorf("features with an API call = %v", areas)
	}
}

func TestLibraryBannerFilesContributeNothingToTheSet(t *testing.T) {
	var names []string
	for _, suffix := range []string{"Alpha", "Beta", "Gamma", "Delta", "Omega", "Sigma", "Kappa", "Theta"} {
		names = append(names, `"sendGadget`+suffix+`"`)
	}
	lib := "/*! gadgetlib v1.2.3 | MIT */\nimport \"./app.js\";var n=[" + strings.Join(names, ",") + `];var u=["/api/v1/gadgets"];`
	result := Analyze([]Source{src("d/gadgetlib.js", lib), src("d/app.js", `fetch("/api/v1/other");`)})
	if len(result.Overall.Features) != 0 {
		t.Errorf("a banner library contributed features: %+v", result.Overall.Features)
	}
}

func TestMergedRunFactsSumUsesAndNameTheFirstFile(t *testing.T) {
	a := `window.location.href;fetch("/a");fetch("/b");`
	b := `document.title="x";fetch("/c");fetch("/d");fetch("/e");`
	result := Analyze([]Source{src("d/a.js", a), src("d/b.js", b)})
	o := result.Overall
	fact, ok := factOf(o.Runs, "Server calls")
	if !ok {
		t.Fatalf("no server calls: %+v", o.Runs)
	}
	var fetch *RunEvidence
	for i := range fact.Evidence {
		if fact.Evidence[i].Signal == "fetch" {
			fetch = &fact.Evidence[i]
		}
	}
	if fetch == nil || fetch.Uses != 5 || fetch.File != "d/a.js" || fetch.Line != 1 {
		t.Errorf("fetch evidence = %+v, want 5 uses first seen in d/a.js", fetch)
	}
	for _, file := range result.Files {
		for _, fact := range file.Purpose.Runs {
			for _, e := range fact.Evidence {
				if e.File != "" {
					t.Errorf("a file's own run evidence names a file: %+v", e)
				}
			}
		}
	}
}

func TestMergedRunFactsFromWordSignalsNeedTwoUsesAcrossFiles(t *testing.T) {
	// A word-only signal (an upload part number) is not stated from one use.
	one := Analyze([]Source{src("d/a.js", `var x={partNumber:1};window.name="a";`), src("d/b.js", `document.title="b";`)})
	if fact, ok := factOf(one.Overall.Runs, "Files"); ok {
		t.Errorf("one word use was stated: %+v", fact)
	}
	two := Analyze([]Source{src("d/a.js", `var x={partNumber:1};window.name="a";`), src("d/b.js", `var y={uploadId:2};document.title="b";`)})
	if fact, ok := factOf(two.Overall.Runs, "Files"); !ok || !strings.Contains(fact.Detail, "chunked uploads") {
		t.Errorf("two word uses across files were not stated: %+v", two.Overall.Runs)
	}
}

func TestRunContextOfTheSetKeepsPageFacts(t *testing.T) {
	cases := []struct {
		contexts []pageContext
		want     pageContext
	}{
		{[]pageContext{contextNode, contextPage}, contextPage},
		{[]pageContext{contextNoDocument, contextMixed}, contextMixed},
		{[]pageContext{contextNode, contextNoDocument}, contextNoDocument},
		{[]pageContext{contextNode, contextNode}, contextNode},
	}
	for _, c := range cases {
		if got := setContext(c.contexts); got != c.want {
			t.Errorf("setContext(%v) = %v, want %v", c.contexts, got, c.want)
		}
	}
	node := `const fs=require("fs");const a=process.argv.slice(2);fs.readFileSync(a[0]);indexedDB.open("db");`
	page := `document.cookie="a=1";window.name="x";`
	mixed := Analyze([]Source{src("d/tool.js", node), src("d/page.js", `import "./tool.js";`+page)})
	if fact, ok := factOf(mixed.Overall.Runs, "Storage"); !ok || !strings.Contains(fact.Detail, "cookies") || mixed.Overall.RunContext != "page" {
		t.Errorf("page facts lost beside a Node.js file: %s (%s)", factDetails(mixed.Overall.Runs), mixed.Overall.RunContext)
	}
	// A Node.js file never contributes page storage on its own.
	nodeOnly := Analyze([]Source{src("d/tool.js", node+`localStorage.setItem("a","b");`), src("d/tool2.js", node)})
	if !nodeOnly.Overall.Related {
		t.Fatalf("two Node.js files in one folder are not related: %s", nodeOnly.Overall.Basis)
	}
	if fact, ok := factOf(nodeOnly.Overall.Runs, "Storage"); ok && strings.Contains(fact.Detail, "localStorage") {
		t.Errorf("Node.js files stated page storage: %s", fact.Detail)
	}
}

func TestFilesLinkWhenOneNamesTheOther(t *testing.T) {
	result := Analyze([]Source{
		src("dist/entry.js", `import("./chunk-Ab12Cd34.js");import "./vendor-Zz9Yy8Xx.js";var m=["assets/tpl-Q1w2E3r4.js"];var t=`+"`./late-Aa11Bb22.js`"+`;var other="./missing-Ab12Cd34.js";`),
		src("dist/chunk-Ab12Cd34.js", `export const a=1;`),
		src("dist/vendor-Zz9Yy8Xx.js", `export const b=1;`),
		src("dist/tpl-Q1w2E3r4.js", `export const c=1;`),
		src("dist/late-Aa11Bb22.js", `import "./chunk-Ab12Cd34.js";import "./late-Aa11Bb22.js";`),
	})
	entry := result.Files[0]
	for _, want := range []string{"dist/chunk-Ab12Cd34.js", "dist/vendor-Zz9Yy8Xx.js", "dist/tpl-Q1w2E3r4.js", "dist/late-Aa11Bb22.js"} {
		if !hasLoad(entry, want) {
			t.Errorf("entry does not load %s: %v", want, entry.Loads)
		}
	}
	if len(entry.Loads) != 4 {
		t.Errorf("entry loads = %v; a name that is not an input must not link", entry.Loads)
	}
	if late := result.Files[4]; len(late.Loads) != 1 || late.Loads[0] != "dist/chunk-Ab12Cd34.js" {
		t.Errorf("late loads = %v; a file never links to itself", late.Loads)
	}
}

func TestAmbiguousBaseNamesLinkOnlyWhenThePathChoosesOne(t *testing.T) {
	result := Analyze([]Source{
		src("app.js", `import "./x/main.js";import "main.js";`),
		src("x/main.js", `export const a=1;`),
		src("y/main.js", `export const b=1;`),
		src("x/index.js", `import "./main.js";`),
		src("y/index.js", `import "../x/main.js";var s="y/main.js";`),
	})
	if got := result.Files[0].Loads; len(got) != 1 || got[0] != "x/main.js" {
		t.Errorf("app.js loads %v, want only x/main.js (the bare main.js is ambiguous)", got)
	}
	if got := result.Files[3].Loads; len(got) != 1 || got[0] != "x/main.js" {
		t.Errorf("x/index.js loads %v, want its sibling x/main.js", got)
	}
	if got := result.Files[4].Loads; len(got) != 2 || got[0] != "x/main.js" || got[1] != "y/main.js" {
		t.Errorf("y/index.js loads %v, want x/main.js by path and y/main.js by suffix", got)
	}
	bare := Analyze([]Source{src("app.js", `import "main.js";`), src("x/main.js", ``), src("y/main.js", ``)})
	if len(bare.Files[0].Loads) != 0 {
		t.Errorf("an ambiguous bare name linked: %v", bare.Files[0].Loads)
	}
}

func TestHTMLScriptSourceLinks(t *testing.T) {
	page := `<html><head><script type="module" src="/chunks/app-Ab12Cd34.js"></script></head></html>`
	result := Analyze([]Source{src("ext/page.html", page), src("ext/chunks/app-Ab12Cd34.js", `export const a=1;`)})
	if !hasLoad(result.Files[0], "ext/chunks/app-Ab12Cd34.js") {
		t.Errorf("page loads %v", result.Files[0].Loads)
	}
}

func TestEntryFileLoadsOthersAndIsNotLoaded(t *testing.T) {
	result := Analyze([]Source{
		src("dist/chunk-Ab12Cd34.js", `export const a=1;`),
		src("dist/entry.js", `import "./chunk-Ab12Cd34.js";import "./mid-Zz9Yy8Xx.js";`),
		src("dist/mid-Zz9Yy8Xx.js", `import "./chunk-Ab12Cd34.js";export const b=1;`),
	})
	o := result.Overall
	if !o.Related || len(o.Entries) != 1 || o.Entries[0].File != "dist/entry.js" {
		t.Fatalf("entries = %+v related=%v", o.Entries, o.Related)
	}
	if o.Entries[0].Loads != 2 || o.Entries[0].Reaches != 0 && o.Entries[0].Reaches != 2 {
		t.Errorf("entry = %+v", o.Entries[0])
	}
	if !result.Files[1].entry || result.Files[0].entry || result.Files[2].entry {
		t.Errorf("entry flags = %v %v %v", result.Files[0].entry, result.Files[1].entry, result.Files[2].entry)
	}
}

func TestEntryFileMountsAUIEvenWhenOthersLoadIt(t *testing.T) {
	// Lazy chunks import their application entry back, so nothing is unloaded.
	result := Analyze([]Source{
		src("dist/main-Ab12Cd34.js", "import \"./lazy-Zz9Yy8Xx.js\";import {useState} from \"react\";ReactDOM.createRoot(document.getElementById(`root`)).render(App);"),
		src("dist/lazy-Zz9Yy8Xx.js", `import "./main-Ab12Cd34.js";export const a=1;`),
	})
	o := result.Overall
	if len(o.Entries) != 1 || o.Entries[0].File != "dist/main-Ab12Cd34.js" || !strings.Contains(o.Entries[0].Why, "mounts") {
		t.Fatalf("entries = %+v", o.Entries)
	}
	// The file's own report is unchanged: a template-literal target is not stated.
	if _, ok := factOf(result.Files[0].Purpose.Runs, "Starts"); ok {
		t.Errorf("a file's own Starts fact changed: %s", factDetails(result.Files[0].Purpose.Runs))
	}
}

func TestSetWithoutLinksStillRollsUpFilesOfOneBuild(t *testing.T) {
	// Webpack chunk names never appear in string literals: no links, one folder,
	// one extension.
	a := `chrome.runtime.sendMessage({});chrome.tabs.query({});`
	b := `chrome.runtime.onMessage.addListener(f);chrome.storage.local.get("k");`
	result := Analyze([]Source{src("ext/a.js", a), src("ext/b.js", b)})
	o := result.Overall
	if !o.Related || o.Links != 0 || !strings.Contains(o.Basis, "share one host (Browser extension)") {
		t.Fatalf("related=%v basis=%q", o.Related, o.Basis)
	}
	if o.Platform == nil || o.Platform.Name != "Browser extension" || !strings.HasPrefix(o.Is, "Browser extension") {
		t.Errorf("platform = %+v, is = %q", o.Platform, o.Is)
	}
	fact, ok := factOf(o.Runs, "Extension APIs")
	if !ok || !strings.Contains(fact.Detail, "tabs") || !strings.Contains(fact.Detail, "storage") {
		t.Errorf("extension APIs were not merged across files: %s", factDetails(o.Runs))
	}
}

func TestUnrelatedFilesReadAsSeparate(t *testing.T) {
	plugin := func(extra string) string {
		return `const obsidian=require("obsidian");module.exports=class P extends obsidian.Plugin{};` + extra
	}
	var sources []Source
	for _, dir := range []string{"plugins/one", "plugins/two", "plugins/three"} {
		sources = append(sources, src(dir+"/main.js", plugin(`var n=["sendThing","editThing"];`)))
	}
	o := Analyze(sources).Overall
	if o.Related || len(o.Features) != 0 || len(o.Runs) != 0 || len(o.Entries) != 0 {
		t.Fatalf("unrelated files were merged: %+v", o)
	}
	if !strings.Contains(o.Is, "3 separate files") || !strings.Contains(o.Is, "3 Obsidian plugins") {
		t.Errorf("is = %q", o.Is)
	}
}

func TestIdentityNamesFrameworksAndBuild(t *testing.T) {
	main := "import \"./c-Ab12Cd34.js\";var __vite__mapDeps=[\"./c-Ab12Cd34.js\"];import {useState,useEffect} from \"react\";useState(1);useEffect(f);ReactDOM.createRoot(document.getElementById(\"root\")).render(App);"
	result := Analyze([]Source{src("dist/main.js", main), src("dist/c-Ab12Cd34.js", `export const a=1;`)})
	o := result.Overall
	if o.Is != "Browser app using React, bundled by Vite." {
		t.Errorf("is = %q", o.Is)
	}
	if len(o.Composition) == 0 || o.Files != 2 {
		t.Errorf("composition = %+v", o.Composition)
	}
}

func overallFixture(files int) Result {
	result := Result{Sources: files}
	overall := &Overall{Files: files, Related: true, Basis: "x", Is: "Browser app.", Composition: []KindCount{{Kind: "library", Files: files}}}
	for i := 0; i < files; i++ {
		file := File{Name: fmt.Sprintf("/work/dist/assets/f%02d.js", i), Bytes: 1000 + i, Format: "ES module", kind: "ES module"}
		file.Purpose = &Purpose{Is: "Browser ES module.", Features: []Feature{{Area: fmt.Sprintf("area%02d", i)}}}
		result.Files = append(result.Files, file)
		result.Bytes += file.Bytes
	}
	result.Overall = overall
	return result
}

func TestSmallSetShowsRollUpThenEveryFileInFull(t *testing.T) {
	result := Analyze([]Source{src("d/a.js", billingApp), src("d/b.js", `fetch("/api/v1/other");document.title="x";`)})
	var out bytes.Buffer
	if err := WriteText(&out, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	rollup := strings.Index(text, "\n## All 2 files\n")
	first, second := strings.Index(text, "\n## d/a.js\n"), strings.Index(text, "\n## d/b.js\n")
	if rollup < 0 || first < rollup || second < first {
		t.Fatalf("order is wrong (%d, %d, %d):\n%s", rollup, first, second, text)
	}
	if strings.Contains(text, "\n## Files\n") || !strings.HasSuffix(text, "Use --verbose for structure, entry paths, and call flow.\n") {
		t.Errorf("small set used the index or lost its footer:\n%s", text)
	}
	for _, heading := range []string{"### What it is", "### What it does"} {
		if strings.Index(text[rollup:first], heading) < 0 {
			t.Errorf("roll-up lacks %q:\n%s", heading, text[rollup:first])
		}
	}
}

func TestCompactIndexCapsListsEntriesFirstAndGroupsTheRest(t *testing.T) {
	result := overallFixture(45)
	result.Files[3].entry = true
	for i := 40; i < 45; i++ {
		result.Files[i].Purpose = &Purpose{Is: "Library."}
		result.Files[i].kind = "library"
	}
	result.Files[3].Purpose.Is = "Browser app using React."
	var out bytes.Buffer
	if err := WriteText(&out, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	index := text[strings.Index(text, "\n## Files\n"):]
	var listed []string
	for _, line := range strings.Split(index, "\n") {
		if strings.HasPrefix(line, "- f") {
			listed = append(listed, line)
		}
	}
	if len(listed) != maxIndexLines {
		t.Errorf("listed %d files, want %d", len(listed), maxIndexLines)
	}
	if !strings.HasPrefix(listed[0], "- f03.js · ") || !strings.HasSuffix(listed[0], "entry file") {
		t.Errorf("entry file is not first: %s", listed[0])
	}
	// After the entry file, larger files come first.
	if !strings.HasPrefix(listed[1], "- f39.js") {
		t.Errorf("second line = %s", listed[1])
	}
	for _, want := range []string{"- +10 more files with something to report", "Nothing to report in 5 files:", "- 5 libraries · ", "Folder: /work/dist/assets/ · file names below are relative to it", "Run --overview on a single file, or add --verbose, for the full section of each file.\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "\n## /work/dist/assets/f00.js") || strings.Contains(text, "Use --verbose for structure") {
		t.Errorf("index printed full sections or the single-file footer:\n%s", text)
	}
}

func TestCompactIndexSaysOnceThatSourceMapsWereNotApplied(t *testing.T) {
	result := overallFixture(8)
	for i := range result.Files {
		result.Files[i].SourceMap = true
		result.Files[i].mapHint = "Source map not applied. one.js.map was not found in this file's folder or is not a valid source map; put it there and rerun to restore names."
	}
	var out bytes.Buffer
	if err := WriteText(&out, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.Contains(text, "Source map not applied.") || strings.Count(text, "8 files reference source maps that were not applied.") != 1 {
		t.Errorf("source map hint:\n%s", text)
	}
	if !strings.Contains(text, "reads a map only from the file's own folder") {
		t.Errorf("local-file advice missing:\n%s", text)
	}
	for i := range result.Files {
		result.Files[i].Name = fmt.Sprintf("https://example.test/a/%d.js", i)
	}
	out.Reset()
	if err := WriteText(&out, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Overview does not fetch maps; run jshound -s URL --output ./source") {
		t.Errorf("URL advice missing:\n%s", out.String())
	}
}

func TestVerboseWithSeveralSourcesKeepsEveryFileInFull(t *testing.T) {
	var sources []Source
	for i := 0; i < 7; i++ {
		sources = append(sources, src(fmt.Sprintf("d/f%d.js", i), fmt.Sprintf(`function start%d(){fetch("/api/v1/x");}start%d();`, i, i)))
	}
	var out bytes.Buffer
	if err := WriteText(&out, Analyze(sources), TextOptions{Verbose: true}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "\n## All 7 files\n") || strings.Contains(text, "\n## Files\n") {
		t.Errorf("verbose layout:\n%s", text)
	}
	for i := 0; i < 7; i++ {
		if !strings.Contains(text, fmt.Sprintf("\n## d/f%d.js\n", i)) || !strings.Contains(text, fmt.Sprintf("start%d()", i)) {
			t.Errorf("file %d is not in full", i)
		}
	}
}

func TestTerminalOutputColorsTheRollUp(t *testing.T) {
	result := overallFixture(7)
	var out bytes.Buffer
	if err := WriteText(&out, result, TextOptions{Terminal: true, Color: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "\x1b[1;97m## All 7 files") || !strings.Contains(out.String(), "\x1b[1;36m### What it is") {
		t.Errorf("roll-up headings are not styled:\n%q", out.String())
	}
}

func TestJSONHasOverallAndJSONLHasItsOwnRecord(t *testing.T) {
	result := Analyze([]Source{src("d/a.js", billingApp), src("d/b.js", `import "./a.js";fetch("/x");`)})
	result.Version = "test"
	var out bytes.Buffer
	if err := WriteJSON(&out, result); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Overall *Overall `json:"overall"`
		Files   []File   `json:"files"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Overall == nil || decoded.Overall.Files != 2 || len(decoded.Files) != 2 {
		t.Fatalf("decoded = %+v", decoded)
	}
	if got := decoded.Files[1].Loads; len(got) != 1 || got[0] != "d/a.js" {
		t.Errorf("loads = %v", got)
	}
	out.Reset()
	if err := WriteJSONL(&out, result); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("jsonl has %d lines", len(lines))
	}
	for _, line := range lines[:2] {
		if !strings.HasPrefix(line, `{"kind":"overview","version":"test",`) {
			t.Errorf("per-file record changed: %.80s", line)
		}
	}
	var record struct {
		Kind    string `json:"kind"`
		Version string `json:"version"`
		Sources int    `json:"sources"`
		Is      string `json:"is"`
		Files   int    `json:"files"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &record); err != nil {
		t.Fatal(err)
	}
	if record.Kind != "overall" || record.Version != "test" || record.Sources != 2 || record.Files != 2 || record.Is == "" {
		t.Errorf("overall record = %+v", record)
	}
}

func TestInsightAPIStaysHiddenInTheRollUp(t *testing.T) {
	result := Analyze([]Source{src("d/a.js", billingApp), src("d/b.js", billingApp)})
	var out bytes.Buffer
	if err := WriteJSON(&out, result); err != nil {
		t.Fatal(err)
	}
	var generic map[string]any
	if err := json.Unmarshal(out.Bytes(), &generic); err != nil {
		t.Fatal(err)
	}
	for _, file := range generic["files"].([]any) {
		if insights, ok := file.(map[string]any)["insights"].(map[string]any); ok {
			if _, exposed := insights["API"]; exposed {
				t.Errorf("insights expose API: %v", insights)
			}
		}
	}
}
