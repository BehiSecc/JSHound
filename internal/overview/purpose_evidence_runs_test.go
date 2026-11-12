package overview

import (
	"encoding/json"
	"strings"
	"testing"
)

func factsOf(source string, ctx pageContext) []RunFact {
	return runFacts(scanRunSignals(lex(source), nil), nil, false, ctx)
}

func factOf(facts []RunFact, aspect string) (RunFact, bool) {
	for _, fact := range facts {
		if fact.Aspect == aspect {
			return fact, true
		}
	}
	return RunFact{}, false
}

func factDetails(facts []RunFact) string {
	var parts []string
	for _, fact := range facts {
		parts = append(parts, fact.Aspect+"="+fact.Detail)
	}
	return strings.Join(parts, " | ")
}

func analyzePurpose(t *testing.T, name, code string) *Purpose {
	t.Helper()
	file := Analyze([]Source{{Name: name, Data: []byte(code)}}).Files[0]
	if file.Purpose == nil {
		t.Fatalf("%s: no purpose", name)
	}
	return file.Purpose
}

func TestRunEvidenceCountsUsesAndLocatesTheFirst(t *testing.T) {
	source := "var a=1;\n  fetch(a);fetch(b);\nx.y=fetch(c)"
	fact, ok := factOf(factsOf(source, contextPage), "Server calls")
	if !ok || len(fact.Evidence) != 1 {
		t.Fatalf("server calls = %+v", fact)
	}
	want := RunEvidence{Signal: "fetch", Uses: 3, Line: 2, Column: 3}
	if fact.Evidence[0] != want {
		t.Errorf("evidence = %+v, want %+v", fact.Evidence[0], want)
	}
}

