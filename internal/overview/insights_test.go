package overview

import (
	"strings"
	"testing"
)

func insightsForTest(t *testing.T, source string) *Insights {
	t.Helper()
	files := Analyze([]Source{{Name: "main.js", Data: []byte(source)}}).Files
	if len(files) != 1 || files[0].Insights == nil {
		t.Fatalf("expected extracted insights, got %#v", files)
	}
	return files[0].Insights
}

func TestInsightsRecoverRoutesAndGeneratedAPIWithOwnership(t *testing.T) {
	source := "(self.webpackChunkdemo=self.webpackChunkdemo||[]).push([[792],{" +
		"10:(e,t,r)=>{var x=r(20),j={};const C=()=>null;" +
		"(0,j.jsx)(Route,{path:x.q.OAUTH,Component:C});" +
		"(0,j.jsx)(Route,{path:'/home',Component:C});" +
		"(0,j.jsx)('a',{href:x.q.SIGN_UP});}," +
		"20:(e,t,r)=>{r.d(t,{q:()=>n});var n=function(e){return e.SIGN_UP='/login/signup',e.OAUTH='/login/oauth/middle',e}({});}," +
		"30:(e,t,r)=>{const o='GET';const client=new class{constructor(){this.uriPrefix='/api'}" +
		"GetConfig(e){const path=`${this.uriPrefix}/open/config`;return(0,a.x3)(path,{method:o},e)}" +
		"CheckPhone(e){const path=`${this.uriPrefix}/open/check/phone`;return(0,a.x3)(path,{method:'POST',body:JSON.stringify(e)},e)}};}" +
		"}]);"
	insights := insightsForTest(t, source)
	for _, want := range []string{"/home", "/login/oauth/middle", "/login/signup"} {
		found := false
		for _, route := range insights.Routes {
			if route.Path == want && route.Module == "10" && route.Runtime == "webpackChunkdemo" {
				found = true
			}
		}
		if !found {
			t.Errorf("missing owned route %s: %#v", want, insights.Routes)
		}
	}
	for _, want := range []struct{ name, method, path string }{
		{"GetConfig", "GET", "/api/open/config"},
		{"CheckPhone", "POST", "/api/open/check/phone"},
	} {
		found := false
		for _, entry := range insights.API {
			if entry.Name == want.name && entry.Method == want.method && entry.Path == want.path && entry.Module == "30" && entry.Transport == "a.x3" {
				found = true
			}
		}
		if !found {
			t.Errorf("missing request definition %+v: %#v", want, insights.API)
		}
	}
}

func TestInsightsRecoverRequestParametersAndExcludeUncalledObjects(t *testing.T) {
	source := `const example={url:"/api/example",method:"POST"};
const routeData={path:"/private",loader:"hello"}; const navData={to:"/api/archive"};
function ReadArtist(e,t){const url=client.genBaseURL("/studio/artist/detail");
  const data={artist_id:e.artist_id,region:e.region};
  return client.request({url:url,method:"GET",params:data},t)}
function Changed(){let url="/api/old";url="/api/new";
  return client.request({url:url,method:"GET"})}`
	insights := insightsForTest(t, source)
	if len(insights.Routes) != 0 {
		t.Fatalf("unrelated objects became routes: %#v", insights.Routes)
	}
	if len(insights.API) != 1 {
		t.Fatalf("expected one request definition, got %#v", insights.API)
	}
	entry := insights.API[0]
	if entry.Name != "ReadArtist" || entry.Method != "GET" || entry.Path != "/studio/artist/detail" ||
		strings.Join(entry.Parameters, ",") != "artist_id,region" || entry.Transport != "client.request" {
		t.Fatalf("wrong recovered request: %#v", entry)
	}
}

func TestInsightsDoNotConflateSameMemberAcrossExportedObjects(t *testing.T) {
	source := "(self.webpackChunkdemo=self.webpackChunkdemo||[]).push([[1],{" +
		"10:(e,t,r)=>{const x=r(20),C=()=>null;(0,j.jsx)(Route,{path:x.A.PATH,Component:C});(0,j.jsx)(Route,{path:'/other',Component:C})}," +
		"20:(e,t,r)=>{r.d(t,{A:()=>a,B:()=>b});const a={PATH:'/first'},b={PATH:'/second'};}" +
		"}]);"
	insights := insightsForTest(t, source)
	first, second := false, false
	for _, route := range insights.Routes {
		first = first || route.Path == "/first"
		second = second || route.Path == "/second"
	}
	if !first || second {
		t.Fatalf("cross-object route resolution: %#v", insights.Routes)
	}
}

func TestInsightTokenSourceColumnsOnMinifiedInput(t *testing.T) {
	tokens := lex("const a='x';\n  const b=`${a}/x`;")
	for _, tok := range tokens {
		if tok.text == "b" && (tok.line != 2 || tok.column != 9 || tok.offset != 21) {
			t.Fatalf("wrong token source position: %#v", tok)
		}
	}
}

