package input

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsPublicHost(t *testing.T) {
	ctx := context.Background()
	for _, host := range []string{"localhost", "app.localhost", "LOCALHOST.", "127.0.0.1", "10.1.2.3", "172.16.0.1", "192.168.1.1", "169.254.169.254", "100.100.100.200", "0.0.0.0", "0.1.2.3", "::1", "::", "fe80::1", "fd00::1", "::ffff:127.0.0.1", "::ffff:169.254.169.254"} {
		if IsPublicHost(ctx, host) {
			t.Errorf("%s is treated as public", host)
		}
	}
	for _, host := range []string{"8.8.8.8", "1.1.1.1", "2606:4700:4700::1111"} {
		if !IsPublicHost(ctx, host) {
			t.Errorf("%s is treated as non-public", host)
		}
	}
}

func TestPublicOnlyFetcherRefusesNonPublicHosts(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests++ }))
	defer server.Close()
	_, err := NewFetcher(Options{PublicOnly: true}).Fetch(context.Background(), server.URL+"/app.js.map")
	if !errors.Is(err, ErrNonPublic) || requests != 0 {
		t.Fatalf("err=%v requests=%d", err, requests)
	}
	if _, err := NewFetcher(Options{}).Fetch(context.Background(), server.URL+"/app.js.map"); err != nil || requests != 1 {
		t.Fatalf("default fetcher: err=%v requests=%d", err, requests)
	}
}

func TestFetchNamesDropCredentialsAndURLFollowsRedirects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/old.js" {
			http.Redirect(w, r, "/static/new.js", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("ok"))
	}))
	defer server.Close()
	withCredentials := strings.Replace(server.URL, "http://", "http://user:secret@", 1) + "/old.js"
	source, err := NewFetcher(Options{}).Fetch(context.Background(), withCredentials)
	if err != nil {
		t.Fatal(err)
	}
	if source.Name != server.URL+"/old.js" || strings.Contains(source.URL.String(), "secret") || source.URL.Path != "/static/new.js" {
		t.Fatalf("name=%q url=%q", source.Name, source.URL)
	}
}

func TestIsURL(t *testing.T) {
	for value, want := range map[string]bool{"http://a": true, "HTTPS://a": true, "Http://a": true, "ftp://a": false, "app.js": false, "http": false, "": false} {
		if got := IsURL(value); got != want {
			t.Errorf("IsURL(%q)=%v", value, got)
		}
	}
}
