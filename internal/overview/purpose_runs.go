package overview

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// RunFact is one line of how a file behaves at run time, such as
// {Aspect: "Starts", Detail: "mounts a React UI into #root"}.
type RunFact struct {
	Aspect string `json:"aspect"`
	Detail string `json:"detail"`
}

const maxAuthHeaders = 4

// runSignals are run-time behaviors observed anywhere in a file. Each comes
// from a specific call, constructor, property, or string, never from names
// alone, so minified bundles report what their code can actually do.
type runSignals struct {
	mountTarget, mountCall              string
	serviceWorker                       bool
	historyAPI, hashRouting, angularNav bool
	fetch, xhr, webSocket, eventSource  bool
	beacon, graphQL, interceptors       bool
	authHeaders                         []string
	cookies, localStorage, sessionStore bool
	indexedDB, cacheAPI                 bool
	signIn                              []string
	chunkedUpload                       bool
	s3Upload, googleUpload, azureUpload bool
	readsFiles, audio, media            bool
	workers, broadcast, messages        bool
	services                            map[string]int
}

var (
	headerName = regexp.MustCompile(`^(?:[Xx]-[A-Za-z0-9][A-Za-z0-9-]*|[Aa]uthorization)$`)
	authHint   = []string{"csrf", "xsrf", "token", "auth", "session", "signature", "api-key", "apikey", "account", "user", "tenant"}
)

func scanRunSignals(tokens []token) runSignals {
	s := runSignals{services: map[string]int{}}
	headers := map[string]bool{}
	signIn := map[string]bool{}
	for i, t := range tokens {
		switch t.kind {
		case 'i':
			s.identifier(tokens, i)
			for _, service := range identifierServices[t.text] {
				s.services[service]++
			}
		case 's':
			value := stringValue(t.text)
			if value == "" {
				continue
			}
			s.stringValue(tokens, i, value, headers, signIn)
		}
	}
	for header := range headers {
		s.authHeaders = append(s.authHeaders, header)
	}
	sort.Strings(s.authHeaders)
	for _, method := range signInMethods {
		if signIn[method] {
			s.signIn = append(s.signIn, method)
		}
	}
	return s
}

func (s *runSignals) identifier(tokens []token, i int) {
	name := tokens[i].text
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
			}
		}
	case "mount", "$mount":
		// Vue: createApp(App).mount("#app") or new Vue(...).$mount("#app").
		if prevDot && next == "(" && at(tokens, i+3) == ")" && s.mountTarget == "" {
			if target := stringValue(at(tokens, i+2)); strings.HasPrefix(target, "#") {
				s.mountTarget, s.mountCall = target, "mount"
			}
		}
	case "serviceWorker":
		s.serviceWorker = s.serviceWorker || next == "." && at(tokens, i+2) == "register"
	case "pushState", "replaceState":
		s.historyAPI = s.historyAPI || prevDot && next == "("
	case "fetch":
		s.fetch = s.fetch || next == "(" && global && prev != "function"
	case "XMLHttpRequest":
		s.xhr = true
	case "WebSocket":
		s.webSocket = s.webSocket || created
	case "EventSource":
		s.eventSource = s.eventSource || created
	case "sendBeacon":
		s.beacon = s.beacon || prevDot
	case "interceptors":
		if next == "." && (at(tokens, i+2) == "request" || at(tokens, i+2) == "response") && at(tokens, i+4) == "use" {
			s.interceptors = true
		}
	case "cookie":
		s.cookies = s.cookies || prevDot && at(tokens, i-2) == "document"
	case "localStorage", "sessionStorage":
		switch storageUse(tokens, i) {
		case "localStorage":
			s.localStorage = true
		case "sessionStorage":
			s.sessionStore = true
		}
	case "indexedDB":
		s.indexedDB = s.indexedDB || global && next == "."
	case "caches":
		s.cacheAPI = s.cacheAPI || !prevDot && next == "." && at(tokens, i+2) == "open"
	case "FileReader":
		s.readsFiles = s.readsFiles || created
	case "createObjectURL":
		s.readsFiles = s.readsFiles || prevDot
	case "AudioContext", "webkitAudioContext":
		s.audio = s.audio || created
	case "getUserMedia", "MediaRecorder":
		s.media = true
	case "partNumber", "PartNumber", "uploadId", "UploadId":
		// Multipart upload fields, as in {partNumber: n, uploadId: id}.
		s.chunkedUpload = true
	case "Worker", "SharedWorker":
		s.workers = s.workers || created
	case "BroadcastChannel":
		s.broadcast = s.broadcast || created
	case "postMessage":
		s.messages = s.messages || prevDot && next == "("
	}
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

