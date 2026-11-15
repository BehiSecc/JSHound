package app

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/BehiSecc/JSHound/internal/analyzer"
	inputpkg "github.com/BehiSecc/JSHound/internal/input"
	"github.com/BehiSecc/JSHound/internal/overview"
	"github.com/BehiSecc/JSHound/internal/report"
	"github.com/BehiSecc/JSHound/internal/secrets"
	"github.com/BehiSecc/JSHound/internal/sourcemap"
	"github.com/BehiSecc/JSHound/internal/wordlist"
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
	output          string
	format          string
	maxBytesMB      int64
	timeout         time.Duration
	kingfisher      string
	overview        bool
	verbose         bool
	version         bool
	urls            stringList
	sourceMapTarget string
}

type stringList []string

func (s *stringList) String() string         { return strings.Join(*s, ",") }
func (s *stringList) Set(value string) error { *s = append(*s, value); return nil }

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
	flags.StringVar(&opts.output, "output", "", "write results to this file (default stdout)")
	flags.StringVar(&opts.format, "format", "text", "output format: text, json, or jsonl")
	flags.Int64Var(&opts.maxBytesMB, "max-size", 32, "maximum input or source-map size in MiB")
	flags.DurationVar(&opts.timeout, "timeout", 15*time.Second, "HTTP request and Kingfisher startup timeout")
	flags.StringVar(&opts.kingfisher, "kingfisher", "", "path to Kingfisher executable")
	flags.BoolVar(&opts.overview, "overview", false, "describe what each JavaScript file is and what it does")
	flags.BoolVar(&opts.verbose, "verbose", false, "with --overview, add structure, entry paths, and call flow")
	flags.BoolVar(&opts.version, "version", false, "print version and exit")
	flags.Var(&opts.urls, "u", "HTTP(S) URL to analyze (repeatable)")
	flags.StringVar(&opts.sourceMapTarget, "s", "", "JavaScript or source-map URL/path to extract")
	flags.StringVar(&opts.sourceMapTarget, "sourcemap", "", "JavaScript or source-map URL/path to extract")
	flags.Usage = func() {
		fmt.Fprintf(cfg.Stderr, "Usage: jshound [flags] [file|directory ...]\n       jshound [flags] -u URL\n\nFlags:\n")
		flags.PrintDefaults()
		fmt.Fprintln(cfg.Stderr, "\nWith no input, JSHound reads JavaScript or HTML from stdin.")
	}
	inputs, err := parseArgs(flags, args)
	if err != nil {
		return 2
	}
	if opts.version {
		fmt.Fprintln(cfg.Stdout, cfg.Version)
		return 0
	}
	if opts.maxBytesMB <= 0 || opts.maxBytesMB > maxSizeMB {
		fmt.Fprintf(cfg.Stderr, "jshound: --max-size must be between 1 and %d MiB\n", maxSizeMB)
		return 2
	}
	if opts.format != "text" && opts.format != "json" && opts.format != "jsonl" {
		fmt.Fprintln(cfg.Stderr, "jshound: --format must be text, json, or jsonl")
		return 2
	}
	if opts.overview {
		// Overview takes precedence over scan-only options.
		opts.parameters = false
		opts.wordlist = false
		opts.validateSecrets = false
		opts.kingfisher = ""
	}
	if opts.validateSecrets && opts.kingfisher == "" {
		fmt.Fprintln(cfg.Stderr, "jshound: --validate-secrets requires -kingfisher PATH")
		return 2
	}
	if opts.sourceMapTarget != "" {
		if len(inputs) != 0 {
			fmt.Fprintln(cfg.Stderr, "jshound: -s/--sourcemap accepts exactly one target")
			return 2
		}
		return runSourceMapMode(ctx, opts, cfg)
	}

	for _, value := range inputs {
		if inputpkg.IsURL(value) {
			fmt.Fprintf(cfg.Stderr, "jshound: URL inputs require -u: %s\n", value)
			return 2
		}
	}
	// An input that cannot be read is reported and skipped; the others are
	// still analyzed, and the exit status is 1 instead of 0.
	values, expandErrs := expandInputs(inputs)
	failed := len(expandErrs)
	for _, expandErr := range expandErrs {
		fmt.Fprintf(cfg.Stderr, "jshound: %v\n", expandErr)
	}
	values = append(values, opts.urls...)
	fetcher := inputpkg.NewFetcher(inputpkg.Options{MaxBytes: opts.maxBytesMB << 20, Timeout: opts.timeout})
	if len(inputs) == 0 && len(opts.urls) == 0 {
		values = []string{"-"}
	}

	var secretScanner *secrets.Scanner
	if opts.kingfisher != "" {
		secretCtx, cancel := context.WithTimeout(ctx, opts.timeout)
		secretScanner, err = secrets.New(secretCtx, secrets.Options{Binary: opts.kingfisher})
		cancel()
		if err != nil {
			fmt.Fprintf(cfg.Stderr, "jshound: secrets: %v\n", err)
			return 2
		}
	}

	checkers := newMapCheckers(opts)
	findings := make([]report.Finding, 0, 64)
	overviewSources := make([]overview.Source, 0, len(values))
	localSecretPaths := make([]string, 0, len(values))
	loaded := 0
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
			failed++
			continue
		}
		loaded++
		if opts.overview {
			item := overview.Source{Name: source.Name, Data: source.Data, SourceMap: source.SourceMap}
			item.MapName, item.Map = offlineSourceMap(ctx, checkers.any, source)
			overviewSources = append(overviewSources, item)
			continue
		}
		analysis := analyzer.AnalyzeWithOptions(source.Data, analyzer.Options{IncludeParameters: opts.parameters || opts.wordlist})
		findings = appendAnalysis(findings, source.Name, analysis, opts.parameters, opts.wordlist)
		checker := checkers.forSource(ctx, source)
		for _, reference := range sourcemap.Discover(source.Data, mapBase(source)) {
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
			if info, statErr := os.Stat(source.Name); source.Local && statErr == nil && info.Mode().IsRegular() {
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
	if loaded == 0 && failed > 0 {
		return 2
	}
	status := 0
	if failed > 0 {
		status = 1
	}
	if opts.overview {
		if code := writeOverview(opts, cfg, overview.Analyze(overviewSources)); code != 0 {
			return code
		}
		return status
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
	switch opts.format {
	case "json":
		err = report.WriteJSON(writer, result)
	case "jsonl":
		err = report.WriteJSONL(writer, result)
	default:
		err = report.WriteText(writer, result, report.TextOptions{Color: opts.output == "" && isTerminalWriter(cfg.Stdout)})
	}
	if closeWriter != nil {
		if closeErr := closeWriter(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		fmt.Fprintf(cfg.Stderr, "jshound: write output: %v\n", err)
		return 2
	}
	return status
}

// maxSizeMB bounds --max-size so its byte count cannot overflow (1 TiB).
const maxSizeMB = 1 << 20

// parseArgs parses flags anywhere on the command line, so "jshound app.js
// --overview" works like "jshound --overview app.js", and returns the inputs
// in order. Every argument after "--" is an input.
func parseArgs(flags *flag.FlagSet, args []string) ([]string, error) {
	var inputs []string
	for {
		if err := flags.Parse(args); err != nil {
			return nil, err
		}
		rest := flags.Args()
		if len(rest) == 0 {
			return inputs, nil
		}
		if consumed := len(args) - len(rest); consumed > 0 && args[consumed-1] == "--" {
			return append(inputs, rest...), nil
		}
		inputs = append(inputs, rest[0])
		args = rest[1:]
	}
}

func runSourceMapMode(ctx context.Context, opts options, cfg Config) int {
	if opts.output == "" || opts.output == "-" {
		fmt.Fprintln(cfg.Stderr, "jshound: -s/--sourcemap requires --output DIR")
		return 2
	}
	if opts.overview || opts.parameters || opts.wordlist || opts.validateSecrets || opts.kingfisher != "" {
		fmt.Fprintln(cfg.Stderr, "jshound: scan-only flags cannot be used with -s/--sourcemap")
		return 2
	}
	if info, err := os.Stat(opts.output); err == nil && !info.IsDir() {
		fmt.Fprintf(cfg.Stderr, "jshound: source-map output is not a directory: %s\n", opts.output)
		return 2
	}
	if len(opts.urls) > 0 {
		fmt.Fprintln(cfg.Stderr, "jshound: -s/--sourcemap accepts its URL directly; do not use -u")
		return 2
	}
	fetcher := inputpkg.NewFetcher(inputpkg.Options{MaxBytes: opts.maxBytesMB << 20, Timeout: opts.timeout})
	source, err := fetcher.Load(ctx, opts.sourceMapTarget)
	if err != nil {
		fmt.Fprintf(cfg.Stderr, "jshound: %v\n", err)
		return 2
	}
	// Extraction is an explicit request for this target's map, so the map is
	// followed wherever it is; scans check references only on public hosts.
	checker := newMapCheckers(opts).any

	mapName, mapData := source.Name, source.Data
	if _, validErr := sourcemap.Validate(source.Data); validErr != nil {
		refs := sourcemap.Discover(source.Data, mapBase(source))
		if source.SourceMap != "" {
			refs = append(refs, sourcemap.Reference{Raw: source.SourceMap, URL: sourcemap.Resolve(mapBase(source), source.SourceMap)})
		}
		if len(refs) == 0 {
			fmt.Fprintf(cfg.Stderr, "jshound: no source map found in %s\n", source.Name)
			return 2
		}
		var loaded bool
		for i := len(refs) - 1; i >= 0; i-- {
			data, result := checker.Load(ctx, refs[i])
			if !result.Valid {
				continue
			}
			mapName = refs[i].URL
			if mapName == "" {
				mapName = refs[i].Raw
			}
			mapData, loaded = data, true
			break
		}
		if !loaded {
			fmt.Fprintln(cfg.Stderr, "jshound: discovered source map is unavailable or invalid")
			return 2
		}
	}
	extracted, err := sourcemap.Extract(ctx, mapName, mapData, sourcemap.ExtractOptions{MaxBytes: opts.maxBytesMB << 20, Output: opts.output})
	if err != nil {
		fmt.Fprintf(cfg.Stderr, "jshound: extract source map: %v\n", err)
		return 2
	}
	fmt.Fprintf(cfg.Stdout, "Extracted %d source files to %s", len(extracted.Written), opts.output)
	if len(extracted.Skipped) > 0 {
		fmt.Fprintf(cfg.Stdout, " (%d skipped)", len(extracted.Skipped))
	}
	fmt.Fprintln(cfg.Stdout)
	if extracted.Truncated {
		fmt.Fprintf(cfg.Stderr, "jshound: stopped at %d files; the rest of the map was not extracted\n", sourcemap.MaxExtractedFiles)
	}
	if extracted.Failed > 0 {
		fmt.Fprintf(cfg.Stderr, "jshound: %d sources could not be written (first: %v)\n", extracted.Failed, extracted.FirstError)
	}
	return 0
}

// mapCheckers check source-map references found in analyzed content. In a
// scan, a reference to a loopback or private address is followed only when
// the input itself came from such a host, so JavaScript under analysis cannot
// make JSHound send requests into the network it runs on.
type mapCheckers struct {
	any, public *sourcemap.Checker
}

func newMapCheckers(opts options) mapCheckers {
	limits := sourcemap.CheckerOptions{MaxBytes: opts.maxBytesMB << 20, Timeout: opts.timeout}
	public := limits
	public.PublicOnly = true
	return mapCheckers{any: sourcemap.NewChecker(limits), public: sourcemap.NewChecker(public)}
}

func (c mapCheckers) forSource(ctx context.Context, source inputpkg.Source) *sourcemap.Checker {
	if source.URL != nil && !inputpkg.IsPublicHost(ctx, source.URL.Hostname()) {
		return c.any
	}
	return c.public
}

// mapBase is what a source's relative map references resolve against: the
// URL it was served from after redirects, or its local path.
func mapBase(source inputpkg.Source) string {
	if source.URL != nil {
		return source.URL.String()
	}
	return source.Name
}

// offlineSourceMap loads a source map for overview without network access:
// an inline data URL, or a regular map file in the input's directory tree.
func offlineSourceMap(ctx context.Context, checker *sourcemap.Checker, source inputpkg.Source) (string, []byte) {
	directive := sourceMapDirective(source.Data)
	if directive < 0 {
		return "", nil
	}
	refs := sourcemap.Discover(source.Data[directive:], source.Name)
	if len(refs) == 0 {
		return "", nil
	}
	ref := refs[len(refs)-1]
	target := ref.URL
	if target == "" {
		target = ref.Raw
	}
	if strings.HasPrefix(target, "data:") {
		if data, result := checker.Load(ctx, ref); result.Valid {
			return "inline source map", data
		}
		return "", nil
	}
	if !source.Local || strings.Contains(target, ":") || strings.HasPrefix(ref.Raw, "/") || !localMapFile(source.Name, target) {
		return "", nil
	}
	data, result := checker.Load(ctx, sourcemap.Reference{Raw: ref.Raw, URL: target})
	if !result.Valid {
		return "", nil
	}
	return target, data
}

// sourceMapDirective returns the offset of the last sourceMappingURL comment.
// Mentions inside strings or code are skipped.
func sourceMapDirective(data []byte) int {
	marker := []byte("sourceMappingURL")
	for end := len(data); ; {
		index := bytes.LastIndex(data[:end], marker)
		if index < 0 {
			return -1
		}
		if index >= 4 {
			switch string(data[index-4 : index]) {
			case "//# ", "//@ ", "/*# ", "/*@ ":
				return index - 4
			}
		}
		end = index
	}
}

// localMapFile keeps overview reads to plain files at or below the input's
// directory, so a crafted directive cannot point at devices or other trees.
func localMapFile(sourceName, target string) bool {
	base, err := filepath.Abs(filepath.Dir(sourceName))
	if err != nil {
		return false
	}
	path, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	if relative, err := filepath.Rel(base, path); err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func writeOverview(opts options, cfg Config, result overview.Result) int {
	result.Version = cfg.Version
	writer, closeWriter, err := outputWriter(opts.output, cfg.Stdout)
	if err != nil {
		fmt.Fprintf(cfg.Stderr, "jshound: %v\n", err)
		return 2
	}
	switch opts.format {
	case "json":
		err = overview.WriteJSON(writer, result)
	case "jsonl":
		err = overview.WriteJSONL(writer, result)
	default:
		terminal := opts.output == "" && isTTYWriter(cfg.Stdout)
		err = overview.WriteText(writer, result, overview.TextOptions{Terminal: terminal, Color: terminal && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb", Verbose: opts.verbose})
	}
	if closeWriter != nil {
		if closeErr := closeWriter(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		fmt.Fprintf(cfg.Stderr, "jshound: write overview: %v\n", err)
		return 2
	}
	return 0
}

func isTerminalWriter(writer io.Writer) bool {
	return isTTYWriter(writer) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
}

func isTTYWriter(writer io.Writer) bool {
	file, ok := writer.(*os.File)
	if !ok {
		return false
	}
	info, err := file.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

func appendSecrets(dst []report.Finding, values []secrets.Finding) []report.Finding {
	for _, item := range values {
		dst = append(dst, report.Finding{Kind: "secret", Value: item.Value, Type: item.RuleName, Source: item.Source, Line: item.Line, Rule: item.RuleID, Status: item.ValidationStatus, Confidence: item.Confidence})
	}
	return dst
}

func appendAnalysis(dst []report.Finding, source string, a analyzer.Result, includeParams, includeWords bool) []report.Finding {
	for kind, values := range map[string][]string{"endpoint": a.Endpoints, "url": a.URLs, "email": a.Emails, "file": a.Files, "graphql_operation": a.GraphQLOperations, "rpc_method": a.RPCMethods} {
		for _, value := range values {
			finding := report.Finding{Kind: kind, Value: value, Source: source}
			if kind == "rpc_method" {
				finding.WireID = a.RPCWireIDs[value]
				finding.Path = a.RPCPaths[value]
			}
			dst = append(dst, finding)
		}
	}
	if includeParams {
		for _, value := range a.Parameters {
			dst = append(dst, report.Finding{Kind: "parameter", Value: value, Source: source})
		}
	}
	if includeWords {
		for _, value := range wordlist.Build(slices.Concat(a.Endpoints, a.RPCSpellings), a.URLs, a.Files, a.Parameters, a.RPCMethods) {
			dst = append(dst, report.Finding{Kind: "word", Value: value, Source: source})
		}
	}
	return dst
}

// expandInputs lists the files to analyze: plain paths as given, and the
// JavaScript and HTML files below each directory. Paths that cannot be read
// are returned as errors and left out.
func expandInputs(values []string) ([]string, []error) {
	var result []string
	var errs []error
	for _, value := range values {
		if value == "-" {
			result = append(result, value)
			continue
		}
		info, err := os.Stat(value)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if !info.IsDir() {
			result = append(result, value)
			continue
		}
		err = filepath.WalkDir(value, func(path string, entry os.DirEntry, walkErr error) error {
			if walkErr != nil {
				errs = append(errs, walkErr)
				return nil
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
			errs = append(errs, err)
		}
	}
	return result, errs
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
