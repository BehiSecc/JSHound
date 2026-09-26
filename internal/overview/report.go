package overview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"unicode"
)

type TextOptions struct{ Color bool }

func WriteJSON(w io.Writer, result Result) error {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	encoder.SetEscapeHTML(false)
	return encoder.Encode(result)
}

// JSONL emits one self-contained record per source.
func WriteJSONL(w io.Writer, result Result) error {
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	for _, file := range result.Files {
		if err := encoder.Encode(struct {
			Kind    string `json:"kind"`
			Version string `json:"version,omitempty"`
			File
		}{Kind: "overview", Version: result.Version, File: file}); err != nil {
			return err
		}
	}
	return nil
}

// Text output remains Markdown when redirected. In a terminal, headings and
// evidence use the same small ANSI palette as the standard findings report.
func WriteText(w io.Writer, result Result, opts TextOptions) error {
	if !opts.Color {
		return writeTextPlain(w, result)
	}
	var plain bytes.Buffer
	if err := writeTextPlain(&plain, result); err != nil {
		return err
	}
	_, err := io.WriteString(w, colorizeOverview(plain.String()))
	return err
}

func writeTextPlain(w io.Writer, result Result) error {
	if _, err := fmt.Fprintf(w, "# JavaScript overview\n\n%d %s · %s\n", result.Sources, plural(result.Sources, "source"), humanBytes(result.Bytes)); err != nil {
		return err
	}
	for _, file := range result.Files {
		if _, err := fmt.Fprintf(w, "\n## %s\n\n", safeText(file.Name)); err != nil {
			return err
		}
		profile := file.Format + " · " + humanBytes(file.Bytes)
		if file.Build != "" {
			profile += " · " + file.Build
		}
		if file.SourceMap {
			profile += " · source map referenced"
		}
		if _, err := fmt.Fprintln(w, profile); err != nil {
			return err
		}
		if file.Bundle != nil {
			if err := writeBundleText(w, file); err != nil {
				return err
			}
			continue
		}
		if err := writeList(w, "Structure", file.Shape, 8); err != nil {
			return err
		}
		if err := writeList(w, "Imports and requires", file.Imports, 8); err != nil {
			return err
		}
		if err := writeList(w, "Exports", file.Exports, 12); err != nil {
			return err
		}
		if len(file.EntryPoints) > 0 {
			if _, err := fmt.Fprint(w, "\n### Entry paths\n\n"); err != nil {
				return err
			}
			for _, entry := range file.EntryPoints[:min(len(file.EntryPoints), 12)] {
				if _, err := fmt.Fprintf(w, "- Line %d: %s\n", entry.Line, safeText(entry.String())); err != nil {
					return err
				}
			}
			if len(file.EntryPoints) > 12 {
				if _, err := fmt.Fprintf(w, "- +%d more entry paths\n", len(file.EntryPoints)-12); err != nil {
					return err
				}
			}
		}
		if len(file.Registrations) > 0 {
			if _, err := fmt.Fprint(w, "\n### Event registrations\n\n"); err != nil {
				return err
			}
			for _, entry := range file.Registrations[:min(len(file.Registrations), 8)] {
				if _, err := fmt.Fprintf(w, "- Line %d: %s\n", entry.Line, safeText(entry.String())); err != nil {
					return err
				}
			}
			if len(file.Registrations) > 8 {
				if _, err := fmt.Fprintf(w, "- +%d more registrations\n", len(file.Registrations)-8); err != nil {
					return err
				}
			}
		}
		if len(file.Units) > 0 {
			if _, err := fmt.Fprint(w, "\n### Main units\n\n"); err != nil {
				return err
			}
			for _, unit := range file.Units {
				if _, err := fmt.Fprintf(w, "- %s\n", safeText(describeUnit(unit))); err != nil {
					return err
				}
			}
			if file.MoreUnits > 0 {
				if _, err := fmt.Fprintf(w, "- +%d more declarations\n", file.MoreUnits); err != nil {
					return err
				}
			}
		}
		if len(file.Links) > 0 {
			if _, err := fmt.Fprint(w, "\n### Observed links\n\n"); err != nil {
				return err
			}
			for _, link := range file.Links[:min(len(file.Links), 12)] {
				arrow := "→"
				if link.Relation != "" {
					arrow = "— " + link.Relation + " →"
				}
				if _, err := fmt.Fprintf(w, "- %s %s %s (line %d)\n", safeText(link.From), arrow, safeText(link.To), link.Line); err != nil {
					return err
				}
			}
			if len(file.Links) > 12 {
				if _, err := fmt.Fprintf(w, "- +%d more links\n", len(file.Links)-12); err != nil {
					return err
				}
			}
		}
		if err := writeList(w, "Code interactions", file.Interactions, 12); err != nil {
			return err
		}
		if file.Diagram != "" {
			if _, err := fmt.Fprintf(w, "\n### Observed flow\n\n%smermaid\n%s%s\n", "```", file.Diagram, "```"); err != nil {
				return err
			}
		}
		for _, note := range file.Notes {
			if _, err := fmt.Fprintf(w, "\n%s\n", safeText(note)); err != nil {
				return err
			}
		}
	}
	return nil
}

