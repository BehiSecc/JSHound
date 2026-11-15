package report

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewResultSortsAndSummarizes(t *testing.T) {
	result := NewResult("dev", []Finding{{Kind: "url", Value: "z", Source: "one"}, {Kind: "endpoint", Value: "/a"}, {Kind: "url", Value: "a"}, {Kind: "url", Value: "z", Source: "two"}})
	if result.Summary["url"] != 2 || result.Findings[0].Kind != "endpoint" || result.Findings[1].Value != "a" {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestJSONDoesNotEscapeURLs(t *testing.T) {
	var output bytes.Buffer
	if err := WriteJSON(&output, NewResult("dev", []Finding{{Kind: "url", Value: "https://example.test/?a=1&b=2"}})); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), `\u0026`) {
		t.Fatalf("URL was HTML-escaped: %s", output.String())
	}
}

func TestTextReportIsGroupedAndClear(t *testing.T) {
	result := NewResult("0.1.0", []Finding{
		{Kind: "url", Value: "https://api.example.test/v1", Source: "app.js"},
		{Kind: "endpoint", Value: "/api/v1/users", Source: "app.js"},
		{Kind: "secret", Value: "[REDACTED:abcd]", Source: "app.js", Line: 9, Rule: "kingfisher.test.1", Status: "Not Attempted", Confidence: "high"},
	})
	var output bytes.Buffer
	if err := WriteText(&output, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	text := output.String()
	if !strings.HasPrefix(text, "\nJSHound 0.1.0") {
		t.Fatalf("text report does not start with a blank line: %q", text)
	}
	for _, expected := range []string{"JSHound 0.1.0", "3 findings", "ENDPOINTS (1)", "URLS (1)", "SECRETS (1)", "kingfisher.test.1"} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q in:\n%s", expected, text)
		}
	}
	if !strings.Contains(text, "JSHound 0.1.0  │  3 findings  │  1 secret  │  1 endpoint  │  1 URL") {
		t.Fatalf("unexpected single-line header:\n%s", text)
	}
	// Only a secret names its file: lists of endpoints and URLs stay clean,
	// but a secret is only actionable with its location.
	if strings.Count(text, "app.js") != 1 || !strings.Contains(text, "↳ app.js:9  •  kingfisher.test.1  •  high confidence") {
		t.Fatalf("text report source paths:\n%s", text)
	}
	if strings.Contains(text, "\x1b[") {
		t.Fatalf("non-color output contains ANSI escapes: %q", text)
	}
}

func TestTextReportEmptyState(t *testing.T) {
	var output bytes.Buffer
	if err := WriteText(&output, NewResult("dev", nil), TextOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "No findings") {
		t.Fatalf("missing empty state: %s", output.String())
	}
}

func TestRPCWireIDsAreShownNextToTheMethod(t *testing.T) {
	result := NewResult("dev", []Finding{
		{Kind: "rpc_method", Value: "Billing.Charge", Source: "a.js"},
		{Kind: "rpc_method", Value: "Billing.Charge", Source: "b.js", WireID: "aB3xYz"},
		{Kind: "rpc_method", Value: "Billing.Refund", Source: "a.js", WireID: "Qw3Rt"},
		{Kind: "rpc_method", Value: "Billing.Void", Source: "a.js"},
	})
	if len(result.Findings) != 3 || result.Findings[0].WireID != "aB3xYz" {
		t.Fatalf("an id from any source is kept: %#v", result.Findings)
	}
	var text bytes.Buffer
	if err := WriteText(&text, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"• Billing.Charge  (id aB3xYz)\n", "• Billing.Refund  (id Qw3Rt)\n", "• Billing.Void\n"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text lacks %q:\n%s", want, text.String())
		}
	}
	var data, lines bytes.Buffer
	if err := WriteJSON(&data, result); err != nil {
		t.Fatal(err)
	}
	if err := WriteJSONL(&lines, result); err != nil {
		t.Fatal(err)
	}
	if strings.Count(data.String(), `"wire_id"`) != 2 || !strings.Contains(data.String(), `"wire_id": "aB3xYz"`) {
		t.Errorf("json:\n%s", data.String())
	}
	if !strings.Contains(lines.String(), `"value":"Billing.Refund","source":"a.js","wire_id":"Qw3Rt"`) || strings.Count(lines.String(), "wire_id") != 2 {
		t.Errorf("jsonl:\n%s", lines.String())
	}
}

