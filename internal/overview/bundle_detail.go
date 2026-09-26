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
	Summary       string         `json:"summary,omitempty"`
	InternalLinks int            `json:"internal_require_links,omitempty"`
	ExternalRefs  int            `json:"references_outside_chunk,omitempty"`
	Modules       []BundleModule `json:"notable_modules,omitempty"`
}

type BundleModule struct {
	ID           string   `json:"id"`
	Line         int      `json:"line"`
	Role         string   `json:"role"`
	Share        int      `json:"token_share_percent,omitempty"`
	Facts        []string `json:"facts,omitempty"`
	Flow         []string `json:"observed_flow,omitempty"`
	Dependencies []string `json:"dependencies_in_chunk,omitempty"`
}

type bundleCandidate struct {
	BundleModule
	score, size int
	requires    map[string]struct{}
	paths       []string
	flags       bundleFlags
}

type bundleFlags struct {
	verify, passport, credentials, interceptors, challenge          bool
	login, register, qrLogin, forgotPassword                        bool
	verifyData, successCallback, fingerprint, resend, requestConfig bool
	appLog, autotrack, abTest, storage                              bool
	coverage, qaReport, interval                                    bool
	hooks, resize, orientation                                      bool
	lazy, i18n, swiper                                              bool
}

func analyzeBundle(tokens []token, chunk *chunkInfo) *Bundle {
	bundle := &Bundle{}
	ids := make(map[string]struct{}, len(chunk.factories))
	for _, factory := range chunk.factories {
		ids[factory.id] = struct{}{}
	}
	candidates := make([]bundleCandidate, 0, len(chunk.factories))
	internal, external := make(map[string]struct{}), make(map[string]struct{})
	for _, factory := range chunk.factories {
		candidate := inspectFactory(tokens, factory)
		candidate.size = factory.end - factory.start
		for dependency := range candidate.requires {
			if _, ok := ids[dependency]; ok {
				candidate.Dependencies = append(candidate.Dependencies, dependency)
				internal[factory.id+":"+dependency] = struct{}{}
			} else {
				external[dependency] = struct{}{}
			}
		}
		sort.Strings(candidate.Dependencies)
		classifyFactory(&candidate, len(tokens))
		if candidate.score > 0 {
			candidates = append(candidates, candidate)
		}
	}
	bundle.InternalLinks, bundle.ExternalRefs = len(internal), len(external)
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
			parts = append(parts, module.Role)
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
		case "verifyData":
			c.flags.verifyData = true
		case "successCb":
			c.flags.successCallback = true
		case "getFp":
			c.flags.fingerprint = true
		case "isResend":
			c.flags.resend = true
		case "requestConfig":
			c.flags.requestConfig = true
		case "AppLog", "__tea_sdk":
			c.flags.appLog = true
		case "autotrack", "autoTrack":
			c.flags.autotrack = true
		case "abtest", "ABTest", "ab_test":
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
		case "swiper", "swiperSlideSize", "Swiper":
			c.flags.swiper = true
		}
	}
	sort.Strings(c.paths)
	return c
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
	case f.verify && c.flags.passport:
		c.Role, c.score = "account and verification SDK", 100
		c.Facts = append(c.Facts, "TTVerifyCenter is present alongside passport/account routes and challenge UI.")
		if f.login && f.register && f.qrLogin && f.forgotPassword {
			c.Facts = append(c.Facts, "Exposes login, registration, QR login, and password recovery operations.")
		}
		if f.credentials && f.interceptors {
			c.Facts = append(c.Facts, "Request client configures credentials and response interceptors.")
		}
		if f.challenge {
			c.Facts = append(c.Facts, "Response handling checks verify_center_decision_conf before a verification challenge.")
		}
		if f.challenge && f.verifyData && f.successCallback && f.fingerprint && f.resend && f.requestConfig {
			c.Flow = []string{
				"Account operations use the request client and passport route tables.",
				"When a response carries verify_center_decision_conf, the SDK renders its verification UI from verifyData.",
				"On challenge success, it gets a fingerprint and retries requestConfig with fp and isResend.",
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
		c.Facts = append(c.Facts, fmt.Sprintf("Defines %d route-like strings; main families: %s.", len(c.paths), strings.Join(families, ", ")))
		examples := routeExamples(c.paths)
		if len(examples) > 0 {
			c.Facts = append(c.Facts, "Examples: "+strings.Join(examples, ", "))
		}
	}
}

func routeFamilies(paths []string) []string {
	counts := make(map[string]int)
	for _, path := range paths {
		parts := strings.Split(strings.Trim(path, "/"), "/")
		if len(parts) == 0 {
			continue
		}
		family := "/" + parts[0]
		if len(parts) > 1 && (parts[0] == "passport" || parts[0] == "web") {
			family += "/" + parts[1]
			if len(parts) > 2 && parts[1] == "aff" {
				family += "/" + parts[2]
			}
		}
		counts[family]++
	}
	type item struct {
		name  string
		count int
	}
	items := make([]item, 0, len(counts))
	for name, count := range counts {
		items = append(items, item{name, count})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].count != items[j].count {
			return items[i].count > items[j].count
		}
		return items[i].name < items[j].name
	})
	result := make([]string, 0, min(3, len(items)))
	for _, item := range items[:min(3, len(items))] {
		result = append(result, fmt.Sprintf("%s (%d)", item.name, item.count))
	}
	return result
}

func routeExamples(paths []string) []string {
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
	if bundle == nil || len(bundle.Modules) < 2 {
		return ""
	}
	var out strings.Builder
	out.WriteString("flowchart LR\n")
	fmt.Fprintf(&out, "  host[\"Webpack runtime\"] -->|registers| chunk[\"chunk %s · %d factories\"]\n", mermaidLabel(chunk.id), chunk.modules)
	for i, module := range bundle.Modules[:min(4, len(bundle.Modules))] {
		fmt.Fprintf(&out, "  chunk -->|contains| m%d[\"%s · %s\"]\n", i, mermaidLabel(module.ID), mermaidLabel(module.Role))
	}
	for i, module := range bundle.Modules[:min(4, len(bundle.Modules))] {
		if module.Role == "account and verification SDK" {
			if len(module.Flow) > 0 {
				fmt.Fprintf(&out, "  m%d -->|uses| request[\"request client\"]\n", i)
				out.WriteString("  request -->|targets| routes[\"passport route tables\"]\n")
				out.WriteString("  request -->|verification response| verify[\"TTVerifyCenter challenge UI\"]\n")
				out.WriteString("  verify -->|success: fingerprint and retry| request\n")
			} else {
				fmt.Fprintf(&out, "  m%d -->|defines| routes[\"passport route tables\"]\n", i)
				fmt.Fprintf(&out, "  m%d -->|contains| verify[\"TTVerifyCenter challenge UI\"]\n", i)
			}
			break
		}
	}
	return out.String()
}
