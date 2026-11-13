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
	if !strings.Contains(text, "JSHound 0.1.0  │  3 findings  │  1 secrets  │  1 endpoints  │  1 URLs") {
		t.Fatalf("unexpected single-line header:\n%s", text)
	}
	if strings.Contains(text, "app.js") {
		t.Fatalf("text report includes source path:\n%s", text)
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
	if !strings.Contains(out, "JSHound dev  │  5 findings  │  1 endpoints  │  1 GraphQL  │  2 RPC methods  │  1 parameters") {
		t.Errorf("header:\n%s", out)
	}
	if strings.Contains(out, "OTHER") || !strings.Contains(out, "RPC METHODS (2)") {
		t.Errorf("section:\n%s", out)
	}
	if strings.Index(out, "GRAPHQL OPERATIONS") > strings.Index(out, "RPC METHODS") || strings.Index(out, "RPC METHODS") > strings.Index(out, "PARAMETERS") {
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
