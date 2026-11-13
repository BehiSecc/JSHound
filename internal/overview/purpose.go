package overview

import (
	"regexp"
	"sort"
	"strings"

	"github.com/BehiSecc/JSHound/internal/analyzer"
)

// Purpose answers two questions about a file: what it is and what it does.
// Minifiers rename code but keep strings, selectors, property names, file
// names, and licence banners, so the answer is built from those words.
type Purpose struct {
	Is       string    `json:"is"`
	Platform *Platform `json:"platform,omitempty"`
	Library  string    `json:"library,omitempty"`
	Features []Feature `json:"features,omitempty"`
	Evidence *Evidence `json:"evidence,omitempty"`
	Runs     []RunFact `json:"runs,omitempty"`
}

// Feature is a domain noun that recurs across readable names, with the
// actions that appear next to it, such as "creative: copy, approve". API calls
// and screens count the server calls and UI routes whose paths name the area.
type Feature struct {
	Area           string   `json:"area"`
	Actions        []string `json:"actions,omitempty"`
	Names          int      `json:"names"`
	APICalls       int      `json:"api_calls,omitempty"`
	Screens        int      `json:"screens,omitempty"`
	APIExamples    []string `json:"api_examples,omitempty"`
	ScreenExamples []string `json:"screen_examples,omitempty"`
}

const (
	maxFeatures       = 6
	maxFeatureActions = 5
	minFeatureNames   = 3
	// minNameOnlyNames is the bar for an area supported by names alone.
	minNameOnlyNames = 20
	// setSupportBonus ranks areas backed by an API call or screen first when
	// the vocabulary covers a whole set of files.
	setSupportBonus = 1 << 24
)

type vocabulary struct {
	nouns   map[string]int            // distinct structured names containing the noun
	prose   map[string]int            // distinct prose strings containing the noun
	actions map[string]map[string]int // noun -> verb -> co-occurrences
	pairs   map[string]int            // "first second" noun pairs within one name
	leading map[string]int            // names whose first word is the noun
	next    map[string]map[string]int // noun -> word after it -> names
	prev    map[string]map[string]int // noun -> word before it -> names
	seen    map[string]struct{}
	paths   *evidence // API and route paths, which are not read again as names
	// setLevel marks the vocabulary of a whole set of files: an area then needs
	// a second kind of evidence somewhere in the set, never names alone.
	setLevel bool
}

func newVocabulary() *vocabulary {
	return &vocabulary{
		nouns: map[string]int{}, prose: map[string]int{}, actions: map[string]map[string]int{},
		pairs: map[string]int{}, leading: map[string]int{}, seen: map[string]struct{}{},
		next: map[string]map[string]int{}, prev: map[string]map[string]int{},
	}
}

// addName records one readable name, such as "approveCreatives",
// "creative-table", or "asset_library". Each distinct name counts once.
func (v *vocabulary) addName(name string) {
	if _, ok := v.seen[name]; ok {
		return
	}
	v.seen[name] = struct{}{}
	words := nameWords(name)
	if len(words) == 0 || len(words) > 6 || libraryPrefix(words[0]) {
		return
	}
	var nouns, verbs []string
	for _, word := range words {
		switch {
		case actionVerbs[word] != "":
			verbs = appendUnique(verbs, actionVerbs[word])
		case domainNoun(word):
			nouns = appendUnique(nouns, word)
		}
	}
	if len(nouns) > 0 && nouns[0] == words[0] {
		v.leading[words[0]]++
	}
	for i, word := range words {
		if domainNoun(word) && actionVerbs[word] == "" {
			if i+1 < len(words) {
				countNeighbour(v.next, word, words[i+1])
			}
			if i > 0 {
				countNeighbour(v.prev, word, words[i-1])
			}
		}
	}
	for i, noun := range nouns {
		v.nouns[noun]++
		if v.actions[noun] == nil {
			v.actions[noun] = map[string]int{}
		}
		for _, verb := range verbs {
			v.actions[noun][verb]++
		}
		if i > 0 {
			v.pairs[nouns[i-1]+" "+noun]++
		}
	}
}

func countNeighbour(counts map[string]map[string]int, noun, word string) {
	if counts[noun] == nil {
		counts[noun] = map[string]int{}
	}
	counts[noun][word]++
}

// addProse records a user-facing sentence. Prose only supports nouns that
// also appear in structured names, so ordinary English never becomes a
// feature on its own.
func (v *vocabulary) addProse(text string, words []string) {
	key := "\x00" + text
	if _, ok := v.seen[key]; ok {
		return
	}
	v.seen[key] = struct{}{}
	counted := map[string]bool{}
	for _, word := range words {
		word = singular(strings.ToLower(word))
		if domainNoun(word) && !counted[word] {
			counted[word] = true
			v.prose[word]++
		}
	}
}

