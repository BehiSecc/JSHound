package overview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func apiItems(group string, n int) []evidenceItem {
	var items []evidenceItem
	for i := 0; i < n; i++ {
		items = append(items, evidenceItem{label: fmt.Sprintf("Method%d", i), group: group})
	}
	return items
}

func pathItems(root string, n int) []evidenceItem {
	var items []evidenceItem
	for i := 0; i < n; i++ {
		items = append(items, evidenceItem{label: fmt.Sprintf("/%s/m%d", root, i)})
	}
	return items
}

func TestAPILineGroupsByServiceOrFirstSegment(t *testing.T) {
	join := func(parts ...[]evidenceItem) *evidence {
		e := &evidence{}
		for _, items := range parts {
			e.items = append(e.items, items...)
		}
		return e
	}
	cases := map[string]struct {
		ev   *evidence
		want string
	}{
		"services": {join(apiItems("OrderService", 5), apiItems("UserService", 2)), "7 calls in 2 services (OrderService 5, UserService 2)"},
		"one call": {join(apiItems("OrderService", 1)), "1 call in 1 service (OrderService 1)"},
		"paths":    {join(pathItems("email", 3), pathItems("passport", 5)), "8 calls (/passport 5, /email 3)"},
		"prefix":   {&evidence{items: []evidenceItem{{label: "/api/v1/orders/list"}, {label: "/api/v1/orders/get"}, {label: "/api/users"}}}, "3 calls (/orders 2, /users 1)"},
		"mixed":    {join(apiItems("OrderService", 4), pathItems("health", 1)), "5 calls in 2 groups (OrderService 4, /health 1)"},
		"more": {
			join(apiItems("A", 9), apiItems("B", 8), apiItems("C", 7), apiItems("D", 1), apiItems("E", 1)),
			"26 calls in 5 services (A 9, B 8, C 7, +2 more)",
		},
		"more paths": {join(pathItems("a", 4), pathItems("b", 3), pathItems("c", 2), pathItems("d", 1)), "10 calls (/a 4, /b 3, /c 2, +1 more)"},
	}
	for name, c := range cases {
		if got := c.ev.apiDetail(); got != c.want {
			t.Errorf("%s: apiDetail = %q, want %q", name, got, c.want)
		}
	}
	if (&evidence{items: []evidenceItem{{label: "/a", screen: true}}}).apiDetail() != "" || (*evidence)(nil).apiDetail() != "" {
		t.Error("screens or no evidence produced an API line")
	}
}

func TestScreensLineListsShortestFirstThenCountsTheRest(t *testing.T) {
	e := &evidence{}
	for _, path := range []string{"/orders/:id/edit", "/orders", "/", "/users", "/orders/new"} {
		e.items = append(e.items, evidenceItem{label: path, screen: true})
	}
	e.items = append(e.items, evidenceItem{label: "/api/x"})
	if got, want := e.screensDetail(), "5 routes (/, /users, /orders, +2 more)"; got != want {
		t.Errorf("screensDetail = %q, want %q", got, want)
	}
	one := &evidence{items: []evidenceItem{{label: "/home", screen: true}}}
	if got, want := one.screensDetail(), "1 route (/home)"; got != want {
		t.Errorf("screensDetail = %q, want %q", got, want)
	}
}

func TestDerivedRunsFollowTheAspectOrder(t *testing.T) {
	facts := []RunFact{{Aspect: "Navigation"}, {Aspect: "Server calls"}, {Aspect: "Storage"}}
	got := withDerivedRuns(facts, RunFact{Aspect: "Starts"}, RunFact{Aspect: "Screens"}, RunFact{Aspect: "API"})
	var order []string
	for _, fact := range got {
		order = append(order, fact.Aspect)
	}
	if want := "Starts Navigation Screens Server calls API Storage"; strings.Join(order, " ") != want {
		t.Errorf("order = %v, want %s", order, want)
	}
	only := withDerivedRuns([]RunFact{{Aspect: "Sign-in"}}, RunFact{Aspect: "API"})
	if only[0].Aspect != "API" {
		t.Errorf("API line should come before Sign-in without Server calls: %v", only)
	}
}

func TestBehaviorShowsAPIAndScreensLines(t *testing.T) {
	p := analyzePurpose(t, "billing.js", billingApp)
	if fact, ok := factOf(p.Runs, "API"); !ok || fact.Detail != "4 calls in 2 services (InvoiceService 3, CustomerService 1)" || len(fact.Evidence) != 0 {
		t.Errorf("API = %+v", fact)
	}
	if fact, ok := factOf(p.Runs, "Screens"); !ok || !strings.HasPrefix(fact.Detail, "6 routes (/invoices, ") {
		t.Errorf("Screens = %+v", fact)
	}
	var aspects []string
	for _, fact := range p.Runs {
		aspects = append(aspects, fact.Aspect)
	}
	if got := strings.Join(aspects, " "); got != "Starts Screens API" {
		t.Errorf("aspects = %s", got)
	}
}

