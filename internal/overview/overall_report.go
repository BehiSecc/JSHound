package overview

import (
	"fmt"
	"sort"
	"strings"
)

const (
	// maxFullSections is how many sources get a full section each in the
	// default report; more sources get the compact index.
	maxFullSections = 5
	// maxIndexLines caps the files the compact index lists one by one.
	maxIndexLines = 30
	// maxIndexAreas is how many feature areas an index line names.
	maxIndexAreas = 3
	maxEntryNames = 4
)

// indexed reports whether the default report shows the compact index instead
// of a full section per file.
func indexed(result Result) bool {
	return result.Overall != nil && len(result.Files) > maxFullSections
}

// writeOverallText writes the roll-up as a section of its own with the three
// headings of a file section, so it reads like the summary of one big file.
func writeOverallText(out *strings.Builder, result Result) {
	o := result.Overall
	if !o.Related {
		// Nothing ties these files together, so there is nothing to roll up.
		fmt.Fprintf(out, "\n%s\n", reportText(o.Is))
		return
	}
	names := newShortNames(result.Files)
	fmt.Fprintf(out, "\n## All %d files\n", o.Files)
	if line := names.line(); line != "" {
		fmt.Fprintf(out, "\n%s\n", line)
	}
	fmt.Fprintf(out, "\n### What it is\n\n%s\n\n", reportText(o.Is))
	fmt.Fprintf(out, "- %s\n", reportText(compositionLine(o)))
	if len(o.Runtimes) > 1 {
		var parts []string
		for _, runtime := range o.Runtimes {
			parts = append(parts, fmt.Sprintf("%s (%d %s)", runtime.Kind, runtime.Files, plural(runtime.Files, "file")))
		}
		fmt.Fprintf(out, "- Runs in: %s\n", reportText(strings.Join(parts, ", ")))
	}
	if line := entryLine(o, names); line != "" {
		fmt.Fprintf(out, "- %s\n", reportText(line))
	}
	switch {
	case len(o.Features) > 0:
		out.WriteString("\n### What it does\n\nFeature areas named throughout the code, with the actions next to them:\n\n")
		writeFeatures(out, o.Features, o.Evidence)
	default:
		out.WriteString("\n### What it does\n\nNo readable feature areas were found across these files.\n")
	}
	writeRuns(out, o.Runs)
}

// compositionLine reads "655 files (25.8 MiB): 356 libraries, 212 ES modules".
func compositionLine(o *Overall) string {
	var total int
	var parts []string
	for _, kind := range o.Composition {
		total += kind.Bytes
		parts = append(parts, fmt.Sprintf("%d %s", kind.Files, kindPlural(kind.Kind, kind.Files)))
	}
	return fmt.Sprintf("%d files (%s): %s", o.Files, humanBytes(total), strings.Join(parts, ", "))
}

// entryLine names the files that start the application and what shows it.
func entryLine(o *Overall, names shortNames) string {
	if len(o.Entries) == 0 {
		return ""
	}
	var parts []string
	for _, entry := range o.Entries[:min(len(o.Entries), maxEntryNames)] {
		why := entry.Why
		if entry.Loads > 0 && !strings.Contains(why, "loads ") {
			why += fmt.Sprintf("; loads %d %s", entry.Loads, plural(entry.Loads, "file"))
		}
		parts = append(parts, fmt.Sprintf("%s (%s)", names.short(entry.File), why))
	}
	total := len(o.Entries) + o.MoreEntries
	if total > len(parts) {
		parts = append(parts, fmt.Sprintf("and %d more", total-len(parts)))
	}
	if total == 1 {
		return "Entry file: " + parts[0]
	}
	return "Entry files: " + strings.Join(parts, ", ")
}