func writeBundleText(w io.Writer, file File) error {
	bundle := file.Bundle
	if _, err := fmt.Fprint(w, "\n### What this file provides\n\n"); err != nil {
		return err
	}
	if bundle.Summary != "" {
		if _, err := fmt.Fprintln(w, safeText(bundle.Summary)); err != nil {
			return err
		}
	} else if _, err := fmt.Fprintln(w, "A Webpack chunk of module factories; no distinct feature area was identified from this file alone."); err != nil {
		return err
	}
	if _, err := fmt.Fprint(w, "\n### Loading and boundaries\n\n"); err != nil {
		return err
	}
	for _, item := range file.Shape {
		if _, err := fmt.Fprintf(w, "- %s\n", safeText(item)); err != nil {
			return err
		}
	}
	if _, err := fmt.Fprintln(w, "- The host Webpack runtime chooses when to execute each factory; this chunk does not show its callers or page startup."); err != nil {
		return err
	}
	if len(bundle.Modules) > 0 {
		if _, err := fmt.Fprint(w, "\n### Notable modules\n\n"); err != nil {
			return err
		}
		for _, module := range bundle.Modules {
			share := ""
			if module.Share > 0 {
				share = fmt.Sprintf(" · ~%d%% of tokens", module.Share)
			}
			if _, err := fmt.Fprintf(w, "- Module %s — %s%s\n", safeText(module.ID), safeText(module.Role), share); err != nil {
				return err
			}
			for _, fact := range module.Facts {
				if _, err := fmt.Fprintf(w, "  - %s\n", safeText(fact)); err != nil {
					return err
				}
			}
			if len(module.Dependencies) > 0 {
				deps := module.Dependencies[:min(len(module.Dependencies), 4)]
				more := ""
				if len(module.Dependencies) > len(deps) {
					more = fmt.Sprintf(" (+%d more)", len(module.Dependencies)-len(deps))
				}
				if _, err := fmt.Fprintf(w, "  - Directly requires in this chunk: %s%s\n", strings.Join(deps, ", "), more); err != nil {
					return err
				}
			}
		}
	}
	for _, module := range bundle.Modules {
		if len(module.Flow) == 0 {
			continue
		}
		if _, err := fmt.Fprintf(w, "\n### Observed flow inside module %s\n\n", safeText(module.ID)); err != nil {
			return err
		}
		for i, step := range module.Flow {
			if _, err := fmt.Fprintf(w, "%d. %s\n", i+1, safeText(step)); err != nil {
				return err
			}
		}
	}
	if file.Diagram != "" {
		if _, err := fmt.Fprintf(w, "\n### Observed architecture\n\n%smermaid\n%s%s\n", "```", file.Diagram, "```"); err != nil {
			return err
		}
	}
	return nil
}

