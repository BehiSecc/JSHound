package overview

import (
	"fmt"
	"path"
	"sort"
	"strings"
)

// Overall answers the three questions of a file section for a whole set of
// files: what it is, what it does, and how it runs. It is built from the
// per-file results, never from the files again. Related is false when nothing
// shows the files belong together; the set is then described as separate files
// and its features and run facts are not merged.
type Overall struct {
	Is          string      `json:"is"`
	Files       int         `json:"files"`
	Related     bool        `json:"related"`
	Basis       string      `json:"basis"`
	Composition []KindCount `json:"composition,omitempty"`
	Platform    *Platform   `json:"platform,omitempty"`
	Frameworks  []string    `json:"frameworks,omitempty"`
	Build       string      `json:"build,omitempty"`
	Runtimes    []KindCount `json:"runtimes,omitempty"`
	// Links counts the files that name another input file; Entries lists the
	// files that start the application, most reaching first.
	Links       int         `json:"linked_files,omitempty"`
	Entries     []EntryFile `json:"entry_files,omitempty"`
	MoreEntries int         `json:"more_entry_files,omitempty"`
	Features    []Feature   `json:"features,omitempty"`
	Evidence    *Evidence   `json:"evidence,omitempty"`
	Runs        []RunFact   `json:"runs,omitempty"`
	// RunContext is the context the run facts are stated for: "page" when any
	// file is a page script, then "mixed", "no document", and "node".
	RunContext string `json:"run_context,omitempty"`
}

// KindCount counts the files of one kind, such as "ES module", with their size.
type KindCount struct {
	Kind  string `json:"kind"`
	Files int    `json:"files"`
	Bytes int    `json:"bytes,omitempty"`
}

// EntryFile is a file that starts the application. Loads counts the input
// files it names directly and Reaches those it names through other files.
type EntryFile struct {
	File    string `json:"file"`
	Why     string `json:"why"`
	Loads   int    `json:"loads,omitempty"`
	Reaches int    `json:"reaches,omitempty"`
}

const maxEntryFiles = 10

// fileFacts is what the roll-up keeps of one file besides its report.
type fileFacts struct {
	sig     fileSignals
	runtime string
	ctx     pageContext
	banner  bool // identified as a distributed library by its licence banner
	library bool // banner, or exports without startup
	starts  string
}

// rollupSink collects, while the files are analysed one by one, the facts the
// roll-up needs: one merged vocabulary and evidence set, and per-file signals
// and links. Nothing is lexed or scanned a second time.
type rollupSink struct {
	current int
	inputs  *inputIndex
	vocab   *vocabulary
	ev      *evidence
	facts   map[int]*fileFacts
	links   map[int][]int
}

func newRollupSink(sources []Source) *rollupSink {
	vocab := newVocabulary()
	vocab.setLevel = true
	return &rollupSink{
		inputs: newInputIndex(sources), vocab: vocab,
		ev:    &evidence{claimed: map[string]struct{}{}, byNoun: map[string][]int{}},
		facts: map[int]*fileFacts{}, links: map[int][]int{},
	}
}

// link records and names the input files the current file loads.
func (k *rollupSink) link(tokens []token, raw string, html bool) []string {
	targets := k.inputs.loads(k.current, tokens, raw, html)
	if len(targets) == 0 {
		return nil
	}
	k.links[k.current] = targets
	names := make([]string, len(targets))
	for i, target := range targets {
		names[i] = k.inputs.names[target]
	}
	return names
}

func (k *rollupSink) record(file *File, s fileSignals, runtime string) {
	banner := file.Purpose != nil && file.Purpose.Library != ""
	facts := &fileFacts{sig: s, runtime: runtime, banner: banner, library: banner || s.isLib}
	facts.ctx = runContext(s, runtime, s.platform)
	if !facts.library {
		facts.starts = startsApplication(s, runtime)
	}
	k.facts[k.current] = facts
}