func TestRPCMethodsHaveTheirOwnSection(t *testing.T) {
	result := NewResult("dev", []Finding{
		{Kind: "rpc_method", Value: "OrderService.get", Source: "a.js"},
		{Kind: "rpc_method", Value: "OrderService.cancel", Source: "a.js"},
		{Kind: "rpc_method", Value: "OrderService.get", Source: "b.js"},
		{Kind: "graphql_operation", Value: "query Orders", Source: "a.js"},
		{Kind: "endpoint", Value: "/a/b", Source: "a.js"},
		{Kind: "parameter", Value: "id", Source: "a.js"},
	})
	if result.Summary["rpc_method"] != 2 {
		t.Fatalf("summary: %#v", result.Summary)
	}
	var text bytes.Buffer
	if err := WriteText(&text, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	out := text.String()
	if !strings.Contains(out, "JSHound dev  │  5 findings  │  1 endpoint  │  2 RPC methods  │  1 GraphQL operation  │  1 parameter") {
		t.Errorf("header:\n%s", out)
	}
	if strings.Contains(out, "OTHER") || !strings.Contains(out, "RPC METHODS (2)") {
		t.Errorf("section:\n%s", out)
	}
	// RPC methods sit directly below endpoints: both are the server's API.
	if strings.Index(out, "ENDPOINTS") > strings.Index(out, "RPC METHODS") || strings.Index(out, "RPC METHODS") > strings.Index(out, "GRAPHQL OPERATIONS") || strings.Index(out, "GRAPHQL OPERATIONS") > strings.Index(out, "PARAMETERS") {
		t.Errorf("section order:\n%s", out)
	}
	if strings.Index(out, "OrderService.cancel") > strings.Index(out, "OrderService.get") {
		t.Errorf("values are not sorted:\n%s", out)
	}
	var data bytes.Buffer
	if err := WriteJSON(&data, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(data.String(), `"rpc_method": 2`) || !strings.Contains(data.String(), `"kind": "rpc_method"`) {
		t.Errorf("json:\n%s", data.String())
	}
	var lines bytes.Buffer
	if err := WriteJSONL(&lines, result); err != nil {
		t.Fatal(err)
	}
	if strings.Count(lines.String(), `"kind":"rpc_method"`) != 2 {
		t.Errorf("jsonl:\n%s", lines.String())
	}
}

func TestRPCPathShownOnlyWhenItSaysMoreThanTheMethod(t *testing.T) {
	result := NewResult("dev", []Finding{
		{Kind: "rpc_method", Value: "Billing.Charge", Source: "a.js", Path: "/Billing.Charge", WireID: "aB3xYz"},
		{Kind: "rpc_method", Value: "Billing.Refund", Source: "a.js", Path: "/Billing.Refund"},
		{Kind: "rpc_method", Value: "Greeter.SayHello", Source: "a.js", Path: "/$rpc/acme.v1.Greeter/SayHello"},
		{Kind: "rpc_method", Value: "Ledger.Post", Source: "a.js", Path: "/acme.Ledger/Post", WireID: "Qw3Rt"},
		{Kind: "rpc_method", Value: "Orders.List", Source: "a.js"},
	})
	var text bytes.Buffer
	if err := WriteText(&text, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"• Billing.Charge  (id aB3xYz)\n",
		"• Billing.Refund\n",
		"• Greeter.SayHello  (/$rpc/acme.v1.Greeter/SayHello)\n",
		"• Ledger.Post  (/acme.Ledger/Post, id Qw3Rt)\n",
		"• Orders.List\n",
	} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text lacks %q:\n%s", want, text.String())
		}
	}
	var lines bytes.Buffer
	if err := WriteJSONL(&lines, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lines.String(), `"value":"Greeter.SayHello","source":"a.js","path":"/$rpc/acme.v1.Greeter/SayHello"`) || strings.Count(lines.String(), `"path"`) != 4 {
		t.Errorf("jsonl:\n%s", lines.String())
	}
}

