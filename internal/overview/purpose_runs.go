package overview

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// RunFact is one line of how a file behaves at run time, such as
// {Aspect: "Starts", Detail: "mounts a React UI into #root"}. Evidence lists
// the signals behind the line and where each was first seen.
type RunFact struct {
	Aspect   string        `json:"aspect"`
	Detail   string        `json:"detail"`
	Evidence []RunEvidence `json:"evidence,omitempty"`
}

// RunEvidence is one signal behind a run fact: how many times the file uses
// it and where the first use sits. Column is a one-based byte column. File
// names the input file of the first use; it is set only on facts that cover
// several files.
type RunEvidence struct {
	Signal string `json:"signal"`
	Uses   int    `json:"uses"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
	File   string `json:"file,omitempty"`
}

const maxAuthHeaders = 4

// sighting counts the uses of one signal and remembers the first. A solid use
// is a specific call, constructor, property access, or protocol or vendor
// constant (an SDK host, a signing algorithm, a vendor header). A word use is
// an ordinary word or name, such as "qr_code" or "partNumber", that only
// suggests the behavior.
type sighting struct {
	uses, solid  int
	line, column int
	file         string // input file of the first use; set only when merging files
}

// headerSighting is a header seen under any spelling of its name.
type headerSighting struct {
	name string // the first spelling in the code
	sighting
}

func (h *sighting) note(t token, solid bool) {
	if h.uses == 0 {
		h.line, h.column = t.line, t.column
	}
	h.uses++
	if solid {
		h.solid++
	}
}

// minWordUses is how often a word-only signal must appear before it is
// stated. One use of a call, constructor, or constant is enough; one word is
// not.
const minWordUses = 2

// stands reports whether the evidence is strong enough to state a claim.
func (h sighting) stands() bool {
	return h.solid > 0 || h.uses >= minWordUses
}

// runSignals are run-time behaviors observed anywhere in a file. Each comes
// from a specific call, constructor, property, or string, never from names
// alone, so minified bundles report what their code can actually do.
type runSignals struct {
	mountTarget, mountCall              string
	mount, bootstrap, serviceWorker     sighting
	historyCall, popState, hashRouting  sighting
	angularNav                          sighting
	fetch, xhr, webSocket, eventSource  sighting
	beacon, graphQL, interceptors       sighting
	authHeaders                         map[string]*headerSighting // by lowercase name
	cookies, localStorage, sessionStore sighting
	indexedDB, cacheAPI                 sighting
	signIn                              map[string]*sighting
	chunkedUpload                       sighting
	s3Upload, googleUpload, azureUpload sighting
	readsFiles, audio, media            sighting
	workers, broadcast, messages        sighting
	services                            map[string]*sighting
	platformSignals
}

var (
	headerName = regexp.MustCompile(`^(?:[Xx]-[A-Za-z0-9][A-Za-z0-9-]*|[Aa]uthorization)$`)
	authHint   = []string{"csrf", "xsrf", "token", "auth", "session", "signature", "api-key", "apikey", "account", "user", "tenant"}
)

// scanRunSignals reads every token once. Tokens inside code that is passed to
// a page for evaluation (masked) belong to that page, not to this file.
func scanRunSignals(tokens []token, mask []bool) runSignals {
	s := runSignals{
		services: map[string]*sighting{}, authHeaders: map[string]*headerSighting{}, signIn: map[string]*sighting{},
		platformSignals: newPlatformSignals(),
	}
	for i, t := range tokens {
		if isMasked(mask, i) {
			continue
		}
		switch t.kind {
		case 'i':
			s.identifier(tokens, i)
			for _, service := range identifierServices[t.text] {
				s.service(service).note(t, true)
			}
		case 's':
			value := stringValue(t.text)
			if value == "" {
				continue
			}
			s.stringValue(tokens, i, value)
		}
	}
	return s
}

// startsUI is the one definition of a file that starts a UI: a root or mount
// call that receives a container, or Angular's bootstrap call. Type and
// "Starts" both use it; a bare view.render() or router.mount(...) is not one.
func (s runSignals) startsUI() bool {
	return s.mount.stands() && s.mountTarget != "" || s.bootstrap.stands()
}

// noteAuthHeader counts a header use. Headers differing only in case are one
// header, named as the code first spells it.
func (s *runSignals) noteAuthHeader(lower, name string, t token) {
	if h := s.authHeaders[lower]; h != nil {
		h.note(t, true)
	} else if len(s.authHeaders) < 32 {
		s.authHeaders[lower] = &headerSighting{name: name}
		s.authHeaders[lower].note(t, true)
	}
}

func (s *runSignals) service(name string) *sighting {
	if s.services[name] == nil {
		s.services[name] = &sighting{}
	}
	return s.services[name]
}

func (s *runSignals) identifier(tokens []token, i int) {
	t := tokens[i]
	name := t.text
	prev, next := at(tokens, i-1), at(tokens, i+1)
	prevDot := prev == "." || prev == "?."
	created := prev == "new" || prevDot && at(tokens, i-2) == "window" && at(tokens, i-3) == "new"
	global := !prevDot || globalReceiver(at(tokens, i-2))
	switch name {
	case "getElementById", "querySelector":
		if next == "(" && at(tokens, i+3) == ")" && s.mountTarget == "" {
			target := stringValue(at(tokens, i+2))
			if name == "getElementById" && target != "" {
				target = "#" + target
			}
			if call := mountCallBefore(tokens, i); target != "" && call != "" {
				s.mountTarget, s.mountCall = target, call
				s.mount.note(t, true)
			}
		}
	case "mount", "$mount":
		// Vue: createApp(App).mount("#app") or new Vue(...).$mount("#app").
		if prevDot && next == "(" && at(tokens, i+3) == ")" && s.mountTarget == "" {
			if target := stringValue(at(tokens, i+2)); strings.HasPrefix(target, "#") {
				s.mountTarget, s.mountCall = target, "mount"
				s.mount.note(t, true)
			}
		}
	case "bootstrapApplication", "bootstrapModule", "bootstrapModuleFactory":
		if next == "(" {
			s.bootstrap.note(t, true)
		}
	case "Authorization", "authorization":
		// An unquoted object key, as in {headers:{Authorization:"Bearer "+t}},
		// is the same header as the quoted form.
		if (prev == "{" || prev == ",") && next == ":" {
			s.noteAuthHeader("authorization", name, t)
		}
	case "serviceWorker":
		if next == "." && at(tokens, i+2) == "register" {
			s.serviceWorker.note(t, true)
		}
	case "pushState", "replaceState":
		if prevDot && next == "(" {
			s.historyCall.note(t, true)
		}
	case "onpopstate":
		s.popState.note(t, false)
	case "fetch":
		if next == "(" && global && prev != "function" {
			s.fetch.note(t, true)
		}
	case "XMLHttpRequest":
		// Patching or testing for XMLHttpRequest is not a request.
		if created {
			s.xhr.note(t, true)
		}
	case "WebSocket":
		if created {
			s.webSocket.note(t, true)
		}
	case "EventSource":
		if created {
			s.eventSource.note(t, true)
		}
	case "sendBeacon":
		if prevDot {
			s.beacon.note(t, true)
		}
	case "interceptors":
		if next == "." && (at(tokens, i+2) == "request" || at(tokens, i+2) == "response") && at(tokens, i+4) == "use" {
			s.interceptors.note(t, true)
		}
	case "cookie":
		if prevDot && at(tokens, i-2) == "document" {
			s.cookies.note(t, true)
		}
	case "localStorage", "sessionStorage":
		switch storageUse(tokens, i) {
		case "localStorage":
			s.localStorage.note(t, true)
		case "sessionStorage":
			s.sessionStore.note(t, true)
		}
	case "indexedDB":
		if global && next == "." {
			s.indexedDB.note(t, true)
		}
	case "caches":
		if !prevDot && next == "." && at(tokens, i+2) == "open" {
			s.cacheAPI.note(t, true)
		}
	case "FileReader":
		if created {
			s.readsFiles.note(t, true)
		}
	case "createObjectURL":
		if prevDot {
			s.readsFiles.note(t, true)
		}
	case "AudioContext", "webkitAudioContext":
		if created {
			s.audio.note(t, true)
		}
	case "getUserMedia", "MediaRecorder":
		s.media.note(t, true)
	case "partNumber", "PartNumber", "uploadId", "UploadId":
		// Multipart upload fields, as in {partNumber: n, uploadId: id}.
		s.chunkedUpload.note(t, false)
	case "Worker", "SharedWorker":
		if created {
			s.workers.note(t, true)
		}
	case "BroadcastChannel":
		if created {
			s.broadcast.note(t, true)
		}
	case "postMessage":
		if prevDot && next == "(" {
			s.messages.note(t, true)
		}
	}
	s.platformIdentifier(tokens, i)
}

func globalReceiver(name string) bool {
	return name == "window" || name == "self" || name == "globalThis"
}

// mountCallBefore finds the UI mount call that receives a container lookup,
// as in createRoot(document.getElementById("root")).
func mountCallBefore(tokens []token, i int) string {
	for j := i - 1; j >= max(0, i-12); j-- {
		switch tokens[j].text {
		case "createRoot", "hydrateRoot", "render", "hydrate", "mount", "createApp", "bootstrap":
			return tokens[j].text
		case ";", "{", "}":
			return ""
		}
	}
	return ""
}

func (s *runSignals) stringValue(tokens []token, i int, value string) {
	t := tokens[i]
	switch value {
	case "popstate":
		s.popState.note(t, false)
		return
	case "hashchange":
		s.hashRouting.note(t, false)
		return
	case "router-outlet":
		s.angularNav.note(t, false)
		return
	case "AWS4-HMAC-SHA256":
		// Protocol and vendor constants are specific: one use is a use.
		s.s3Upload.note(t, true)
		return
	}
	if s.platformString(tokens, i, value) || len(value) > 300 {
		return
	}
	lower := strings.ToLower(value)
	if headerName.MatchString(value) && headerUse(tokens, i) {
		switch {
		case strings.HasPrefix(lower, "x-amz-"):
			s.s3Upload.note(t, true)
		case strings.HasPrefix(lower, "x-goog-upload-"):
			s.googleUpload.note(t, true)
		case strings.HasPrefix(lower, "x-ms-blob-"):
			s.azureUpload.note(t, true)
		case authHeader(lower):
			s.noteAuthHeader(lower, value, t)
		}
	}
	if strings.Contains(lower, "partnumber") || strings.Contains(lower, "part-number") || strings.Contains(lower, "part_number") || lower == "uploadid" {
		s.chunkedUpload.note(t, false)
	}
	if strings.Contains(lower, "/graphql") || lower == "graphql" {
		s.graphQL.note(t, false)
	}
	for _, service := range stringServiceMatches(lower) {
		s.service(service).note(t, true)
	}
	if len(value) <= 80 {
		for _, method := range signInWords(nameWords(value)) {
			if s.signIn[method] == nil {
				s.signIn[method] = &sighting{}
			}
			s.signIn[method].note(t, false)
		}
	}
}

// headerUse reports a header name used as an object key, an index, or the
// first argument of a header setter.
func headerUse(tokens []token, i int) bool {
	if next := at(tokens, i+1); next == ":" || next == "]" && at(tokens, i-1) == "[" {
		return true
	}
	if at(tokens, i-1) == "(" {
		switch at(tokens, i-2) {
		case "setRequestHeader", "set", "append", "header", "setHeader":
			return true
		}
	}
	return false
}

func authHeader(lower string) bool {
	if lower == "authorization" {
		return true
	}
	for _, hint := range authHint {
		if strings.Contains(lower, hint) {
			return true
		}
	}
	return false
}

var signInMethods = []string{"passkeys", "two-step verification", "one-time codes", "QR-code login", "OAuth", "single sign-on", "magic links"}

// signInWords maps the words of one name or message to sign-in methods, so
// "two_step_verification" and "scan_qr_code" count, while "Resso" does not.
func signInWords(words []string) []string {
	has := make(map[string]bool, len(words))
	for _, word := range words {
		has[word] = true
	}
	var methods []string
	if has["passkey"] || has["webauthn"] {
		methods = append(methods, "passkeys")
	}
	if has["two"] && (has["step"] || has["factor"]) || has["mfa"] || has["twostep"] || has["totp"] {
		methods = append(methods, "two-step verification")
	}
	if has["otp"] || has["one"] && has["time"] && (has["code"] || has["password"]) {
		methods = append(methods, "one-time codes")
	}
	if has["qr"] && (has["code"] || has["qrcode"]) && (has["scan"] || has["login"] || has["sign"] || has["passport"]) || has["qrcode"] && (has["login"] || has["scan"]) {
		methods = append(methods, "QR-code login")
	}
	if has["oauth"] {
		methods = append(methods, "OAuth")
	}
	if has["sso"] || has["saml"] {
		methods = append(methods, "single sign-on")
	}
	if has["magic"] && has["link"] {
		methods = append(methods, "magic links")
	}
	return methods
}

// runFacts turns signals into a few labelled lines. Navigation and startup are
// skipped for a distributed library, whose code only runs when called. Each
// signal must stand (see sighting), and a signal that needs a page is skipped
// when ctx shows the file runs without one.
func runFacts(s runSignals, frameworks []string, library bool, ctx pageContext) []RunFact {
	var facts []RunFact
	var evidence []RunEvidence
	cite := func(signal string, h sighting) { evidence = append(evidence, evidenceOf(signal, h)) }
	add := func(aspect string, parts ...string) {
		var kept []string
		for _, part := range parts {
			if part != "" {
				kept = append(kept, part)
			}
		}
		if len(kept) > 0 {
			facts = append(facts, RunFact{Aspect: aspect, Detail: strings.Join(kept, "; "), Evidence: evidence})
		}
		evidence = nil
	}
	hasPage := hasPageAPIs(ctx)
	if detail, apis := s.extensionFact(); detail != "" {
		evidence = apis
		add("Extension APIs", detail)
	}
	if !library && bootsInPage(ctx) {
		start := ""
		if s.mount.stands() && s.mountTarget != "" {
			ui := "its UI"
			for _, name := range frameworks {
				if name == "React" || name == "Vue" || name == "Preact" || name == "Svelte" {
					ui = "a " + name + " UI"
					break
				}
			}
			start = "mounts " + ui + " into " + s.mountTarget
			cite("UI mount", s.mount)
		} else if s.bootstrap.stands() {
			start = "bootstraps an Angular app"
			cite("bootstrap call", s.bootstrap)
		}
		worker := ""
		if s.serviceWorker.stands() {
			worker = "registers a service worker"
			cite("serviceWorker.register", s.serviceWorker)
		}
		add("Starts", start, worker)
		// Routing is more than one API call: a lone pushState or replaceState
		// call only edits the address bar. It needs a second call or a popstate
		// listener beside the call, and a popstate string alone is a listener
		// without a router.
		switch {
		case s.angularNav.stands():
			cite("router-outlet", s.angularNav)
			add("Navigation", "Angular router switches views without page reloads")
		case s.historyCall.stands() && (s.historyCall.uses >= 2 || s.popState.uses > 0):
			cite("pushState/replaceState", s.historyCall)
			if s.popState.uses > 0 {
				cite("popstate", s.popState)
			}
			add("Navigation", "client-side routing with the History API")
		case s.hashRouting.stands():
			cite("hashchange", s.hashRouting)
			add("Navigation", "hash-based routing")
		}
	}
	var clients []string
	for _, client := range []struct {
		name   string
		signal string
		h      sighting
	}{
		{"fetch", "fetch", s.fetch},
		{"XMLHttpRequest", "XMLHttpRequest", s.xhr},
		{"WebSocket", "WebSocket", s.webSocket},
		{"server-sent events", "EventSource", s.eventSource},
		{"beacons", "sendBeacon", s.beacon},
	} {
		if client.h.stands() {
			clients = append(clients, client.name)
			cite(client.signal, client.h)
		}
	}
	calls := ""
	if len(clients) > 0 {
		calls = joinWords(clients)
		if s.graphQL.stands() {
			calls += ", including GraphQL"
			cite("graphql", s.graphQL)
		}
	}
	headers, wrapped := "", ""
	var names []string
	spelled := map[string]*headerSighting{}
	for _, h := range s.authHeaders {
		if h.stands() {
			names = append(names, h.name)
			spelled[h.name] = h
		}
	}
	if len(names) > 0 {
		sort.Strings(names)
		list := names[:min(len(names), maxAuthHeaders)]
		headers = fmt.Sprintf("sends %s %s", joinWords(list), plural(len(list), "header"))
		for _, name := range list {
			cite("header "+name, spelled[name].sighting)
		}
	}
	if s.interceptors.stands() && calls != "" {
		wrapped = "interceptors wrap requests"
		cite("interceptors", s.interceptors)
	}
	add("Server calls", calls, headers, wrapped)
	var methods []string
	for _, method := range signInMethods {
		if h := s.signIn[method]; h != nil && h.stands() {
			methods = append(methods, method)
			cite(method, *h)
		}
	}
	add("Sign-in", joinWords(methods))
	var files []string
	chunked := ""
	if s.chunkedUpload.stands() {
		chunked = "chunked "
	}
	switch {
	case s.s3Upload.stands():
		files = append(files, chunked+"uploads to S3-compatible storage with signed requests")
		cite("S3 signing", s.s3Upload)
	case s.googleUpload.stands():
		files = append(files, "resumable uploads with the X-Goog-Upload protocol")
		cite("X-Goog-Upload header", s.googleUpload)
	case s.azureUpload.stands():
		files = append(files, chunked+"uploads to Azure Blob Storage")
		cite("x-ms-blob header", s.azureUpload)
	case s.chunkedUpload.stands():
		files = append(files, "chunked uploads")
	}
	if chunked != "" {
		cite("part and upload ids", s.chunkedUpload)
	}
	if s.readsFiles.stands() && ctx != contextNode {
		files = append(files, "reads files in the browser")
		cite("FileReader", s.readsFiles)
	}
	if s.audio.stands() && hasPage {
		files = append(files, "processes audio")
		cite("AudioContext", s.audio)
	}
	if s.media.stands() && hasPage {
		files = append(files, "records from the camera or microphone")
		cite("getUserMedia", s.media)
	}
	add("Files", strings.Join(files, ", "))
	var storage []string
	for _, store := range []struct {
		name, signal string
		h            sighting
		needsPage    bool
	}{
		{"cookies", "document.cookie", s.cookies, true},
		{"localStorage", "localStorage", s.localStorage, true},
		{"sessionStorage", "sessionStorage", s.sessionStore, true},
		{"IndexedDB", "indexedDB", s.indexedDB, false},
		{"Cache Storage", "caches.open", s.cacheAPI, false},
		{"extension storage", "chrome.storage", s.extensionStorage(), false},
	} {
		if store.h.stands() && (hasPage || !store.needsPage) {
			storage = append(storage, store.name)
			cite(store.signal, store.h)
		}
	}
	add("Storage", joinWords(storage))
	var background []string
	for _, item := range []struct {
		name, signal string
		h            sighting
	}{
		{"web workers", "Worker", s.workers},
		{"messages with other windows or workers", "postMessage", s.messages},
		{"BroadcastChannel", "BroadcastChannel", s.broadcast},
	} {
		if item.h.stands() {
			background = append(background, item.name)
			cite(item.signal, item.h)
		}
	}
	add("Background", joinWords(background))
	counts := map[string]int{}
	for name, h := range s.services {
		if h.stands() {
			counts[name] = h.uses
		}
	}
	shown := topServices(counts)
	for _, name := range shown {
		cite(name, *s.services[name])
	}
	add("Third parties", thirdParties(shown))
	return facts
}
