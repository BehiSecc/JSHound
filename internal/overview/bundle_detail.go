package overview

import (
	"fmt"
	"sort"
	"strings"
)

// Bundle contains facts about Webpack registrations in one source. Factories
// belong to a runtime, and registration alone does not execute a factory.
type Bundle struct {
	Summary      string             `json:"summary,omitempty"`
	Chunks       []BundleChunk      `json:"chunks,omitempty"`
	Modules      []BundleModule     `json:"notable_modules,omitempty"`
	Dependencies []BundleDependency `json:"observed_dependencies,omitempty"`
	Startup      *BundleStartup     `json:"startup,omitempty"`
}

type BundleChunk struct {
	ID          string         `json:"id"`
	Runtime     string         `json:"runtime"`
	Line        int            `json:"line"`
	ModuleCount int            `json:"module_count"`
	Startup     *BundleStartup `json:"startup,omitempty"`
}

type BundleDependency struct {
	Runtime     string `json:"runtime"`
	FromChunk   string `json:"from_chunk"`
	From        string `json:"from"`
	To          string `json:"to"`
	ToChunk     string `json:"to_chunk,omitempty"`
	DefinedHere bool   `json:"defined_here"`
	Ambiguous   bool   `json:"ambiguous,omitempty"`
	Line        int    `json:"line"`
}

type BundleStartup struct {
	WaitFor []string        `json:"wait_for_chunks,omitempty"`
	Modules []StartupModule `json:"requires"`
}

type StartupModule struct {
	ID             string `json:"id"`
	DefinedHere    bool   `json:"defined_here"`
	Ambiguous      bool   `json:"ambiguous,omitempty"`
	DefinedInChunk string `json:"defined_in_chunk,omitempty"`
	AssetPrefix    string `json:"asset_prefix,omitempty"`
	MountedAt      string `json:"react_mount,omitempty"`
}

type BundleModule struct {
	ID                    string   `json:"id"`
	Chunk                 string   `json:"chunk,omitempty"`
	Runtime               string   `json:"runtime,omitempty"`
	Line                  int      `json:"line"`
	Role                  string   `json:"role"`
	Share                 int      `json:"token_share_percent,omitempty"`
	Facts                 []string `json:"facts,omitempty"`
	Flow                  []string `json:"observed_flow,omitempty"`
	Dependencies          []string `json:"dependencies_in_chunk,omitempty"`
	OtherDependencies     []string `json:"dependencies_in_file,omitempty"`
	ExternalDependencies  []string `json:"unresolved_dependencies,omitempty"`
	AmbiguousDependencies []string `json:"ambiguous_dependencies,omitempty"`
	LazyPages             bool     `json:"lazy_page_loading,omitempty"`
	Units                 []Unit   `json:"local_units,omitempty"`
	ObservedLinks         []Link   `json:"local_links,omitempty"`
	MoreUnits             int      `json:"more_local_units,omitempty"`
}