// writeFileIndex is the default report for many files: one line for each file
// that has something to report, entry files first and then the largest, and
// the rest grouped by kind.
func writeFileIndex(out *strings.Builder, result Result) {
	names := newShortNames(result.Files)
	var listed, quiet []File
	for _, file := range result.Files {
		if file.entry || file.Purpose != nil && (len(file.Purpose.Features) > 0 || len(file.Purpose.Runs) > 0) {
			listed = append(listed, file)
		} else {
			quiet = append(quiet, file)
		}
	}
	// Entry files first, then files that share a feature area with the whole
	// application, then the rest, each group by size. Vendor chunks (icon
	// sets, grammars, renderers) have areas of their own and fall behind.
	areas := map[string]struct{}{}
	if result.Overall != nil && result.Overall.Related {
		for _, feature := range result.Overall.Features {
			for _, word := range strings.Fields(feature.Area) {
				areas[word] = struct{}{}
			}
		}
	}
	shares := func(file File) bool {
		if file.Purpose == nil {
			return false
		}
		for _, feature := range file.Purpose.Features {
			for _, word := range strings.Fields(feature.Area) {
				if _, ok := areas[word]; ok {
					return true
				}
			}
		}
		return false
	}
	sort.SliceStable(listed, func(i, j int) bool {
		if listed[i].entry != listed[j].entry {
			return listed[i].entry
		}
		if a, b := shares(listed[i]), shares(listed[j]); a != b {
			return a
		}
		return listed[i].Bytes > listed[j].Bytes
	})
	out.WriteString("\n## Files\n")
	if line := names.line(); line != "" && !result.Overall.Related {
		fmt.Fprintf(out, "\n%s\n", line)
	}
	if len(listed) > 0 {
		out.WriteString("\n")
	}
	for _, file := range listed[:min(len(listed), maxIndexLines)] {
		fmt.Fprintf(out, "- %s\n", reportText(indexLine(file, names)))
	}
	if len(listed) > maxIndexLines {
		fmt.Fprintf(out, "- +%d more %s with something to report\n", len(listed)-maxIndexLines, plural(len(listed)-maxIndexLines, "file"))
	}
	if len(quiet) > 0 {
		type group struct{ files, bytes int }
		groups := map[string]*group{}
		var kinds []string
		for _, file := range quiet {
			kind := kindOf(file)
			if groups[kind] == nil {
				groups[kind] = &group{}
				kinds = append(kinds, kind)
			}
			groups[kind].files++
			groups[kind].bytes += file.Bytes
		}
		sort.Slice(kinds, func(i, j int) bool {
			if groups[kinds[i]].files != groups[kinds[j]].files {
				return groups[kinds[i]].files > groups[kinds[j]].files
			}
			return kinds[i] < kinds[j]
		})
		fmt.Fprintf(out, "\nNothing to report in %d %s:\n\n", len(quiet), plural(len(quiet), "file"))
		for _, kind := range kinds {
			fmt.Fprintf(out, "- %d %s · %s\n", groups[kind].files, kindPlural(kind, groups[kind].files), humanBytes(groups[kind].bytes))
		}
	}
	if hint := sourceMapSetHint(result.Files); hint != "" {
		fmt.Fprintf(out, "\n%s\n", safeText(hint))
	}
}

// indexLine reads "name · size · identity · areas".
func indexLine(file File, names shortNames) string {
	parts := []string{names.short(file.Name), humanBytes(file.Bytes)}
	if file.Purpose != nil {
		parts = append(parts, strings.TrimSuffix(file.Purpose.Is, "."))
		var areas []string
		for _, feature := range file.Purpose.Features[:min(len(file.Purpose.Features), maxIndexAreas)] {
			areas = append(areas, feature.Area)
		}
		if len(areas) > 0 {
			parts = append(parts, strings.Join(areas, ", "))
		} else if len(file.Purpose.Runs) > 0 {
			var aspects []string
			for _, fact := range file.Purpose.Runs {
				aspects = append(aspects, strings.ToLower(fact.Aspect))
			}
			parts = append(parts, "runs: "+strings.Join(aspects, ", "))
		}
	}
	if file.entry {
		parts = append(parts, "entry file")
	}
	return strings.Join(parts, " · ")
}

// sourceMapSetHint says once for the set that some files reference source
// maps that were not applied. The advice depends on where the inputs came
// from, as the per-file hint does: overview never fetches a map and reads one
// only from the folder of a local file.
func sourceMapSetHint(files []File) string {
	count, urls, local := 0, 0, 0
	for _, file := range files {
		if file.mapHint == "" {
			continue
		}
		count++
		if isHTTPURL(file.Name) {
			urls++
		} else if file.Name != "stdin" {
			local++
		}
	}
	if count == 0 {
		return ""
	}
	subject := fmt.Sprintf("%d %s reference source maps that were not applied.", count, plural(count, "file"))
	const then = " --output ./source, then jshound --overview ./source to read the original sources."
	switch {
	case urls == count:
		return subject + " Overview does not fetch maps; run jshound -s URL" + then
	case local == count:
		return subject + " Overview reads a map only from the file's own folder; put the map file there and rerun, or run jshound -s FILE" + then
	}
	return subject + " Overview does not fetch maps and reads one only from a local file's folder; run jshound -s FILE-OR-URL" + then
}

// shortNames shortens file names by the folder they all share, which is
// printed once, so index lines stay short and every full path can be rebuilt.
type shortNames struct{ prefix string }

func newShortNames(files []File) shortNames {
	if len(files) < 2 {
		return shortNames{}
	}
	prefix := folderOf(files[0].Name)
	for _, file := range files[1:] {
		for prefix != "" && !strings.HasPrefix(file.Name, prefix) {
			prefix = folderOf(strings.TrimSuffix(prefix, "/"))
		}
	}
	// A bare root, or the scheme and host of a URL, is not worth stripping.
	if prefix == "" || prefix == "/" || strings.HasSuffix(prefix, "://") || strings.Count(prefix, "/") == 2 && isHTTPURL(prefix) {
		return shortNames{}
	}
	return shortNames{prefix}
}

// folderOf returns a name up to and including its last slash.
func folderOf(name string) string {
	if i := strings.LastIndexByte(name, '/'); i >= 0 {
		return name[:i+1]
	}
	return ""
}

func (n shortNames) short(name string) string {
	return safeText(strings.TrimPrefix(name, n.prefix))
}

func (n shortNames) line() string {
	if n.prefix == "" {
		return ""
	}
	return "Folder: " + safeText(n.prefix) + " · file names below are relative to it"
}
