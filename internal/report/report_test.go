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
	for _, expected := range []string{"JSHound 0.1.0", "3 findings", "ENDPOINTS (1)", "URLS (1)", "SECRETS (1)", "kingfisher.test.1"} {
		if !strings.Contains(text, expected) {
			t.Errorf("missing %q in:\n%s", expected, text)
		}
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