func (v *vocabulary) features(ev *evidence) []Feature {
	type scored struct {
		noun  string
		score int
	}
	var ranked []scored
	for noun, count := range v.nouns {
		if count < minFeatureNames || v.namespace(noun) {
			continue
		}
		support := ev.support(noun)
		if !v.supported(noun, support) {
			continue
		}
		score := count*2 + min(v.prose[noun], count) + min(len(support), count)
		if v.setLevel && len(support) > 0 {
			// Across a whole application an API call or a screen is the
			// stronger second kind of evidence: it names something the
			// application really does. A noun that only a few sentences of
			// text back, as in the messages of bundled grammars and parsers,
			// ranks after every noun that has one.
			score += setSupportBonus
		}
		ranked = append(ranked, scored{noun, score})
	}
	sort.Slice(ranked, func(i, j int) bool {
		if ranked[i].score != ranked[j].score {
			return ranked[i].score > ranked[j].score
		}
		return ranked[i].noun < ranked[j].noun
	})
	var result []Feature
	used := map[string]bool{}
	for _, item := range ranked {
		if len(result) == maxFeatures {
			break
		}
		if used[item.noun] {
			continue
		}
		area := item.noun
		// Prefer a recurring two-word area such as "external preview" when
		// most uses of the noun appear in that pair.
		best := 0
		for pair, count := range v.pairs {
			first, second, _ := strings.Cut(pair, " ")
			if first != item.noun && second != item.noun || used[first] || used[second] || v.namespace(first) {
				continue
			}
			if count*2 >= v.nouns[item.noun] && count >= minFeatureNames && (count > best || count == best && pair < area) {
				area, best = pair, count
			}
		}
		for _, word := range strings.Fields(area) {
			used[word] = true
		}
		feature := Feature{Area: area, Names: v.nouns[item.noun]}
		actions := map[string]int{}
		for verb, count := range v.actions[item.noun] {
			actions[verb] = count
		}
		feature.attach(ev.support(area), actions)
		feature.Actions = topActions(actions)
		result = append(result, feature)
	}
	if len(result) > 0 && result[0].Names < 2*minFeatureNames {
		// A handful of weak words says nothing reliable about a file.
		return nil
	}
	cut := result[:0]
	for _, feature := range result {
		if feature.Names*8 >= result[0].Names {
			cut = append(cut, feature)
		}
	}
	return cut
}

// supported reports whether an area has more than names behind it. Names
// alone are how generated SDK code and bundled packages look like features, so
// an area needs a second kind of evidence: user-facing text, a screen, or an
// API call.
func (v *vocabulary) supported(noun string, support []evidenceItem) bool {
	if v.prose[noun] > 0 || len(support) > 0 {
		return true
	}
	if v.setLevel {
		return false
	}
	// Some application code has no text, routes, or API paths in the file. A
	// name-only area then needs far more names, and must not be generated
	// code, where nearly every name pairs the noun with the same word, as in
	// "FilesDelete$inboundSchema" and "UsersList$inboundSchema". It must also
	// be acted on: a noun that no verb appears next to, that no text mentions,
	// and that no route or API call names is the vocabulary of a bundled
	// library or platform ("arrow", "vector"), not a feature.
	count := v.nouns[noun]
	return count >= minNameOnlyNames && len(v.actions[noun]) > 0 && !concentrated(v.next[noun], count) && !concentrated(v.prev[noun], count)
}

// concentrated reports whether one neighbouring word accounts for most of the
// names that contain a noun.
func concentrated(neighbours map[string]int, total int) bool {
	for _, count := range neighbours {
		if count*4 >= total*3 {
			return true
		}
	}
	return false
}

// namespace reports a word that prefixes a UI kit's names, such as "xap" in
// "xap-picker" and "xapSelection". It nearly always leads a name and never
// appears in user-facing text, unlike a feature such as "creative".
func (v *vocabulary) namespace(noun string) bool {
	count := v.nouns[noun]
	return count >= 10 && v.leading[noun]*10 >= count*9 && v.prose[noun]*20 <= count
}

func topActions(verbs map[string]int) []string {
	type pair struct {
		verb  string
		count int
	}
	var list []pair
	for verb, count := range verbs {
		list = append(list, pair{verb, count})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].count != list[j].count {
			return list[i].count > list[j].count
		}
		return list[i].verb < list[j].verb
	})
	var result []string
	for _, item := range list[:min(len(list), maxFeatureActions)] {
		result = append(result, item.verb)
	}
	return result
}

