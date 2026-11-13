package analyzer

import (
	"reflect"
	"testing"
)

// Each test pairs the values that are not request paths with the nearest real
// routes, which must stay: a value is only dropped on evidence specific to its
// kind.

func TestMonitoringMetricNamesAndClientConfigKeysAreNotEndpoints(t *testing.T) {
	result := Analyze([]byte(`
		this.name = "/client_streamz/shop_app/checkout_clicks/count";
		this.other = '/client_streamz/shop_app/render_latencies';
		cfg = {"googleapis.config/auth/someFlag": !0};
		x("googleapis.config/auth/key");
		fetch("/rest/api/2/issue"); fetch("/api/client_streamz/count"); fetch("/orders/count");
		fetch("/metrics/shop_app/count"); get("cdn.shop.test/assets/list"); fetch("/client_streamz");
	`))
	want := []string{"/api/client_streamz/count", "/client_streamz", "/metrics/shop_app/count", "/orders/count", "/rest/api/2/issue", "cdn.shop.test/assets/list"}
	if !reflect.DeepEqual(result.Endpoints, want) {
		t.Fatalf("endpoints = %#v, want %#v", result.Endpoints, want)
	}
}

func TestSchemaReferencesAreNotEndpoints(t *testing.T) {
	result := Analyze([]byte(`
		a = "REF:shapes.json#/$defs/Circle"; b = "shapes.json#/definitions/Circle"; c = "schema.yaml#/components/schemas/Pet";
		fetch("/app/settings#/profile/edit"); fetch("/docs/shapes.json#/$defs/Circle"); fetch("/spa#/definitions/list");
		fetch("/api/v1/users#/top"); get("api/shapes.json#top");
	`))
	want := []string{"/api/v1/users#/top", "/app/settings#/profile/edit", "/docs/shapes.json#/$defs/Circle", "/spa#/definitions/list", "api/shapes.json#top"}
	if !reflect.DeepEqual(result.Endpoints, want) {
		t.Fatalf("endpoints = %#v, want %#v", result.Endpoints, want)
	}
}

func TestJSONPatchPathsAreNotEndpoints(t *testing.T) {
	result := Analyze([]byte(`
		ops = [{op: "replace", path: "/final_output", value: {a: 1}}, {"path": "/items/-", "op": "add", "value": x}, {path:"/profile/name",op:'test',value:1}];
		route = {path: "/dashboard/reports", component: Reports};
		step = {op: "sum", path: "/dashboard/totals"};
		other = {name: "x", path: "/orders/export", method: "POST"};
		fetch("/final_output");
	`))
	want := []string{"/dashboard/reports", "/dashboard/totals", "/final_output", "/orders/export"}
	if !reflect.DeepEqual(result.Endpoints, want) {
		t.Fatalf("endpoints = %#v, want %#v", result.Endpoints, want)
	}
}

func TestJSONPointerExamplesAreNotEndpoints(t *testing.T) {
	result := Analyze([]byte(`
		d = {path: z.string().describe("The JSON pointer to the field that failed (e.g. '/components/0/text')."), message: m};
		e = "A JSON pointer is a string", f = "/components/0/text";
		g = "JSON pointer"; fetch('/rest/api/2/issue');
		h = "the JSON pointer; see " + load('/api/v1/pointers');
	`))
	want := []string{"/api/v1/pointers", "/components/0/text", "/rest/api/2/issue"}
	if !reflect.DeepEqual(result.Endpoints, want) {
		t.Fatalf("endpoints = %#v, want %#v", result.Endpoints, want)
	}
}
