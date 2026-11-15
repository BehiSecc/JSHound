package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func mapServer(t *testing.T, requests *int) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ".map"):
			*requests++
			_, _ = w.Write([]byte(`{"version":3,"sources":["a.ts"],"mappings":""}`))
		case r.URL.Path == "/old/app.js":
			http.Redirect(w, r, "/static/app.js", http.StatusFound)
		default:
			_, _ = w.Write([]byte("fetch('/api/x');\n//# sourceMappingURL=app.js.map\n"))
		}
	}))
	t.Cleanup(server.Close)
	return server
}

func scan(t *testing.T, args ...string) string {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), append([]string{"--format", "jsonl"}, args...), Config{Version: "test", Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatalf("%v: exit=%d stderr=%s", args, code, stderr.String())
	}
	return stdout.String()
}

// A file under analysis cannot make a scan request a private address.
func TestScanDoesNotFollowMapsIntoPrivateNetworks(t *testing.T) {
	requests := 0
	server := mapServer(t, &requests)
	local := writeFile(t, filepath.Join(t.TempDir(), "app.js"), "fetch('/api/x');\n//# sourceMappingURL="+server.URL+"/internal.js.map\n")
	if out := scan(t, local); !strings.Contains(out, `"status":"unreachable"`) || requests != 0 {
		t.Fatalf("map on a private address was requested (%d):\n%s", requests, out)
	}
	// An input served from a private host may have its maps there too.
	if out := scan(t, "-u", server.URL+"/app.js"); !strings.Contains(out, `"status":"valid"`) || requests != 1 {
		t.Fatalf("map next to a private-host input was not checked (%d):\n%s", requests, out)
	}
}

func TestScanResolvesMapsAgainstTheRedirectedURL(t *testing.T) {
	requests := 0
	server := mapServer(t, &requests)
	out := scan(t, "-u", server.URL+"/old/app.js")
	if !strings.Contains(out, `"value":"`+server.URL+`/static/app.js.map","source":"`+server.URL+`/old/app.js","status":"valid"`) {
		t.Fatalf("map not resolved against the final URL:\n%s", out)
	}
}

func TestScanDoesNotBlockOnAPipeNamedAsMap(t *testing.T) {
	dir := t.TempDir()
	if err := syscall.Mkfifo(filepath.Join(dir, "app.js.map"), 0o600); err != nil {
		t.Skipf("mkfifo: %v", err)
	}
	app := writeFile(t, filepath.Join(dir, "app.js"), "fetch('/api/x');\n//# sourceMappingURL=app.js.map\n")
	done := make(chan string, 1)
	go func() { done <- scan(t, app) }()
	select {
	case out := <-done:
		if !strings.Contains(out, `"status":"invalid"`) && !strings.Contains(out, `"status":"unreachable"`) {
			t.Fatalf("pipe reported as a map:\n%s", out)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("scan blocked reading a pipe named as the source map")
	}
}
