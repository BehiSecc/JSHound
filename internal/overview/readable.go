package overview

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
)

// readableName reports whether a name looks written by a person. Minifiers
// leave one- to three-letter names such as "o", "ZK", or "e.C", so every
// dotted part must be a longer word or join several words ("parseCSS").
func readableName(name string) bool {
	for _, part := range strings.Split(strings.TrimSuffix(name, "()"), ".") {
		if len(part) < 4 && !multiWord(part) {
			return false
		}
	}
	return true
}

// nameJudge decides which names the verbose report lists. A file that is not
// minified keeps every name, since short words such as "run" are real there.
// In a minified file a name is listed when it is readable or when a source
// map restored it.
type nameJudge struct {
	strict   bool
	restored map[string]struct{}
}

func newNameJudge(file File) nameJudge {
	return nameJudge{strict: file.Minified, restored: file.restored}
}

// label judges a node of the call flow: "parse()", "module load",
// "click event", or "orig() [min]" after a source map renamed it.
func (j nameJudge) label(label string) bool {
	if !j.strict {
		return true
	}
	if _, ok := j.restored[label]; ok {
		return true
	}
	if i := strings.Index(label, "()"); i >= 0 {
		// A call on a returned value has no receiver to name.
		return !strings.HasSuffix(label, " on returned value") && readableName(label[:i])
	}
	if words := strings.Fields(label); len(words) == 3 && words[2] == "event" {
		return readableName(words[0])
	}
	return true
}

// call judges a callee listed under a unit, which has no trailing "()" unless
// a source map disambiguated it.
func (j nameJudge) call(call string) bool {
	if !j.strict {
		return true
	}
	if _, ok := j.restored[call+"()"]; ok {
		return true
	}
	if strings.Contains(call, "()") {
		return j.label(call)
	}
	return readableName(call)
}

func (j nameJudge) unit(unit Unit) bool {
	return !j.strict || unit.OriginalName != "" || readableName(unit.Name)
}

func (j nameJudge) export(name string) bool {
	return !j.strict || strings.Contains(name, " from ") || readableName(name)
}

func (j nameJudge) link(link Link) bool {
	return j.label(link.From) && j.label(link.To)
}

func (j nameJudge) entry(entry Entry) bool {
	return j.label(entry.Trigger) && j.label(entry.Action) && (entry.RegisteredBy == "" || j.label(entry.RegisteredBy))
}

// dispatch judges a dispatch point by the value it switches on and the
// function it sits in; its cases are strings and stay as they are.
func (j nameJudge) dispatch(dispatch Dispatch) bool {
	return !j.strict || readableName(dispatch.On) && (dispatch.In == "" || j.call(dispatch.In))
}

// dispatchRow clears the names of a dispatch point that would read as noise:
// the function it sits in, the value it switches on, and the handler map. The
// kind, the count, and the cases stay, since they are what the row tells.
func (j nameJudge) dispatchRow(dispatch Dispatch) Dispatch {
	if !j.strict {
		return dispatch
	}
	if dispatch.In != "" && !j.call(dispatch.In) {
		dispatch.In = ""
	}
	if !readableName(dispatch.On) {
		dispatch.On = ""
	}
	if table, ok := strings.CutPrefix(dispatch.Kind, "handler map "); ok && !readableName(table) {
		dispatch.Kind = "handler map"
	}
	return dispatch
}

// hiddenNames counts what the verbose report left out because the names were
// minified.
type hiddenNames struct {
	declarations, links, exports, entries, registrations int
}

// line is the one sentence that says what was left out, or "".
func (h hiddenNames) line() string {
	var parts []string
	for _, item := range []struct {
		count        int
		one, several string
	}{
		{h.declarations, "declaration", "declarations"},
		{h.links, "call link", "call links"},
		{h.exports, "export", "exports"},
		{h.entries, "entry path", "entry paths"},
		{h.registrations, "event registration", "event registrations"},
	} {
		if item.count == 1 {
			parts = append(parts, "1 "+item.one)
		} else if item.count > 1 {
			parts = append(parts, groupDigits(item.count)+" "+item.several)
		}
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		if strings.HasPrefix(parts[0], "1 ") {
			return parts[0] + " found; its name is minified, so it is not listed."
		}
		return parts[0] + " found; their names are minified, so they are not listed."
	}
	list := strings.Join(parts[:len(parts)-1], ", ")
	if len(parts) > 2 {
		list += ","
	}
	return list + " and " + parts[len(parts)-1] + " found; their names are minified, so they are not listed."
}