var (
	plainWord      = regexp.MustCompile(`^[A-Za-z][a-z]{2,}$`)
	cssSourceMap   = regexp.MustCompile(`sourceMappingURL=([A-Za-z0-9_-]+)\.css\.map`)
	structuredName = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(?:[-_.][A-Za-z0-9]+)*$`)
	pathName       = regexp.MustCompile(`^/[A-Za-z0-9_/-]*[A-Za-z][A-Za-z0-9_/-]*$`)
)

// collectVocabulary reads readable names from application code. Tokens that
// belong to bundled packages are skipped when their origin is known.
func collectVocabulary(file *File, tokens []token, fromPackage func(int) bool, paths *evidence) *vocabulary {
	v := newVocabulary()
	v.paths = paths
	for i, t := range tokens {
		if fromPackage != nil && fromPackage(i) {
			continue
		}
		switch t.kind {
		case 'i':
			if multiWord(t.text) && !paths.tableKey(i) {
				v.addName(t.text)
			}
		case 's':
			addStringValue(v, stringValue(t.text))
		}
	}
	if file.Modules != nil {
		for _, area := range file.Modules.Areas {
			v.addName(area.Name)
		}
	}
	return v
}

func addStringValue(v *vocabulary, value string) {
	switch {
	case value == "":
	case len(value) > 160:
		// Large strings are usually CSS or templates. Component styles keep the
		// name of their source file in a source map comment.
		for _, match := range cssSourceMap.FindAllStringSubmatch(value, -1) {
			v.addName(match[1])
		}
	case strings.ContainsRune(value, ' '):
		if words := proseWords(value); words != nil {
			v.addProse(value, words)
		}
	case pathName.MatchString(value):
		if v.paths.claims(value) {
			// Counted as an API call or screen, not as a name as well.
			break
		}
		for _, segment := range strings.Split(value, "/") {
			if segment != "" {
				v.addName(segment)
			}
		}
	case structuredName.MatchString(value) && (multiWord(value) || strings.ContainsAny(value, "-_")):
		v.addName(value)
	}
}

// proseWords returns the plain words of user-facing text, such as an error
// message or button label. Class lists, selectors, and code fragments that
// contain spaces are rejected because most of their words are not plain.
func proseWords(value string) []string {
	fields := strings.Fields(value)
	if len(fields) < 2 || len(fields) > 24 {
		return nil
	}
	var words []string
	for _, field := range fields {
		field = strings.Trim(field, `.,!?:;"'()`)
		if plainWord.MatchString(field) {
			words = append(words, field)
		}
	}
	if len(words) < 2 || len(words)*10 < len(fields)*7 {
		return nil
	}
	return words
}

// multiWord reports whether a name joins several words, such as camelCase,
// PascalCase, snake_case, or kebab-case. Single minified names never qualify.
func multiWord(name string) bool {
	if len(name) < 6 {
		return false
	}
	if strings.ContainsAny(name, "-_") {
		return true
	}
	for i := 1; i < len(name); i++ {
		if name[i] >= 'A' && name[i] <= 'Z' && name[i-1] >= 'a' && name[i-1] <= 'z' {
			return true
		}
	}
	return false
}

// nameWords splits a structured name into lowercase singular words.
func nameWords(name string) []string {
	var words []string
	var current strings.Builder
	flush := func() {
		if current.Len() > 0 {
			words = append(words, singular(strings.ToLower(current.String())))
			current.Reset()
		}
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		isUpper := c >= 'A' && c <= 'Z'
		isLetter := isUpper || c >= 'a' && c <= 'z'
		if !isLetter {
			flush()
			continue
		}
		if isUpper && i > 0 {
			prev := name[i-1]
			nextLower := i+1 < len(name) && name[i+1] >= 'a' && name[i+1] <= 'z'
			if prev >= 'a' && prev <= 'z' || prev >= 'A' && prev <= 'Z' && nextLower {
				flush()
			}
		}
		current.WriteByte(c)
	}
	flush()
	return words
}

// wordAliases merges common abbreviations into the word users read.
var wordAliases = map[string]string{
	"repo": "repository", "org": "organization", "pref": "preference", "notif": "notification",
	"msg": "message", "doc": "document", "img": "image", "pwd": "password", "acct": "account",
}

