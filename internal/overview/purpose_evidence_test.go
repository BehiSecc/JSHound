package overview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// billingApp is an application chunk whose areas show up in RPC paths, router
// paths, readable names, and user-facing messages.
const billingApp = `ReactDOM.createRoot(document.getElementById("root")).render(App);
var rpc=["/acme.billing.v1.InvoiceService/CreateInvoice","/acme.billing.v1.InvoiceService/ListInvoices","/acme.billing.v1.InvoiceService/DeleteInvoice","/acme.billing.v1.CustomerService/SearchCustomers"];
var routes=["/invoices","invoices/:id","invoices/:id/edit","/invoices/new","/vouchers/new","vouchers/:id"];
var names=["invoiceTable","invoiceList","sendInvoice","invoice-details","invoice-preview","payInvoice","customerCard","customer-list","searchCustomer"];
var text=["Your invoice was sent to the customer.","The invoice could not be saved."];`

func billingFile(t *testing.T) File {
	t.Helper()
	return Analyze([]Source{{Name: "billing.js", Data: []byte(billingApp)}}).Files[0]
}

func featureByArea(p *Purpose, area string) (Feature, bool) {
	for _, feature := range p.Features {
		if feature.Area == area {
			return feature, true
		}
	}
	return Feature{}, false
}

func TestEvidenceAttachesToFeatureAreasWithActions(t *testing.T) {
	file := billingFile(t)
	invoice, ok := featureByArea(file.Purpose, "invoice")
	if !ok {
		t.Fatalf("invoice area missing: %+v", file.Purpose.Features)
	}
	if invoice.APICalls != 3 || !reflect.DeepEqual(invoice.APIExamples, []string{"InvoiceService"}) {
		t.Errorf("invoice API evidence = %d %v", invoice.APICalls, invoice.APIExamples)
	}
	if invoice.Screens != 4 || !reflect.DeepEqual(invoice.ScreenExamples, []string{"/invoices", "invoices/:id", "/invoices/new"}) {
		t.Errorf("invoice screens = %d %v", invoice.Screens, invoice.ScreenExamples)
	}
	actions := strings.Join(invoice.Actions, ",")
	for _, action := range []string{"create", "delete", "edit"} {
		if !strings.Contains(actions, action) {
			t.Errorf("invoice actions %v missing %q", invoice.Actions, action)
		}
	}
	if customer, ok := featureByArea(file.Purpose, "customer"); ok {
		if customer.APICalls != 1 || customer.Screens != 0 || !strings.Contains(strings.Join(customer.Actions, ","), "search") {
			t.Errorf("customer = %+v", customer)
		}
	}
	if totals := file.Purpose.Evidence; totals == nil || totals.APICalls != 4 || totals.Screens != 6 {
		t.Errorf("totals = %+v", totals)
	}
}

func TestClassifyPathKeepsOnlyAPIAndRouteEvidence(t *testing.T) {
	cases := map[string]pathKind{
		"/acme.v1.OrderService/ListOrders": pathAPI,
		"/OrderService.ListOrders":         pathAPI,
		"/api/orders":                      pathAPI,
		"/v2/orders":                       pathAPI,
		"orders/{order_id}/lines":          pathAPI,
		"orders/:id/edit":                  pathScreen,
		"/orders":                          pathUnknown,
		"/Settings/Profile":                pathUnknown,
		"./OrderTable-AbC123.js":           pathIgnored,
		"../shared/orders":                 pathIgnored,
		"/assets/logo.svg":                 pathIgnored,
		"/static/app.min.js":               pathIgnored,
		"/opt/google/chrome/chrome":        pathIgnored,
		"/etc/os-release":                  pathIgnored,
		"/error/i":                         pathIgnored,
		"/":                                pathIgnored,
		"/orders/new?draft=1":              pathUnknown,
		"/api/orders/":                     pathAPI,
		"https://example.com/api/orders":   pathIgnored,
		"/this has spaces":                 pathIgnored,
	}
	for path, want := range cases {
		if got := classifyPath(path); got != want {
			t.Errorf("classifyPath(%q) = %d, want %d", path, got, want)
		}
	}
}