type bundleCandidate struct {
	BundleModule
	score, size int
	requires    map[string]int
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

type registeredFactory struct {
	chunk   *chunkInfo
	factory factoryRange
}

type moduleEvidence struct {
	role                            string
	components, routes, apiWrappers int
}

type moduleIdentity struct{ runtime, id string }

func indexModuleEvidence(insights *Insights) map[moduleIdentity]moduleEvidence {
	indexed := make(map[moduleIdentity]moduleEvidence)
	if insights == nil {
		return indexed
	}
	for _, component := range insights.Components {
		key := moduleIdentity{component.Runtime, component.Module}
		item := indexed[key]
		item.components++
		if item.role == "" {
			item.role = component.Role
		}
		indexed[key] = item
	}
	for _, route := range insights.Routes {
		if route.Kind != "declared route" {
			continue
		}
		key := moduleIdentity{route.Runtime, route.Module}
		item := indexed[key]
		item.routes++
		indexed[key] = item
	}
	for _, api := range insights.API {
		if api.Kind != "request wrapper" {
			continue
		}
		key := moduleIdentity{api.Runtime, api.Module}
		item := indexed[key]
		item.apiWrappers++
		indexed[key] = item
	}
	return indexed
}

func analyzeBundle(tokens []token, chunks []*chunkInfo, insights *Insights) *Bundle {
	if len(chunks) == 0 {
		return nil
	}
	bundle := &Bundle{}
	byRuntime := make(map[string]map[string][]registeredFactory)
	totalModules := 0
	for _, chunk := range chunks {
		totalModules += chunk.modules
		if byRuntime[chunk.global] == nil {
			byRuntime[chunk.global] = make(map[string][]registeredFactory)
		}
		for _, factory := range chunk.factories {
			byRuntime[chunk.global][factory.id] = append(byRuntime[chunk.global][factory.id], registeredFactory{chunk, factory})
		}
	}
	for _, chunk := range chunks {
		entry := BundleChunk{ID: chunk.id, Runtime: chunk.global, Line: chunk.line, ModuleCount: chunk.modules}
		if chunk.startup != nil {
			entry.Startup = &BundleStartup{WaitFor: append([]string(nil), chunk.startup.waits...)}
			for _, id := range chunk.startup.requires {
				module := StartupModule{ID: id}
				if targets := byRuntime[chunk.global][id]; len(targets) == 1 {
					module.DefinedHere = true
					module.DefinedInChunk = targets[0].chunk.id
					module.AssetPrefix, module.MountedAt = startupModuleFacts(tokens, targets[0].factory)
				} else if len(targets) > 1 {
					module.Ambiguous = true
				}
				entry.Startup.Modules = append(entry.Startup.Modules, module)
			}
			if bundle.Startup == nil {
				bundle.Startup = entry.Startup
			}
		}
		bundle.Chunks = append(bundle.Chunks, entry)
	}
	evidence := indexModuleEvidence(insights)
	candidates := make([]bundleCandidate, 0, totalModules)
	for _, chunk := range chunks {
		for _, factory := range chunk.factories {
			candidate := inspectFactory(tokens, factory)
			candidate.Chunk, candidate.Runtime = chunk.id, chunk.global
			candidate.size = factory.end - factory.start
			classifyFactory(&candidate, len(tokens))
			if len(byRuntime[chunk.global][factory.id]) == 1 {
				rankFactoryEvidence(&candidate, evidence[moduleIdentity{chunk.global, factory.id}])
			}
			if candidate.score > 0 {
				if len(tokens) > 0 && candidate.Share == 0 {
					candidate.Share = (100*candidate.size + len(tokens)/2) / len(tokens)
				}
				if len(candidate.paths) > 0 {
					candidate.Facts = append(candidate.Facts, fmt.Sprintf("Contains %d path-like strings.", len(candidate.paths)))
				}
			}
			for dependency, line := range candidate.requires {
				dep := BundleDependency{Runtime: chunk.global, FromChunk: chunk.id, From: factory.id, To: dependency, Line: line}
				if targets := byRuntime[chunk.global][dependency]; len(targets) == 1 {
					dep.DefinedHere, dep.ToChunk = true, targets[0].chunk.id
					if dep.ToChunk == chunk.id {
						candidate.Dependencies = append(candidate.Dependencies, dependency)
					} else {
						candidate.OtherDependencies = append(candidate.OtherDependencies, dependency)
					}
				} else if len(byRuntime[chunk.global][dependency]) > 1 {
					dep.Ambiguous = true
					candidate.AmbiguousDependencies = append(candidate.AmbiguousDependencies, dependency)
				} else {
					candidate.ExternalDependencies = append(candidate.ExternalDependencies, dependency)
				}
				bundle.Dependencies = append(bundle.Dependencies, dep)
			}
			sort.Strings(candidate.Dependencies)
			sort.Strings(candidate.OtherDependencies)
			sort.Strings(candidate.ExternalDependencies)
			sort.Strings(candidate.AmbiguousDependencies)
			for _, registered := range bundle.Chunks {
				if registered.Runtime != chunk.global || registered.Startup == nil {
					continue
				}
				for _, startup := range registered.Startup.Modules {
					if startup.ID != factory.id || !startup.DefinedHere || startup.DefinedInChunk != chunk.id {
						continue
					}
					if startup.AssetPrefix != "" {
						candidate.Role, candidate.score = "asset configuration", max(candidate.score, 85)
						candidate.Facts = append(candidate.Facts, "Sets the asset prefix when the startup callback requires this module.")
					} else {
						candidate.Role, candidate.score = "startup entry module", max(candidate.score, 120)
						candidate.Facts = append([]string{"Required by the runtime startup callback."}, candidate.Facts...)
					}
					if startup.MountedAt != "" {
						candidate.Facts = append(candidate.Facts, "Mounts a React root at "+startup.MountedAt+".")
					}
				}
			}
			if candidate.score > 0 {
				candidates = append(candidates, candidate)
			}
		}
	}
	sort.Slice(bundle.Dependencies, func(i, j int) bool {
		a, b := bundle.Dependencies[i], bundle.Dependencies[j]
		if a.Runtime != b.Runtime {
			return a.Runtime < b.Runtime
		}
		if a.FromChunk != b.FromChunk {
			return a.FromChunk < b.FromChunk
		}
		if a.From != b.From {
			return a.From < b.From
		}
		return a.To < b.To
	})
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].score != candidates[j].score {
			return candidates[i].score > candidates[j].score
		}
		if candidates[i].size != candidates[j].size {
			return candidates[i].size > candidates[j].size
		}
		if candidates[i].Runtime != candidates[j].Runtime {
			return candidates[i].Runtime < candidates[j].Runtime
		}
		return candidates[i].ID < candidates[j].ID
	})
	for _, candidate := range candidates[:min(12, len(candidates))] {
		module := candidate.BundleModule
		for _, registered := range byRuntime[module.Runtime][module.ID] {
			if registered.chunk.id == module.Chunk {
				module.Units, module.ObservedLinks, module.MoreUnits = factoryLocalFlow(tokens, registered.factory)
				break
			}
		}
		bundle.Modules = append(bundle.Modules, module)
	}
	if len(bundle.Modules) > 0 {
		parts, roles := make([]string, 0, len(bundle.Modules)), make(map[string]struct{})
		for _, module := range bundle.Modules {
			if _, seen := roles[module.Role]; seen {
				continue
			}
			roles[module.Role] = struct{}{}
			parts = append(parts, module.Role)
			if len(parts) == 5 {
				break
			}
		}
		bundle.Summary = "Contains " + strings.Join(parts, ", ") + "."
	}
	return bundle
}

