package overview

import (
	"fmt"
	"sort"
	"strings"
)

// Bundle contains facts about factories registered by one Webpack chunk. A
// registered factory is not an entry point: the host runtime decides when it
// is required.
type Bundle struct {
	Summary string         `json:"summary,omitempty"`
	Modules []BundleModule `json:"notable_modules,omitempty"`
	Startup *BundleStartup `json:"startup,omitempty"`
}

type BundleStartup struct {
	WaitFor []string        `json:"wait_for_chunks,omitempty"`
	Modules []StartupModule `json:"requires"`
}

type StartupModule struct {
	ID          string `json:"id"`
	DefinedHere bool   `json:"defined_here"`
	AssetPrefix string `json:"asset_prefix,omitempty"`
	MountedAt   string `json:"react_mount,omitempty"`
}

type BundleModule struct {
	ID            string   `json:"id"`
	Line          int      `json:"line"`
	Role          string   `json:"role"`
	Share         int      `json:"token_share_percent,omitempty"`
	Facts         []string `json:"facts,omitempty"`
	Flow          []string `json:"observed_flow,omitempty"`
	RouteFamilies []string `json:"route_families,omitempty"`
	Dependencies  []string `json:"dependencies_in_chunk,omitempty"`
}

type bundleCandidate struct {
	BundleModule
	score, size int
	requires    map[string]struct{}
	paths       []string
	flags       bundleFlags
}

type bundleFlags struct {
	verify, passport, credentials, interceptors, challenge bool
	login, register, qrLogin, forgotPassword               bool
	challengeResendCall                                    bool
	appLog, autotrack, abTest, storage                     bool
	coverage, qaReport, interval                           bool
	hooks, resize, orientation                             bool
	lazy, reactLazy, suspense, i18n, swiper                bool
	edenSSR, edenContext, appInit, rootTarget              bool
}

func startupModuleFacts(tokens []token, factory factoryRange) (assetPrefix, mountedAt string) {
	for i := factory.start; i+2 < factory.end; i++ {
		if at(tokens, i-2) == "window" && at(tokens, i-1) == "." && at(tokens, i) == "__assetPrefix__" &&
			at(tokens, i+1) == "=" && tokens[i+2].kind == 's' {
			assetPrefix = stringValue(tokens[i+2].text)
		}
		if at(tokens, i) != "createRoot" {
			continue
		}
		call := i + 1
		if at(tokens, call) == ")" { // (0, ReactDOM.createRoot)(...)
			call++
		}
		if at(tokens, call) != "(" || tokens[call].match <= call ||
			at(tokens, tokens[call].match+1) != "." || at(tokens, tokens[call].match+2) != "render" ||
			at(tokens, tokens[call].match+3) != "(" {
			continue
		}
		for j := call + 1; j+3 < tokens[call].match; j++ {
			if at(tokens, j-2) == "document" && at(tokens, j-1) == "." && at(tokens, j) == "getElementById" &&
				at(tokens, j+1) == "(" && tokens[j+2].kind == 's' && at(tokens, j+3) == ")" {
				mountedAt = "#" + stringValue(tokens[j+2].text)
				break
			}
		}
	}
	return assetPrefix, mountedAt
}

