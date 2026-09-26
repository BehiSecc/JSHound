package overview

import (
	"bytes"
	"strings"
	"testing"
)

func TestTerminalOverviewRendersGraphInsteadOfMermaidSource(t *testing.T) {
	file := File{
		Name: "chunk.js", Format: "loader chunk", Build: "Webpack-style bundle",
		Bundle: &Bundle{Summary: "Contains app initialization."},
		Diagram: `flowchart LR
  host["Webpack runtime"] -->|registers| chunk["chunk 628 · 630 factories"]
  chunk -->|contains| app["app initialization"]
  app -->|configures| plugins["SSR plugins"]
  chunk -->|contains| plugins
`,
	}
	result := Result{Sources: 1, Files: []File{file}}
	var plain, terminal, colored bytes.Buffer
	if err := WriteText(&plain, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := WriteText(&terminal, result, TextOptions{Terminal: true}); err != nil {
		t.Fatal(err)
	}
	if err := WriteText(&colored, result, TextOptions{Terminal: true, Color: true}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(plain.String(), "```mermaid") || strings.Contains(terminal.String(), "```mermaid") ||
		!strings.Contains(terminal.String(), "● Webpack runtime") ||
		!strings.Contains(terminal.String(), "registers → chunk 628 · 630 factories") ||
		!strings.Contains(terminal.String(), "configures → SSR plugins") ||
		!strings.Contains(terminal.String(), "↪") {
		t.Fatalf("terminal graph did not render:\n%s", terminal.String())
	}
	if strings.Contains(terminal.String(), "\x1b[") || !strings.Contains(colored.String(), "\x1b[") || strings.Contains(colored.String(), "```mermaid") {
		t.Fatalf("terminal color handling failed:\n%s", colored.String())
	}
}

func TestTerminalGraphHandlesCycleAndSeparateRoots(t *testing.T) {
	graph := renderTerminalGraph([]string{
		"flowchart LR",
		`  host["runtime"] --> chunk["chunk"]`,
		`  chunk --> host`,
		`  wait["other chunks"] -->|unblocks| chunk`,
		`  detached["standalone module"]`,
	})
	if !strings.Contains(graph, "● other chunks") || !strings.Contains(graph, "● standalone module") ||
		!strings.Contains(graph, "runtime") || !strings.Contains(graph, "↪") {
		t.Fatalf("cycle or secondary root was lost:\n%s", graph)
	}
}

func TestTerminalDiagramFallsBackForUnsupportedMermaid(t *testing.T) {
	markdown := "### Architecture\n\n```mermaid\nflowchart LR\n  app[\"App\"] --> api[\"API\"]\n  subgraph extra\n```\n"
	if got := renderTerminalDiagrams(markdown); got != markdown {
		t.Fatalf("unsupported syntax should keep source diagram for review:\n%s", got)
	}
}

func TestTerminalTablesAreReadableAtEightyColumns(t *testing.T) {
	markdown := "### UI routes and navigation\n\n| Path | Use | Source |\n|---|---|---|\n| /login | declared route | module 16206 · line 1:902 |\n| /home | navigation target | module 16206 · line 1:941 |\n"
	got := renderTerminalTables(markdown)
	if strings.Contains(got, "|---|") || !strings.Contains(got, "● /login — declared route") ||
		!strings.Contains(got, "● /home — navigation target") ||
		!strings.Contains(got, "module 16206 · line 1:941") {
		t.Fatalf("terminal table lost useful facts:\n%s", got)
	}
	for _, line := range strings.Split(got, "\n") {
		if len([]rune(line)) > 80 {
			t.Fatalf("line exceeds eighty columns: %q", line)
		}
	}
}