// startsApplication says how a file starts an application on its own: it
// mounts a UI into a page, is the background of an extension or the main
// process of an Electron app, or is a Node.js server or command-line program.
func startsApplication(s fileSignals, runtime string) string {
	switch {
	case s.runs.mount.stands() && s.runs.mountTarget != "":
		ui := "a UI"
		for _, name := range s.frameworks {
			if uiFramework([]string{name}) {
				ui = "a " + name + " UI"
				break
			}
		}
		return "mounts " + ui + " into " + s.runs.mountTarget
	case s.runs.bootstrap.stands():
		return "bootstraps an Angular app"
	case s.platform != nil && s.platform.Name == "Browser extension" && (s.platform.Role == "service worker" || s.platform.Role == "background script"):
		return "is the extension's " + s.platform.Role
	case s.platform != nil && s.platform.Name == "Electron app" && s.platform.Role == "main process":
		return "is the Electron main process"
	case runtime == "Node.js" && s.server:
		return "starts a server"
	case runtime == "Node.js" && s.cli && s.hasEntryPoints:
		return "is a command-line program"
	}
	return ""
}

// fileKind says what sort of file one is, for counting and grouping.
func fileKind(file *File, s fileSignals) string {
	switch {
	case file.Purpose != nil && file.Purpose.Library != "", s.isLib && s.platform == nil:
		// A file written for a host, such as a plugin, is not called a library
		// even though it exports what its host calls.
		return "library"
	case strings.HasPrefix(file.Format, "HTML"):
		return "HTML page"
	case file.Bundle != nil:
		return "bundle chunk"
	case strings.HasSuffix(file.Format, "module"):
		return file.Format
	}
	return "script"
}

// kindOf is fileKind for a finished file, which also covers files without
// code.
func kindOf(file File) string {
	switch {
	case file.kind != "":
		return file.kind
	case strings.HasPrefix(file.Format, "HTML"):
		return "HTML page"
	case file.Purpose == nil:
		return "empty file"
	}
	return "script"
}

func kindPlural(kind string, count int) string {
	if count == 1 {
		return kind
	}
	if kind == "library" {
		return "libraries"
	}
	return kind + "s"
}

// absorb adds the names and sentences of another vocabulary, counting each
// distinct one once however many files carry it.
func (v *vocabulary) absorb(from *vocabulary) {
	for key := range from.seen {
		if _, ok := v.seen[key]; ok {
			continue
		}
		if text, prose := strings.CutPrefix(key, "\x00"); prose {
			if words := proseWords(text); words != nil {
				v.addProse(strings.Clone(text), words)
			}
			continue
		}
		v.addName(strings.Clone(key))
	}
}

// absorb adds the API calls and routes of another evidence set, counting each
// distinct path or operation once however many files carry it.
func (e *evidence) absorb(from *evidence) {
	for _, item := range from.items {
		if _, ok := e.claimed[item.key]; ok {
			continue
		}
		item.key, item.label, item.group = strings.Clone(item.key), strings.Clone(item.label), strings.Clone(item.group)
		e.claimed[item.key] = struct{}{}
		e.items = append(e.items, item)
	}
}

// index builds the lookup by domain noun, once all items are in.
func (e *evidence) index() {
	e.byNoun = map[string][]int{}
	for i, item := range e.items {
		for _, noun := range item.nouns {
			e.byNoun[noun] = append(e.byNoun[noun], i)
		}
	}
}

// merge adds the uses of another file's sighting and keeps the first location.
func (h *sighting) merge(other sighting, file string) {
	if other.uses == 0 {
		return
	}
	if h.uses == 0 {
		h.line, h.column, h.file = other.line, other.column, file
	}
	h.uses += other.uses
	h.solid += other.solid
}

// mergeScope says which signals of a file may enter the merge. A file
// contributes only what its own run context allows, as it would on its own.
type mergeScope struct {
	boots bool // starting and navigation: only page files that are not libraries
	page  bool // storage, audio, and camera: files with a page
	files bool // reading files: files that are not Node.js programs
}