func analyzeBundle(tokens []token, chunk *chunkInfo) *Bundle {
	bundle := &Bundle{}
	ids := make(map[string]factoryRange, len(chunk.factories))
	for _, factory := range chunk.factories {
		ids[factory.id] = factory
	}
	if chunk.startup != nil {
		bundle.Startup = &BundleStartup{WaitFor: chunk.startup.waits}
		for _, id := range chunk.startup.requires {
			module := StartupModule{ID: id}
			if factory, ok := ids[id]; ok {
				module.DefinedHere = true
				module.AssetPrefix, module.MountedAt = startupModuleFacts(tokens, factory)
			}
			bundle.Startup.Modules = append(bundle.Startup.Modules, module)
		}
	}
	candidates := make([]bundleCandidate, 0, len(chunk.factories))
	for _, factory := range chunk.factories {
		candidate := inspectFactory(tokens, factory)
		candidate.size = factory.end - factory.start
		for dependency := range candidate.requires {
			if _, ok := ids[dependency]; ok {
				candidate.Dependencies = append(candidate.Dependencies, dependency)
			}
		}
		sort.Strings(candidate.Dependencies)
		classifyFactory(&candidate, len(tokens))
		if bundle.Startup != nil {
			for _, entry := range bundle.Startup.Modules {
				if entry.ID == factory.id && entry.AssetPrefix == "" {
					candidate.Role, candidate.score = "startup entry module", 120
					candidate.Facts = append([]string{"Required by the runtime startup callback."}, candidate.Facts...)
					if entry.MountedAt != "" {
						candidate.Facts = append(candidate.Facts, "Mounts a React root at "+entry.MountedAt+".")
					}
				}
			}
		}
		if candidate.score > 0 {
			candidates = append(candidates, candidate)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		return candidates[i].size > candidates[j].size
	})
	roles := make(map[string]struct{})
	for _, candidate := range candidates {
		if _, found := roles[candidate.Role]; found {
			continue
		}
		roles[candidate.Role] = struct{}{}
		bundle.Modules = append(bundle.Modules, candidate.BundleModule)
		if len(bundle.Modules) == 6 {
			break
		}
	}
	if len(bundle.Modules) > 0 {
		parts := make([]string, 0, len(bundle.Modules))
		for _, module := range bundle.Modules {
			label := module.Role
			if (label == "route catalog" || label == "passport route catalog") && len(module.RouteFamilies) > 0 {
				label += " (" + strings.SplitN(module.RouteFamilies[0], " (", 2)[0] + ")"
			}
			parts = append(parts, label)
		}
		bundle.Summary = "Contains " + strings.Join(parts, ", ") + "."
	}
	return bundle
}

func inspectFactory(tokens []token, factory factoryRange) bundleCandidate {
	c := bundleCandidate{BundleModule: BundleModule{ID: factory.id, Line: factory.line}, requires: make(map[string]struct{})}
	basePaths := make(map[string]string)
	moduleDepth := int16(0)
	if factory.start < len(tokens) {
		moduleDepth = tokens[factory.start].depth
	}
	for i := factory.start; i+2 < factory.end; i++ {
		if tokens[i].depth == moduleDepth && tokens[i].kind == 'i' && at(tokens, i+1) == "=" && tokens[i+2].kind == 's' &&
			(at(tokens, i-1) == "var" || at(tokens, i-1) == "const" || at(tokens, i-1) == "let" || at(tokens, i-1) == ",") {
			value := stringValue(tokens[i+2].text)
			if isRoutePath(value) {
				basePaths[tokens[i].text] = value
			}
		}
	}
	seenPaths := make(map[string]struct{})
	for i := factory.start; i < factory.end; i++ {
		t := tokens[i]
		word := t.text
		if t.kind == 's' {
			value := stringValue(t.text)
			word = value
			if strings.HasPrefix(value, "__tea_sdk_") {
				c.flags.appLog = true
				if strings.Contains(value, "_ab_") {
					c.flags.abTest = true
				}
			}
			if isRoutePath(value) {
				if at(tokens, i-1) == "+" {
					value = basePaths[at(tokens, i-2)] + value
				}
				if _, ok := seenPaths[value]; !ok {
					seenPaths[value] = struct{}{}
					c.paths = append(c.paths, value)
				}
			}
		}
		if factory.loader != "" && t.text == factory.loader && at(tokens, i+1) == "(" && i+3 < factory.end && tokens[i+2].kind == 'n' && at(tokens, i+3) == ")" {
			c.requires[tokens[i+2].text] = struct{}{}
		}
		if factory.loader != "" && t.text == factory.loader && at(tokens, i+1) == "." && at(tokens, i+2) == "e" && at(tokens, i+3) == "(" {
			c.flags.lazy = true
		}
		switch word {
		case "TTVerifyCenter":
			c.flags.verify = true
		case "login":
			c.flags.login = true
		case "register":
			c.flags.register = true
		case "qrcodeLogin":
			c.flags.qrLogin = true
		case "forgetPwd":
			c.flags.forgotPassword = true
		case "AppLog", "__tea_sdk":
			c.flags.appLog = true
		case "autotrack", "autoTrack":
			c.flags.autotrack = true
		case "abtest", "ABTest", "ab_test", "enable_ab_test", "ab_sdk_version":
			c.flags.abTest = true
		case "localForage", "localStorage", "sessionStorage":
			c.flags.storage = true
		case "__coverage__":
			c.flags.coverage = true
		case "qaPreReport", "setSendInterval":
			c.flags.qaReport = true
		case "setInterval":
			c.flags.interval = true
		case "useEffect", "useState", "useRef":
			c.flags.hooks = true
		case "resize":
			c.flags.resize = true
		case "orientationchange":
			c.flags.orientation = true
		case "withCredentials":
			c.flags.credentials = true
		case "interceptors":
			c.flags.interceptors = true
		case "verify_center_decision_conf":
			c.flags.challenge = true
		case "i18nInstance", "IntlMessageFormat":
			c.flags.i18n = true
		case "@edenx/plugin-ssr-compat":
			c.flags.edenSSR = true
		case "@edenx/plugin-context-compat":
			c.flags.edenContext = true
		case "appInit":
			c.flags.appInit = true
		case "root":
			if t.kind == 's' {
				c.flags.rootTarget = true
			}
		case "lazy":
			if at(tokens, i+1) == "(" {
				c.flags.reactLazy = true
			}
		case "Suspense":
			c.flags.suspense = true
		case "swiper", "swiperSlideSize", "Swiper":
			c.flags.swiper = true
		}
	}
	if c.flags.challenge && c.flags.verify {
		c.flags.challengeResendCall = hasChallengeResendCall(tokens, factory.start, factory.end)
	}
	sort.Strings(c.paths)
	return c
}

