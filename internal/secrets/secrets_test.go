package secrets

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewChecksVersion(t *testing.T) {
	t.Parallel()
	scanner, err := New(context.Background(), Options{Binary: fakeKingfisher(t)})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := scanner.Version(), "1.110.0"; got != want {
		t.Fatalf("Version() = %q, want %q", got, want)
	}
}

func TestNewRejectsMissingBinary(t *testing.T) {
	t.Parallel()
	_, err := New(context.Background(), Options{Binary: filepath.Join(t.TempDir(), "missing")})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("New() error = %v, want ErrUnavailable", err)
	}
}

func TestNewRejectsOldVersion(t *testing.T) {
	t.Setenv("KF_FAKE_VERSION", "1.109.9")
	_, err := New(context.Background(), Options{Binary: fakeKingfisher(t)})
	if !errors.Is(err, ErrIncompatible) {
		t.Fatalf("New() error = %v, want ErrIncompatible", err)
	}
}

func TestScanContentUsesSafeFullCoverageArgumentsAndParsesFinding(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("KF_ARGS_FILE", argsFile)
	t.Setenv("KF_FAKE_EXIT", "200")
	scanner, err := New(context.Background(), Options{Binary: fakeKingfisher(t)})
	if err != nil {
		t.Fatal(err)
	}

	findings, err := scanner.ScanContent(context.Background(), "https://example.test/app.js", []byte("placeholder"), false)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("got %d findings, want 1", len(findings))
	}
	finding := findings[0]
	if finding.RuleID != "kingfisher.test.1" || finding.Source != "https://example.test/app.js" {
		t.Fatalf("unexpected finding: %#v", finding)
	}
	if finding.Value != "[REDACTED:deadbeef]" || finding.ValidationStatus != "Not Attempted" {
		t.Fatalf("unsafe or malformed finding: %#v", finding)
	}

	argsBytes, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	args := string(argsBytes)
	for _, required := range []string{"--rule\nall\n", "--no-base64\n", "--redact\n", "--no-validate\n", "--format\njson\n"} {
		if !strings.Contains(args, required) {
			t.Errorf("arguments lack %q:\n%s", required, args)
		}
	}
	if strings.Contains(args, "--turbo") {
		t.Errorf("arguments unexpectedly enable turbo: %s", args)
	}
}

func TestScanContentEnablesValidation(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("KF_ARGS_FILE", argsFile)
	scanner, err := New(context.Background(), Options{Binary: fakeKingfisher(t)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.ScanContent(context.Background(), "page.html", nil, true); err != nil {
		t.Fatal(err)
	}
	argsBytes, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(argsBytes), "--no-validate\n") {
		t.Fatalf("validation scan contains --no-validate:\n%s", argsBytes)
	}
}

func TestScanFilesUsesOneProcessWithAllPaths(t *testing.T) {
	argsFile := filepath.Join(t.TempDir(), "args")
	t.Setenv("KF_ARGS_FILE", argsFile)
	dir := t.TempDir()
	first, second := filepath.Join(dir, "a.js"), filepath.Join(dir, "b.html")
	for _, path := range []string{first, second} {
		if err := os.WriteFile(path, []byte("placeholder"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	scanner, err := New(context.Background(), Options{Binary: fakeKingfisher(t)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := scanner.ScanFiles(context.Background(), []string{first, second}, false); err != nil {
		t.Fatal(err)
	}
	args, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{first, second} {
		if !strings.Contains(string(args), path) {
			t.Errorf("arguments lack %s: %s", path, args)
		}
	}
}

func TestScanFailureIncludesDiagnostic(t *testing.T) {
	t.Setenv("KF_FAKE_FAIL", "1")
	scanner, err := New(context.Background(), Options{Binary: fakeKingfisher(t)})
	if err != nil {
		t.Fatal(err)
	}
	_, err = scanner.ScanContent(context.Background(), "page.js", nil, false)
	if err == nil || !strings.Contains(err.Error(), "fake failure") {
		t.Fatalf("ScanContent() error = %v, want diagnostic", err)
	}
}

func fakeKingfisher(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "kingfisher")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  echo "kingfisher ${KF_FAKE_VERSION:-1.110.0}"
  exit 0
fi
if [ -n "$KF_ARGS_FILE" ]; then
  printf '%s\n' "$@" > "$KF_ARGS_FILE"
fi
if [ -n "$KF_FAKE_FAIL" ]; then
  echo "fake failure" >&2
  exit 2
fi
cat <<'EOF'
{"findings":[{"rule":{"name":"Test Token","id":"kingfisher.test.1"},"finding":{"snippet":"[REDACTED:deadbeef]","fingerprint":"1234","confidence":"high","validation":{"status":"Not Attempted","response":"[REDACTED:none]"},"language":"JavaScript","line":7,"column_start":9,"column_end":24,"path":"/tmp/generated.js"}}]}
EOF
exit "${KF_FAKE_EXIT:-0}"
`
	if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	return path
}
