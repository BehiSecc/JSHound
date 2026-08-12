package input

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestFetcherFetch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("User-Agent"); got != "test-agent" {
			t.Errorf("User-Agent = %q", got)
		}
		w.Header().Set("Content-Type", "application/javascript")
		_, _ = w.Write([]byte("fetch('/api')"))
	}))
	defer server.Close()

	fetcher := NewFetcher(Options{MaxBytes: 1024, UserAgent: "test-agent"})
	source, err := fetcher.Fetch(context.Background(), server.URL+"/app.js")
	if err != nil {
		t.Fatal(err)
	}
	if string(source.Data) != "fetch('/api')" || source.StatusCode != http.StatusOK {
		t.Fatalf("unexpected source: %+v", source)
	}
}

func TestFetcherRejectsOversizedInputs(t *testing.T) {
	fetcher := NewFetcher(Options{MaxBytes: 4})
	if _, err := fetcher.Read("stdin", strings.NewReader("12345")); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("Read error = %v, want ErrTooLarge", err)
	}
	if _, err := fetcher.FromBytes("bytes", []byte("12345")); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("FromBytes error = %v, want ErrTooLarge", err)
	}
}

func TestFetcherReadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.js")
	if err := os.WriteFile(path, []byte("ok"), 0o600); err != nil {
		t.Fatal(err)
	}
	source, err := NewFetcher(Options{MaxBytes: 10}).Load(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	if source.Name != path || string(source.Data) != "ok" {
		t.Fatalf("unexpected source: %+v", source)
	}
}

func TestFetcherReportsHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	source, err := NewFetcher(Options{}).Fetch(context.Background(), server.URL)
	if err == nil || source.StatusCode != http.StatusNotFound {
		t.Fatalf("source=%+v err=%v", source, err)
	}
}

func TestFetcherTimeoutAppliesWithCustomClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(100 * time.Millisecond)
		_, _ = w.Write([]byte("late"))
	}))
	defer server.Close()

	fetcher := NewFetcher(Options{Timeout: 10 * time.Millisecond, Client: server.Client()})
	if _, err := fetcher.Fetch(context.Background(), server.URL); err == nil {
		t.Fatal("Fetch unexpectedly succeeded")
	}
}
