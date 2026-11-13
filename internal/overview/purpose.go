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
	// Folded lists the areas a sign-in area gathers, such as "email" and
	// "mobile": the ways a user gets into an account, not product areas.
	Folded []string `json:"folded,omitempty"`
}

const (
	maxFeatures       = 6
	maxFeatureActions = 5
	minFeatureNames   = 3
	// minNameOnlyNames is the bar for an area supported by names alone.
	minNameOnlyNames = 20
	// minRichEvidence is the number of API calls and screens that make a file
	// evidence-rich: its real areas then show up in its paths, and a word that
	// only a stray sentence mentions needs the name-only bar as well.
	minRichEvidence = 20
	// minConventionNames is the least a word needs to be read as a naming
	// convention when it ends nine in ten of the names it appears in, as
	// "Obs" does in "updateSkillObs".
	minConventionNames = 10
	// minPrefixCalls is the number of plain API paths from which a word that
	// sits in three out of four of them is read as a path prefix.
	minPrefixCalls = 12
	// minPathAreaItems is how many screens or API calls a first path segment
	// must head to name an area when no name does.
	minPathAreaItems = 3
	// overlapShare is the share of the smaller support, in tenths, that two
	// areas must have in common to be one area.
	overlapShare = 9
	// setSupportBonus ranks areas backed by an API call or screen first when
	// the vocabulary covers a whole set of files.
	setSupportBonus = 1 << 24
)

type vocabulary struct {
	nouns    map[string]int            // distinct structured names containing the noun
	prose    map[string]int            // distinct prose strings containing the noun
	actions  map[string]map[string]int // noun -> verb -> co-occurrences
	pairs    map[string]int            // "first second" noun pairs within one name
	leading  map[string]int            // names whose first word is the noun
	trailing map[string]int            // names whose last word is the noun
	next     map[string]map[string]int // noun -> word after it -> names
	prev     map[string]map[string]int // noun -> word before it -> names
	seen     map[string]struct{}
	paths    *evidence // API and route paths, which are not read again as names
	// setLevel marks the vocabulary of a whole set of files: an area then needs
	// a second kind of evidence somewhere in the set, never names alone.
	setLevel bool
}

