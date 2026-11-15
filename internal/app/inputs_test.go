package app

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BehiSecc/JSHound/internal/report"
)

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestFlagsMayFollowInputs(t *testing.T) {
	dir := t.TempDir()
	app := writeFile(t, filepath.Join(dir, "app.js"), `fetch("/api/orders")`)
	dash := writeFile(t, filepath.Join(dir, "-dash.js"), `fetch("/api/dash")`)
	t.Chdir(dir)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{app, "--format", "json", "--", "-dash.js"}, Config{Version: "test", Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	var result report.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("flag after input was not parsed: %v\n%s", err, stdout.String())
	}
	if result.Summary["endpoint"] != 2 {
		t.Fatalf("an input after -- was not analyzed (%s): %#v", dash, result.Findings)
	}
}

func TestUnreadableInputIsReportedAndTheRestAnalyzed(t *testing.T) {
	dir := t.TempDir()
	app := writeFile(t, filepath.Join(dir, "app.js"), `fetch("/api/orders")`)
	big := writeFile(t, filepath.Join(dir, "big.js"), strings.Repeat("x", 2<<20))
	missing := filepath.Join(dir, "missing.js")
	for _, overview := range []bool{false, true} {
		args := []string{"--max-size", "1", "--format", "json", app, missing, big}
		if overview {
			args = append([]string{"--overview"}, args...)
		}
		var stdout, stderr bytes.Buffer
		code := Run(context.Background(), args, Config{Version: "test", Stdin: strings.NewReader("unused"), Stdout: &stdout, Stderr: &stderr})
		if code != 1 {
			t.Fatalf("overview=%v exit=%d, want 1; stderr=%s", overview, code, stderr.String())
		}
		if !strings.Contains(stderr.String(), "missing.js") || !strings.Contains(stderr.String(), "big.js") {
			t.Errorf("overview=%v failures not reported: %s", overview, stderr.String())
		}
		if !strings.Contains(stdout.String(), "app.js") && !strings.Contains(stdout.String(), "/api/orders") {
			t.Errorf("overview=%v readable input missing from output: %s", overview, stdout.String())
		}
	}
}

func TestNoReadableInputIsAnErrorWithoutOutput(t *testing.T) {
	var stdout, stderr bytes.Buffer
	stdin := strings.NewReader(`fetch("/api/from-stdin")`)
	code := Run(context.Background(), []string{filepath.Join(t.TempDir(), "missing.js")}, Config{Version: "test", Stdin: stdin, Stdout: &stdout, Stderr: &stderr})
	if code != 2 || stdout.Len() != 0 {
		t.Fatalf("exit=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if stdin.Len() == 0 {
		t.Fatal("a missing input fell back to reading stdin")
	}
}

func TestDirectoryWithUnreadableEntryIsStillScanned(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can read any directory")
	}
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app.js"), `fetch("/api/orders")`)
	locked := filepath.Join(dir, "locked")
	if err := os.Mkdir(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(locked, "hidden.js"), `fetch("/api/hidden")`)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o700) })
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--format", "jsonl", dir}, Config{Version: "test", Stdout: &stdout, Stderr: &stderr})
	if code != 1 || !strings.Contains(stdout.String(), "/api/orders") || !strings.Contains(stderr.String(), "locked") {
		t.Fatalf("exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
}

func TestPositionalURLRequiresUInAnyCase(t *testing.T) {
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"HTTPS://example.test/app.js"}, Config{Version: "test", Stdout: &bytes.Buffer{}, Stderr: &stderr})
	if code != 2 || !strings.Contains(stderr.String(), "require -u") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

func TestMaxSizeIsBounded(t *testing.T) {
	for _, value := range []string{"0", "-1", "9223372036854775807"} {
		var stderr bytes.Buffer
		code := Run(context.Background(), []string{"--max-size", value}, Config{Version: "test", Stdin: strings.NewReader("x"), Stdout: &bytes.Buffer{}, Stderr: &stderr})
		if code != 2 || !strings.Contains(stderr.String(), "--max-size must be between") {
			t.Errorf("--max-size %s: exit=%d stderr=%q", value, code, stderr.String())
		}
	}
}

func TestOutputCloseErrorFailsTheRun(t *testing.T) {
	if _, err := os.Stat("/dev/full"); err != nil {
		t.Skip("no /dev/full")
	}
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"--output", "/dev/full"}, Config{Version: "test", Stdin: strings.NewReader(`fetch("/api/x")`), Stdout: &bytes.Buffer{}, Stderr: &stderr})
	if code != 2 || !strings.Contains(stderr.String(), "write output") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}

// A local file named "stdin" must not stand in for piped input.
func TestPipedInputIsScannedForSecretsNotAFileNamedStdin(t *testing.T) {
	dir := t.TempDir()
	record := filepath.Join(dir, "scanned")
	binary := writeFile(t, filepath.Join(dir, "kingfisher"), "#!/bin/sh\n"+
		"if [ \"$1\" = --version ]; then echo 'kingfisher 1.113.0'; exit 0; fi\n"+
		"for last; do :; done\nprintf '%s' \"$last\" > '"+record+"'\necho '{\"findings\":[]}'\n")
	if err := os.Chmod(binary, 0o700); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "stdin"), "decoy")
	t.Chdir(dir)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--kingfisher", binary}, Config{Version: "test", Stdin: strings.NewReader(`fetch("/api/x")`), Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("exit=%d stderr=%s", code, stderr.String())
	}
	scanned, err := os.ReadFile(record)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(filepath.Base(string(scanned)), "jshound-secret-") {
		t.Fatalf("Kingfisher scanned %q instead of the piped input", scanned)
	}
}