func (d *runSignals) merge(s runSignals, file string, scope mergeScope) {
	m := func(dst *sighting, src sighting) { dst.merge(src, file) }
	if scope.boots {
		if d.mountTarget == "" && s.mountTarget != "" {
			d.mountTarget, d.mountCall = s.mountTarget, s.mountCall
		}
		m(&d.mount, s.mount)
		m(&d.bootstrap, s.bootstrap)
		m(&d.serviceWorker, s.serviceWorker)
		m(&d.historyCall, s.historyCall)
		m(&d.popState, s.popState)
		m(&d.hashRouting, s.hashRouting)
		m(&d.angularNav, s.angularNav)
	}
	if scope.page {
		m(&d.cookies, s.cookies)
		m(&d.localStorage, s.localStorage)
		m(&d.sessionStore, s.sessionStore)
		m(&d.audio, s.audio)
		m(&d.media, s.media)
	}
	if scope.files {
		m(&d.readsFiles, s.readsFiles)
	}
	m(&d.fetch, s.fetch)
	m(&d.xhr, s.xhr)
	m(&d.webSocket, s.webSocket)
	m(&d.eventSource, s.eventSource)
	m(&d.beacon, s.beacon)
	m(&d.graphQL, s.graphQL)
	m(&d.interceptors, s.interceptors)
	m(&d.indexedDB, s.indexedDB)
	m(&d.cacheAPI, s.cacheAPI)
	m(&d.chunkedUpload, s.chunkedUpload)
	m(&d.s3Upload, s.s3Upload)
	m(&d.googleUpload, s.googleUpload)
	m(&d.azureUpload, s.azureUpload)
	m(&d.workers, s.workers)
	m(&d.broadcast, s.broadcast)
	m(&d.messages, s.messages)
	m(&d.extensionRuntime, s.extensionRuntime)
	m(&d.extensionBackground, s.extensionBackground)
	for key, h := range s.authHeaders {
		into := d.authHeaders[key]
		if into == nil {
			if len(d.authHeaders) >= 32 {
				continue
			}
			into = &headerSighting{name: h.name}
			d.authHeaders[key] = into
		}
		into.merge(h.sighting, file)
	}
	for name, h := range s.signIn {
		if d.signIn[name] == nil {
			d.signIn[name] = &sighting{}
		}
		d.signIn[name].merge(*h, file)
	}
	for name, h := range s.services {
		if d.services[name] == nil {
			d.services[name] = &sighting{}
		}
		d.services[name].merge(*h, file)
	}
	for name, h := range s.extensionAPIs {
		if d.extensionAPIs[name] == nil {
			d.extensionAPIs[name] = &sighting{}
		}
		d.extensionAPIs[name].merge(*h, file)
	}
}

func newMergedSignals() runSignals {
	return runSignals{
		services: map[string]*sighting{}, authHeaders: map[string]*headerSighting{}, signIn: map[string]*sighting{},
		platformSignals: newPlatformSignals(),
	}
}

// contextName names a run context in the report and in JSON.
func contextName(ctx pageContext) string {
	switch ctx {
	case contextNoDocument:
		return "no document"
	case contextNode:
		return "node"
	case contextMixed:
		return "mixed"
	}
	return "page"
}

// setContext decides the run context of the set. Page facts are true for the
// set when any file is a page script, so the most permissive context among
// the files wins: page, then mixed, then no document, then Node.js.
func setContext(contexts []pageContext) pageContext {
	best := contextNode
	rank := map[pageContext]int{contextPage: 3, contextMixed: 2, contextNoDocument: 1, contextNode: 0}
	for _, ctx := range contexts {
		if rank[ctx] > rank[best] {
			best = ctx
		}
	}
	if len(contexts) == 0 {
		return contextPage
	}
	return best
}