func TestEvidenceDeduplicatesSources(t *testing.T) {
	tokens := lex(`var a=["/orders/:id","orders/:id/","/api/orders"];`)
	file := &File{Insights: &Insights{
		Routes: []InsightRoute{{Path: "/orders/:id", Kind: "declared route"}},
		API:    []InsightAPI{{Path: "/api/orders", Method: "GET"}, {Path: "https://example.com/api/orders", Method: "GET"}},
	}}
	ev := collectEvidence(file, tokens, nil)
	if total := ev.totals(); total == nil || total.APICalls != 1 || total.Screens != 1 {
		t.Errorf("totals = %+v, items = %+v", total, ev.items)
	}
}

func TestEvidenceSkipsBundledPackageStrings(t *testing.T) {
	tokens := lex(`var a=["/api/orders"];`)
	if ev := collectEvidence(&File{}, tokens, func(int) bool { return true }); ev.totals() != nil {
		t.Errorf("package strings counted: %+v", ev.items)
	}
}

func TestPathIsNotCountedAsNameAndEvidence(t *testing.T) {
	tokens := lex(`var r=["/vouchers/new","vouchers/:id"];`)
	ev := collectEvidence(&File{}, tokens, nil)
	with := collectVocabulary(&File{}, tokens, nil, ev)
	without := collectVocabulary(&File{}, tokens, nil, nil)
	if without.nouns["voucher"] != 1 {
		t.Fatalf("path should be a name without evidence: %v", without.nouns)
	}
	if with.nouns["voucher"] != 0 {
		t.Errorf("path counted as name and evidence: %v", with.nouns)
	}
	if support := ev.support("voucher"); len(support) != 2 {
		t.Errorf("voucher support = %+v", support)
	}
	// A path that is not evidence still reads as a name.
	tokens = lex(`var r=["/vouchers/new"];`)
	ev = collectEvidence(&File{}, tokens, nil)
	if vocab := collectVocabulary(&File{}, tokens, nil, ev); vocab.nouns["voucher"] != 1 {
		t.Errorf("unclaimed path lost its name: %v", vocab.nouns)
	}
}

func TestFeatureNeedsMoreThanNames(t *testing.T) {
	var source strings.Builder
	source.WriteString(billingApp)
	// Generated SDK code: one noun repeated next to the same word.
	for _, prefix := range []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet", "kilo", "lima",
		"mike", "november", "oscar", "papa", "quebec", "romeo", "sierra", "tango", "uniform", "victor", "whiskey", "xray"} {
		fmt.Fprintf(&source, "var %sGet$inboundSchema=1;\n", prefix)
	}
	// Eight names, no text, no paths: below the bar for names alone.
	for _, name := range []string{"ledgerTable", "ledgerList", "ledger-view", "ledger_row", "ledgerCard", "ledgerMenu", "ledgerForm", "ledger-tab"} {
		fmt.Fprintf(&source, "var x=%q;\n", name)
	}
	file := Analyze([]Source{{Name: "app.js", Data: []byte(source.String())}}).Files[0]
	if _, ok := featureByArea(file.Purpose, "invoice"); !ok {
		t.Errorf("supported area dropped: %+v", file.Purpose.Features)
	}
	for _, area := range []string{"inbound", "ledger"} {
		if _, ok := featureByArea(file.Purpose, area); ok {
			t.Errorf("name-only area %q listed: %+v", area, file.Purpose.Features)
		}
	}
}

func TestNameOnlyAreaNeedsManyVariedNames(t *testing.T) {
	build := func(names []string) *vocabulary {
		v := newVocabulary()
		for _, name := range names {
			v.addName(name)
		}
		return v
	}
	var varied, repeated []string
	for _, prefix := range []string{"open", "empty", "sorted", "pinned", "recent", "shared", "linked", "stale"} {
		for _, suffix := range []string{"Basket", "Count", "Row"} {
			varied = append(varied, prefix+"Cart"+suffix)
		}
	}
	for _, prefix := range []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel", "india", "juliet", "kilo", "lima", "mike", "november", "oscar", "papa", "quebec", "romeo", "sierra", "tango"} {
		repeated = append(repeated, prefix+"Cart$schema")
	}
	if v := build(varied); v.supported("cart", nil, false) {
		t.Errorf("area nothing acts on accepted: %v", v.actions["cart"])
	}
	if v := build(append([]string{"removeCartRow"}, varied...)); !v.supported("cart", nil, false) {
		t.Errorf("varied names with an action rejected: %v", v.nouns["cart"])
	}
	// A second kind of evidence needs no action next to the name.
	withProse := build(varied)
	const sentence = "Your cart is empty today"
	withProse.addProse(sentence, proseWords(sentence))
	if !withProse.supported("cart", nil, false) {
		t.Errorf("area named in user-facing text rejected")
	}
	if !build(varied).supported("cart", []evidenceItem{{label: "/carts"}}, false) {
		t.Errorf("area named by a route or API call rejected")
	}
	if v := build(repeated); v.supported("cart", nil, false) {
		t.Errorf("generated pattern accepted: next=%v", v.next["cart"])
	}
	if v := build(varied[:10]); v.supported("cart", nil, false) {
		t.Errorf("few names accepted")
	}
}