func TestStartsLineComesFromTheLoadFlow(t *testing.T) {
	cases := map[string]struct {
		name, code, want string
	}{
		"readable":  {"app.js", "function bootServer(){}function loadSettings(){}\nbootServer();loadSettings();document.addEventListener('click', bootServer);", "calls bootServer() and loadSettings() at load; handles click events"},
		"events":    {"app.js", "document.addEventListener('message', function(){});", "handles message events"},
		"mount":     {"app.js", `function boot(){}boot();ReactDOM.createRoot(document.getElementById("root")).render(App);`, "mounts its UI into #root"},
		"library":   {"lib.js", "/*! Widget v1.2.3 | MIT */\nfunction boot(){}boot();", ""},
		"minified":  {"app.min.js", "function a(){}function bootServer(){}\na();bootServer();\n" + minifiedPad(), ""},
		"returned":  {"app.js", "function make(){return {run(){}}}\nmake().run();", ""},
		"no starts": {"app.js", "function helper(){return 1}", ""},
	}
	for name, c := range cases {
		p := analyzePurpose(t, c.name, c.code)
		fact, ok := factOf(p.Runs, "Starts")
		if c.want == "" && ok {
			t.Errorf("%s: unexpected Starts %q", name, fact.Detail)
		} else if c.want != "" && (!ok || fact.Detail != c.want) {
			t.Errorf("%s: Starts = %q (%v), want %q", name, fact.Detail, ok, c.want)
		}
	}
}

func TestStartsLineIsNotRepeatedByTheFlowSentenceInDefaultOutput(t *testing.T) {
	result := Analyze([]Source{{Name: "app.js", Data: []byte("function bootApp(){}\nbootApp();")}})
	text := renderText(t, result, TextOptions{})
	for _, want := range []string{"### Features\n\nNo feature areas were identified.\n", "### Behavior\n\n- Starts: calls bootApp() at load\n"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "Starts with") || strings.Contains(text, "Contains ") {
		t.Errorf("flow or insight sentence in the default report:\n%s", text)
	}
}

func TestLibraryHasNoFeaturesSection(t *testing.T) {
	text := renderText(t, Analyze([]Source{{Name: "lib.js", Data: []byte("/*! Widget v1.2.3 | MIT */\nfunction boot(){}boot();")}}), TextOptions{})
	if strings.Contains(text, "### Features") || strings.Contains(text, "### Behavior") {
		t.Errorf("library has sections it does not need:\n%s", text)
	}
}

func TestVerboseHasNoHintAndDefaultHasItOnce(t *testing.T) {
	result := Analyze([]Source{{Name: "billing.js", Data: []byte(billingApp)}})
	plain, verbose := renderText(t, result, TextOptions{}), renderText(t, result, TextOptions{Verbose: true})
	if strings.Count(plain, overviewHint) != 1 || !strings.HasSuffix(plain, "\n"+overviewHint+"\n") {
		t.Errorf("default hint:\n%s", plain)
	}
	if strings.Contains(verbose, "Use --verbose") || strings.Contains(verbose, "### What") || !strings.Contains(verbose, "### Behavior") {
		t.Errorf("verbose report:\n%s", verbose)
	}
}

func TestJSONRunsGainTheDerivedEntriesAndKeepEveryKey(t *testing.T) {
	var out bytes.Buffer
	if err := WriteJSON(&out, Result{Sources: 1, Files: Analyze([]Source{{Name: "billing.js", Data: []byte(billingApp)}}).Files}); err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Files []struct {
			Purpose map[string]json.RawMessage `json:"purpose"`
		} `json:"files"`
	}
	if err := json.Unmarshal(out.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	purpose := decoded.Files[0].Purpose
	for _, key := range []string{"is", "features", "evidence", "runs"} {
		if _, ok := purpose[key]; !ok {
			t.Errorf("purpose lost %q: %s", key, out.String())
		}
	}
	var runs []RunFact
	if err := json.Unmarshal(purpose["runs"], &runs); err != nil {
		t.Fatal(err)
	}
	if _, ok := factOf(runs, "API"); !ok {
		t.Errorf("runs lack API: %+v", runs)
	}
	if _, ok := factOf(runs, "Screens"); !ok {
		t.Errorf("runs lack Screens: %+v", runs)
	}
}

func TestSeveralFilesSummaryShowsAPIAndScreens(t *testing.T) {
	result := Analyze([]Source{
		src("d/a.js", `import "./b.js";`+billingApp),
		src("d/b.js", `var routes=["/reports","reports/:id"];var u=["/acme.reports.v1.ReportService/ListReports"];`),
	})
	o := result.Overall
	if o == nil || !o.Related {
		t.Fatalf("overall = %+v", o)
	}
	if fact, ok := factOf(o.Runs, "API"); !ok || fact.Detail != "5 calls in 3 services (InvoiceService 3, CustomerService 1, ReportService 1)" {
		t.Errorf("API = %+v", fact)
	}
	if fact, ok := factOf(o.Runs, "Screens"); !ok || !strings.HasPrefix(fact.Detail, "8 routes (") {
		t.Errorf("Screens = %+v", fact)
	}
	text := renderText(t, result, TextOptions{})
	summary := text[strings.Index(text, "## All 2 files"):strings.Index(text, "\n## d/a.js")]
	for _, want := range []string{"### Type", "### Features", "### Behavior", "- Screens: 8 routes (", "- API: 5 calls in 3 services ("} {
		if !strings.Contains(summary, want) {
			t.Errorf("summary lacks %q:\n%s", want, summary)
		}
	}
}

func TestIndexNamesBehaviorForFilesWithoutFeatures(t *testing.T) {
	file := File{Name: "/work/a.js", Bytes: 10, Purpose: &Purpose{Is: "Browser script.", Runs: []RunFact{{Aspect: "Storage", Detail: "localStorage"}, {Aspect: "API", Detail: "1 call (/x 1)"}}}}
	if got := indexLine(file, shortNames{}); !strings.HasSuffix(got, "behavior: storage, api") {
		t.Errorf("index line = %q", got)
	}
}
