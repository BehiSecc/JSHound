package overview

import (
	"fmt"
	"sort"
	"strings"
)

// maxAPIGroups is how many groups the API line names before "+N more".
const maxAPIGroups = 3

// runOrder is the order of the aspects in "Behavior". Facts derived from the
// evidence or the load flow are inserted by it.
var runOrder = map[string]int{
	"Extension APIs": 0, "Starts": 1, "Navigation": 2, "Screens": 3, "Server calls": 4, "API": 5,
	"Sign-in": 6, "Files": 7, "Storage": 8, "Background": 9, "Third parties": 10,
}

// withDerivedRuns adds facts that are not read from run signals to the facts
// that are, each placed by runOrder.
func withDerivedRuns(facts []RunFact, extra ...RunFact) []RunFact {
	for _, fact := range extra {
		at := len(facts)
		for i, other := range facts {
			if runOrder[other.Aspect] > runOrder[fact.Aspect] {
				at = i
				break
			}
		}
		facts = append(facts, RunFact{})
		copy(facts[at+1:], facts[at:])
		facts[at] = fact
	}
	return facts
}

// evidenceRuns gives the API and Screens facts of the counted evidence.
func evidenceRuns(e *evidence) []RunFact {
	var facts []RunFact
	if screens := e.screensDetail(); screens != "" {
		facts = append(facts, RunFact{Aspect: "Screens", Detail: screens})
	}
	if api := e.apiDetail(); api != "" {
		facts = append(facts, RunFact{Aspect: "API", Detail: api})
	}
	return facts
}

// screensDetail reads "12 routes (/a, /b, /c, +9 more)".
func (e *evidence) screensDetail() string {
	if e == nil {
		return ""
	}
	var screens []evidenceItem
	for _, item := range e.items {
		if item.screen {
			screens = append(screens, item)
		}
	}
	if len(screens) == 0 {
		return ""
	}
	paths := sortedScreenPaths(screens)
	shown := firstN(paths, maxFeatureExamples)
	if more := len(paths) - len(shown); more > 0 {
		shown = append(shown, fmt.Sprintf("+%d more", more))
	}
	return fmt.Sprintf("%d %s (%s)", len(paths), plural(len(paths), "route"), strings.Join(shown, ", "))
}

// apiDetail reads "152 calls in 19 services (A 26, B 13, C 13, +16 more)".
// Calls of one RPC service or GraphQL are one group; plain paths group by
// their first segment and are written with a leading slash. When every group
// is a service the line counts services, when every group is a path it counts
// calls only, and a mix counts groups.
func (e *evidence) apiDetail() string {
	if e == nil {
		return ""
	}
	counts := map[string]int{}
	total, services := 0, 0
	for _, item := range e.items {
		if item.screen {
			continue
		}
		total++
		name := item.group
		if name == "" {
			name = "/" + apiSegment(item.label)
		}
		if counts[name] == 0 && item.group != "" {
			services++
		}
		counts[name]++
	}
	if total == 0 {
		return ""
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if counts[names[i]] != counts[names[j]] {
			return counts[names[i]] > counts[names[j]]
		}
		return names[i] < names[j]
	})
	var parts []string
	for _, name := range names[:min(len(names), maxAPIGroups)] {
		parts = append(parts, fmt.Sprintf("%s %d", name, counts[name]))
	}
	if more := len(names) - len(parts); more > 0 {
		parts = append(parts, fmt.Sprintf("+%d more", more))
	}
	detail := fmt.Sprintf("%d %s", total, plural(total, "call"))
	switch {
	case services == len(names):
		detail += fmt.Sprintf(" in %d %s", len(names), plural(len(names), "service"))
	case services > 0:
		detail += fmt.Sprintf(" in %d groups", len(names))
	}
	return detail + " (" + strings.Join(parts, ", ") + ")"
}

// apiSegment is the first segment of a path that names a resource, in the
// spelling of the code: parameters, "api", and version segments such as "v1"
// are the same on every path and say nothing about the group. Without a
// resource segment it is the first segment.
func apiSegment(path string) string {
	segments := strings.Split(strings.Trim(path, "/"), "/")
	for _, segment := range segments {
		if _, prefix := apiSegments[strings.ToLower(segment)]; segment == "" || prefix || versionSegment.MatchString(segment) || paramSegment.MatchString(segment) || strings.Contains(segment, "{") {
			continue
		}
		return segment
	}
	return segments[0]
}

// startsRun states how the file starts from the load flow: the leading calls
// at load and the events it handles. It stays silent when the startup calls
// are minified names, for a library, and for a file with a startup fact
// already.
func startsRun(file *File, facts []RunFact, library bool) (RunFact, bool) {
	if library {
		return RunFact{}, false
	}
	for _, fact := range facts {
		if fact.Aspect == "Starts" {
			return RunFact{}, false
		}
	}
	starts := startActions(file)
	var events []string
	for _, entry := range file.EntryPoints {
		if entry.Trigger != "module load" && len(events) < 3 {
			events = appendUnique(events, strings.TrimSuffix(entry.Trigger, " event"))
		}
	}
	if len(starts) == 0 && len(events) == 0 || !readableStarts(*file) {
		return RunFact{}, false
	}
	for _, start := range starts {
		// A call on a value that another call returned has no name to state.
		if strings.HasSuffix(start, " on returned value") {
			return RunFact{}, false
		}
	}
	var parts []string
	if len(starts) > 0 {
		parts = append(parts, "calls "+strings.Join(starts, " and ")+" at load")
	}
	if len(events) > 0 {
		parts = append(parts, "handles "+strings.Join(events, ", ")+" events")
	}
	return RunFact{Aspect: "Starts", Detail: strings.Join(parts, "; ")}, true
}