func TestRunEvidenceIsPartOfTheJSON(t *testing.T) {
	purpose := analyzePurpose(t, "app.js", "var a=1;\nfetch(a);\n")
	data, err := json.Marshal(purpose)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"aspect":"Server calls"`, `"detail":"fetch"`, `"evidence":[{"signal":"fetch","uses":1,"line":2,"column":1}]`} {
		if !strings.Contains(string(data), want) {
			t.Errorf("json lacks %s: %s", want, data)
		}
	}
}

func TestOneWordIsNotAClaimButOneCallIs(t *testing.T) {
	for _, tc := range []struct {
		name, source, aspect, want string
	}{
		// Calls and constructors stand on one use.
		{"fetch call", `fetch("/a")`, "Server calls", "fetch"},
		{"web socket", `new WebSocket("wss://x")`, "Server calls", "WebSocket"},
		{"indexedDB", `window.indexedDB.open("a")`, "Storage", "IndexedDB"},
		{"service worker", `navigator.serviceWorker.register("/sw.js")`, "Starts", "registers a service worker"},
		{"header setter", `r.setRequestHeader("Authorization",t)`, "Server calls", "sends Authorization header"},
		// Words and strings need a second piece of evidence.
		{"one graphql string", `fetch("/a");var u="/graphql"`, "Server calls", "fetch"},
		{"two graphql strings", `fetch("/a");var u="/graphql",v="/api/graphql"`, "Server calls", "fetch, including GraphQL"},
		{"one sign-in name", `var m="scan_qr_code_login"`, "Sign-in", ""},
		{"two sign-in names", `var m=["scan_qr_code_login","qr_code_sign"]`, "Sign-in", "QR-code login"},
		{"one upload field", `var p={partNumber:1}`, "Files", ""},
		{"two upload fields", `var p={partNumber:1,uploadId:u}`, "Files", "chunked uploads"},
		// Protocol and vendor constants stand on one use.
		{"one header key", `fetch(u);var o={"X-Auth-Token":t}`, "Server calls", "fetch; sends X-Auth-Token header"},
		{"one header index", `fetch(u);o["X-Csrf-Token"]=t`, "Server calls", "fetch; sends X-Csrf-Token header"},
		{"one SDK host", `var s="https://js.stripe.com/v3"`, "Third parties", "Stripe (payments)"},
		{"one service global", `window.gtag("js",d)`, "Third parties", "Google Analytics (analytics)"},
		{"one S3 constant", `var a="AWS4-HMAC-SHA256"`, "Files", "uploads to S3-compatible storage with signed requests"},
		{"one S3 header", `var h={"x-amz-date":d}`, "Files", "uploads to S3-compatible storage with signed requests"},
		{"one Azure header", `r.setRequestHeader("x-ms-blob-type","BlockBlob")`, "Files", "uploads to Azure Blob Storage"},
		{"a header name in prose", `var m="Send the X-Auth-Token header",n="X-Auth-Token"`, "Server calls", ""},
		{"XMLHttpRequest mentions", `var a=typeof XMLHttpRequest&&XMLHttpRequest.prototype.open`, "Server calls", ""},
		{"XMLHttpRequest constructed", `var a=new XMLHttpRequest()`, "Server calls", "XMLHttpRequest"},
	} {
		fact, ok := factOf(factsOf(tc.source, contextPage), tc.aspect)
		if tc.want == "" {
			if ok {
				t.Errorf("%s: stated %q", tc.name, fact.Detail)
			}
			continue
		}
		if !ok || fact.Detail != tc.want {
			t.Errorf("%s: got %q (stated %v), want %q", tc.name, fact.Detail, ok, tc.want)
		}
	}
}

func TestHeadersDifferingInCaseAreOneHeaderNamedAsFirstSpelled(t *testing.T) {
	fact, ok := factOf(factsOf(`var a={"x-api-key":k};var b={"X-Api-Key":k};b["X-API-KEY"]=k;`, contextPage), "Server calls")
	if !ok || fact.Detail != "sends x-api-key header" {
		t.Fatalf("detail = %q", fact.Detail)
	}
	if want := (RunEvidence{Signal: "header x-api-key", Uses: 3, Line: 1, Column: 8}); len(fact.Evidence) != 1 || fact.Evidence[0] != want {
		t.Errorf("evidence = %+v, want %+v", fact.Evidence, want)
	}
}

func TestHistoryRoutingNeedsMoreThanOneCallOrOnePopstate(t *testing.T) {
	for _, tc := range []struct {
		name, source string
		want         bool
	}{
		{"popstate string alone", `window.addEventListener("popstate",f)`, false},
		{"popstate twice", `addEventListener("popstate",f);removeEventListener("popstate",f)`, false},
		{"one pushState call", `history.pushState({},"","/a")`, false},
		{"method names as strings", `fill(history,"pushState",f);fill(history,"replaceState",f)`, false},
		{"call and popstate", `history.pushState({},"","/a");addEventListener("popstate",f)`, true},
		{"two calls", `history.pushState({},"","/a");history.replaceState({},"","/b")`, true},
	} {
		_, got := factOf(factsOf(tc.source, contextPage), "Navigation")
		if got != tc.want {
			t.Errorf("%s: navigation stated = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestFactsAreGatedByRuntimeOnlyWhenTheContextIsKnown(t *testing.T) {
	page := `history.pushState({},"","/a");history.replaceState({},"","/b");document.cookie="a=1";
window.localStorage.getItem("k");window.indexedDB.open("db");new AudioContext;
ReactDOM.createRoot(document.getElementById("root")).render(App);`
	got := factDetails(factsOf(page, contextPage))
	for _, want := range []string{"Navigation=client-side", "Starts=mounts", "Storage=cookies, localStorage, and IndexedDB", "processes audio"} {
		if !strings.Contains(got, want) {
			t.Errorf("page facts lack %q: %s", want, got)
		}
	}
	got = factDetails(factsOf(page, contextNoDocument))
	for _, banned := range []string{"Navigation", "Starts", "cookies", "localStorage", "audio"} {
		if strings.Contains(got, banned) {
			t.Errorf("no-document context states %s: %s", banned, got)
		}
	}
	if !strings.Contains(got, "Storage=IndexedDB") {
		t.Errorf("no-document context lost IndexedDB: %s", got)
	}
	node := factsOf(`var r=new FileReader;`+page, contextNode)
	if _, ok := factOf(node, "Files"); ok {
		t.Errorf("Node context reads files in the browser: %s", factDetails(node))
	}
}

func TestServiceWorkerAndNodeFilesDropPageFacts(t *testing.T) {
	pageCode := `history.pushState({},"","/a");history.replaceState({},"","/b");document.cookie="a=1";fetch("/x");`
	worker := analyzePurpose(t, "sw.js", `self.addEventListener("install",function(){self.skipWaiting()});`+pageCode)
	if !strings.HasPrefix(worker.Is, "Service worker") {
		t.Fatalf("is = %q", worker.Is)
	}
	for _, aspect := range []string{"Navigation", "Storage"} {
		if _, ok := factOf(worker.Runs, aspect); ok {
			t.Errorf("service worker states %s: %s", aspect, factDetails(worker.Runs))
		}
	}
	if _, ok := factOf(worker.Runs, "Server calls"); !ok {
		t.Errorf("service worker lost fetch: %s", factDetails(worker.Runs))
	}

	nodeCode := `const fs=require("fs");const a=process.argv.slice(2);fs.readFileSync(a[0]);history.pushState({},"","/a");history.replaceState({},"","/b");fetch("/x");indexedDB.open("db");new FileReader;`
	node := analyzePurpose(t, "tool.js", nodeCode)
	if !strings.HasPrefix(node.Is, "Node.js") {
		t.Fatalf("is = %q", node.Is)
	}
	for _, aspect := range []string{"Navigation", "Files"} {
		if _, ok := factOf(node.Runs, aspect); ok {
			t.Errorf("Node.js file states %s: %s", aspect, factDetails(node.Runs))
		}
	}
	if _, ok := factOf(node.Runs, "Server calls"); !ok {
		t.Errorf("Node.js file lost fetch: %s", factDetails(node.Runs))
	}

	// A file that also touches the DOM is not established as Node.js.
	page := analyzePurpose(t, "page.js", `var e=process.env.NODE_ENV;document.title="x";window.name="y";`+pageCode)
	if _, ok := factOf(page.Runs, "Navigation"); !ok {
		t.Errorf("page lost navigation: %s %s", page.Is, factDetails(page.Runs))
	}
}
