package overview

import (
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

// Text output is Markdown so observed diagrams can be rendered as Mermaid.
func WriteText(w io.Writer, result Result, _ TextOptions) error {
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