func TestExtensionGlobalIsNotADomainNoun(t *testing.T) {
	if domainNoun("chrome") {
		t.Error("the extension global names an area")
	}
}

func TestPurposeTextEndsWithHintToFullList(t *testing.T) {
	result := Result{Sources: 1, Files: []File{billingFile(t)}}
	var out bytes.Buffer
	if err := WriteText(&out, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	for _, expected := range []string{
		"- invoice: ",
		"\n    3 API calls (InvoiceService) · 4 screens (/invoices, invoices/:id, /invoices/new)\n",
		"\n- Screens: 6 routes (/invoices, invoices/:id, vouchers/:id, +3 more)\n",
		"\n- API: 4 calls in 2 services (InvoiceService 3, CustomerService 1)\n",
	} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q:\n%s", expected, text)
		}
	}
	features := text[strings.Index(text, "### Features"):strings.Index(text, "### Behavior")]
	if strings.Count(text, "Run without --overview") != 1 || !strings.HasSuffix(text, defaultFooter) || strings.Contains(text, "counted.") || strings.Contains(text, "/acme.billing") {
		t.Errorf("report lists endpoints, repeats the hint, or keeps the totals sentence:\n%s", text)
	}
	if strings.Contains(features, "--overview") || strings.Contains(features, "named throughout") {
		t.Errorf("Features carries prose:\n%s", features)
	}
}