// The module ranking and the source fact extractor see different evidence.
// Keep their labels consistent before either the report or diagram is built.
func mergeBundleComponents(file *File, chunks []*chunkInfo) {
	if file.Bundle == nil {
		return
	}
	if file.Insights == nil {
		file.Insights = &Insights{}
	}
	positions := make(map[moduleIdentity]int)
	for i, component := range file.Insights.Components {
		if component.Module != "" {
			positions[moduleIdentity{component.Runtime, component.Module}] = i
		}
	}
	for i := range file.Bundle.Modules {
		module := &file.Bundle.Modules[i]
		key := moduleIdentity{module.Runtime, module.ID}
		if index, found := positions[key]; found {
			component := &file.Insights.Components[index]
			if bundleRoleHasStrongerEvidence(*module) {
				component.Role = module.Role
				if len(module.Facts) > 0 {
					component.Evidence = module.Facts[0]
				}
			} else if component.Role != "" {
				module.Role = component.Role
			}
			continue
		}
		if module.Role == "" || module.Role == "path definitions" {
			continue
		}
		column := 0
		for _, chunk := range chunks {
			if chunk.global != module.Runtime || chunk.id != module.Chunk {
				continue
			}
			for _, factory := range chunk.factories {
				if factory.id == module.ID {
					column = factory.column
					break
				}
			}
		}
		evidence := "Observed in this module's factory."
		if len(module.Facts) > 0 {
			evidence = module.Facts[0]
		}
		file.Insights.Components = append(file.Insights.Components, InsightComponent{
			Name: "Module " + module.ID, Role: module.Role, Runtime: module.Runtime,
			Module: module.ID, Line: module.Line, Column: column, Evidence: evidence,
		})
		positions[key] = len(file.Insights.Components) - 1
	}
	file.Insights.Summary = insightSummary(file.Insights.Components, file.Insights.Routes, file.Insights.API)
	roles, seen := make([]string, 0, 5), make(map[string]struct{})
	for _, module := range file.Bundle.Modules {
		if _, found := seen[module.Role]; found {
			continue
		}
		seen[module.Role] = struct{}{}
		roles = append(roles, module.Role)
		if len(roles) == 5 {
			break
		}
	}
	if len(roles) > 0 {
		file.Bundle.Summary = "Contains " + strings.Join(roles, ", ") + "."
	}
}