func groupDigits(n int) string {
	digits := strconv.Itoa(n)
	var b strings.Builder
	for i, c := range digits {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}

// verboseView is a file prepared for the verbose report: the same facts with
// minified names removed from its lists, the counts of what was removed, and
// the flow sentence built from the names that remain.
type verboseView struct {
	file   File
	hidden hiddenNames
	flow   string
}

// minifiedNamesHidden drops the units, links, exports, entry paths, and
// diagram edges whose names would print as "ZK() → yH()", and clears such
// names from dispatch rows. Text output only:
// the File that JSON and JSONL encode is not touched.
func minifiedNamesHidden(file File) verboseView {
	judge := newNameJudge(file)
	if !judge.strict {
		return verboseView{file: file, flow: describeFlow(&file)}
	}
	view := verboseView{file: file}
	// The flow sentence says how the file starts only when the leading startup
	// calls are readable. Naming the readable calls that happen to follow
	// minified ones would misstate it, so otherwise it keeps events and
	// dispatches alone.
	flow := file
	flow.EntryPoints = nil
	startsReadable := readableStarts(file)
	for _, entry := range file.EntryPoints {
		if entry.Trigger != "module load" || startsReadable {
			flow.EntryPoints = append(flow.EntryPoints, entry)
		}
	}
	flow.Dispatches = nil
	for _, dispatch := range file.Dispatches {
		if judge.dispatch(dispatch) {
			flow.Dispatches = append(flow.Dispatches, dispatch)
		}
	}
	view.flow = describeFlow(&flow)

	view.file.Units = nil
	for _, unit := range file.Units {
		if !judge.unit(unit) {
			view.hidden.declarations++
			continue
		}
		var calls []string
		for _, call := range unit.Calls {
			if judge.call(call) {
				calls = append(calls, call)
			}
		}
		unit.Calls = calls
		view.file.Units = append(view.file.Units, unit)
	}
	if len(view.file.Units) == 0 {
		// Only the most central declarations are kept, so when none of them
		// reads well the rest are taken to be minified as well.
		view.hidden.declarations += file.MoreUnits
		view.file.MoreUnits = 0
	}
	view.file.Links = nil
	for _, link := range file.Links {
		if judge.link(link) {
			view.file.Links = append(view.file.Links, link)
		}
	}
	view.hidden.links = len(file.Links) - len(view.file.Links)
	view.file.Exports = nil
	for _, name := range file.Exports {
		if judge.export(name) {
			view.file.Exports = append(view.file.Exports, name)
		}
	}
	view.hidden.exports = len(file.Exports) - len(view.file.Exports)
	view.file.loadingHidden = len(file.EntryPoints) > 0 && !judge.entry(file.EntryPoints[0])
	view.file.EntryPoints = nil
	for _, entry := range file.EntryPoints {
		if judge.entry(entry) {
			view.file.EntryPoints = append(view.file.EntryPoints, entry)
		}
	}
	view.hidden.entries = len(file.EntryPoints) - len(view.file.EntryPoints)
	view.file.Registrations = nil
	for _, entry := range file.Registrations {
		if judge.entry(entry) {
			view.file.Registrations = append(view.file.Registrations, entry)
		}
	}
	view.hidden.registrations = len(file.Registrations) - len(view.file.Registrations)
	view.file.Diagram = readableDiagram(file.Diagram, judge)
	view.file.Dispatches = nil
	for _, dispatch := range file.Dispatches {
		view.file.Dispatches = append(view.file.Dispatches, judge.dispatchRow(dispatch))
	}
	return view
}

// diagramEdge matches a Mermaid edge line such as `n0["a()"] -->|rel| n1["b()"]`.
var diagramEdge = regexp.MustCompile(`^\s*\w+(?:\["([^"]*)"\])? -->(?:\|[^|]*\|)? \w+(?:\["([^"]*)"\])?\s*$`)

// readableDiagram removes the edges that join a minified name. What remains
// is kept only if it is still a graph worth drawing.
func readableDiagram(diagram string, judge nameJudge) string {
	if diagram == "" {
		return ""
	}
	var kept []string
	edges, dropped := 0, false
	nodes := make(map[string]struct{})
	for line := range strings.SplitSeq(strings.TrimRight(diagram, "\n"), "\n") {
		if match := diagramEdge.FindStringSubmatch(line); match != nil {
			if match[1] != "" && !judge.label(match[1]) || match[2] != "" && !judge.label(match[2]) {
				dropped = true
				continue
			}
			edges++
			nodes[match[1]] = struct{}{}
			nodes[match[2]] = struct{}{}
		}
		kept = append(kept, line)
	}
	if !dropped {
		return diagram
	}
	if edges < 3 || len(nodes) < 4 {
		return ""
	}
	return strings.Join(kept, "\n") + "\n"
}

// writeFlowText gives the startup, event, and dispatch sentence, which the
// three plain sections do not. The recognized component summary is not
// repeated here: it only restates the components, routes, and modules the
// detail sections list, or the API facts "Behavior" already gives.
func writeFlowText(w io.Writer, view verboseView) {
	if view.flow != "" {
		fmt.Fprintf(w, "\n### Startup and dispatch\n\n%s\n", reportText(view.flow))
	}
}