// Require a fingerprint call and a later function call that receives one
// object with both fp and isResend. Mere markers elsewhere are insufficient.
func hasChallengeResendCall(tokens []token, start, end int) bool {
	for i := start; i < end; i++ {
		if at(tokens, i) != "successCb" || at(tokens, i+1) != ":" {
			continue
		}
		body := -1
		if at(tokens, i+2) == "function" {
			params := i + 3
			if at(tokens, params) != "(" {
				params++
			}
			if at(tokens, params) == "(" && tokens[params].match > params {
				body = tokens[params].match + 1
			}
		} else if at(tokens, i+2) == "(" && tokens[i+2].match > i+2 && at(tokens, tokens[i+2].match+1) == "=>" {
			body = tokens[i+2].match + 2
		}
		if body < 0 || at(tokens, body) != "{" || tokens[body].match <= body {
			continue
		}
		before := max(start, i-400)
		hasConfig, hasData := false, false
		for j := before; j < i; j++ {
			hasConfig = hasConfig || tokens[j].text == "requestConfig"
			hasData = hasData || tokens[j].text == "verifyData"
		}
		if !hasConfig || !hasData {
			continue
		}
		fingerprintAt := -1
		for j := body + 1; j < tokens[body].match; j++ {
			if at(tokens, j) == "getFp" && at(tokens, j+1) == "(" {
				fingerprintAt = j
				break
			}
		}
		if fingerprintAt < 0 {
			continue
		}
		// Require the retry markers inside a later call's arguments. A
		// callback merely returning an object with those keys is insufficient.
		for j := fingerprintAt + 2; j < tokens[body].match; j++ {
			if at(tokens, j) != "(" || tokens[j].match <= j || tokens[j].match >= tokens[body].match ||
				j == 0 || tokens[j-1].kind != 'i' && at(tokens, j-1) != ")" {
				continue
			}
			for k := j + 1; k < tokens[j].match; k++ {
				if at(tokens, k) != "{" || tokens[k].match <= k || tokens[k].match >= tokens[j].match {
					continue
				}
				fp, resend := false, false
				for p := k + 1; p < tokens[k].match; p++ {
					if tokens[p].depth != tokens[k].depth+1 {
						continue
					}
					if at(tokens, p) == "fp" && (at(tokens, p+1) == ":" || at(tokens, p+1) == "," || at(tokens, p+1) == "}") {
						fp = true
					}
					if at(tokens, p) == "isResend" && at(tokens, p+1) == ":" &&
						(at(tokens, p+2) == "true" || at(tokens, p+2) == "!" && at(tokens, p+3) == "0") {
						resend = true
					}
				}
				if fp && resend {
					return true
				}
			}
		}
	}
	return false
}

