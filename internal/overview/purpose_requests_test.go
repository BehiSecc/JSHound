package overview

import (
	"reflect"
	"sort"
	"strings"
	"testing"
)

// evidenceOf runs the insight pass and the evidence collection on a source, the
// way a file goes through the overview, and lists API paths and screen paths.
func pathsOf(t *testing.T, source string) (api, screens []string) {
	t.Helper()
	file := Analyze([]Source{{Name: "app.js", Data: []byte(source)}}).Files[0]
	ev := collectEvidence(&file, lex(source), nil)
	for _, item := range ev.items {
		if item.screen {
			screens = append(screens, item.label)
		} else {
			api = append(api, item.label)
		}
	}
	sort.Strings(api)
	sort.Strings(screens)
	return api, screens
}

func TestRequestCallsWithAnUnshownBaseCountTheLiteralPath(t *testing.T) {
	for name, source := range map[string]string{
		"fetch concat":   `function load(){return fetch(BASE + "/orders/list")}`,
		"fetch template": "function load(){return fetch(`${base}/orders/list`)}",
		"compiled":       `function load(){return fetch("".concat(base,"/orders/list"))}`,
		"get":            `function load(){return axios.get("/orders/list")}`,
		"post":           `function load(){return axios.post("/orders/list",{a:1})}`,
		"put":            `function load(){return http.put(base+"/orders/list",data)}`,
		"patch":          `function load(){return this.http.patch("".concat(this.base,"/orders/list"),data)}`,
		"delete":         `function load(){return client.delete("/orders/list")}`,
		"request":        `function load(){return axios.request({url:base+"/orders/list",method:"POST"})}`,
		"ajax":           `function load(){return $.ajax({url:base+"/orders/list",type:"POST"})}`,
		"jquery post":    `function load(){return $.post("/orders/list",{a:1})}`,
		"xhr":            `function load(){var x=new XMLHttpRequest;x.open("POST","/orders/list");x.send()}`,
		"promise":        `function load(){return m.get(base+"/orders/list").then(show)}`,
		"await":          `async function load(){return await m.get("/orders/list")}`,
		"object":         `function load(){return m.get({url:"/orders/list"})}`,
		"variable":       `function load(){var url="".concat(base,"/orders/list");return fetch(url,{method:"POST"})}`,
		"wrapper":        `function load(e){var t="".concat(this.uriPrefix,"/orders/list");return(0,i.x3)(t,{method:"POST",body:e})}`,
	} {
		api, screens := pathsOf(t, source)
		if !reflect.DeepEqual(api, []string{"/orders/list"}) || len(screens) != 0 {
			t.Errorf("%s: api = %v, screens = %v", name, api, screens)
		}
	}
}

func TestRequestPathKeepsDynamicSegments(t *testing.T) {
	for source, want := range map[string]string{
		`function load(id){return axios.get(base+"/orders/"+id+"/items")}`:      "/orders/{dynamic}/items",
		`function load(id){return axios.get(base+"/orders/"+id+"&_="+now)}`:     "/orders/{dynamic}",
		"function load(id){return axios.get(`${base}/orders/${id}/items`)}":     "/orders/{dynamic}/items",
		"function load(id){return axios.get(`${base}${path(`orders/${id}`)}`)}": "",
		`function load(id){return axios.get(base+"/"+id+"/items")}`:             "",
		`function load(){return axios.get(base+"/api/0.1")}`:                    "",
	} {
		api, _ := pathsOf(t, source)
		if want == "" && len(api) != 0 || want != "" && !reflect.DeepEqual(api, []string{want}) {
			t.Errorf("%s: api = %v, want %q", source, api, want)
		}
	}
}

func TestLookAlikesOfRequestCallsAreNotCounted(t *testing.T) {
	for name, source := range map[string]string{
		"link building":    `function share(){el.href=location.origin+"/orders/list";window.open(location.origin+"/orders/list");location.assign(base+"/orders/list")}`,
		"template link":    "function share(){el.href=`${origin}/orders/list`}",
		"express route":    `app.get("/orders/list",function(req,res){res.send(1)});app.post("/orders/list",(req,res)=>res.send(1))`,
		"router":           `router.get("/orders/list",handler);api2.get("/orders/list",(r)=>r)`,
		"map":              `function f(){return routes.get("/orders/list")}`,
		"cache":            `function f(){return cache.put("/orders/list",response)}`,
		"unknown receiver": `function f(){return m.get("/orders/list")}`,
		"set delete":       `function f(){return seen.delete("/orders/list")}`,
		"asset base":       `function f(){return fetch(CDN+"/lib/app.js")}`,
		"asset get":        `function f(){return axios.get(CDN+"/img/logo.png")}`,
		"not a path":       `function f(){return axios.get(base+"orders")}`,
		"root only":        `function f(){return axios.get(base+"/")}`,
		"window open":      `function f(){window.open("/orders/list")}`,
		"known start":      `function f(){return axios.get("https://cdn.example/orders/list")}`,
		"no method":        `function f(){x.open("/orders/list")}`,
		"variable only":    `function f(){return axios.get(url)}`,
	} {
		api, screens := pathsOf(t, source)
		if len(api) != 0 {
			t.Errorf("%s: counted as API calls: %v (screens %v)", name, api, screens)
		}
	}
}

