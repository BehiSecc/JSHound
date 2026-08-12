package app

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/jshound/jshound/internal/analyzer"
	inputpkg "github.com/jshound/jshound/internal/input"
	"github.com/jshound/jshound/internal/report"
	"github.com/jshound/jshound/internal/secrets"
	"github.com/jshound/jshound/internal/sourcemap"
	"github.com/jshound/jshound/internal/wordlist"
)

type Config struct {
	Version string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
}

type options struct {
	validateSecrets bool
	parameters      bool
	wordlist        bool
	noSecrets       bool
	output          string
	format          string
	maxBytesMB      int64
	timeout         time.Duration
	kingfisher      string
	version         bool
}

// Run executes the JSHound CLI and returns a process exit code.
func Run(ctx context.Context, args []string, cfg Config) int {
	if cfg.Stdin == nil {
		cfg.Stdin = os.Stdin
	}
	if cfg.Stdout == nil {
		cfg.Stdout = os.Stdout
	}
	if cfg.Stderr == nil {
		cfg.Stderr = os.Stderr
	}

	flags := flag.NewFlagSet("jshound", flag.ContinueOnError)
	flags.SetOutput(cfg.Stderr)
	opts := options{}
	flags.BoolVar(&opts.validateSecrets, "validate-secrets", false, "validate discovered secrets using provider APIs")
	flags.BoolVar(&opts.parameters, "p", false, "include parameter findings")
	flags.BoolVar(&opts.wordlist, "wordlist", false, "include a target-specific wordlist")
	flags.BoolVar(&opts.noSecrets, "no-secrets", false, "skip Kingfisher secret scanning")
	flags.StringVar(&opts.output, "output", "", "write results to this file (default stdout)")
	flags.StringVar(&opts.format, "format", "text", "output format: text, json, or jsonl")
	flags.Int64Var(&opts.maxBytesMB, "max-size", 32, "maximum input or source-map size in MiB")
	flags.DurationVar(&opts.timeout, "timeout", 15*time.Second, "HTTP request and Kingfisher startup timeout")
	flags.StringVar(&opts.kingfisher, "kingfisher", "", "path to Kingfisher executable")
	flags.BoolVar(&opts.version, "version", false, "print version and exit")
	flags.Usage = func() {
		fmt.Fprintf(cfg.Stderr, "Usage: jshound [flags] [file|directory|URL ...]\n\nFlags:\n")
		flags.PrintDefaults()
		fmt.Fprintln(cfg.Stderr, "\nWith no input, JSHound reads JavaScript or HTML from stdin.")
	}
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if opts.version {
		fmt.Fprintln(cfg.Stdout, cfg.Version)
		return 0
	}
	if opts.maxBytesMB <= 0 {
		fmt.Fprintln(cfg.Stderr, "jshound: --max-size must be positive")
		return 2
	}
	if opts.format != "text" && opts.format != "json" && opts.format != "jsonl" {
		fmt.Fprintln(cfg.Stderr, "jshound: --format must be text, json, or jsonl")
		return 2
	}
	if opts.noSecrets && opts.validateSecrets {
		fmt.Fprintln(cfg.Stderr, "jshound: --no-secrets and --validate-secrets cannot be used together")
		return 2
	}

	values, err := expandInputs(flags.Args())
	if err != nil {
		fmt.Fprintf(cfg.Stderr, "jshound: %v\n", err)
		return 2
	}
	fetcher := inputpkg.NewFetcher(inputpkg.Options{MaxBytes: opts.maxBytesMB << 20, Timeout: opts.timeout})
	if len(values) == 0 {
		values = []string{"-"}
	}

	var secretScanner *secrets.Scanner
	if !opts.noSecrets {
		secretCtx, cancel := context.WithTimeout(ctx, opts.timeout)
		secretScanner, err = secrets.New(secretCtx, secrets.Options{Binary: opts.kingfisher})
		cancel()
		if err != nil {
			fmt.Fprintf(cfg.Stderr, "jshound: secrets: %v (use --no-secrets to skip)\n", err)
			return 2
		}
	}

	checker := sourcemap.NewChecker(sourcemap.CheckerOptions{MaxBytes: opts.maxBytesMB << 20, Timeout: opts.timeout})
	findings := make([]report.Finding, 0, 64)
	localSecretPaths := make([]string, 0, len(values))
	for _, value := range values {
		var source inputpkg.Source
		var loadErr error
		if value == "-" {
			source, loadErr = fetcher.Read("stdin", cfg.Stdin)
		} else {
			source, loadErr = fetcher.Load(ctx, value)
		}
		if loadErr != nil {
			fmt.Fprintf(cfg.Stderr, "jshound: %v\n", loadErr)
			return 2
		}
		analysis := analyzer.Analyze(source.Data)
		findings = appendAnalysis(findings, source.Name, analysis, opts.parameters, opts.wordlist)
		for _, reference := range sourcemap.Discover(source.Data, source.Name) {
			checkCtx, cancel := context.WithTimeout(ctx, opts.timeout)
			result := checker.Check(checkCtx, reference)
			cancel()
			status := "invalid"
			if result.Valid {
				status = "valid"
			} else if !result.Live {
				status = "unreachable"
			}
			live := result.Live
			value := reference.URL
			if value == "" {
				value = reference.Raw
			}
			findings = append(findings, report.Finding{Kind: "sourcemap", Value: value, Source: source.Name, Status: status, Live: &live})
		}
		if secretScanner != nil {
			if info, statErr := os.Stat(source.Name); statErr == nil && info.Mode().IsRegular() {
				localSecretPaths = append(localSecretPaths, source.Name)
			} else {
				items, scanErr := secretScanner.ScanContent(ctx, source.Name, source.Data, opts.validateSecrets)
				if scanErr != nil {
					fmt.Fprintf(cfg.Stderr, "jshound: secrets in %s: %v\n", source.Name, scanErr)
					return 2
				}
				findings = appendSecrets(findings, items)
			}
		}
	}
	if secretScanner != nil {
		if len(localSecretPaths) > 0 {
			items, scanErr := secretScanner.ScanFiles(ctx, localSecretPaths, opts.validateSecrets)
			if scanErr != nil {
				fmt.Fprintf(cfg.Stderr, "jshound: secrets: %v\n", scanErr)
				return 2
			}
			findings = appendSecrets(findings, items)
		}
	}

	result := report.NewResult(cfg.Version, deduplicate(findings))
	writer, closeWriter, err := outputWriter(opts.output, cfg.Stdout)
	if err != nil {
		fmt.Fprintf(cfg.Stderr, "jshound: %v\n", err)
		return 2
	}
	if closeWriter != nil {
		defer closeWriter()
	}
	switch opts.format {
	case "json":
		err = report.WriteJSON(writer, result)
	case "jsonl":
		err = report.WriteJSONL(writer, result)
	default:
		err = report.WriteText(writer, result)
	}
	if err != nil {
		fmt.Fprintf(cfg.Stderr, "jshound: write output: %v\n", err)
		return 2
	}
	return 0
}