func isRoutePath(value string) bool {
	if len(value) < 3 || len(value) > 150 || value[0] != '/' || value[1] == '/' ||
		!(value[1] >= 'a' && value[1] <= 'z' || value[1] >= 'A' && value[1] <= 'Z') {
		return false
	}
	for _, r := range value {
		if r <= ' ' || strings.ContainsRune("\"'<>\\;", r) {
			return false
		}
	}
	return true
}

func classifyFactory(c *bundleCandidate, totalTokens int) {
	f := c.flags
	passportPaths := 0
	for _, path := range c.paths {
		if strings.HasPrefix(path, "/passport/") {
			passportPaths++
		}
	}
	c.flags.passport = passportPaths >= 5
	switch {
	case f.edenSSR && f.edenContext && f.appInit:
		c.Role, c.score = "EdenX app initialization", 115
		c.Facts = append(c.Facts, "Defines EdenX SSR and context compatibility plugins.")
		if f.rootTarget {
			c.Facts = append(c.Facts, "References appInit and a root target in the same factory.")
		}
	case f.verify && c.flags.passport:
		c.Role, c.score = "account and verification SDK", 100
		c.Facts = append(c.Facts, "References TTVerifyCenter and passport/account routes.")
		if f.login && f.register && f.qrLogin && f.forgotPassword {
			c.Facts = append(c.Facts, "Contains login, registration, QR login, and password recovery operations.")
		}
		if f.credentials && f.interceptors {
			c.Facts = append(c.Facts, "Request client configures credentials and response interceptors.")
		}
		if f.challenge {
			c.Facts = append(c.Facts, "References the verify_center_decision_conf response field.")
		}
		if f.challengeResendCall {
			c.Flow = []string{
				"Account operations and passport route tables share this factory.",
				"Verification handling passes verifyData and requestConfig into a challenge callback.",
				"The success callback gets a fingerprint and calls a function with fp and isResend parameters.",
			}
		}
	case f.appLog && (f.autotrack || f.abTest):
		c.Role, c.score = "AppLog analytics SDK", 90
		parts := []string{"event collection"}
		if f.autotrack {
			parts = append(parts, "automatic tracking")
		}
		if f.abTest {
			parts = append(parts, "A/B tests")
		}
		if f.storage {
			parts = append(parts, "browser storage")
		}
		c.Facts = append(c.Facts, "Contains "+strings.Join(parts, ", ")+".")
	case f.coverage && f.qaReport:
		c.Role, c.score = "coverage uploader", 80
		c.Facts = append(c.Facts, "Reads __coverage__ and exposes QA reporting controls, including setSendInterval.")
		if f.interval {
			c.Facts = append(c.Facts, "Schedules reporting with setInterval().")
		}
	case f.lazy && f.reactLazy && f.suspense:
		c.Role, c.score = "application route shell", 95
		c.Facts = append(c.Facts, "Combines React.lazy, Suspense, and Webpack chunk requests to load components on demand.")
	case f.lazy:
		c.Role, c.score = "dynamic chunk loader", 70
		c.Facts = append(c.Facts, "Calls the Webpack loader's e() method to fetch additional chunks.")
	case f.swiper && f.resize:
		c.Role, c.score = "Swiper carousel UI", 65
		c.Facts = append(c.Facts, "Contains Swiper slide and resize behavior.")
	case f.hooks && f.resize && f.orientation:
		c.Role, c.score = "React viewport hooks", 60
		c.Facts = append(c.Facts, "Uses React hooks to register resize and orientationchange listeners.")
	case c.flags.passport:
		c.Role, c.score = "passport route catalog", 50
	case len(c.paths) >= 15:
		c.Role, c.score = "route catalog", 40
	case f.i18n:
		c.Role, c.score = "internationalization helpers", 30
	default:
		return
	}
	if totalTokens > 0 {
		c.Share = (100*c.size + totalTokens/2) / totalTokens
	}
	if len(c.paths) > 0 {
		families := routeFamilies(c.paths)
		c.RouteFamilies = families
		c.Facts = append(c.Facts, fmt.Sprintf("Defines %d route-like strings; main families: %s.", len(c.paths), strings.Join(families, ", ")))
		examples := routeExamples(c.paths)
		if len(examples) > 0 {
			c.Facts = append(c.Facts, "Examples: "+strings.Join(examples, ", "))
		}
	}
}