func singular(word string) string {
	if alias, ok := wordAliases[word]; ok {
		return alias
	}
	switch {
	case len(word) <= 3:
		return word
	case strings.HasSuffix(word, "ies"):
		return word[:len(word)-3] + "y"
	case strings.HasSuffix(word, "sses"), strings.HasSuffix(word, "shes"), strings.HasSuffix(word, "ches"), strings.HasSuffix(word, "xes"):
		return word[:len(word)-2]
	case strings.HasSuffix(word, "ss"), strings.HasSuffix(word, "us"), strings.HasSuffix(word, "is"):
		return word
	case strings.HasSuffix(word, "s"):
		return canonicalWord(word[:len(word)-1])
	}
	return word
}

func canonicalWord(word string) string {
	if alias, ok := wordAliases[word]; ok {
		return alias
	}
	return word
}

func domainNoun(word string) bool {
	if len(word) < 3 || len(word) > 24 {
		return false
	}
	if _, ok := genericWords[word]; ok {
		return false
	}
	if _, ok := serviceNames[word]; ok {
		return false
	}
	return actionVerbs[word] == ""
}

func libraryPrefix(word string) bool {
	_, ok := libraryPrefixes[word]
	return ok
}

// describePurpose builds the plain answer shown by default. identity is the
// evidence-based identity sentence and runtime is where the file runs.
func describePurpose(file *File, tokens []token, raw string, fromPackage func(int) bool, s fileSignals, identity, runtime string, sink *rollupSink) *Purpose {
	if len(tokens) == 0 {
		return nil
	}
	p := &Purpose{Is: identity}
	if name, version := licenseBanner(raw); name != "" {
		p.Library = strings.TrimSpace(name + " " + version)
		where := ""
		if runtime != "" {
			where = " for " + runtimePhrase(runtime)
		}
		p.Is = "The " + p.Library + " library" + where + ", named by its licence banner."
	}
	if p.Library == "" {
		paths := collectEvidence(file, tokens, fromPackage)
		paths.addRPCMethods(analyzer.RPCMethods(raw))
		vocab := collectVocabulary(file, tokens, fromPackage, paths)
		p.Features, p.Evidence = vocab.features(paths), paths.totals()
		if sink != nil {
			sink.vocab.absorb(vocab)
			sink.ev.absorb(paths)
		}
	}
	p.Platform = s.platform
	p.Runs = runFacts(s.runs, s.frameworks, p.Library != "" || s.isLib, runContext(s, runtime, s.platform))
	return p
}

func runtimePhrase(runtime string) string {
	switch runtime {
	case "browser":
		return "the browser"
	case "Node.js":
		return "Node.js"
	default:
		return "a " + runtime
	}
}

var (
	bannerVersion = regexp.MustCompile(`(?:^|[\s*!/(@])([A-Za-z][\w.-]{1,40}?)\s+v?(\d+\.\d+(?:\.\d+)?(?:-[\w.]+)?)\b`)
	bannerLicense = regexp.MustCompile(`@license\s+([A-Z][\w.-]{1,40})`)
	bannerFile    = regexp.MustCompile(`([a-z][\w-]{1,40}?)(?:\.(?:production|development|prod|dev))?(?:\.min)?\.js\b`)
)

// licenseBanner reads the comment that opens a distributed library, such as
// "/*! jQuery v3.7.1 | ..." or "@license React ... react-dom.production.min.js".
// Banners later in a file belong to bundled code, not to the file itself.
func licenseBanner(raw string) (string, string) {
	text := strings.TrimLeft(raw, "\uFEFF \t\r\n")
	if !strings.HasPrefix(text, "/*") {
		return "", ""
	}
	end := strings.Index(text, "*/")
	if end < 0 || end > 2048 {
		return "", ""
	}
	comment := text[2:end]
	if strings.Contains(comment, "For license information") {
		return "", ""
	}
	for _, match := range bannerVersion.FindAllStringSubmatch(comment, -1) {
		switch strings.ToLower(match[1]) {
		case "version", "license", "licensed", "copyright", "mit", "apache", "bsd", "isc", "gpl", "lgpl", "mpl", "build", "custom", "release":
			continue
		}
		return match[1], match[2]
	}
	if match := bannerLicense.FindStringSubmatch(comment); match != nil {
		if file := bannerFile.FindStringSubmatch(comment); file != nil {
			return file[1], ""
		}
		return match[1], ""
	}
	return "", ""
}

// joinWords joins a short list as "a", "a and b", or "a, b, and c".
func joinWords(items []string) string {
	if len(items) == 2 {
		return items[0] + " and " + items[1]
	}
	return joinClauses(items)
}
