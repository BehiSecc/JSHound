package app

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jshound/jshound/internal/report"
)

func TestRunStdinWithoutSecrets(t *testing.T) {
	input := `<form action="/api/v1/users?role=admin"><input name="email"></form><script>query CurrentUser { viewer { id } }</script>`
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"--no-secrets", "-p", "--wordlist", "--format", "json"}, Config{Version: "test", Stdin: bytes.NewBufferString(input), Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("exit %d: %s", code, stderr.String())
	}
	var result report.Result
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"endpoint", "parameter", "graphql_operation", "word"} {
		if result.Summary[kind] == 0 {
			t.Errorf("missing %s: %#v", kind, result)
		}
	}
}

func TestRejectsInvalidFormat(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run(context.Background(), []string{"--format", "xml"}, Config{Version: "test", Stderr: &stderr}); code != 2 {
		t.Fatalf("exit %d", code)
	}
}

func TestPositionalURLRequiresU(t *testing.T) {
	var stderr bytes.Buffer
	code := Run(context.Background(), []string{"--no-secrets", "https://example.test/app.js"}, Config{Version: "test", Stderr: &stderr})
	if code != 2 || !strings.Contains(stderr.String(), "require -u") {
		t.Fatalf("exit=%d stderr=%q", code, stderr.String())
	}
}
