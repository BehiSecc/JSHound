package secrets

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Cancelling a scan returns promptly and removes the temporary input, even
// when Kingfisher leaves a child process holding its output open.
func TestCancelledScanReturnsAndRemovesItsInput(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "input")
	binary := filepath.Join(dir, "kingfisher")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = --version ]; then echo 'kingfisher 1.113.0'; exit 0; fi\n" +
		"for last; do :; done\nprintf '%s' \"$last\" > '" + record + "'\n" +
		"sleep 30 &\nwait\n"
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	scanner, err := New(context.Background(), Options{Binary: binary})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		for i := 0; i < 100; i++ {
			if _, err := os.Stat(record); err == nil {
				break
			}
			time.Sleep(50 * time.Millisecond)
		}
		cancel()
	}()
	start := time.Now()
	if _, err := scanner.ScanContent(ctx, "stdin", []byte(`fetch("/api/x")`), false); err == nil {
		t.Fatal("cancelled scan succeeded")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Fatalf("cancelled scan took %s", elapsed)
	}
	input, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(input), "jshound-secret-") {
		t.Fatalf("unexpected input path %q", input)
	}
	if _, err := os.Stat(string(input)); !os.IsNotExist(err) {
		t.Fatalf("temporary input %s was left behind", input)
	}
}