func TestPurposeTextWithoutEvidenceKeepsOneLinePerArea(t *testing.T) {
	file := Analyze([]Source{{Name: "studio_compiled.js", Data: []byte(`var a=["invoiceTable","invoiceList","sendInvoice","invoice-details","invoice-preview","payInvoice","Your invoice was sent.","The invoice could not be saved."];`)}}).Files[0]
	var out bytes.Buffer
	if err := WriteText(&out, Result{Sources: 1, Files: []File{file}}, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if !strings.Contains(text, "- invoice: ") || strings.Contains(text, "API call") || strings.Contains(text, "screen") || strings.Contains(strings.TrimSuffix(text, defaultFooter), "--overview") {
		t.Errorf("unexpected evidence lines:\n%s", text)
	}
}

func TestEvidenceFieldsInJSON(t *testing.T) {
	file := billingFile(t)
	data, err := json.Marshal(file.Purpose)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Features []map[string]any `json:"features"`
		Evidence map[string]int   `json:"evidence"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Evidence["api_calls"] != 4 || decoded.Evidence["screens"] != 6 {
		t.Errorf("evidence totals = %v", decoded.Evidence)
	}
	for _, feature := range decoded.Features {
		if feature["area"] != "invoice" {
			continue
		}
		for _, key := range []string{"area", "actions", "names", "api_calls", "screens", "api_examples", "screen_examples"} {
			if _, ok := feature[key]; !ok {
				t.Errorf("invoice feature missing %q: %v", key, feature)
			}
		}
		return
	}
	t.Errorf("invoice feature missing from JSON: %s", data)
}

func TestEvidenceFieldsAreOmittedWithoutEvidence(t *testing.T) {
	data, err := json.Marshal(Feature{Area: "invoice", Actions: []string{"send"}, Names: 6})
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != `{"area":"invoice","actions":["send"],"names":6}` {
		t.Errorf("feature JSON = %s", data)
	}
}

func TestGraphQLOperationsAreAPIEvidence(t *testing.T) {
	tokens := lex("var q=`query ListInvoices { invoices { id } }`;")
	ev := collectEvidence(&File{}, tokens, nil)
	if len(ev.items) != 1 || ev.items[0].group != "GraphQL" || ev.items[0].label != "ListInvoices" || !reflect.DeepEqual(ev.items[0].nouns, []string{"invoice"}) {
		t.Errorf("items = %+v", ev.items)
	}
}

// Ordinary routes often start with a word that is also a directory on some
// machine. They are screens and API calls, not file paths.
func TestRoutesNamedLikeSystemDirectoriesAreKept(t *testing.T) {
	source := `var routes=[{path:"users/:id",Component:A},{path:"users/:id/edit",Component:B},{path:"/home/feed/:feedId",Component:C},{path:"/applications/:id",Component:D},{path:"library/:id",Component:E}];
var calls=["/api/v1/users/search","/api/v1/users/{id}/avatar"];
var machine=["/etc/os-release","/usr/bin/node","/opt/app/bin","/Applications/Tool.app/Contents","/Users/someone/.config","/var/log/app/{id}"];`
	file := Analyze([]Source{{Name: "routes.js", Data: []byte(source)}}).Files[0]
	totals := file.Purpose.Evidence
	if totals == nil || totals.Screens != 5 || totals.APICalls != 2 {
		t.Fatalf("totals = %+v", totals)
	}
	// Declared routes from the insight pass are never machine-path filtered.
	ev := collectEvidence(&File{Insights: &Insights{
		Routes: []InsightRoute{{Path: "/home/:id", Kind: "declared route"}, {Path: "/library/:id", Kind: "declared route"}},
		API:    []InsightAPI{{Path: "/usr/:id", Method: "GET"}},
	}}, nil, nil)
	if total := ev.totals(); total == nil || total.Screens != 2 || total.APICalls != 1 {
		t.Errorf("insight totals = %+v", total)
	}
	for _, path := range []string{"/etc/os-release", "/usr/bin/node", "/opt/google/chrome/chrome", "/Users/me/x", "/Applications/Tool.app/Contents", "/var/log/{id}", "/node_modules/pkg"} {
		if got := classifyPath(path); got != pathIgnored {
			t.Errorf("classifyPath(%q) = %d, want ignored", path, got)
		}
	}
	for _, path := range []string{"users/:id", "/home/feed/:feedId", "/library/:id", "/applications/:id", "/api/v1/users/{id}"} {
		if got := classifyPath(path); got == pathIgnored {
			t.Errorf("classifyPath(%q) was ignored", path)
		}
	}
}

func TestKnownServiceNamesAreNotFeatureAreas(t *testing.T) {
	for _, word := range []string{"sentry", "statsig", "stripe", "datadog", "mixpanel"} {
		if domainNoun(word) {
			t.Errorf("service name %q is a domain noun", word)
		}
	}
	// Multi-word names are not single nouns, and application words stay.
	for _, word := range []string{"invoice", "payment", "google", "analytic"} {
		if !domainNoun(word) {
			t.Errorf("%q should stay a domain noun", word)
		}
	}
	source := `var a=["statsigClient","statsigUser","statsigGate","statsigInit","statsigLog","Your statsig gate was set."];`
	file := Analyze([]Source{{Name: "flags.js", Data: []byte(source)}}).Files[0]
	if _, ok := featureByArea(file.Purpose, "statsig"); ok {
		t.Errorf("service listed as a feature: %+v", file.Purpose.Features)
	}
}

func TestFeatureEvidenceLineIsDimmedInTerminal(t *testing.T) {
	plain := "- invoice: send\n    1 API call (InvoiceService)\n- order: edit\n    3 API calls (A) · 2 screens (/a, /b)\n    2 screens (/orders, /orders/new)\n"
	colored := colorizeOverview(plain)
	for _, line := range []string{"    1 API call (InvoiceService)", "    3 API calls (A) · 2 screens (/a, /b)", "    2 screens (/orders, /orders/new)"} {
		if !strings.Contains(colored, ansiOverview("2", line)) {
			t.Errorf("evidence line %q not dimmed:\n%q", line, colored)
		}
	}
	// Other indented lines keep their existing rendering.
	for _, line := range []string{"    plain indented text", "    10 apples (red)", "  - nested item", "    - deeper item"} {
		if got, want := colorizeOverview(line), colorizeOverviewBefore(line); got != want {
			t.Errorf("line %q changed: %q != %q", line, got, want)
		}
	}
}

// colorizeOverviewBefore is the rendering of a single line without the
// evidence rule, to show the rule leaves other lines alone.
func colorizeOverviewBefore(line string) string {
	switch {
	case strings.HasPrefix(line, "  - "):
		return "  " + ansiOverview("36", "↳") + " " + line[4:]
	case strings.Contains(line, " · "):
		return ansiOverview("2", line)
	}
	return line
}