func TestInsightsNativeFetchAndUncoveredBundleCode(t *testing.T) {
	source := "(self.webpackChunkdemo=self.webpackChunkdemo||[]).push([[1],{" +
		"10:(e,t,r)=>{const C=()=>null;(0,j.jsx)(Route,{path:'/home',Component:C});}" +
		"}]);fetch('/api/config');fetch('/api/update',{method:'POST'});"
	insights := insightsForTest(t, source)
	if len(insights.API) != 2 {
		t.Fatalf("expected two top-level fetch definitions outside the factory, got %#v", insights.API)
	}
	got := make(map[string]string)
	for _, entry := range insights.API {
		if entry.Module != "" || entry.Transport != "fetch" || entry.Kind != "direct call expression" {
			t.Fatalf("top-level fetch assigned to a factory: %#v", entry)
		}
		got[entry.Path] = entry.Method
	}
	if got["/api/config"] != "GET" || got["/api/update"] != "POST" {
		t.Fatalf("wrong fetch methods: %#v", got)
	}
}

func TestInsightsRecognizeNavigateArrayTarget(t *testing.T) {
	insights := insightsForTest(t, `function afterSave(){return router.navigate(["/affiliates/account-hierarchy"])}`)
	if len(insights.Routes) != 1 || insights.Routes[0].Path != "/affiliates/account-hierarchy" || insights.Routes[0].Kind != "navigation target" {
		t.Fatalf("navigation call was not recovered: %#v", insights.Routes)
	}
}

func TestInsightsDoNotTreatNestedHelperArgumentAsNavigationTarget(t *testing.T) {
	source := `function open(){navigate(compute("/placeholder", value));
  (0,j.jsx)(Link,{href:compute("/also-placeholder", value)});
  navigate("/account");}`
	insights := insightsForTest(t, source)
	if len(insights.Routes) != 1 || insights.Routes[0].Path != "/account" {
		t.Fatalf("nested helper argument became a navigation target: %#v", insights.Routes)
	}
}

func TestInsightsNestedRequestBelongsToInnerWrapper(t *testing.T) {
	insights := insightsForTest(t, `function outer(){function inner(){return client.request({url:"/api/inner",method:"GET"})}return inner()}`)
	if len(insights.API) != 1 || insights.API[0].Name != "inner" || insights.API[0].Kind != "request wrapper" {
		t.Fatalf("nested request assigned to wrong wrapper: %#v", insights.API)
	}
}

func TestInsightsDoNotReuseReassignedModuleAlias(t *testing.T) {
	source := "(self.webpackChunkdemo=self.webpackChunkdemo||[]).push([[1],{" +
		"10:(e,t,r)=>{var x=r(20),C=()=>null;x={};(0,j.jsx)(Route,{path:x.q.LOGIN,Component:C});(0,j.jsx)(Route,{path:'/home',Component:C})}," +
		"20:(e,t,r)=>{r.d(t,{q:()=>n});var n=function(e){return e.LOGIN='/login',e}({});}" +
		"}]);"
	insights := insightsForTest(t, source)
	for _, route := range insights.Routes {
		if route.Path == "/login" {
			t.Fatalf("route resolved through reassigned module alias: %#v", insights.Routes)
		}
	}
}

func TestInsightsSkipLocallyShadowedFetch(t *testing.T) {
	file := Analyze([]Source{{Name: "shadow.js", Data: []byte(`function fetch(url){return url} fetch("/api/local")`)}}).Files[0]
	if file.Insights != nil && len(file.Insights.API) != 0 {
		t.Fatalf("local function was treated as native fetch: %#v", file.Insights.API)
	}
}

func TestInsightsSkipFetchParameterButKeepWindowFetch(t *testing.T) {
	source := `function apply(fetch){return fetch("/local/value")}
const arrow=(fetch)=>fetch("/local/arrow");
const single=fetch=>fetch("/local/single");
function native(fetch){return window.fetch("/api/public")}`
	insights := insightsForTest(t, source)
	if len(insights.API) != 1 || insights.API[0].Path != "/api/public" ||
		insights.API[0].Transport != "window.fetch" || insights.API[0].Kind != "direct call expression" {
		t.Fatalf("fetch parameter shadowing or explicit window.fetch handling failed: %#v", insights.API)
	}
}

func TestInsightsKeepShortPathsAndDoNotCallDirectFetchAnAPIClient(t *testing.T) {
	source := `(0,j.jsx)(Route,{path:"/x",Component:Screen});
 (0,j.jsx)(Route,{path:"/1",Component:Screen});
 (0,j.jsx)(Route,{path:"/_health",Component:Screen});
 (0,j.jsx)(Route,{path:"settings",Component:Screen});
 (0,j.jsx)(Route,{path:"*",Component:Screen});
 fetch("/x");fetch("/1");fetch("/_health")`
	insights := insightsForTest(t, source)
	wantRoutes := map[string]bool{"/x": false, "/1": false, "/_health": false, "settings": false, "*": false}
	for _, route := range insights.Routes {
		if _, ok := wantRoutes[route.Path]; ok {
			wantRoutes[route.Path] = true
		}
	}
	for path, found := range wantRoutes {
		if !found {
			t.Fatalf("UI route %s lost: %#v", path, insights.Routes)
		}
	}
	if len(insights.API) != 3 {
		t.Fatalf("short fetch targets lost: %#v", insights.API)
	}
	for _, component := range insights.Components {
		if component.Role == "API client" {
			t.Fatalf("direct fetch expressions were mislabeled as an API client: %#v", insights.Components)
		}
	}
}