func bundleRoleHasStrongerEvidence(module BundleModule) bool {
	if len(module.Flow) > 0 {
		return true
	}
	switch module.Role {
	case "EdenX app initialization", "AppLog analytics SDK", "coverage uploader", "account and verification SDK":
		return true
	}
	return false
}

func factoryLocalFlow(tokens []token, factory factoryRange) ([]Unit, []Link, int) {
	if factory.start >= factory.end || factory.start >= len(tokens) {
		return nil, nil, 0
	}
	depth, paren := tokens[factory.start].depth, tokens[factory.start].paren
	all := withClassMethods(tokens, declarationsAtDepth(tokens, depth, paren, factory.start, factory.end))
	byName := make(map[string]struct{})
	var readable []unitRange
	for _, unit := range all {
		if len(unit.Name) < 3 || unit.end-unit.start > 12000 {
			continue
		}
		if _, found := byName[unit.Name]; found {
			continue
		}
		byName[unit.Name] = struct{}{}
		readable = append(readable, unit)
	}
	if len(readable) == 0 {
		return nil, nil, 0
	}
	selected := readable[:min(8, len(readable))]
	result := make([]Unit, 0, len(selected))
	var links []Link
	seenLinks := make(map[string]struct{})
	for _, unit := range selected {
		item := unit.Unit
		called := make(map[string]struct{})
		if item.Kind != "class" {
			nested := nestedFunctionMask(tokens, unit.start, unit.end)
			for _, call := range callsIn(tokens, unit.start, unit.end) {
				target := call.name
				if call.receiver == "this" && item.Kind == "method" {
					target = strings.SplitN(item.Name, ".", 2)[0] + "." + call.name
				} else if call.receiver != "" {
					continue
				}
				if nested[call.index-unit.start] || target == item.Name {
					continue
				}
				if _, found := byName[target]; found {
					called[target] = struct{}{}
					addLink(&links, seenLinks, item.Name+"()", target+"()", call.line)
				}
			}
		}
		item.Calls = sortedKeys(called)
		result = append(result, item)
	}
	return result, links, len(readable) - len(selected)
}

func rankFactoryEvidence(candidate *bundleCandidate, evidence moduleEvidence) {
	componentScore := 85 + min(3, evidence.components)*7
	if evidence.components > 0 && componentScore > candidate.score && evidence.role != "" {
		candidate.Role = evidence.role
		candidate.score = componentScore
	}
	apiScore := 85 + min(5, evidence.apiWrappers)*4
	if evidence.apiWrappers >= 2 && apiScore > candidate.score {
		candidate.Role = "API client"
		candidate.score = apiScore
	}
	if evidence.routes > 0 && candidate.score < 45 {
		candidate.Role = "route definitions"
		candidate.score = 45 + min(20, evidence.routes)*2
	}
	if candidate.score == 0 && len(candidate.paths) >= 3 {
		candidate.Role = "path definitions"
		candidate.score = 25 + min(15, len(candidate.paths))
	}
	if candidate.score > 0 && len(candidate.Facts) == 0 {
		candidate.Facts = append(candidate.Facts, fmt.Sprintf("Contains %d source tokens and %d observed module requires.", candidate.size, len(candidate.requires)))
	}
}

