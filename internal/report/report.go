package report

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Finding is the common representation emitted by JSHound's analyzers.
type Finding struct {
	Kind       string `json:"kind"`
	Value      string `json:"value"`
	Type       string `json:"type,omitempty"`
	Source     string `json:"source,omitempty"`
	Line       int    `json:"line,omitempty"`
	Rule       string `json:"rule,omitempty"`
	Status     string `json:"status,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Live       *bool  `json:"live,omitempty"`
	// WireID is the id an RPC method is registered under, when the code pairs one.
	WireID string `json:"wire_id,omitempty"`
	// Path is the path an RPC method was found through, as written in the code.
	Path string `json:"path,omitempty"`
}

// Summary contains totals by finding kind.
type Summary map[string]int

// Result is the stable machine-readable output contract.
type Result struct {
	Version  string    `json:"version"`
	Findings []Finding `json:"findings"`
	Summary  Summary   `json:"summary"`
}

// NewResult sorts and summarizes findings for deterministic output.
func NewResult(version string, findings []Finding) Result {
	// Match `sort -u` semantics within each category: retain one exact value
	// regardless of source, then order using bytewise lexical comparison.
	seen := make(map[string]int, len(findings))
	unique := make([]Finding, 0, len(findings))
	for _, finding := range findings {
		key := finding.Kind + "\x00" + finding.Value
		if at, exists := seen[key]; exists {
			// An RPC method keeps the wire id of whichever source pairs one,
			if finding.WireID != "" && unique[at].WireID == "" {
				unique[at].WireID = finding.WireID
			}
			// and the fullest of the paths it was found through.
			if fullerPath(finding.Path, unique[at].Path) {
				unique[at].Path = finding.Path
			}
			continue
		}
		seen[key] = len(unique)
		unique = append(unique, finding)
	}
	findings = unique
	sort.SliceStable(findings, func(i, j int) bool {
		if findings[i].Kind != findings[j].Kind {
			return findings[i].Kind < findings[j].Kind
		}
		if findings[i].Value != findings[j].Value {
			return findings[i].Value < findings[j].Value
		}
		return findings[i].Source < findings[j].Source
	})
	summary := make(Summary)
	for _, finding := range findings {
		summary[finding.Kind]++
	}
	return Result{Version: version, Findings: findings, Summary: summary}
}

func WriteJSON(w io.Writer, result Result) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(result)
}

func WriteJSONL(w io.Writer, result Result) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	for _, finding := range result.Findings {
		if err := encoder.Encode(finding); err != nil {
			return err
		}
	}
	return nil
}

type TextOptions struct {
	Color bool
}

var sectionOrder = []struct {
	Kind  string
	Title string
	Icon  string
}{
	{"secret", "SECRETS", "✦"},
	{"endpoint", "ENDPOINTS", "◆"},
	{"rpc_method", "RPC METHODS", "⇄"},
	{"url", "URLS", "◇"},
	{"graphql_operation", "GRAPHQL OPERATIONS", "◉"},
	{"parameter", "PARAMETERS", "◈"},
	{"file", "FILES", "▧"},
	{"email", "EMAILS", "@"},
	{"word", "WORDLIST", "#"},
	{"sourcemap", "SOURCE MAPS", "⌁"},
}

// WriteText renders a grouped terminal report. ANSI styling is opt-in so
// redirected output stays clean and easy to process.
func WriteText(w io.Writer, result Result, opts TextOptions) error {
	style := textStyle{enabled: opts.Color}
	total := len(result.Findings)

	header := "JSHound " + result.Version + "  │  " + plural(total, "finding")
	if total > 0 {
		header += "  │  " + strings.ReplaceAll(summaryLine(result.Summary), "  •  ", "  │  ")
	}
	if _, err := fmt.Fprintf(w, "\n%s\n%s\n", style.cyan(header), style.dim("────────────────────────────────────────────────────────────")); err != nil {
		return err
	}

	if total == 0 {
		_, err := fmt.Fprintf(w, "\n  %s No findings.\n", style.green("✓"))
		return err
	}

	renderedKinds := make(map[string]struct{}, len(sectionOrder))
	for _, section := range sectionOrder {
		renderedKinds[section.Kind] = struct{}{}
		items := findingsOfKind(result.Findings, section.Kind)
		if len(items) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(w, "\n%s %s %s\n", style.section(section.Icon), style.bold(section.Title), style.dim(fmt.Sprintf("(%d)", len(items)))); err != nil {
			return err
		}
		if _, err := fmt.Fprintf(w, "%s\n", style.dim("  "+strings.Repeat("─", min(52, len(section.Title)+8)))); err != nil {
			return err
		}
		for _, finding := range items {
			if _, err := fmt.Fprintf(w, "  %s %s%s%s\n", style.bullet("•"), statusBadge(style, finding), styleValue(style, finding), rpcDetail(style, finding)); err != nil {
				return err
			}
			if metadata := findingMetadata(finding); metadata != "" {
				if _, err := fmt.Fprintf(w, "    %s %s\n", style.dim("↳"), style.dim(metadata)); err != nil {
					return err
				}
			}
		}
	}
	other := make([]Finding, 0)
	for _, finding := range result.Findings {
		if _, ok := renderedKinds[finding.Kind]; !ok {
			other = append(other, finding)
		}
	}
	if len(other) > 0 {
		if _, err := fmt.Fprintf(w, "\n%s %s %s\n", style.section("·"), style.bold("OTHER"), style.dim(fmt.Sprintf("(%d)", len(other)))); err != nil {
			return err
		}
		for _, finding := range other {
			if _, err := fmt.Fprintf(w, "  %s %s\n", style.bullet("•"), styleValue(style, finding)); err != nil {
				return err
			}
		}
	}
	_, err := fmt.Fprintln(w)
	return err
}

func findingsOfKind(findings []Finding, kind string) []Finding {
	var result []Finding
	for _, finding := range findings {
		if finding.Kind == kind {
			result = append(result, finding)
		}
	}
	return result
}

func summaryLine(summary Summary) string {
	labels := map[string]string{"endpoint": "endpoints", "url": "URLs", "parameter": "parameters", "graphql_operation": "GraphQL", "rpc_method": "RPC methods", "secret": "secrets", "email": "emails", "file": "files", "sourcemap": "maps", "word": "words"}
	parts := make([]string, 0, len(sectionOrder))
	for _, section := range sectionOrder {
		if count := summary[section.Kind]; count > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", count, labels[section.Kind]))
		}
	}
	return strings.Join(parts, "  •  ")
}

func findingMetadata(f Finding) string {
	parts := make([]string, 0, 4)
	if f.Type != "" {
		parts = append(parts, f.Type)
	}
	if f.Rule != "" {
		parts = append(parts, f.Rule)
	}
	if f.Confidence != "" {
		parts = append(parts, f.Confidence+" confidence")
	}
	return strings.Join(parts, "  •  ")
}

// rpcDetail renders what an RPC method line adds to its name: the path when it
// says more than "/Service.Method" does (a package or a prefix), and the wire
// id, as "  (/$rpc/pkg.v1.Service/Method, id aB3xYz)".
func rpcDetail(style textStyle, finding Finding) string {
	parts := make([]string, 0, 2)
	if finding.Path != "" && finding.Path != "/"+finding.Value {
		parts = append(parts, finding.Path)
	}
	if finding.WireID != "" {
		parts = append(parts, "id "+finding.WireID)
	}
	if len(parts) == 0 {
		return ""
	}
	return "  " + style.dim("("+strings.Join(parts, ", ")+")")
}

// fullerPath reports whether path is the fuller spelling of an RPC path than
// other: the longer one, and the lexically smaller one when they are as long.
// It is the rule of analyzer.FullerRPCPath, for findings merged across sources.
func fullerPath(path, other string) bool {
	if path == "" {
		return false
	}
	if len(path) != len(other) {
		return len(path) > len(other)
	}
	return path < other
}

func statusBadge(style textStyle, finding Finding) string {
	if finding.Status == "" {
		return ""
	}
	status := strings.ToLower(finding.Status)
	label := strings.ToUpper(finding.Status)
	badge := "[" + label + "] "
	switch {
	case status == "valid" || strings.Contains(status, "active credential"):
		return style.green(badge)
	case status == "invalid" || strings.Contains(status, "inactive"):
		return style.wrap("31", badge)
	default:
		return style.yellow(badge)
	}
}

func styleValue(style textStyle, finding Finding) string {
	if finding.Kind == "secret" {
		return style.yellow(finding.Value)
	}
	if finding.Kind == "sourcemap" && finding.Status == "valid" {
		return style.green(finding.Value)
	}
	return style.value(finding.Value)
}

func plural(count int, word string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, word)
	}
	return fmt.Sprintf("%d %ss", count, word)
}

type textStyle struct{ enabled bool }

func (s textStyle) wrap(code, value string) string {
	if !s.enabled {
		return value
	}
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}
func (s textStyle) bold(v string) string    { return s.wrap("1", v) }
func (s textStyle) dim(v string) string     { return s.wrap("2", v) }
func (s textStyle) cyan(v string) string    { return s.wrap("36", v) }
func (s textStyle) green(v string) string   { return s.wrap("32", v) }
func (s textStyle) yellow(v string) string  { return s.wrap("33", v) }
func (s textStyle) section(v string) string { return s.wrap("1;36", v) }
func (s textStyle) bullet(v string) string  { return s.wrap("36", v) }
func (s textStyle) value(v string) string   { return s.wrap("97", v) }
