package overview

import "strings"

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
