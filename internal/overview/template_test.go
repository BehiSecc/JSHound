package overview

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func tokenKinds(source string) string {
	var out []string
	for _, t := range lex(source) {
		out = append(out, fmt.Sprintf("%c", t.kind))
	}
	return strings.Join(out, "")
}

func TestPlainTemplatesAreStringsAndOthersStayTemplates(t *testing.T) {
	// Kinds are checked one literal at a time.
	check := func(source string, want byte) {
		t.Helper()
		for _, tok := range lex(source) {
			if tok.text[0] == '`' {
				if tok.kind != want {
					t.Errorf("%q: kind %c, want %c", source, tok.kind, want)
				}
				return
			}
		}
		t.Errorf("%q: no template token", source)
	}
	check("x=`/api/users`;", 's')
	check("x=`multi\nline`;", 's')
	check("x=`a${b}c`;", 't')
	check("x=html`<p>`;", 't')
	check("x=f(y)`a`;", 't')
	check("x=a.b`a`;", 't')
	check("x=a[0]`a`;", 't')
	check("function f(){return`a`}", 's')
	check("switch(a){default:`a`}", 's')
	check("x=`unterminated", 't')
	if got := stringValue("`/api/users`"); got != "/api/users" {
		t.Errorf("stringValue of a template = %q", got)
	}
	// After a plain template a slash divides, as after a quoted string.
	if a, b := tokenKinds("x=`a`/2/3;y=1;"), tokenKinds(`x="a"/2/3;y=1;`); a != b {
		t.Errorf("division after a template lexes differently: %s vs %s", a, b)
	}
}

func TestPlainTemplateLiteralsFeedEveryStringReader(t *testing.T) {
	code := "ReactDOM.createRoot(document.getElementById(`root`)).render(App);\n" +
		"var u=[`/api/v1/invoices`,`/api/v1/invoices/:id`];\n" +
		"var n=[`invoiceTable`,`invoiceList`,`sendInvoice`,`payInvoice`,`invoiceReport`,`invoiceDetails`];\n" +
		"var m=[`Your invoice was sent to the customer.`,`The invoice could not be saved.`];\n" +
		"h.set(`X-Auth-Token`,t);\nvar o=`Sign in with a passkey`;fetch(`/api/v1/invoices`);\n"
	file := Analyze([]Source{src("a.js", code)}).Files[0]
	quoted := Analyze([]Source{src("a.js", strings.NewReplacer("`", `"`).Replace(code))}).Files[0]
	if file.Purpose == nil || fmt.Sprint(file.Purpose.Runs) != fmt.Sprint(quoted.Purpose.Runs) || fmt.Sprint(file.Purpose.Features) != fmt.Sprint(quoted.Purpose.Features) {
		t.Errorf("template and quoted strings differ:\n%s\n%s", factDetails(file.Purpose.Runs), factDetails(quoted.Purpose.Runs))
	}
	if fact, ok := factOf(file.Purpose.Runs, "Starts"); !ok || fact.Detail != "mounts its UI into #root" {
		t.Errorf("mount in a template literal: %s", factDetails(file.Purpose.Runs))
	}
	if fact, ok := factOf(file.Purpose.Runs, "Server calls"); !ok || !strings.Contains(fact.Detail, "X-Auth-Token") {
		t.Errorf("header in a template literal: %s", factDetails(file.Purpose.Runs))
	}
	if invoice, ok := featureByArea(file.Purpose, "invoice"); !ok || invoice.APICalls != 2 {
		t.Errorf("names and paths in template literals: %+v", file.Purpose.Features)
	}
	// An interpolated template is still not read as a string.
	interpolated := Analyze([]Source{src("b.js", "var u=`/api/v1/${kind}/invoices`;")}).Files[0]
	if interpolated.Purpose != nil && interpolated.Purpose.Evidence != nil && interpolated.Purpose.Evidence.APICalls > 1 {
		t.Errorf("interpolated template counted as a plain path: %+v", interpolated.Purpose.Evidence)
	}
}

func TestPlainTemplateLiteralsLinkFiles(t *testing.T) {
	result := Analyze([]Source{src("d/a.js", "import(`./b-Ab12Cd34.js`);"), src("d/b-Ab12Cd34.js", "export const x=1;")})
	if len(result.Files[0].Loads) != 1 {
		t.Errorf("loads = %v", result.Files[0].Loads)
	}
}

func TestPathsInPlainTemplatesAreCountedAsEvidence(t *testing.T) {
	// The scanner reads quoted paths only; the overview hands it plain
	// templates as quoted strings.
	quoted := collectEvidence(&File{}, lex(`var u=["/api/v1/widgets","/gadgets/:id"];`), nil)
	templates := collectEvidence(&File{}, lex("var u=[`/api/v1/widgets`,`/gadgets/:id`];"), nil)
	if len(quoted.items) != 2 || len(templates.items) != len(quoted.items) {
		t.Errorf("quoted %+v, templates %+v", quoted.items, templates.items)
	}
	if kept := collectEvidence(&File{}, lex("var u=`/api/v1/${kind}`;"), nil); len(kept.items) != 0 {
		t.Errorf("an interpolated template counted: %+v", kept.items)
	}
}