// overall builds the roll-up from the finished per-file results and the facts
// collected on the way. It marks entry files on the files themselves for the
// text report.
func (k *rollupSink) overall(files []File) *Overall {
	o := &Overall{Files: len(files)}
	loadedBy := make([]int, len(files))
	for _, targets := range k.links {
		for _, target := range targets {
			loadedBy[target]++
		}
	}
	o.Links = len(k.links)
	o.Related, o.Basis = k.related(files)

	// Composition, runtimes, frameworks, and build tools come from the files'
	// own identities.
	kinds := map[string]*KindCount{}
	runtimes := map[string]*KindCount{}
	platforms := map[string]int{}
	frameworks := map[string]int{}
	builders := map[string]int{}
	var firstPlatform = map[string]int{}
	var contexts []pageContext
	merged := newMergedSignals()
	appStarts := false
	for i := range files {
		file := &files[i]
		kind := kindOf(*file)
		if kinds[kind] == nil {
			kinds[kind] = &KindCount{Kind: kind}
		}
		kinds[kind].Files++
		kinds[kind].Bytes += file.Bytes
		if strings.Contains(file.Build, "bundle") {
			builders[strings.TrimSuffix(strings.TrimSuffix(file.Build, " bundle"), "-style")]++
		}
		facts := k.facts[i]
		if facts == nil {
			continue
		}
		if facts.runtime != "" {
			if runtimes[facts.runtime] == nil {
				runtimes[facts.runtime] = &KindCount{Kind: facts.runtime}
			}
			runtimes[facts.runtime].Files++
			runtimes[facts.runtime].Bytes += file.Bytes
		}
		if p := facts.sig.platform; p != nil {
			if platforms[p.Name] == 0 {
				firstPlatform[p.Name] = i
			}
			platforms[p.Name]++
		}
		for _, name := range hostFrameworks(facts.sig) {
			frameworks[name]++
		}
		if facts.starts != "" {
			appStarts = appStarts || strings.HasPrefix(facts.starts, "mounts")
		}
		if facts.banner {
			continue
		}
		contexts = append(contexts, facts.ctx)
		merged.merge(facts.sig.runs, file.Name, mergeScope{
			boots: facts.ctx == contextPage && !facts.library,
			page:  hasPageAPIs(facts.ctx),
			files: facts.ctx != contextNode,
		})
	}
	o.Composition = sortedCounts(kinds)
	o.Runtimes = sortedCounts(runtimes)
	o.Frameworks = topNames(frameworks, 3)
	o.Build = joinWords(topNames(builders, 2))
	if o.Build != "" {
		o.Build = "bundled by " + o.Build
	}
	// The set has a host platform only when every file that shows one shows the
	// same one.
	var platform string
	if len(platforms) == 1 {
		platform = topNames(platforms, 1)[0]
	}

	if o.Related {
		k.entries(o, files, loadedBy)
		if platform != "" {
			o.Platform = &Platform{Name: platform}
			if p := k.facts[firstPlatform[platform]].sig.platform; p != nil {
				for _, e := range p.Evidence {
					e.File = files[firstPlatform[platform]].Name
					o.Platform.Evidence = append(o.Platform.Evidence, e)
				}
			}
		}
		ctx := setContext(contexts)
		o.RunContext = contextName(ctx)
		k.ev.index()
		o.Features, o.Evidence = k.vocab.features(k.ev), k.ev.totals()
		o.Runs = withDerivedRuns(runFacts(merged, topNames(frameworks, 8), false, ctx), evidenceRuns(k.ev)...)
	}
	o.Is = k.identity(o, files, platforms, appStarts)
	return o
}

// related decides whether the files belong together, and says why. They do
// when one names another, or when files in one folder share a build tool, a
// UI framework, a host platform, or a runtime. Several files from different
// folders with nothing in common are separate files, such as the plugins of an
// editor.
func (k *rollupSink) related(files []File) (bool, string) {
	if linked := len(k.links); linked > 0 {
		return true, fmt.Sprintf("%d %s name other input files", linked, plural(linked, "file"))
	}
	type key struct{ dir, trait string }
	counts := map[key]int{}
	for i := range files {
		dir := path.Dir(k.inputs.clean[i])
		var traits []string
		if strings.Contains(files[i].Build, "bundle") {
			builder := strings.TrimSuffix(strings.TrimSuffix(files[i].Build, " bundle"), "-style")
			traits = append(traits, "one build tool ("+builder+")")
		}
		if facts := k.facts[i]; facts != nil {
			if p := facts.sig.platform; p != nil {
				traits = append(traits, "one host ("+p.Name+")")
			}
			for _, name := range facts.sig.frameworks {
				if uiFramework([]string{name}) {
					traits = append(traits, "one UI framework ("+name+")")
				}
			}
			if facts.runtime != "" {
				traits = append(traits, "one runtime ("+facts.runtime+")")
			}
		}
		for _, trait := range traits {
			counts[key{dir, trait}]++
		}
	}
	best := key{}
	for candidate, n := range counts {
		if n >= 2 && (best == key{} || counts[best] < n || counts[best] == n && (candidate.trait < best.trait || candidate.trait == best.trait && candidate.dir < best.dir)) {
			best = candidate
		}
	}
	if best != (key{}) {
		return true, fmt.Sprintf("%d files in one folder share %s", counts[best], best.trait)
	}
	return false, "no file names another input file, and no folder holds several files that share a build tool, UI framework, host, or runtime"
}