func routeFamilies(paths []string) []string {
	type item struct {
		name  string
		count int
	}
	groups := make(map[string][][]string)
	for _, path := range paths {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) > 0 {
			groups[parts[0]] = append(groups[parts[0]], parts)
		}
	}
	items := make([]item, 0, len(groups))
	var visit func([][]string, int, string)
	visit = func(group [][]string, depth int, prefix string) {
		children := make(map[string][][]string)
		if depth < 4 {
			for _, parts := range group {
				if len(parts) > depth && parts[depth] != "" {
					children[parts[depth]] = append(children[parts[depth]], parts)
				}
			}
		}
		for segment, child := range children {
			if len(child)*5 >= len(group)*4 { // a shared wrapper prefix adds no detail
				visit(child, depth+1, prefix+"/"+segment)
				return
			}
		}
		if len(group) >= 20 && len(children) >= 2 {
			major := make(map[string][][]string)
			total := 0
			for segment, child := range children {
				if len(child) >= 5 && len(child)*5 >= len(group) {
					major[segment] = child
					total += len(child)
				}
			}
			if len(major) >= 2 && total*10 >= len(group)*7 {
				for segment, child := range major {
					visit(child, depth+1, prefix+"/"+segment)
				}
				for segment, child := range children {
					if _, ok := major[segment]; !ok && len(child) >= 3 {
						items = append(items, item{prefix + "/" + segment, len(child)})
					}
				}
				return
			}
		}
		items = append(items, item{prefix, len(group)})
	}
	for segment, group := range groups {
		visit(group, 1, "/"+segment)
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].name < items[j].name
	})
	result := make([]string, 0, min(3, len(items)))
	hasRepeated := len(items) > 0 && items[0].count > 1
	for _, item := range items[:min(3, len(items))] {
		if hasRepeated && item.count == 1 {
			break
		}
		result = append(result, fmt.Sprintf("%s (%d)", item.name, item.count))
	}
	return result
}

func routeExamples(paths []string) []string {
	hasPassport := false
	for _, path := range paths {
		hasPassport = hasPassport || strings.HasPrefix(path, "/passport/")
	}
	if !hasPassport {
		families := routeFamilies(paths)
		result := make([]string, 0, 3)
		seen := make(map[string]struct{})
		for _, family := range families {
			prefix := strings.SplitN(family, " (", 2)[0]
			for _, path := range paths {
				if strings.HasPrefix(path, prefix+"/") && path != prefix+"/" {
					result = append(result, path)
					seen[path] = struct{}{}
					break
				}
			}
			if len(result) == 3 {
				return result
			}
		}
		if len(families) > 0 {
			prefix := strings.SplitN(families[0], " (", 2)[0]
			for _, path := range paths {
				if len(result) == 3 {
					break
				}
				if strings.HasPrefix(path, prefix+"/") {
					if _, ok := seen[path]; !ok {
						result = append(result, path)
						seen[path] = struct{}{}
					}
				}
			}
		}
		if len(result) == 0 {
			return paths[:min(3, len(paths))]
		}
		return result
	}
	patterns := []string{"/auth/login/", "/account/info/", "/role/list/", "/qrcode", "/email/send_code/"}
	result := make([]string, 0, 3)
	seen := make(map[string]struct{})
	for _, pattern := range patterns {
		best := ""
		for _, path := range paths {
			if strings.Contains(path, pattern) && (best == "" || routeExampleRank(path) > routeExampleRank(best)) {
				best = path
			}
		}
		if best != "" {
			if _, ok := seen[best]; !ok {
				result = append(result, best)
				seen[best] = struct{}{}
			}
		}
		if len(result) == 3 {
			return result
		}
	}
	for _, path := range paths {
		if len(result) == 3 {
			break
		}
		if _, ok := seen[path]; !ok {
			result = append(result, path)
			seen[path] = struct{}{}
		}
	}
	return result
}

func routeExampleRank(path string) int {
	switch {
	case strings.HasPrefix(path, "/passport/web/"):
		return 4
	case strings.HasPrefix(path, "/passport/aff/web/"):
		return 3
	case strings.HasPrefix(path, "/passport/mobile/"):
		return 2
	case strings.HasPrefix(path, "/passport/"):
		return 1
	}
	return 0
}

