package sourcemap

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDiscoverAndResolve(t *testing.T) {
	data := []byte(`<script src=/assets/vendor.js.map></script>
//# sourceMappingURL="app.js.map"
/*# sourceMappingURL=https://cdn.example/map.js.map */`)
	refs := Discover(data, "https://example.com/static/app.js?v=1")
	want := []string{
		"https://example.com/static/app.js.map",
		"https://cdn.example/map.js.map",
		"https://example.com/assets/vendor.js.map",
	}
	if len(refs) != len(want) {
		t.Fatalf("references = %+v", refs)
	}
	for i := range want {
		if refs[i].URL != want[i] {
			t.Errorf("refs[%d].URL = %q, want %q", i, refs[i].URL, want[i])
		}
	}
}

func TestDiscoverDeduplicates(t *testing.T) {
	refs := Discover([]byte("//# sourceMappingURL=app.js.map\n'./app.js.map'"), "https://example.com/app.js")
	if len(refs) != 1 {
		t.Fatalf("references = %+v", refs)
	}
}

func TestCheckerReportsLiveValidMap(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"version":3,"sources":["src/app.ts"],"sourcesContent":["let x=1"],"mappings":"AAAA"}`))
	}))
	defer server.Close()

	result := NewChecker(CheckerOptions{MaxBytes: 1024}).Check(context.Background(), Reference{Raw: server.URL, URL: server.URL})
	if !result.Live || !result.Valid || result.Metadata == nil || result.Metadata.SourceCount != 1 || !result.Metadata.HasContent {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerSeparatesLiveFromValid(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`<html>not a map</html>`))
	}))
	defer server.Close()

	result := NewChecker(CheckerOptions{}).Check(context.Background(), Reference{URL: server.URL})
	if !result.Live || result.Valid || result.Error == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerSupportsInlineDataMap(t *testing.T) {
	payload := base64.StdEncoding.EncodeToString([]byte(`{"version":3,"sources":[],"mappings":""}`))
	value := "data:application/json;base64," + payload
	result := NewChecker(CheckerOptions{MaxBytes: 1024}).Check(context.Background(), Reference{Raw: value, URL: value})
	if !result.Live || !result.Valid {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestCheckerEnforcesSizeLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"version":3,"sources":[],"mappings":"AAAA"}`))
	}))
	defer server.Close()

	result := NewChecker(CheckerOptions{MaxBytes: 8}).Check(context.Background(), Reference{URL: server.URL})
	if !result.Live || result.Valid || result.Error == "" {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestValidateRequiresSourceMapShape(t *testing.T) {
	for _, data := range []string{
		`{"version":2,"sources":[],"mappings":""}`,
		`{"version":3,"sources":[]}`,
		`{"version":3,"sources":"app.js","mappings":""}`,
	} {
		if _, err := Validate([]byte(data)); err == nil {
			t.Errorf("Validate(%s) unexpectedly succeeded", data)
		}
	}
}
