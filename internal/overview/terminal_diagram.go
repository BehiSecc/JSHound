package overview

import (
	"html"
	"strings"
)

type terminalEdge struct {
	from, to, relation string
}

// JSHound emits a small, predictable Mermaid subset. Render that graph as a
// branching Unicode flowchart for terminals, while files keep the Mermaid.
func renderTerminalDiagrams(markdown string) string {
	lines := strings.Split(markdown, "\n")
	output := make([]string, 0, len(lines))
	for i := 0; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) != "```mermaid" {
			output = append(output, lines[i])
			continue
		}
		end := i + 1
		for end < len(lines) && strings.TrimSpace(lines[end]) != "```" {
			end++
		}
		if end == len(lines) {
			output = append(output, lines[i])
			continue
		}
		if graph := renderTerminalGraph(lines[i+1 : end]); graph != "" {
			output = append(output, strings.Split(graph, "\n")...)
			i = end
		} else {
			output = append(output, lines[i:end+1]...)
			i = end
		}
	}
	return strings.Join(output, "\n")
}

func renderTerminalGraph(lines []string) string {
	labels := make(map[string]string)
	order := make([]string, 0, len(lines))
	edges := make([]terminalEdge, 0, len(lines))
	addNode := func(id, label string) {
		if id == "" {
			return
		}
		if _, exists := labels[id]; !exists {
			labels[id] = id
			order = append(order, id)
		}
		if label != "" {
			labels[id] = label
		}
	}
	for _, raw := range lines {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "flowchart ") {
			continue
		}
		left, right, edge := strings.Cut(line, "-->")
		if !edge {
			id, label := diagramNode(line)
			if id == "" {
				return ""
			}
			addNode(id, label)
			continue
		}
		from, fromLabel := diagramNode(left)
		right = strings.TrimSpace(right)
		relation := ""
		if strings.HasPrefix(right, "|") {
			var found bool
			relation, right, found = strings.Cut(right[1:], "|")
			if !found {
				return ""
			}
		}
		to, toLabel := diagramNode(right)
		if from == "" || to == "" {
			return ""
		}
		addNode(from, fromLabel)
		addNode(to, toLabel)
		edges = append(edges, terminalEdge{from: from, to: to, relation: relation})
	}
	if len(order) == 0 {
		return ""
	}
	children := make(map[string][]terminalEdge)
	incoming := make(map[string]int)
	containedBy := make(map[string]string)
	for _, edge := range edges {
		children[edge.from] = append(children[edge.from], edge)
		incoming[edge.to]++
		if edge.relation == "contains" {
			containedBy[edge.to] = edge.from
		}
	}
	var rows []string
	seen := make(map[string]bool)
	var visit func(string, string)
	visit = func(id, prefix string) {
		outgoing := children[id]
		for i, edge := range outgoing {
			last := i == len(outgoing)-1
			branch, nextPrefix := "├─", prefix+"│  "
			if last {
				branch, nextPrefix = "└─", prefix+"   "
			}
			text := prefix + branch + " "
			if edge.relation != "" {
				text += edge.relation + " → "
			} else {
				text += "→ "
			}
			text += labels[edge.to]
			if parent, ok := containedBy[edge.to]; ok && parent != id && edge.relation != "contains" {
				rows = append(rows, text+" ↪")
				continue
			}
			if seen[edge.to] {
				rows = append(rows, text+" ↪")
				continue
			}
			rows = append(rows, text)
			seen[edge.to] = true
			visit(edge.to, nextPrefix)
		}
	}
	root := func(id string) {
		if len(rows) > 0 {
			rows = append(rows, "")
		}
		rows = append(rows, "  ● "+labels[id])
		seen[id] = true
		visit(id, "  ")
	}
	for _, id := range order {
		if incoming[id] == 0 && !seen[id] {
			root(id)
		}
	}
	for _, id := range order {
		if !seen[id] {
			root(id)
		}
	}
	return strings.Join(rows, "\n")
}

func diagramNode(part string) (id, label string) {
	part = strings.TrimSpace(part)
	if open := strings.Index(part, "[\""); open >= 0 {
		if !strings.HasSuffix(part, "\"]") {
			return "", ""
		}
		return strings.TrimSpace(part[:open]), part[open+2 : len(part)-2]
	}
	if part == "" || strings.ContainsAny(part, " \t[]|") {
		return "", ""
	}
	return part, ""
}

// Keep Markdown tables for files, but render their key facts as compact rows
// on an 80-column terminal. The full values remain available in JSON output.
func renderTerminalTables(markdown string) string {
	lines := strings.Split(markdown, "\n")
	output := make([]string, 0, len(lines))
	inFence := false
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			inFence = !inFence
			output = append(output, line)
			continue
		}
		if inFence || i+2 >= len(lines) {
			output = append(output, line)
			continue
		}
		headers := terminalCells(line)
		kind := terminalTableKind(headers)
		if kind == "" || !terminalSeparator(lines[i+1], len(headers)) {
			output = append(output, line)
			continue
		}
		end := i + 2
		var rows []string
		for end < len(lines) {
			cells := terminalCells(lines[end])
			if len(cells) != len(headers) {
				break
			}
			rows = append(rows, terminalTableRow(kind, cells)...)
			end++
		}
		if len(rows) == 0 {
			output = append(output, line)
			continue
		}
		output = append(output, rows...)
		i = end - 1
	}
	return strings.Join(output, "\n")
}

func terminalTableKind(headers []string) string {
	switch strings.Join(headers, "|") {
	case "Component|Responsibility|Evidence|Source":
		return "components"
	case "Module|Inferred responsibility|Source":
		return "modules"
	case "Path|Use|Source":
		return "routes"
	}
	return ""
}

func terminalSeparator(line string, count int) bool {
	cells := terminalCells(line)
	if len(cells) != count {
		return false
	}
	for _, cell := range cells {
		if strings.Trim(cell, "-:") != "" || len(cell) < 3 {
			return false
		}
	}
	return true
}

func terminalCells(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
		return nil
	}
	line = line[1 : len(line)-1]
	var cells []string
	var part strings.Builder
	escaped := false
	for _, r := range line {
		switch {
		case escaped:
			part.WriteRune(r)
			escaped = false
		case r == '\\':
			escaped = true
		case r == '|':
			cells = append(cells, html.UnescapeString(strings.TrimSpace(part.String())))
			part.Reset()
		default:
			part.WriteRune(r)
		}
	}
	if escaped {
		part.WriteByte('\\')
	}
	cells = append(cells, html.UnescapeString(strings.TrimSpace(part.String())))
	return cells
}

func terminalTableRow(kind string, cells []string) []string {
	const width = 74
	add := func(title string, details ...string) []string {
		rows := []string{"  ● " + terminalClip(title, width)}
		for _, detail := range details {
			if detail != "" && detail != "—" {
				rows = append(rows, "    "+terminalClip(detail, width))
			}
		}
		return rows
	}
	switch kind {
	case "components":
		return add(cells[0]+" — "+cells[1], cells[2], cells[3])
	case "modules":
		return add("Module "+cells[0]+" — "+cells[1], cells[2])
	case "routes":
		return add(cells[0]+" — "+cells[1], cells[2])
	}
	return nil
}

func terminalClip(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return string(runes[:limit-1]) + "…"
}