func TestMergedRPCMethodKeepsTheFullestPath(t *testing.T) {
	result := NewResult("dev", []Finding{
		{Kind: "rpc_method", Value: "Billing.Charge", Source: "a.js"},
		{Kind: "rpc_method", Value: "Billing.Charge", Source: "b.js", Path: "/Billing.Charge"},
		{Kind: "rpc_method", Value: "Billing.Charge", Source: "c.js", Path: "/$rpc/acme.Billing/Charge"},
		{Kind: "rpc_method", Value: "Billing.Charge", Source: "d.js", Path: "/acme.Billing/Charge"},
	})
	if len(result.Findings) != 1 || result.Findings[0].Path != "/$rpc/acme.Billing/Charge" {
		t.Fatalf("got %#v", result.Findings)
	}
}

func TestMergedValuesKeepEverySource(t *testing.T) {
	result := NewResult("dev", []Finding{
		{Kind: "endpoint", Value: "/api/orders", Source: "b.js"},
		{Kind: "endpoint", Value: "/api/orders", Source: "a.js"},
		{Kind: "endpoint", Value: "/api/orders", Source: "b.js"},
		{Kind: "endpoint", Value: "/api/users", Source: "a.js"},
	})
	if len(result.Findings) != 2 || result.Summary["endpoint"] != 2 {
		t.Fatalf("values were not merged: %#v", result.Findings)
	}
	orders, users := result.Findings[0], result.Findings[1]
	if orders.Source != "b.js" || strings.Join(orders.Sources, ",") != "b.js,a.js" {
		t.Errorf("merged value lost a source: %#v", orders)
	}
	if users.Sources != nil {
		t.Errorf("single-source value lists sources: %#v", users)
	}
	var out bytes.Buffer
	if err := WriteJSONL(&out, result); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), `"source":"b.js","sources":["b.js","a.js"]`) || strings.Count(out.String(), `"sources"`) != 1 {
		t.Errorf("jsonl:\n%s", out.String())
	}
}

func TestSecretsAreNeverMerged(t *testing.T) {
	result := NewResult("dev", []Finding{
		{Kind: "secret", Value: "tok", Source: "a.js", Line: 3, Rule: "r1"},
		{Kind: "secret", Value: "tok", Source: "b.js", Line: 7, Rule: "r1"},
		{Kind: "secret", Value: "tok", Source: "a.js", Line: 3, Rule: "r2"},
		{Kind: "secret", Value: "tok", Source: "a.js", Line: 3, Rule: "r1"},
	})
	if result.Summary["secret"] != 3 {
		t.Fatalf("secrets: %#v", result.Findings)
	}
	for _, finding := range result.Findings {
		if finding.Sources != nil {
			t.Errorf("secret merged across sources: %#v", finding)
		}
	}
}

func TestTextEscapesControlCharacters(t *testing.T) {
	result := NewResult("dev", []Finding{
		{Kind: "url", Value: "https://a.test/\x1b]0;owned\x07"},
		{Kind: "secret", Value: "k\u009b2J", Source: "x\x1b.js", Line: 1, Type: "T\x1b[31m"},
	})
	var out bytes.Buffer
	if err := WriteText(&out, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	text := out.String()
	if strings.ContainsAny(text, "\x1b\x07\u009b") {
		t.Fatalf("control characters reached the terminal: %q", text)
	}
	for _, want := range []string{`https://a.test/\x1b]0;owned\x07`, `k\x9b2J`, `x\x1b.js:1`, `T\x1b[31m`} {
		if !strings.Contains(text, want) {
			t.Errorf("missing escaped %s in %q", want, text)
		}
	}
}