func appendSecrets(dst []report.Finding, values []secrets.Finding) []report.Finding {
	for _, item := range values {
		dst = append(dst, report.Finding{Kind: "secret", Value: item.Value, Source: item.Source, Line: item.Line, Rule: item.RuleID, Status: item.ValidationStatus, Confidence: item.Confidence})
	}
	return dst
}

func appendAnalysis(dst []report.Finding, source string, a analyzer.Result, includeParams, includeWords bool) []report.Finding {
	for kind, values := range map[string][]string{"endpoint": a.Endpoints, "url": a.URLs, "email": a.Emails, "file": a.Files, "graphql_operation": a.GraphQLOperations} {
		for _, value := range values {
			dst = append(dst, report.Finding{Kind: kind, Value: value, Source: source})
		}
	}
	if includeParams {
		for _, value := range a.Parameters {
			dst = append(dst, report.Finding{Kind: "parameter", Value: value, Source: source})
		}
	}
	if includeWords {
		for _, value := range wordlist.Build(a.Endpoints, a.URLs, a.Files, a.Parameters) {
			dst = append(dst, report.Finding{Kind: "word", Value: value, Source: source})
		}
	}
	return dst
}

func expandInputs(values []string) ([]string, error) {
	var result []string
	for _, value := range values {
		if value == "-" || strings.HasPrefix(value, "http://") || strings.HasPrefix(value, "https://") {
			result = append(result, value)
			continue
		}
		info, err := os.Stat(value)
		if err != nil {
			return nil, err
		}
		if !info.IsDir() {
			result = append(result, value)
			continue
		}
		err = filepath.WalkDir(value, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() {
				if entry.Name() == ".git" || entry.Name() == "node_modules" {
					return filepath.SkipDir
				}
				return nil
			}
			switch strings.ToLower(filepath.Ext(path)) {
			case ".js", ".mjs", ".cjs", ".jsx", ".ts", ".tsx", ".html", ".htm":
				result = append(result, path)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func deduplicate(values []report.Finding) []report.Finding {
	seen := make(map[string]struct{}, len(values))
	result := make([]report.Finding, 0, len(values))
	for _, value := range values {
		key := strings.Join([]string{value.Kind, value.Value, value.Source, value.Rule}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, value)
	}
	return result
}

func outputWriter(path string, fallback io.Writer) (io.Writer, func() error, error) {
	if path == "" || path == "-" {
		return fallback, nil, nil
	}
	file, err := os.Create(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open output: %w", err)
	}
	return file, file.Close, nil
}