func (s *runSignals) stringValue(tokens []token, i int, value string, headers, signIn map[string]bool) {
	switch value {
	case "popstate":
		s.historyAPI = true
		return
	case "hashchange":
		s.hashRouting = true
		return
	case "router-outlet":
		s.angularNav = true
		return
	case "AWS4-HMAC-SHA256":
		s.s3Upload = true
		return
	}
	if len(value) > 300 {
		return
	}
	lower := strings.ToLower(value)
	if headerName.MatchString(value) && headerUse(tokens, i) {
		switch {
		case strings.HasPrefix(lower, "x-amz-"):
			s.s3Upload = true
		case strings.HasPrefix(lower, "x-goog-upload-"):
			s.googleUpload = true
		case strings.HasPrefix(lower, "x-ms-blob-"):
			s.azureUpload = true
		case authHeader(lower) && len(headers) < 32:
			headers[value] = true
		}
	}
	if strings.Contains(lower, "partnumber") || strings.Contains(lower, "part-number") || strings.Contains(lower, "part_number") || lower == "uploadid" {
		s.chunkedUpload = true
	}
	if strings.Contains(lower, "/graphql") || lower == "graphql" {
		s.graphQL = true
	}
	for _, service := range stringServiceMatches(lower) {
		s.services[service]++
	}
	if len(value) <= 80 {
		for _, method := range signInWords(nameWords(value)) {
			signIn[method] = true
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
// skipped for a distributed library, whose code only runs when called.
func runFacts(s runSignals, frameworks []string, library bool) []RunFact {
	var facts []RunFact
	add := func(aspect string, parts ...string) {
		var kept []string
		for _, part := range parts {
			if part != "" {
				kept = append(kept, part)
			}
		}
		if len(kept) > 0 {
			facts = append(facts, RunFact{Aspect: aspect, Detail: strings.Join(kept, "; ")})
		}
	}
	if !library {
		start := ""
		if s.mountTarget != "" {
			ui := "its UI"
			for _, name := range frameworks {
				if name == "React" || name == "Vue" || name == "Preact" || name == "Svelte" {
					ui = "a " + name + " UI"
					break
				}
			}
			start = "mounts " + ui + " into " + s.mountTarget
		}
		worker := ""
		if s.serviceWorker {
			worker = "registers a service worker"
		}
		add("Starts", start, worker)
		switch {
		case s.angularNav:
			add("Navigation", "Angular router switches views without page reloads")
		case s.historyAPI:
			add("Navigation", "client-side routing with the History API")
		case s.hashRouting:
			add("Navigation", "hash-based routing")
		}
	}
	var clients []string
	if s.fetch {
		clients = append(clients, "fetch")
	}
	if s.xhr {
		clients = append(clients, "XMLHttpRequest")
	}
	if s.webSocket {
		clients = append(clients, "WebSocket")
	}
	if s.eventSource {
		clients = append(clients, "server-sent events")
	}
	if s.beacon {
		clients = append(clients, "beacons")
	}
	calls := ""
	if len(clients) > 0 {
		calls = joinWords(clients)
		if s.graphQL {
			calls += ", including GraphQL"
		}
	}
	headers, wrapped := "", ""
	if len(s.authHeaders) > 0 {
		list := s.authHeaders[:min(len(s.authHeaders), maxAuthHeaders)]
		headers = fmt.Sprintf("sends %s %s", joinWords(list), plural(len(list), "header"))
	}
	if s.interceptors && calls != "" {
		wrapped = "interceptors wrap requests"
	}
	add("Server calls", calls, headers, wrapped)
	if len(s.signIn) > 0 {
		add("Sign-in", joinWords(s.signIn))
	}
	var files []string
	chunked := ""
	if s.chunkedUpload {
		chunked = "chunked "
	}
	switch {
	case s.s3Upload:
		files = append(files, chunked+"uploads to S3-compatible storage with signed requests")
	case s.googleUpload:
		files = append(files, "resumable uploads with the X-Goog-Upload protocol")
	case s.azureUpload:
		files = append(files, chunked+"uploads to Azure Blob Storage")
	case s.chunkedUpload:
		files = append(files, "chunked uploads")
	}
	if s.readsFiles {
		files = append(files, "reads files in the browser")
	}
	if s.audio {
		files = append(files, "processes audio")
	}
	if s.media {
		files = append(files, "records from the camera or microphone")
	}
	add("Files", strings.Join(files, ", "))
	var storage []string
	if s.cookies {
		storage = append(storage, "cookies")
	}
	if s.localStorage {
		storage = append(storage, "localStorage")
	}
	if s.sessionStore {
		storage = append(storage, "sessionStorage")
	}
	if s.indexedDB {
		storage = append(storage, "IndexedDB")
	}
	if s.cacheAPI {
		storage = append(storage, "Cache Storage")
	}
	add("Storage", joinWords(storage))
	var background []string
	if s.workers {
		background = append(background, "web workers")
	}
	if s.messages {
		background = append(background, "messages with other windows or workers")
	}
	if s.broadcast {
		background = append(background, "BroadcastChannel")
	}
	add("Background", joinWords(background))
	add("Third parties", thirdParties(s.services))
	return facts
}