// entries finds the files that start the application: files that others do
// not load and that load others, and files that start the app on their own.
// Reach counts the input files a file names through other files.
func (k *rollupSink) entries(o *Overall, files []File, loadedBy []int) {
	type found struct {
		index        int
		why          string
		loads, reach int
	}
	var all []found
	for i := range files {
		direct := len(k.links[i])
		var why []string
		if direct > 0 && loadedBy[i] == 0 {
			why = append(why, fmt.Sprintf("loads %d %s and no other file loads it", direct, plural(direct, "file")))
		}
		if facts := k.facts[i]; facts != nil && facts.starts != "" {
			why = append(why, facts.starts)
		}
		if len(why) == 0 {
			continue
		}
		all = append(all, found{index: i, why: strings.Join(why, "; "), loads: direct, reach: k.reach(i)})
	}
	sort.SliceStable(all, func(a, b int) bool {
		if all[a].reach != all[b].reach {
			return all[a].reach > all[b].reach
		}
		return files[all[a].index].Bytes > files[all[b].index].Bytes
	})
	for n, item := range all {
		files[item.index].entry = true
		if n >= maxEntryFiles {
			o.MoreEntries++
			continue
		}
		entry := EntryFile{File: files[item.index].Name, Why: item.why, Loads: item.loads}
		if item.reach > item.loads {
			entry.Reaches = item.reach
		}
		o.Entries = append(o.Entries, entry)
	}
}

// reach counts the other input files a file leads to through its links.
func (k *rollupSink) reach(from int) int {
	seen := map[int]struct{}{from: {}}
	queue := []int{from}
	for len(queue) > 0 {
		next := queue[0]
		queue = queue[1:]
		for _, target := range k.links[next] {
			if _, ok := seen[target]; !ok {
				seen[target] = struct{}{}
				queue = append(queue, target)
			}
		}
	}
	return len(seen) - 1
}

// identity is the answer to "what it is" for the set.
func (k *rollupSink) identity(o *Overall, files []File, platforms map[string]int, appStarts bool) string {
	if !o.Related {
		count := len(files)
		var parts []string
		for _, name := range topNames(platforms, 3) {
			parts = append(parts, fmt.Sprintf("%d %s", platforms[name], plural(platforms[name], name)))
		}
		sentence := fmt.Sprintf("%d separate files, not one application", count)
		if len(parts) > 0 {
			sentence += " (" + joinWords(parts) + ")"
		}
		sentence += "."
		return sentence
	}
	head := ""
	switch {
	case o.Platform != nil:
		head = o.Platform.Name
	default:
		runtime := ""
		if len(o.Runtimes) > 0 {
			runtime = o.Runtimes[0].Kind
		}
		kind := "scripts"
		if appStarts || uiFramework(o.Frameworks) {
			kind = "app"
		}
		if runtime == "" {
			head = capitalize(kind)
		} else {
			head = capitalize(runtime + " " + kind)
		}
	}
	if len(o.Frameworks) > 0 {
		head += " using " + strings.Join(o.Frameworks, ", ")
	}
	if o.Build != "" {
		head += ", " + o.Build
	}
	return head + "."
}

func sortedCounts(counts map[string]*KindCount) []KindCount {
	result := make([]KindCount, 0, len(counts))
	for _, item := range counts {
		result = append(result, *item)
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Files != result[j].Files {
			return result[i].Files > result[j].Files
		}
		return result[i].Kind < result[j].Kind
	})
	return result
}

// topNames returns the names with the highest counts, most first.
func topNames(counts map[string]int, limit int) []string {
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
	return names[:min(len(names), limit)]
}
