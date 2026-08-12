package report

import (
	"bytes"
	"strings"
	"testing"
)

func TestNewResultSortsAndSummarizes(t *testing.T) {
	result := NewResult("dev", []Finding{{Kind: "url", Value: "z"}, {Kind: "endpoint", Value: "/a"}, {Kind: "url", Value: "a"}})
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