func bundleDiagram(bundle *Bundle, chunk *chunkInfo) string {
	if bundle == nil || len(bundle.Modules) == 0 {
		return ""
	}
	hasFlow := bundle.Startup != nil
	for _, module := range bundle.Modules {
		hasFlow = hasFlow || len(module.Flow) > 0
	}
	if !hasFlow {
		return ""
	}
	var out strings.Builder
	out.WriteString("flowchart LR\n")
	count := fmt.Sprintf("%d factories", chunk.modules)
	if chunk.recovered {
		count = fmt.Sprintf("%d+ recognized factories", chunk.modules)
	}
	fmt.Fprintf(&out, "  host[\"Webpack runtime\"] -->|registers| chunk[\"chunk %s · %s\"]\n", mermaidLabel(chunk.id), count)
	visible := bundle.Modules[:min(4, len(bundle.Modules))]
	for i, module := range visible {
		fmt.Fprintf(&out, "  chunk -->|contains| m%d[\"%s · %s\"]\n", i, mermaidLabel(module.ID), mermaidLabel(module.Role))
	}
	if bundle.Startup != nil {
		out.WriteString("  chunk -->|provides| boot[\"startup callback\"]\n")
		if len(bundle.Startup.WaitFor) > 0 {
			fmt.Fprintf(&out, "  wait[\"chunks %s ready\"] -->|unblocks| boot\n", mermaidLabel(strings.Join(bundle.Startup.WaitFor, ", ")))
		}
		for i, entry := range bundle.Startup.Modules {
			target := fmt.Sprintf("entry%d", i)
			for j, module := range visible {
				if module.ID == entry.ID {
					target = fmt.Sprintf("m%d", j)
					break
				}
			}
			if strings.HasPrefix(target, "entry") {
				label := "module " + entry.ID + " in another chunk"
				if entry.DefinedHere {
					label = "module " + entry.ID
				}
				fmt.Fprintf(&out, "  %s[\"%s\"]\n", target, mermaidLabel(label))
			}
			fmt.Fprintf(&out, "  boot -->|requires| %s\n", target)
			if entry.AssetPrefix != "" {
				fmt.Fprintf(&out, "  %s -->|sets| asset%d[\"asset prefix\"]\n", target, i)
			}
			if entry.MountedAt != "" {
				fmt.Fprintf(&out, "  %s -->|mounts React| mount%d[\"%s\"]\n", target, i, mermaidLabel(entry.MountedAt))
			}
		}
	}
	for i, module := range visible {
		for _, dependency := range module.Dependencies {
			for j, target := range visible {
				if i != j && dependency == target.ID {
					fmt.Fprintf(&out, "  m%d -->|requires| m%d\n", i, j)
				}
			}
		}
	}
	for i, module := range visible {
		if module.Role != "route catalog" && module.Role != "passport route catalog" {
			continue
		}
		for j, family := range module.RouteFamilies[:min(2, len(module.RouteFamilies))] {
			fmt.Fprintf(&out, "  m%d -->|defines paths| routes%d_%d[\"%s\"]\n", i, i, j, mermaidLabel(family))
		}
	}
	for i, module := range visible {
		if module.Role == "EdenX app initialization" {
			fmt.Fprintf(&out, "  m%d -->|configures| plugins[\"SSR and context plugins\"]\n", i)
			fmt.Fprintf(&out, "  m%d -->|references| init[\"appInit and root target\"]\n", i)
		}
		if module.Role == "account and verification SDK" {
			if len(module.Flow) > 0 {
				fmt.Fprintf(&out, "  m%d -->|contains| request[\"request and passport routes\"]\n", i)
				fmt.Fprintf(&out, "  m%d -->|contains| verify[\"TTVerifyCenter callback\"]\n", i)
				out.WriteString("  verify -->|success callback| retry[\"getFp; call with fp and isResend\"]\n")
			} else {
				fmt.Fprintf(&out, "  m%d -->|defines| routes[\"passport route tables\"]\n", i)
				fmt.Fprintf(&out, "  m%d -->|contains| verify[\"TTVerifyCenter challenge UI\"]\n", i)
			}
			break
		}
	}
	return out.String()
}