func TestUnquotedAuthorizationKeyIsAHeader(t *testing.T) {
	header := func(code string) (RunFact, bool) {
		return factOf(analyzePurpose(t, "h.js", code).Runs, "Server calls")
	}
	fact, ok := header(`fetch("/a",{headers:{Authorization:"Bearer "+t}});`)
	if !ok || !strings.Contains(fact.Detail, "sends Authorization header") {
		t.Fatalf("unquoted key: %+v", fact)
	}
	// Spellings of one header are one header, named as first written.
	fact, _ = header(`fetch("/a",{headers:{authorization:a,"Authorization":b}});fetch("/b",{headers:{Authorization:c}});`)
	var evidence RunEvidence
	for _, e := range fact.Evidence {
		if strings.HasPrefix(e.Signal, "header ") {
			if evidence.Signal != "" {
				t.Errorf("one header cited twice: %+v", fact.Evidence)
			}
			evidence = e
		}
	}
	if evidence.Signal != "header authorization" || evidence.Uses != 3 {
		t.Errorf("header evidence = %+v", evidence)
	}
	for _, code := range []string{`fetch("/a");var a=req.Authorization;`, `fetch("/a");var a={Authorization};`, `fetch("/a");x=cond?Authorization:y;`} {
		if fact, _ := header(code); strings.Contains(fact.Detail, "Authorization") {
			t.Errorf("%s: not a header key: %s", code, fact.Detail)
		}
	}
}

func TestPlaceholderInsideAPathSegmentIsNotAWord(t *testing.T) {
	for _, path := range []string{"/api/v1/files/upload_content_bytes{dynamic}", "/api/v1/files/upload_content_bytes${dynamic}"} {
		ev := &evidence{claimed: map[string]struct{}{}, byNoun: map[string][]int{}}
		ev.addPath(path, pathAPI)
		if len(ev.items) != 1 {
			t.Fatalf("%s: items = %+v", path, ev.items)
		}
		for _, noun := range ev.items[0].nouns {
			if noun == "dynamic" {
				t.Errorf("%s: placeholder became a noun: %v", path, ev.items[0].nouns)
			}
		}
		if got := strings.Join(ev.items[0].verbs, " "); !strings.Contains(got, "upload") {
			t.Errorf("%s: real words lost: %v", path, ev.items[0].verbs)
		}
	}
}

func TestSeparateFilesGetAShortStatementNotARollUp(t *testing.T) {
	plugin := `const obsidian=require("obsidian");module.exports=class P extends obsidian.Plugin{};`
	var sources []Source
	for _, name := range []string{"one", "two", "three"} {
		sources = append(sources, src("plugins/"+name+"/main.js", plugin))
	}
	var out bytes.Buffer
	if err := WriteText(&out, Analyze(sources), TextOptions{}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "\n3 separate files, not one application (3 Obsidian plugins).\n") {
		t.Errorf("statement missing:\n%s", text)
	}
	head := text[:strings.Index(text, "\n## plugins/")]
	if strings.Contains(head, "## All") || strings.Contains(head, "### ") || strings.Count(head, "\n") > 6 {
		t.Errorf("separate files got a roll-up section:\n%s", head)
	}
	for _, name := range []string{"one", "two", "three"} {
		if !strings.Contains(text, "\n## plugins/"+name+"/main.js\n") {
			t.Errorf("file %s has no section", name)
		}
	}

	// Many separate files: the statement, then the index with its folder line.
	sources = nil
	for i := 0; i < 7; i++ {
		sources = append(sources, src(fmt.Sprintf("plugins/p%d/main.js", i), plugin+fmt.Sprintf("fetch(\"/x%d\");", i)))
	}
	out.Reset()
	if err := WriteText(&out, Analyze(sources), TextOptions{}); err != nil {
		t.Fatal(err)
	}
	text = out.String()
	for _, want := range []string{"7 separate files, not one application (7 Obsidian plugins).", "\n## Files\n", "Folder: plugins/ · file names below are relative to it", "p0/main.js"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "## All") || strings.Contains(text, "not merged") {
		t.Errorf("separate index has a roll-up:\n%s", text)
	}
}

func TestEntryLineStatesDirectLoadsNextToTheReason(t *testing.T) {
	result := Analyze([]Source{
		src("dist/entry.js", `import "./a-Ab12Cd34.js";import "./b-Zz9Yy8Xx.js";`),
		src("dist/a-Ab12Cd34.js", `import "./b-Zz9Yy8Xx.js";export const a=1;`),
		src("dist/b-Zz9Yy8Xx.js", `export const b=1;`),
	})
	var out bytes.Buffer
	if err := WriteText(&out, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	if want := "- Entry file: entry.js (loads 2 files and no other file loads it)\n"; !strings.Contains(out.String(), want) || strings.Contains(out.String(), "reaches") {
		t.Errorf("entry line:\n%s", out.String())
	}
	mounting := Analyze([]Source{
		src("dist/main.js", "import \"./a-Ab12Cd34.js\";ReactDOM.createRoot(document.getElementById(`root`)).render(App);"),
		src("dist/a-Ab12Cd34.js", `import "./main.js";`),
	})
	out.Reset()
	if err := WriteText(&out, mounting, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	if want := "- Entry file: main.js (mounts a UI into #root; loads 1 file)\n"; !strings.Contains(out.String(), want) {
		t.Errorf("mounting entry line:\n%s", out.String())
	}
}

func TestIndexListsFilesSharingTheApplicationsAreasBeforeOthers(t *testing.T) {
	result := overallFixture(8)
	result.Overall.Features = []Feature{{Area: "thread"}, {Area: "project"}}
	result.Files[1].entry = true
	// f02 is small but names an area of the application; f07 is the largest
	// and names only vendor areas.
	result.Files[2].Purpose.Features = []Feature{{Area: "thread composer"}}
	var out bytes.Buffer
	if err := WriteText(&out, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	var order []string
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "- f") {
			order = append(order, line[2:5])
		}
	}
	if got := strings.Join(order, " "); got != "f01 f02 f07 f06 f05 f04 f03 f00" {
		t.Errorf("order = %s", got)
	}
}