func inspectFactory(tokens []token, factory factoryRange) bundleCandidate {
	c := bundleCandidate{BundleModule: BundleModule{ID: factory.id, Line: factory.line}, requires: make(map[string]int)}
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
		if factory.loader != "" && t.text == factory.loader && at(tokens, i+1) == "(" && i+3 < factory.end &&
			(tokens[i+2].kind == 'n' || tokens[i+2].kind == 's') && at(tokens, i+3) == ")" {
			id := tokens[i+2].text
			if tokens[i+2].kind == 's' {
				id = stringValue(id)
			}
			if _, exists := c.requires[id]; !exists {
				c.requires[id] = t.line
			}
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
	c.LazyPages = c.flags.lazy && (c.flags.reactLazy || c.flags.suspense)
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
		c.Role, c.score = "path catalog", 40
	case f.i18n:
		c.Role, c.score = "internationalization helpers", 30
	default:
		return
	}
	if totalTokens > 0 {
		c.Share = (100*c.size + totalTokens/2) / totalTokens
	}
}

func bundleDiagram(bundle *Bundle, chunks []*chunkInfo, insights *Insights) string {
	if bundle == nil || len(chunks) == 0 {
		return ""
	}
	hasFlow := len(bundle.Dependencies) > 0
	for _, chunk := range bundle.Chunks {
		hasFlow = hasFlow || chunk.Startup != nil
	}
	for _, module := range bundle.Modules {
		hasFlow = hasFlow || len(module.Flow) > 0
	}
	if insights != nil {
		hasFlow = hasFlow || len(insights.Routes) > 0 || len(insights.API) > 0
	}
	if !hasFlow {
		return ""
	}
	var out strings.Builder
	out.WriteString("flowchart LR\n")
	visible := make([]BundleModule, 0, 5)
	seenRoles := make(map[string]struct{})
	for _, module := range bundle.Modules {
		if _, seen := seenRoles[module.Role]; seen {
			continue
		}
		seenRoles[module.Role] = struct{}{}
		visible = append(visible, module)
		if len(visible) == 5 {
			break
		}
	}
	runtimes := make(map[string]int)
	for i, chunk := range chunks {
		if _, found := runtimes[chunk.global]; !found {
			runtimes[chunk.global] = len(runtimes)
		}
		count := fmt.Sprintf("%d factories", chunk.modules)
		if chunk.recovered {
			count = fmt.Sprintf("%d+ recognized factories", chunk.modules)
		}
		fmt.Fprintf(&out, "  host%d[\"Webpack runtime %s\"] -->|registers| chunk%d[\"chunk %s · %s\"]\n", runtimes[chunk.global], mermaidLabel(chunk.global), i, mermaidLabel(chunk.id), count)
		for j, module := range visible {
			if module.Runtime == chunk.global && module.Chunk == chunk.id {
				fmt.Fprintf(&out, "  chunk%d -->|defines| m%d[\"%s · %s\"]\n", i, j, mermaidLabel(module.ID), mermaidLabel(module.Role))
			}
		}
		startup := bundle.Chunks[i].Startup
		if startup == nil {
			continue
		}
		fmt.Fprintf(&out, "  chunk%d -->|provides| boot%d[\"startup callback\"]\n", i, i)
		if len(startup.WaitFor) > 0 {
			fmt.Fprintf(&out, "  boot%d -->|waits for| wait%d[\"chunks %s\"]\n", i, i, mermaidLabel(strings.Join(startup.WaitFor, " and ")))
		}
		for k, entry := range startup.Modules {
			target := fmt.Sprintf("entry%d_%d", i, k)
			for j, module := range visible {
				if module.Runtime == chunk.global && module.ID == entry.ID && module.Chunk == entry.DefinedInChunk {
					target = fmt.Sprintf("m%d", j)
					break
				}
			}
			if strings.HasPrefix(target, "entry") {
				label := "module " + entry.ID + " outside this file"
				if entry.DefinedHere {
					label = "module " + entry.ID + " in chunk " + entry.DefinedInChunk
				} else if entry.Ambiguous {
					label = "module " + entry.ID + " has multiple definitions here"
				}
				fmt.Fprintf(&out, "  %s[\"%s\"]\n", target, mermaidLabel(label))
			}
			fmt.Fprintf(&out, "  boot%d -->|requires| %s\n", i, target)
			if entry.AssetPrefix != "" {
				fmt.Fprintf(&out, "  %s -->|sets| asset%d_%d[\"asset prefix\"]\n", target, i, k)
			}
			if entry.MountedAt != "" {
				fmt.Fprintf(&out, "  %s -->|mounts React| mount%d_%d[\"%s\"]\n", target, i, k, mermaidLabel(entry.MountedAt))
			}
		}
	}
	for i, module := range visible {
		if len(module.Flow) > 0 && module.Role == "account and verification SDK" {
			fmt.Fprintf(&out, "  m%d -->|contains| verify[\"TTVerifyCenter callback\"]\n", i)
			out.WriteString("  verify -->|success callback| retry[\"getFp; call with fp and isResend\"]\n")
		}
		declaredRoutes, navigationTargets, wrappers, directCalls, transport := 0, 0, 0, 0, ""
		if insights != nil {
			for _, route := range insights.Routes {
				if route.Module == module.ID && route.Runtime == module.Runtime {
					if route.Kind == "declared route" {
						declaredRoutes++
					} else if route.Kind == "navigation target" {
						navigationTargets++
					}
				}
			}
			for _, api := range insights.API {
				if api.Module == module.ID && api.Runtime == module.Runtime {
					if api.Kind == "request wrapper" {
						wrappers++
						if transport == "" {
							transport = api.Transport
						} else if api.Transport != "" && api.Transport != transport {
							transport = "mixed"
						}
					} else if api.Kind == "direct call expression" {
						directCalls++
					}
				}
			}
		}
		if declaredRoutes > 0 {
			fmt.Fprintf(&out, "  m%d -->|declares| routes%d[\"%d UI route declarations\"]\n", i, i, declaredRoutes)
		}
		if navigationTargets > 0 {
			fmt.Fprintf(&out, "  m%d -->|references| navigation%d[\"%d navigation targets\"]\n", i, i, navigationTargets)
		}
		if module.LazyPages {
			fmt.Fprintf(&out, "  m%d -->|lazy loads| pages%d[\"additional page chunks\"]\n", i, i)
		}
		if wrappers > 0 {
			fmt.Fprintf(&out, "  m%d -->|defines| api%d[\"%d HTTP request wrappers\"]\n", i, i, wrappers)
			if transport != "" && transport != "mixed" {
				fmt.Fprintf(&out, "  m%d -->|calls| transport%d[\"request helper %s\"]\n", i, i, mermaidLabel(transport))
			}
		}
		if directCalls > 0 {
			fmt.Fprintf(&out, "  m%d -->|contains| direct%d[\"%d direct HTTP call expressions\"]\n", i, i, directCalls)
		}
	}
	// Module imports are a useful secondary detail. Keep a few edges whose
	// source is a visible application component, and avoid repetitive cycles.
	seenEdges := make(map[string]struct{})
	depEdges := 0
	for from, module := range visible {
		if depEdges == 3 {
			break
		}
		for _, dependency := range bundle.Dependencies {
			if dependency.Runtime != module.Runtime || dependency.FromChunk != module.Chunk || dependency.From != module.ID || !dependency.DefinedHere {
				continue
			}
			for to, target := range visible {
				if from == to || target.Runtime != dependency.Runtime || target.Chunk != dependency.ToChunk || target.ID != dependency.To {
					continue
				}
				key, reverse := fmt.Sprintf("%d:%d", from, to), fmt.Sprintf("%d:%d", to, from)
				if _, found := seenEdges[key]; found {
					continue
				}
				if _, found := seenEdges[reverse]; found {
					continue
				}
				seenEdges[key] = struct{}{}
				fmt.Fprintf(&out, "  m%d -->|requires| m%d\n", from, to)
				depEdges++
				if depEdges == 3 {
					break
				}
			}
			if depEdges == 3 {
				break
			}
		}
	}
	return out.String()
}