func colorizeOverview(plain string) string {
	var out strings.Builder
	inDiagram := false
	for line := range strings.SplitSeq(plain, "\n") {
		if out.Len() > 0 {
			out.WriteByte('\n')
		}
		if strings.HasPrefix(line, "```") {
			inDiagram = !inDiagram
			out.WriteString(line)
			continue
		}
		if inDiagram {
			out.WriteString(line)
			continue
		}
		switch {
		case strings.HasPrefix(line, "# "):
			out.WriteString(ansiOverview("1;36", line))
		case strings.HasPrefix(line, "## "):
			out.WriteString(ansiOverview("1;97", line))
		case strings.HasPrefix(line, "### "):
			out.WriteString(ansiOverview("1;36", line))
		case strings.HasPrefix(line, "- Module "):
			out.WriteString(ansiOverview("36", "•") + " " + ansiOverview("1;97", strings.TrimPrefix(line, "- ")))
		case strings.HasPrefix(line, "- "):
			out.WriteString(ansiOverview("36", "•") + " " + line[2:])
		case strings.HasPrefix(line, "  - "):
			out.WriteString("  " + ansiOverview("36", "↳") + " " + line[4:])
		case len(line) > 3 && line[0] >= '1' && line[0] <= '9' && line[1] == '.' && line[2] == ' ':
			out.WriteString(ansiOverview("36", line[:2]) + line[2:])
		case strings.Contains(line, " · ") && !strings.HasPrefix(line, "Contains "):
			out.WriteString(ansiOverview("2", line))
		default:
			out.WriteString(line)
		}
	}
	return out.String()
}

func ansiOverview(code, value string) string {
	return "\x1b[" + code + "m" + value + "\x1b[0m"
}

func writeList(w io.Writer, heading string, items []string, limit int) error {
	if len(items) == 0 {
		return nil
	}
	if _, err := fmt.Fprintf(w, "\n### %s\n\n", heading); err != nil {
		return err
	}
	for _, item := range items[:min(len(items), limit)] {
		if _, err := fmt.Fprintf(w, "- %s\n", safeText(item)); err != nil {
			return err
		}
	}
	if len(items) > limit {
		_, err := fmt.Fprintf(w, "- +%d more\n", len(items)-limit)
		return err
	}
	return nil
}

func buildDiagram(file File) string {
	if file.Build != "" || len(file.Links) < 3 {
		return ""
	}
	edges := file.Links[:min(len(file.Links), 12)]
	nodes := make(map[string]int)
	for _, edge := range edges {
		for _, label := range []string{edge.From, edge.To} {
			if _, exists := nodes[label]; !exists {
				nodes[label] = len(nodes)
			}
		}
	}
	if len(nodes) < 4 {
		return ""
	}
	var flow strings.Builder
	flow.WriteString("flowchart LR\n")
	for _, edge := range edges {
		arrow := "-->"
		if edge.Relation != "" {
			arrow = "-->|" + mermaidLabel(edge.Relation) + "|"
		}
		fmt.Fprintf(&flow, "  n%d[\"%s\"] %s n%d[\"%s\"]\n",
			nodes[edge.From], mermaidLabel(edge.From), arrow, nodes[edge.To], mermaidLabel(edge.To))
	}
	return flow.String()
}

func mermaidLabel(value string) string {
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(" ._:/()-", r) {
			b.WriteRune(r)
		}
		if b.Len() >= 48 {
			break
		}
	}
	return b.String()
}

func safeText(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r == '\n' || r == '\r' || r == '\t' {
			b.WriteByte(' ')
		} else if r >= 32 && r != 127 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func humanBytes(value int) string {
	if value < 1024 {
		return fmt.Sprintf("%d B", value)
	}
	if value < 1024*1024 {
		return fmt.Sprintf("%.1f KiB", float64(value)/1024)
	}
	return fmt.Sprintf("%.1f MiB", float64(value)/(1024*1024))
}

func plural(count int, word string) string {
	if count == 1 {
		return word
	}
	return word + "s"
}