func newVocabulary() *vocabulary {
	return &vocabulary{
		nouns: map[string]int{}, prose: map[string]int{}, actions: map[string]map[string]int{},
		pairs: map[string]int{}, leading: map[string]int{}, trailing: map[string]int{}, seen: map[string]struct{}{},
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
	if len(nouns) > 0 && nouns[len(nouns)-1] == words[len(words)-1] {
		v.trailing[nouns[len(nouns)-1]]++
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
	prefixes := ev.prefixWords()
	rich := ev != nil && len(ev.items) >= minRichEvidence
	var ranked []scored
	for noun, count := range v.nouns {
		if count < minFeatureNames || v.namespace(noun) || v.suffix(noun, ev) {
			continue
		}
		support := areaSupport(ev, prefixes, noun)
		if !v.supported(noun, support, rich) {
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
	var picked, signIns []pickedArea
	used := map[string]bool{}
	for _, item := range ranked {
		// Once the report is full only sign-in areas still matter: they fold
		// into one area that ranks by its combined support.
		full := len(picked) >= maxFeatures
		if used[item.noun] {
			continue
		}
		area := item.noun
		// Prefer a recurring two-word area such as "external preview" when
		// most uses of the noun appear in that pair.
		best := 0
		for pair, count := range v.pairs {
			if full {
				break
			}
			first, second, _ := strings.Cut(pair, " ")
			if first != item.noun && second != item.noun || used[first] || used[second] || v.namespace(first) || v.suffix(first, ev) {
				continue
			}
			if count*2 >= v.nouns[item.noun] && count >= minFeatureNames && (count > best || count == best && pair < area) {
				area, best = pair, count
			}
		}
		support := areaSupport(ev, prefixes, area)
		actions := map[string]int{}
		weights := map[string]int{}
		for verb, count := range v.actions[item.noun] {
			actions[verb] = count
			weights[verb] = count
		}
		for _, evidence := range support {
			for _, verb := range evidence.verbs {
				weights[verb]++
			}
		}
		if signInArea(weights, support) {
			signIns = append(signIns, pickedArea{Feature{Area: area, Names: v.nouns[item.noun]}, support, item.score, v.nouns[item.noun], actions})
			for _, word := range strings.Fields(area) {
				used[word] = true
			}
			continue
		}
		if full {
			continue
		}
		// Two areas whose API calls and screens are the same ones, such as
		// "ads" and "advertising", are one area: the better supported stays.
		at, replace := overlapping(picked, support, v.nouns[item.noun])
		if at >= 0 && !replace {
			continue
		}
		feature := Feature{Area: area, Names: v.nouns[item.noun]}
		feature.attach(support, actions)
		feature.Actions = topActions(actions)
		entry := pickedArea{feature, support, item.score, feature.Names, nil}
		if at >= 0 {
			for _, word := range strings.Fields(picked[at].feature.Area) {
				used[word] = false
			}
			picked[at] = entry
		} else {
			picked = append(picked, entry)
		}
		for _, word := range strings.Fields(area) {
			used[word] = true
		}
	}
	if len(signIns) > 0 {
		fold := foldSignIns(signIns)
		at := len(picked)
		for i, other := range picked {
			if other.score < fold.score {
				at = i
				break
			}
		}
		picked = append(picked[:at], append([]pickedArea{fold}, picked[at:]...)...)
		picked = picked[:min(len(picked), maxFeatures)]
	}
	if len(picked) > 0 && picked[0].names < 2*minFeatureNames {
		// A handful of weak words says nothing reliable about a file.
		picked = nil
	}
	var cut []Feature
	for _, item := range picked {
		if item.names*8 >= picked[0].names {
			cut = append(cut, item.feature)
		}
	}
	if len(cut) == 0 {
		return v.pathFeatures(ev)
	}
	return cut
}

const (
	// signInArea thresholds, in percent. An area is about getting into an
	// account when most of the actions seen next to it are account-access
	// actions and, where it has API calls, so are the calls: the paths of a
	// product area name what it does to its objects. Without API calls the
	// actions alone must be nearly all access actions, and there must be
	// enough of them to say so.
	signInActionShare = 60
	signInPathShare   = 35
	signInOnlyShare   = 85
	// minSignInPaths is how many API calls make the paths a second signal.
	minSignInPaths   = 3
	minSignInActions = 6
	// minSignInOnlyActions is the least actions that decide without API calls.
	minSignInOnlyActions = 12
)

// signInArea reports whether an area is about getting into an account. The
// noun never decides: "email" or "mobile" can be a product area. The actions
// seen next to it (names and API paths) and the API paths themselves must
// both be about account access.
func signInArea(actions map[string]int, support []evidenceItem) bool {
	total, access := 0, 0
	for verb, count := range actions {
		total += count
		if _, ok := accessActions[verb]; ok {
			access += count
		}
	}
	calls, accessCalls := 0, 0
	for _, item := range support {
		if item.screen {
			continue
		}
		calls++
		for _, verb := range item.verbs {
			if _, ok := accessActions[verb]; ok {
				accessCalls++
				break
			}
		}
	}
	if calls >= minSignInPaths {
		return total >= minSignInActions && access*100 >= total*signInActionShare && accessCalls*100 >= calls*signInPathShare
	}
	return total >= minSignInOnlyActions && access*100 >= total*signInOnlyShare
}

// foldSignIns gathers sign-in areas into one area. Each API call and screen
// counts once, however many of the areas it names.
func foldSignIns(areas []pickedArea) pickedArea {
	fold := pickedArea{feature: Feature{Area: "sign-in"}, actions: map[string]int{}}
	seen := map[string]bool{}
	for _, area := range areas {
		fold.feature.Names += area.feature.Names
		fold.feature.Folded = append(fold.feature.Folded, area.feature.Area)
		fold.score += area.score
		fold.names = max(fold.names, area.names)
		for verb, count := range area.actions {
			fold.actions[verb] += count
		}
		for _, item := range area.support {
			if !seen[item.key] {
				seen[item.key] = true
				fold.support = append(fold.support, item)
			}
		}
	}
	fold.feature.attach(fold.support, fold.actions)
	fold.feature.Actions = topActions(fold.actions)
	return fold
}

// supported reports whether an area has more than names behind it. Names
// alone are how generated SDK code and bundled packages look like features, so
// an area needs a second kind of evidence: user-facing text, a screen, or an
// API call. In a file that has plenty of API calls and screens, one sentence
// is not enough: the real areas are named by those paths.
func (v *vocabulary) supported(noun string, support []evidenceItem, rich bool) bool {
	count := v.nouns[noun]
	if len(support) > 0 || v.prose[noun] > 0 && (!rich || count >= minNameOnlyNames) {
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

// suffix reports a word that ends nearly every name it appears in, such as
// "obs" in "deleteSkillObs" and "routeObs": the mirror of namespace, and just as
// much a naming convention. A word that user-facing text mentions, or that an
// API call or screen names, is a real area that merely tends to end names, as
// "report" does.
func (v *vocabulary) suffix(noun string, ev *evidence) bool {
	count := v.nouns[noun]
	if count < minConventionNames || v.trailing[noun]*10 < count*9 || v.prose[noun]*20 > count {
		return false
	}
	return len(ev.support(noun)) == 0
}

// pickedArea is an area chosen for the report with the items that back it.
type pickedArea struct {
	feature Feature
	support []evidenceItem
	score   int
	names   int // the names that rank the area, for the cut against the top one
	actions map[string]int
}

// overlapping finds a chosen area that shares most of its API calls and screens
// with a new area: nearly all of the smaller set and a third of the larger. It
// says whether the new one is the better supported. A narrow area inside a broad
// one stays, and so does one with many names of its own: its names are evidence
// too.
func overlapping(picked []pickedArea, support []evidenceItem, names int) (at int, better bool) {
	keys := map[string]struct{}{}
	for _, item := range support {
		keys[item.key] = struct{}{}
	}
	for i, other := range picked {
		smaller := min(len(support), len(other.support))
		if smaller < 2 {
			continue
		}
		shared := 0
		for _, item := range other.support {
			if _, ok := keys[item.key]; ok {
				shared++
			}
		}
		if shared*10 < smaller*overlapShare || shared*3 < max(len(support), len(other.support)) {
			continue
		}
		if len(support) > len(other.support) {
			if other.feature.Names <= 4*shared {
				return i, true
			}
			continue
		}
		if names <= 4*shared {
			return i, false
		}
	}
	return -1, false
}

// areaSupport is the evidence behind an area. A word that sits in nearly all
// of a file's API paths is where the paths start, as in "/svc/music_api/...",
// so it is backed by its screens only.
func areaSupport(ev *evidence, prefixes map[string]bool, area string) []evidenceItem {
	items := ev.support(area)
	if len(prefixes) == 0 {
		return items
	}
	prefix := false
	for _, word := range strings.Fields(area) {
		prefix = prefix || prefixes[word]
	}
	if !prefix {
		return items
	}
	kept := items[:0:0]
	for _, item := range items {
		if item.screen || item.group != "" {
			kept = append(kept, item)
		}
	}
	return kept
}

// prefixWords returns the words that three out of four plain API paths of a
// file name and that the paths rarely name alone. They are a service or product
// prefix, not something the application does.
func (e *evidence) prefixWords() map[string]bool {
	if e == nil {
		return nil
	}
	total := 0
	has, alone := map[string]int{}, map[string]int{}
	for _, item := range e.items {
		if item.screen || item.group != "" {
			continue
		}
		total++
		for _, noun := range item.nouns {
			has[noun]++
			if len(item.nouns) == 1 {
				alone[noun]++
			}
		}
	}
	if total < minPrefixCalls {
		return nil
	}
	var prefixes map[string]bool
	for noun, count := range has {
		if count*4 >= total*3 && alone[noun]*4 <= count {
			if prefixes == nil {
				prefixes = map[string]bool{}
			}
			prefixes[noun] = true
		}
	}
	return prefixes
}

// pathFeatures names areas from the first segment of screens and API paths. It
// is the fallback for a file whose names give no area: a segment that heads
// several paths and is an ordinary domain noun is something the file has.
func (v *vocabulary) pathFeatures(ev *evidence) []Feature {
	if ev == nil || v.setLevel {
		return nil
	}
	// API paths that all start the same way are one service's root, not a family
	// among others, so they name an area only next to a second root.
	roots := map[string]struct{}{}
	for _, item := range ev.items {
		if !item.screen && item.group == "" {
			roots[firstSegment(item.label)] = struct{}{}
		}
	}
	heads := map[string]int{}
	for _, item := range ev.items {
		if item.group != "" || !item.screen && len(roots) < 2 {
			continue
		}
		if words := nameWords(firstSegment(item.label)); len(words) == 1 && domainNoun(words[0]) {
			heads[words[0]]++
		}
	}
	var nouns []string
	for noun, count := range heads {
		if count >= minPathAreaItems {
			nouns = append(nouns, noun)
		}
	}
	sort.Slice(nouns, func(i, j int) bool {
		if heads[nouns[i]] != heads[nouns[j]] {
			return heads[nouns[i]] > heads[nouns[j]]
		}
		return nouns[i] < nouns[j]
	})
	var result []Feature
	for _, noun := range nouns[:min(len(nouns), maxFeatures)] {
		feature := Feature{Area: noun}
		actions := map[string]int{}
		feature.attach(ev.support(noun), actions)
		feature.Actions = topActions(actions)
		result = append(result, feature)
	}
	return result
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

// invariantPlurals have the same form in the singular and the plural, and
// irregularPlurals do not end the way their singular suggests.
var (
	invariantPlurals = setOf("series", "species", "news")
	irregularPlurals = map[string]string{
		"indices": "index", "vertices": "vertex", "matrices": "matrix", "appendices": "appendix",
		"statuses": "status", "bonuses": "bonus", "viruses": "virus",
		"cookies": "cookie", "movies": "movie", "selfies": "selfie", "zombies": "zombie", "rookies": "rookie",
	}
)

func singular(word string) string {
	if alias, ok := wordAliases[word]; ok {
		return alias
	}
	if _, ok := invariantPlurals[word]; ok {
		return word
	}
	if one, ok := irregularPlurals[word]; ok {
		return one
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
	if len(word) >= 4 && !strings.ContainsAny(word, "aeiou") {
		// A hash such as the end of "slide-top-in_qzxk", or an abbreviation.
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
	var paths *evidence
	if name, version := licenseBanner(raw); name != "" {
		p.Library = strings.TrimSpace(name + " " + version)
		where := ""
		if runtime != "" {
			where = " for " + runtimePhrase(runtime)
		}
		p.Is = "The " + p.Library + " library" + where + ", named by its licence banner."
	}
	if p.Library == "" {
		paths = collectEvidence(file, tokens, fromPackage)
		paths.addRPCMethods(analyzer.RPCMethods(raw))
		vocab := collectVocabulary(file, tokens, fromPackage, paths)
		p.Features, p.Evidence = vocab.features(paths), paths.totals()
		if sink != nil {
			sink.vocab.absorb(vocab)
			sink.ev.absorb(paths)
		}
	}
	p.Platform = s.platform
	library := p.Library != "" || s.isLib
	p.Runs = runFacts(s.runs, s.frameworks, library, runContext(s, runtime, s.platform))
	extra := evidenceRuns(paths)
	if starts, ok := startsRun(file, p.Runs, library); ok {
		extra = append(extra, starts)
	}
	p.Runs = withDerivedRuns(p.Runs, extra...)
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
