// Package secrets integrates JSHound with MongoDB Kingfisher's detection and
// validation engine. It deliberately delegates rule matching to Kingfisher so
// JSHound does not maintain a subtly different copy of its rules.
package secrets

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

const MinimumVersion = "1.110.0"

var (
	ErrUnavailable  = errors.New("kingfisher is not installed or not executable")
	ErrIncompatible = errors.New("kingfisher version is incompatible")
	versionPattern  = regexp.MustCompile(`(?m)\bkingfisher\s+v?([0-9]+)\.([0-9]+)\.([0-9]+)\b`)
)

// Options controls a Kingfisher scan. Validation is intentionally opt-in,
// because it can transmit discovered credentials to their providers.
type Options struct {
	Binary string
}

// Scanner is a checked Kingfisher executable.
type Scanner struct {
	binary  string
	version string
}

// Finding is the stable, secret-safe subset of a Kingfisher finding exposed by
// JSHound. Value is Kingfisher's hash-redacted value, never the raw credential.
type Finding struct {
	RuleID             string `json:"rule_id"`
	RuleName           string `json:"rule_name"`
	Value              string `json:"value"`
	Fingerprint        string `json:"fingerprint,omitempty"`
	Confidence         string `json:"confidence,omitempty"`
	ValidationStatus   string `json:"validation_status,omitempty"`
	ValidationResponse string `json:"validation_response,omitempty"`
	Language           string `json:"language,omitempty"`
	Source             string `json:"source"`
	Line               int    `json:"line,omitempty"`
	ColumnStart        int    `json:"column_start,omitempty"`
	ColumnEnd          int    `json:"column_end,omitempty"`
}

// New locates Kingfisher and verifies that its JSON/CLI contract is new enough
// for this adapter. Set Binary to use a specific executable.
func New(ctx context.Context, opts Options) (*Scanner, error) {
	binary := opts.Binary
	if binary == "" {
		var err error
		binary, err = exec.LookPath("kingfisher")
		if err != nil {
			return nil, fmt.Errorf("%w: install MongoDB Kingfisher >= %s and ensure it is on PATH", ErrUnavailable, MinimumVersion)
		}
	}

	out, err := exec.CommandContext(ctx, binary, "--version").CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%w: %q --version: %v", ErrUnavailable, binary, err)
	}
	version, ok := parseVersion(string(out))
	if !ok {
		return nil, fmt.Errorf("%w: could not parse %q", ErrIncompatible, strings.TrimSpace(string(out)))
	}
	if compareVersion(version, MinimumVersion) < 0 {
		return nil, fmt.Errorf("%w: found %s, need >= %s", ErrIncompatible, version, MinimumVersion)
	}
	return &Scanner{binary: binary, version: version}, nil
}

func (s *Scanner) Version() string { return s.version }

// ScanFile scans one regular file. The source path in results is the supplied
// path. Directories and repository history are deliberately outside this API.
func (s *Scanner) ScanFile(ctx context.Context, path string, validate bool) ([]Finding, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("inspect secret scan input: %w", err)
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("secret scan input is not a regular file: %s", path)
	}
	return s.scan(ctx, path, validate, "")
}

// ScanContent scans in-memory JS or HTML through a private temporary file and
// rewrites its generated path to source in returned findings.
func (s *Scanner) ScanContent(ctx context.Context, source string, content []byte, validate bool) ([]Finding, error) {
	ext := filepath.Ext(source)
	if ext != ".js" && ext != ".html" && ext != ".htm" && ext != ".mjs" && ext != ".cjs" {
		ext = ".js"
	}
	f, err := os.CreateTemp("", "jshound-secret-*"+ext)
	if err != nil {
		return nil, fmt.Errorf("create secret scan input: %w", err)
	}
	name := f.Name()
	defer os.Remove(name)
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return nil, fmt.Errorf("secure secret scan input: %w", err)
	}
	if _, err := f.Write(content); err != nil {
		f.Close()
		return nil, fmt.Errorf("write secret scan input: %w", err)
	}
	if err := f.Close(); err != nil {
		return nil, fmt.Errorf("close secret scan input: %w", err)
	}
	return s.scan(ctx, name, validate, source)
}

func (s *Scanner) scan(ctx context.Context, path string, validate bool, sourceOverride string) ([]Finding, error) {
	args := []string{"--no-update-check", "-q", "scan", "--rule", "all", "--no-base64", "--redact", "--commit-metadata=false", "--format", "json"}
	if !validate {
		args = append(args, "--no-validate")
	}
	args = append(args, path)
	cmd := exec.CommandContext(ctx, s.binary, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	if err != nil && !acceptedExitCode(err) {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		message := strings.TrimSpace(stderr.String())
		if message == "" {
			message = strings.TrimSpace(stdout.String())
		}
		return nil, fmt.Errorf("kingfisher scan failed: %v: %s", err, message)
	}

	var report kingfisherReport
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		return nil, fmt.Errorf("decode kingfisher JSON: %w", err)
	}
	findings := make([]Finding, 0, len(report.Findings))
	for _, item := range report.Findings {
		source := item.Finding.Path
		if sourceOverride != "" {
			source = sourceOverride
		}
		findings = append(findings, Finding{
			RuleID: item.Rule.ID, RuleName: item.Rule.Name,
			Value: item.Finding.Snippet, Fingerprint: item.Finding.Fingerprint,
			Confidence:         item.Finding.Confidence,
			ValidationStatus:   item.Finding.Validation.Status,
			ValidationResponse: item.Finding.Validation.Response,
			Language:           item.Finding.Language, Source: source,
			Line: item.Finding.Line, ColumnStart: item.Finding.ColumnStart,
			ColumnEnd: item.Finding.ColumnEnd,
		})
	}
	return findings, nil
}

type kingfisherReport struct {
	Findings []struct {
		Rule struct {
			Name string `json:"name"`
			ID   string `json:"id"`
		} `json:"rule"`
		Finding struct {
			Snippet     string `json:"snippet"`
			Fingerprint string `json:"fingerprint"`
			Confidence  string `json:"confidence"`
			Validation  struct {
				Status   string `json:"status"`
				Response string `json:"response"`
			} `json:"validation"`
			Language    string `json:"language"`
			Line        int    `json:"line"`
			ColumnStart int    `json:"column_start"`
			ColumnEnd   int    `json:"column_end"`
			Path        string `json:"path"`
		} `json:"finding"`
	} `json:"findings"`
}

func acceptedExitCode(err error) bool {
	var exitErr *exec.ExitError
	return errors.As(err, &exitErr) && (exitErr.ExitCode() == 200 || exitErr.ExitCode() == 205)
}

func parseVersion(output string) (string, bool) {
	m := versionPattern.FindStringSubmatch(output)
	if len(m) != 4 {
		return "", false
	}
	return strings.Join(m[1:], "."), true
}

func compareVersion(a, b string) int {
	ap, bp := strings.Split(a, "."), strings.Split(b, ".")
	for i := 0; i < 3; i++ {
		ai, _ := strconv.Atoi(ap[i])
		bi, _ := strconv.Atoi(bp[i])
		if ai < bi {
			return -1
		}
		if ai > bi {
			return 1
		}
	}
	return 0
}