func TestRequestCallsAreCountedOnce(t *testing.T) {
	// The wrapper, the client call, and the scanner see the same request.
	source := `function load(){return axios.request({url:base+"/api/orders/list",method:"POST"})}
function again(){return axios.post("".concat(base,"/api/orders/list"))}
var u="/api/orders/list";fetch(base+u)`
	api, _ := pathsOf(t, source)
	if !reflect.DeepEqual(api, []string{"/api/orders/list"}) {
		t.Errorf("api = %v", api)
	}
	// A request to an RPC path is the RPC method, once.
	ev := collectEvidence(&File{}, lex(`function f(){return client.post(base+"/acme.v1.ShipmentService/CancelShipment")}`), nil)
	ev.addRPCMethods([]string{"ShipmentService.cancelShipment"})
	if len(ev.items) != 1 || ev.items[0].group != "ShipmentService" {
		t.Errorf("items = %+v", ev.items)
	}
}

func TestPortFragmentIsNotAScreen(t *testing.T) {
	for _, path := range []string{":8443/status", ":8080/api", ":8443"} {
		if kind := classifyPath(path); kind != pathIgnored && kind != pathUnknown {
			t.Errorf("classifyPath(%q) = %v", path, kind)
		}
		if evidencePath(path) {
			t.Errorf("evidencePath(%q) accepts a port", path)
		}
	}
	for _, segment := range []string{":8443", "host:8080"} {
		if paramSegment.MatchString(segment) {
			t.Errorf("%q is a router parameter", segment)
		}
	}
	for _, segment := range []string{":id", ":_slug", ":id(\\d+)", "{id}", "[id]", "orders:cancel"} {
		if !paramSegment.MatchString(segment) {
			t.Errorf("%q is not a router parameter", segment)
		}
	}
	_, screens := pathsOf(t, `var s="x",r=1;console.log(s+":8443/status/"+r);var q=["/orders/:id"];`)
	if !reflect.DeepEqual(screens, []string{"/orders/:id"}) {
		t.Errorf("screens = %v", screens)
	}
}

func TestVersionAndActionSegmentsAreNotFileNames(t *testing.T) {
	for path, want := range map[string]bool{
		"/api/0.1/structure/":         true,
		"/api/0.2/create_user_login":  true,
		"/v1.2/orders/list":           true,
		"/api/cameras.update":         true,
		"/api/v1/cameras.set_target":  true,
		"/api/config.json":            false,
		"/api/v1/report.pdf":          false,
		"/api/cameras.update/more":    false, // the action ends the path
		"/orders/list.update":         false, // not under an API path
		"/assets/app.min.js":          false,
		"/static/img/logo.png":        false,
		"/api/v1/logo.PNG":            false,
		"/.well-known/openid-config":  false,
		"/api/0.1/structure/file.xml": false,
	} {
		if got := evidencePath(path); got != want {
			t.Errorf("evidencePath(%q) = %v, want %v", path, got, want)
		}
	}
	if classifyPath("/api/0.1/structure/") != pathAPI || classifyPath("/v1.2/orders/list") != pathAPI {
		t.Error("versioned paths are not API calls")
	}
	api, _ := pathsOf(t, `var u="/api/0.2/create_user_login";fetch(u,{method:"POST"});fetch("/orders/cameras.update",{method:"POST"})`)
	if !reflect.DeepEqual(api, []string{"/api/0.2/create_user_login"}) {
		t.Errorf("api = %v", api)
	}
}

func TestRoutePropertiesAndPathListsAreScreens(t *testing.T) {
	_, screens := pathsOf(t, `var menu=[{id:"a",route:{default:"/settings/manage-teams",perRole:{admin:"/settings/manage-users",user:"/home/teams"}}},
{id:"b",routerLink:"/reports/weekly"},{id:"c",route:"/api/internal/data"},{route:"orders"}];`)
	want := []string{"/home/teams", "/reports/weekly", "/settings/manage-teams", "/settings/manage-users"}
	if !reflect.DeepEqual(screens, want) {
		t.Errorf("screens = %v", screens)
	}
	_, screens = pathsOf(t, `var f0=["/dashboard/partner-view","/dashboard/customer-view"];function g(){return f0.includes(this.router.url)}
function h(){return ["/explore/","/looks/"].some(function(e){return 0===window.location.pathname.indexOf(e)})}
var unused=["/billing/never-used","/billing/not-compared"];`)
	want = []string{"/dashboard/customer-view", "/dashboard/partner-view", "/explore", "/looks"}
	if !reflect.DeepEqual(screens, want) {
		t.Errorf("screens = %v", screens)
	}
}

