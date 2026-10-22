package overview

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"unicode"
)

type TextOptions struct {
	Terminal bool
	Color    bool
}

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
	if !opts.Terminal && !opts.Color {
		return writeTextPlain(w, result)
	}
	var plain bytes.Buffer
	if err := writeTextPlain(&plain, result); err != nil {
		return err
	}
	output := plain.String()
	if opts.Terminal {
		output = renderTerminalDiagrams(output)
		output = renderTerminalTables(output)
	}
	if opts.Color {
		output = colorizeOverview(output)
	}
	_, err := io.WriteString(w, output)
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
		if file.Minified && file.Build != "minified" {
			profile += " · minified"
		}
		if file.SourceMapUsed != "" {
			profile += " · source map applied"
		} else if file.SourceMap {
			profile += " · source map referenced"
		}
		if _, err := fmt.Fprintln(w, profile); err != nil {
			return err
		}
		writeModulesText(w, file.Modules)
		writeDispatchText(w, file.Dispatches)
		rich := hasReportInsights(file.Insights)
		if rich {
			if err := writeInsightsText(w, file); err != nil {
				return err
			}
		}
		if file.Bundle != nil {
			if err := writeBundleText(w, file, rich); err != nil {
				return err
			}
			continue
		}
		if file.Compiled != nil {
			if err := writeCompiledText(w, file, rich); err != nil {
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
		if file.Diagram != "" && !rich {
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

func writeCompiledText(w io.Writer, file File, rich bool) error {
	if err := writeList(w, "Structure", file.Shape, 4); err != nil {
		return err
	}
	if len(file.EntryPoints) > 0 || file.Compiled.Boot != "" {
		if _, err := fmt.Fprint(w, "\n### Loading\n\n"); err != nil {
			return err
		}
		if len(file.EntryPoints) > 0 {
			if _, err := fmt.Fprintf(w, "- %s\n", safeText(file.EntryPoints[0].String())); err != nil {
				return err
			}
		}
		if file.Compiled.Boot != "" {
			if _, err := fmt.Fprintf(w, "- %s\n", safeText(file.Compiled.Boot)); err != nil {
				return err
			}
		}
	}
	if err := writeList(w, "Imports and requires", file.Imports, 6); err != nil {
		return err
	}
	if err := writeList(w, "Exports", file.Exports, 8); err != nil {
		return err
	}
	if len(file.Units) > 0 {
		if _, err := fmt.Fprint(w, "\n### Main units\n\n"); err != nil {
			return err
		}
		for _, unit := range file.Units[:min(len(file.Units), 8)] {
			if _, err := fmt.Fprintf(w, "- %s\n", safeText(describeUnit(unit))); err != nil {
				return err
			}
		}
		if len(file.Units) > 8 || file.MoreUnits > 0 {
			if _, err := fmt.Fprintf(w, "- +%d more declarations\n", max(0, len(file.Units)-8)+file.MoreUnits); err != nil {
				return err
			}
		}
	}
	if len(file.Links) > 0 {
		if _, err := fmt.Fprint(w, "\n### Observed links\n\n"); err != nil {
			return err
		}
		for _, link := range file.Links[:min(len(file.Links), 8)] {
			if _, err := fmt.Fprintf(w, "- %s → %s (line %d)\n", safeText(link.From), safeText(link.To), link.Line); err != nil {
				return err
			}
		}
		if len(file.Links) > 8 {
			if _, err := fmt.Fprintf(w, "- +%d more links\n", len(file.Links)-8); err != nil {
				return err
			}
		}
	}
	if err := writeList(w, "Code interactions", file.Interactions, 8); err != nil {
		return err
	}
	if len(file.Compiled.Services) > 0 {
		if _, err := fmt.Fprint(w, "\n### RPC interfaces named in the code\n\n"); err != nil {
			return err
		}
		for _, service := range file.Compiled.Services {
			if _, err := fmt.Fprintf(w, "- %s · %d %s: %s\n", safeText(service.Name), service.Count, plural(service.Count, "method"), safeText(strings.Join(service.Methods, ", "))); err != nil {
				return err
			}
		}
	}
	if file.Diagram != "" && !rich {
		if _, err := fmt.Fprintf(w, "\n### Observed architecture\n\n%smermaid\n%s%s\n", "```", file.Diagram, "```"); err != nil {
			return err
		}
	}
	var notes []string
	if file.Insights != nil {
		notes = append(notes, file.Insights.Notes...)
	}
	notes = append(notes, file.Notes...)
	if len(file.Compiled.Services) > 0 {
		notes = append(notes, "RPC names are definitions or references; they do not prove a request occurred on page load.")
	}
	if len(notes) == 0 {
		return nil
	}
	if _, err := fmt.Fprint(w, "\n### Visibility limits\n\n"); err != nil {
		return err
	}
	seen := make(map[string]bool)
	for _, note := range notes {
		if seen[note] {
			continue
		}
		seen[note] = true
		if _, err := fmt.Fprintf(w, "- %s\n", reportText(note)); err != nil {
			return err
		}
	}
	return nil
}

func writeBundleText(w io.Writer, file File, rich bool) error {
	bundle := file.Bundle
	if rich {
		return writeBundleBoundaries(w, file)
	}
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
	if err := writeBundleBoundaries(w, file); err != nil {
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

func writeModulesText(w io.Writer, modules *BundledModules) {
	if modules == nil {
		return
	}
	var out strings.Builder
	fmt.Fprintf(&out, "\n### Bundled modules\n\nFrom %s: %d %s", modules.Source, modules.Total, plural(modules.Total, "module"))
	if len(modules.Packages) > 0 {
		fmt.Fprintf(&out, " — %d from project sources, %d from %d %s", modules.Own, modules.Total-modules.Own, len(modules.Packages), plural(len(modules.Packages), "package"))
	}
	out.WriteString(".\n")
	if len(modules.Areas) > 0 {
		lines := make(map[int]bool)
		for _, area := range modules.Areas {
			lines[area.Line] = true
		}
		// One-line bundles give every area the same line; omit it there.
		if len(lines) > 1 {
			out.WriteString("\n| Project area | Modules | First line |\n|---|---|---|\n")
		} else {
			out.WriteString("\n| Project area | Modules |\n|---|---|\n")
		}
		for _, area := range modules.Areas[:min(len(modules.Areas), 8)] {
			if len(lines) > 1 {
				fmt.Fprintf(&out, "| %s | %d | %s |\n", reportCell(area.Name), area.Count, reportCell(lineText(area.Line)))
			} else {
				fmt.Fprintf(&out, "| %s | %d |\n", reportCell(area.Name), area.Count)
			}
		}
		writeOmitted(&out, len(modules.Areas), 8, "areas")
	}
	if len(modules.Packages) > 0 {
		names := make([]string, 0, 10)
		for _, item := range modules.Packages[:min(len(modules.Packages), 10)] {
			names = append(names, fmt.Sprintf("%s (%d)", item.Name, item.Count))
		}
		if len(modules.Packages) > 10 {
			names = append(names, fmt.Sprintf("+%d more", len(modules.Packages)-10))
		}
		fmt.Fprintf(&out, "\nPackages: %s\n", reportText(strings.Join(names, ", ")))
	}
	io.WriteString(w, out.String())
}

func writeDispatchText(w io.Writer, dispatches []Dispatch) {
	if len(dispatches) == 0 {
		return
	}
	var out strings.Builder
	out.WriteString("\n### Dispatch points\n\nPlaces where one value selects among many handlers, such as RPC methods, message types, or routes.\n\n")
	for _, dispatch := range dispatches[:min(len(dispatches), 6)] {
		out.WriteString("- " + reportText(describeDispatch(dispatch)) + "\n")
	}
	writeOmitted(&out, len(dispatches), 6, "dispatch points")
	io.WriteString(w, out.String())
}

func describeDispatch(dispatch Dispatch) string {
	var subject, unit string
	switch {
	case dispatch.Kind == "switch":
		subject = "switch on " + dispatch.On
		unit = "case"
	case dispatch.Kind == "routes":
		subject = dispatch.On + " route registrations"
		unit = "route"
	case strings.HasPrefix(dispatch.Kind, "handler map "):
		subject = strings.TrimPrefix(dispatch.Kind, "handler map ") + "[" + dispatch.On + "]() handler map"
		unit = "handler"
	default:
		subject, unit = dispatch.Kind, "case"
	}
	if dispatch.In != "" {
		subject = dispatch.In + "() · " + subject
	}
	cases := strings.Join(dispatch.Cases, ", ")
	if dispatch.Count > len(dispatch.Cases) {
		cases += fmt.Sprintf(", +%d more", dispatch.Count-len(dispatch.Cases))
	}
	location := fmt.Sprintf("line %d", dispatch.Line)
	if dispatch.Origin != "" {
		location += " · " + dispatch.Origin
	}
	return fmt.Sprintf("%s — %d %s: %s (%s)", subject, dispatch.Count, plural(dispatch.Count, unit), cases, location)
}

func lineText(line int) string {
	if line <= 0 {
		return ""
	}
	return fmt.Sprintf("%d", line)
}

func hasReportInsights(insights *Insights) bool {
	return insights != nil && (insights.Summary != "" || len(insights.Components) > 0 ||
		len(insights.Routes) > 0 || len(insights.Behaviors) > 0)
}

// The richer facts are also present in JSON and JSONL through File.Insights.
// This function only selects and lays them out for a human reading one file.
func writeInsightsText(w io.Writer, file File) error {
	insights := file.Insights
	var out strings.Builder
	summary := insights.Summary
	if summary == "" && file.Bundle != nil {
		summary = file.Bundle.Summary
	}
	if summary != "" {
		fmt.Fprintf(&out, "\n### What this file does\n\n%s\n", reportText(summary))
	}
	if len(insights.Components) > 0 {
		out.WriteString("\n### Main components\n\nResponsibilities are inferred from the code; source locations are observed.\n\n")
		out.WriteString("| Component | Responsibility | Evidence | Source |\n|---|---|---|---|\n")
		for _, component := range insights.Components[:min(len(insights.Components), 7)] {
			fmt.Fprintf(&out, "| %s | %s | %s | %s |\n",
				reportCell(component.Name), reportCell(component.Role), reportCell(component.Evidence),
				reportCell(reportInsightLocation(file, component.Runtime, component.Module, component.Line, component.Column)))
		}
		writeOmitted(&out, len(insights.Components), 7, "components")
	} else if file.Bundle != nil && len(file.Bundle.Modules) > 0 {
		out.WriteString("\n### Main components\n\n| Module | Inferred responsibility | Source |\n|---|---|---|\n")
		for _, module := range file.Bundle.Modules[:min(len(file.Bundle.Modules), 7)] {
			fmt.Fprintf(&out, "| %s | %s | %s |\n", reportCell(module.ID), reportCell(module.Role), reportCell(reportLocation(module.ID, module.Line, 0)))
		}
		writeOmitted(&out, len(file.Bundle.Modules), 7, "modules")
	}
	if file.Diagram != "" {
		fmt.Fprintf(&out, "\n### Architecture\n\n%smermaid\n%s%s\n", "```", file.Diagram, "```")
	}
	if len(insights.Routes) > 0 {
		fmt.Fprintf(&out, "\n### UI routes and navigation (%d)\n\nDeclared routes and navigation targets are distinguished by use.\n\n", len(insights.Routes))
		out.WriteString("| Path | Use | Source |\n|---|---|---|\n")
		for _, route := range representativeRoutes(insights.Routes, 10) {
			fmt.Fprintf(&out, "| %s | %s | %s |\n", reportCell(route.Path), reportCell(route.Kind), reportCell(reportInsightLocation(file, route.Runtime, route.Module, route.Line, route.Column)))
		}
		writeOmitted(&out, len(insights.Routes), 10, "routes")
	}
	if len(insights.Behaviors) > 0 {
		out.WriteString("\n### Notable behavior\n\n")
		for _, behavior := range insights.Behaviors[:min(len(insights.Behaviors), 6)] {
			trail := reportInsightLocation(file, behavior.Runtime, behavior.Module, behavior.Line, behavior.Column)
			if behavior.Evidence != "" {
				trail += "; " + behavior.Evidence
			}
			fmt.Fprintf(&out, "- %s (%s)\n", reportText(behavior.Action), reportText(trail))
		}
		writeOmitted(&out, len(insights.Behaviors), 6, "behaviors")
	}
	if file.Bundle != nil {
		for _, module := range file.Bundle.Modules {
			if len(module.Flow) == 0 {
				continue
			}
			fmt.Fprintf(&out, "\n### Observed sequence in module %s\n\n", reportText(module.ID))
			for i, step := range module.Flow[:min(len(module.Flow), 4)] {
				fmt.Fprintf(&out, "%d. %s\n", i+1, reportText(step))
			}
			writeOmitted(&out, len(module.Flow), 4, "steps")
			break
		}
	}
	if len(insights.Components) > 0 {
		seen := make(map[string]bool)
		seenRole := make(map[string]bool)
		count := 0
		for _, component := range insights.Components {
			key := component.Runtime + ":" + component.Module
			if component.Module == "" || seen[key] || seenRole[component.Role] {
				continue
			}
			seen[key] = true
			seenRole[component.Role] = true
			if count == 0 {
				out.WriteString("\n### Where to read first\n\n")
			}
			fmt.Fprintf(&out, "- %s — %s.\n", reportText(reportInsightLocation(file, component.Runtime, component.Module, component.Line, component.Column)), reportText(component.Role))
			count++
			if count == 4 {
				break
			}
		}
	}
	if file.Bundle == nil && file.Compiled == nil && len(insights.Notes) > 0 {
		out.WriteString("\n### Visibility limits\n\n")
		writeInsightNotes(&out, insights.Notes)
	}
	_, err := io.WriteString(w, out.String())
	return err
}

func writeBundleBoundaries(w io.Writer, file File) error {
	bundle := file.Bundle
	var out strings.Builder
	out.WriteString("\n### Loading and visibility\n\n")
	if len(bundle.Chunks) > 0 {
		hasStartup := false
		for _, chunk := range bundle.Chunks {
			hasStartup = hasStartup || chunk.Startup != nil
		}
		for _, chunk := range bundle.Chunks[:min(len(bundle.Chunks), 4)] {
			factories := "module factories"
			if chunk.ModuleCount == 1 {
				factories = "module factory"
			}
			fmt.Fprintf(&out, "- Chunk %s registers %d %s.\n", reportText(chunk.ID), chunk.ModuleCount, factories)
			if chunk.Startup != nil {
				writeStartupBoundary(&out, chunk.Startup)
			}
		}
		writeOmitted(&out, len(bundle.Chunks), 4, "chunks")
		if !hasStartup {
			out.WriteString("- No direct startup module require was identified; the host runtime chooses when to execute registered factories.\n")
		}
	} else {
		for _, item := range file.Shape[:min(len(file.Shape), 4)] {
			fmt.Fprintf(&out, "- %s\n", reportText(item))
		}
		writeStartupBoundary(&out, bundle.Startup)
	}
	unresolved := make(map[string]bool)
	ambiguous := make(map[string]bool)
	for _, dependency := range bundle.Dependencies {
		key := dependency.Runtime + ":" + dependency.To
		switch {
		case dependency.Ambiguous:
			ambiguous[key] = true
		case !dependency.DefinedHere:
			unresolved[key] = true
		}
	}
	if len(unresolved) > 0 {
		fmt.Fprintf(&out, "- %d required %s outside this input; companion chunks are needed to inspect their behavior.\n",
			len(unresolved), plural(len(unresolved), "module ID"))
	}
	if len(ambiguous) > 0 {
		verb := "has"
		if len(ambiguous) > 1 {
			verb = "have"
		}
		fmt.Fprintf(&out, "- %d required %s %s multiple definitions in this input; the target is ambiguous.\n",
			len(ambiguous), plural(len(ambiguous), "module ID"), verb)
	}
	if file.Insights != nil {
		writeInsightNotes(&out, file.Insights.Notes)
	}
	_, err := io.WriteString(w, out.String())
	return err
}

func writeStartupBoundary(out *strings.Builder, startup *BundleStartup) {
	if startup == nil {
		out.WriteString("  - No direct startup module require was identified; the host runtime chooses when to execute registered factories.\n")
		return
	}
	if len(startup.WaitFor) > 0 {
		fmt.Fprintf(out, "  - The runtime callback waits for chunks %s before requiring modules.\n", reportText(strings.Join(startup.WaitFor, ", ")))
	}
	for _, module := range startup.Modules {
		location := "defined here"
		if module.Ambiguous {
			location = "ambiguous: the ID has multiple definitions in this runtime"
		} else if module.DefinedInChunk != "" {
			location = "defined in chunk " + module.DefinedInChunk
		} else if !module.DefinedHere {
			location = "outside this input"
		}
		fmt.Fprintf(out, "  - Startup requires module %s (%s).\n", reportText(module.ID), reportText(location))
		if module.AssetPrefix != "" {
			fmt.Fprintf(out, "    - Sets window.__assetPrefix__ to %s.\n", reportText(module.AssetPrefix))
		}
		if module.MountedAt != "" {
			fmt.Fprintf(out, "    - Mounts a React root at %s.\n", reportText(module.MountedAt))
		}
	}
}

func writeInsightNotes(out *strings.Builder, notes []string) {
	for _, note := range notes[:min(len(notes), 5)] {
		fmt.Fprintf(out, "- %s\n", reportText(note))
	}
	writeOmitted(out, len(notes), 5, "notes")
}

func writeOmitted(out *strings.Builder, total, limit int, name string) {
	if total > limit {
		fmt.Fprintf(out, "\n+%d more %s in JSON output.\n", total-limit, name)
	}
}

func representativeRoutes(items []InsightRoute, limit int) []InsightRoute {
	if len(items) <= limit {
		return items
	}
	type choice struct {
		route InsightRoute
		index int
		score int
	}
	type family struct {
		name  string
		first int
		best  choice
	}
	families := make(map[string]*family)
	for i, route := range items {
		name := routeFamily(route.Path)
		if name == "relative" || name == "wildcard" {
			continue
		}
		candidate := choice{route: route, index: i, score: routeReportScore(route)}
		if current := families[name]; current == nil {
			families[name] = &family{name: name, first: i, best: candidate}
		} else if candidate.score > current.best.score {
			current.best = candidate
		}
	}
	orderedFamilies := make([]*family, 0, len(families))
	for _, group := range families {
		orderedFamilies = append(orderedFamilies, group)
	}
	sort.Slice(orderedFamilies, func(i, j int) bool {
		left, right := orderedFamilies[i], orderedFamilies[j]
		leftDeclared := strings.HasPrefix(left.best.route.Kind, "declared")
		rightDeclared := strings.HasPrefix(right.best.route.Kind, "declared")
		if leftDeclared != rightDeclared {
			return leftDeclared
		}
		return left.first < right.first
	})
	selected := make([]choice, 0, limit)
	used := make(map[int]bool)
	for _, group := range orderedFamilies {
		if !strings.HasPrefix(group.best.route.Kind, "declared") {
			continue
		}
		selected = append(selected, group.best)
		used[group.best.index] = true
		if len(selected) == limit {
			break
		}
	}
	if len(selected) < limit {
		remaining := make([]choice, 0, len(items)-len(selected))
		for i, route := range items {
			if !used[i] && strings.HasPrefix(route.Kind, "declared") {
				remaining = append(remaining, choice{route: route, index: i, score: routeReportScore(route)})
			}
		}
		sort.SliceStable(remaining, func(i, j int) bool { return remaining[i].score > remaining[j].score })
		for _, item := range remaining[:min(len(remaining), limit-len(selected))] {
			selected = append(selected, item)
			used[item.index] = true
		}
	}
	if len(selected) < limit {
		for _, group := range orderedFamilies {
			if used[group.best.index] {
				continue
			}
			selected = append(selected, group.best)
			used[group.best.index] = true
			if len(selected) == limit {
				break
			}
		}
	}
	if len(selected) < limit {
		for i, route := range items {
			if used[i] {
				continue
			}
			selected = append(selected, choice{route: route, index: i})
			if len(selected) == limit {
				break
			}
		}
	}
	result := make([]InsightRoute, 0, len(selected))
	for _, item := range selected {
		result = append(result, item.route)
	}
	return result
}

func routeFamily(path string) string {
	if path == "/" {
		return "/"
	}
	if !strings.HasPrefix(path, "/") {
		return "relative"
	}
	segment := strings.SplitN(strings.TrimPrefix(path, "/"), "/", 2)[0]
	if segment == "*" || segment == "" {
		return "wildcard"
	}
	return segment
}

func routeReportScore(route InsightRoute) int {
	score := 0
	if strings.HasPrefix(route.Kind, "declared") {
		score += 100
	}
	if strings.HasPrefix(route.Path, "/") {
		score += 20
	}
	if !strings.ContainsAny(route.Path, "*:") {
		score += 20
	}
	if route.Path == "/" || route.Path == "/"+routeFamily(route.Path) {
		score += 20
	}
	if len(route.Path) < 60 {
		score += (60 - len(route.Path)) / 6
	}
	return score
}

func reportLocation(module string, line, column int) string {
	var location string
	if module != "" {
		location = "module " + module
	}
	if line > 0 {
		if location != "" {
			location += " · "
		}
		location += fmt.Sprintf("line %d", line)
		if column > 0 {
			location += fmt.Sprintf(":%d", column)
		}
	}
	if location == "" {
		return "—"
	}
	return location
}

func reportInsightLocation(file File, runtime, module string, line, column int) string {
	location := reportLocation(module, line, column)
	if runtime == "" || file.Bundle == nil {
		return location
	}
	var first string
	for _, chunk := range file.Bundle.Chunks {
		if first == "" {
			first = chunk.Runtime
		} else if chunk.Runtime != first {
			return "runtime " + runtime + " · " + location
		}
	}
	return location
}

func reportText(value string) string {
	value = safeText(value)
	return strings.NewReplacer("\\", "\\\\", "|", "\\|", "`", "\\`", "&", "&amp;", "<", "&lt;", ">", "&gt;", "*", "\\*", "[", "\\[", "]", "\\]").Replace(value)
}

func reportCell(value string) string {
	if value == "" {
		return "—"
	}
	return reportText(value)
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
		case strings.HasPrefix(line, "  ● "):
			out.WriteString("  " + ansiOverview("36", "●") + " " + ansiOverview("1;97", strings.TrimPrefix(line, "  ● ")))
		case strings.Contains(line, "├─ ") || strings.Contains(line, "└─ "):
			out.WriteString(colorizeGraphLine(line))
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

func colorizeGraphLine(line string) string {
	index := strings.Index(line, "├─ ")
	if index < 0 {
		index = strings.Index(line, "└─ ")
	}
	connector := line[:index+len("├─")]
	rest := line[index+len("├─ "):]
	if relation, target, ok := strings.Cut(rest, " → "); ok {
		return ansiOverview("36", connector) + " " + ansiOverview("2", relation) + " " + ansiOverview("36", "→") + " " + ansiOverview("1;97", target)
	}
	return ansiOverview("36", connector) + " " + ansiOverview("1;97", rest)
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
	if len(file.Links) < 3 {
		return ""
	}
	// Links arrive ordered by centrality. Capping children per node keeps a
	// wide dispatcher from crowding out the rest of the flow.
	var edges []Link
	children := make(map[string]int)
	for _, link := range file.Links {
		if children[link.From] >= maxDiagramChildren {
			continue
		}
		children[link.From]++
		edges = append(edges, link)
		if len(edges) == maxDiagramEdges {
			break
		}
	}
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
		if unicode.IsLetter(r) || unicode.IsDigit(r) || strings.ContainsRune(" ._:/()-#", r) {
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