func TestViewTablesAreScreensAndNotAPICalls(t *testing.T) {
	for name, source := range map[string]string{
		"keys name views": `var T={SHOP_VIEW_HOME:"/home/overview",SHOP_VIEW_CART:"/cart/review",SHOP_VIEW_PAY:"/cart/pay",SHOP_NAME_PAY:"pay",SHOP_LABEL:"x"};`,
		"table name":      `var Routes={HOME:"/home",CART:"/cart/review",PAY:"/cart/pay"};`,
		"property name":   `var C={pages:{home:"/home",cart:"/cart/review",pay:"/cart/pay"},other:1};`,
		"router params":   `var T={HOME:"/home",CART:"/cart/list",PAY:"/cart/pay",ITEM:"/cart/:id",LABEL:"x"};`,
	} {
		api, screens := pathsOf(t, source)
		if len(api) != 0 || len(screens) < 3 {
			t.Errorf("%s: api = %v, screens = %v", name, api, screens)
		}
	}
	// Words of requests beside views settle nothing for those entries.
	api, screens := pathsOf(t, `var T={HOME_VIEW:"/home/overview",CART_VIEW:"/cart/review",PAY_VIEW:"/cart/pay",PAY_URL:"/cart/pay/submit",CART_URL:"/cart/submit"};`)
	if len(api) != 0 || !reflect.DeepEqual(screens, []string{"/cart/pay", "/cart/review", "/home/overview"}) {
		t.Errorf("mixed: api = %v, screens = %v", api, screens)
	}
}

func TestEndpointTablesStayAPICalls(t *testing.T) {
	for name, source := range map[string]string{
		"url keys":     `var T={SEND_URL:"/basket/send/",CANCEL_URL:"/basket/cancel/",TRACK_URL:"/basket/track/"};`,
		"with base":    `var T={SEND_VIEW:base+"/basket/send/",CANCEL_VIEW:base+"/basket/cancel/",TRACK_VIEW:base+"/basket/track/"};`,
		"slashes":      `var Pages={SEND:"/basket/send/",CANCEL:"/basket/cancel/",TRACK:"/basket/track/"};`,
		"view and url": `var T={A_VIEW_URL:"/home/overview",B_VIEW_URL:"/cart/review",C_VIEW_URL:"/cart/pay"};`,
		"named urls":   `var Urls={SEND:"/basket/send",CANCEL:"/basket/cancel",TRACK:"/basket/track"};`,
	} {
		api, screens := pathsOf(t, source)
		if len(api) != 3 || len(screens) != 0 {
			t.Errorf("%s: api = %v, screens = %v", name, api, screens)
		}
	}
}

func TestAmbiguousTablesAreNotCounted(t *testing.T) {
	for name, source := range map[string]string{
		"links":         `var L={home:"/home",about:"/about",contact:"/contact",help:"/help"};`,
		"short keys":    `var T={a:"/home/overview",b:"/cart/review",c:"/cart/pay",d:1,e:2,f:3};`,
		"two views":     `var T={A_VIEW:"/home/overview",B_VIEW:"/cart/review",C:"/cart/pay",D:1,E:2,F:3};`,
		"file paths":    `var Paths={A:"/static/a.png",B:"/static/b.css",C:"/static/c.js"};`,
		"declared":      `var R={path:"/a/b",A_VIEW:"/home/overview",B_VIEW:"/cart/review",C_VIEW:"/cart/pay"};`,
		"api with view": `var Views={A:"/api/home/overview",B:"/api/cart/review",C:"/api/cart/pay"};`,
	} {
		api, screens := pathsOf(t, source)
		if name == "api with view" {
			// API-shaped paths are API calls by their shape, never screens.
			if len(screens) != 0 {
				t.Errorf("%s: screens = %v", name, screens)
			}
			continue
		}
		if len(api) != 0 || len(screens) != 0 {
			t.Errorf("%s: api = %v, screens = %v", name, api, screens)
		}
	}
}

func TestNoTableIsBothScreensAndAPICalls(t *testing.T) {
	source := `var Views={HOME_VIEW:"/home/overview",CART_VIEW:"/cart/review",PAY_VIEW:"/cart/pay"};
var T={SEND_URL:"/home/overview",PAY_URL:"/cart/pay",LIST_URL:"/cart/list"};
var menu={route:{default:"/cart/review",perRole:{a:"/cart/review",b:"/cart/pay",c:"/cart/list"}}};`
	api, screens := pathsOf(t, source)
	seen := map[string]bool{}
	for _, path := range screens {
		seen[path] = true
	}
	for _, path := range api {
		if seen[path] {
			t.Errorf("%s is a screen and an API call", path)
		}
	}
	if len(strings.Join(api, "")) == 0 && len(screens) == 0 {
		t.Error("nothing counted")
	}
}
