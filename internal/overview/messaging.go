package overview

import (
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

type jsCall struct {
	receiver string
	args     []string
}

var (
	functionParameter = regexp.MustCompile(`(?is)^\s*(?:async\s+)?function(?:\s+[A-Za-z_$][\w$]*)?\s*\(\s*([A-Za-z_$][\w$]*)`)
	arrowParameter    = regexp.MustCompile(`(?is)^\s*(?:async\s+)?(?:\(\s*)?([A-Za-z_$][\w$]*)\s*\)?\s*=>`)
	targetOriginField = regexp.MustCompile(`(?is)\btargetOrigin\s*:\s*([^,}\n]+)`)
)

func analyzeMessaging(source string) *MessageBoundary {
	receiverCalls := findJSCalls(source, "addEventListener")
	senderCalls := findJSCalls(source, "postMessage")
	result := &MessageBoundary{}
	validatedOrigins := stringSet()

	for _, call := range receiverCalls {
		if len(call.args) < 2 || stringLiteral(call.args[0]) != "message" {
			continue
		}
		if !windowEventReceiver(call.receiver) {
			result.UnknownReceivers++
			continue
		}
		result.Receivers++
		parameter := callbackParameter(call.args[1])
		if parameter == "" {
			result.ValidationUnknown++
			continue
		}
		origin, weakOrigin, origins := propertyValidation(call.args[1], parameter, "origin")
		sourceChecked, _, _ := propertyValidation(call.args[1], parameter, "source")
		for _, value := range origins {
			validatedOrigins[value] = struct{}{}
		}
		switch {
		case origin && sourceChecked:
			result.OriginAndSource++
		case origin:
			result.OriginOnly++
		case weakOrigin:
			result.WeakOrigin++
		case sourceChecked:
			result.SourceOnly++
		default:
			result.Unvalidated++
		}
	}

	for _, call := range senderCalls {
		result.Senders++
		kind, target := classifySender(call)
		switch kind {
		case "window":
			result.WindowSenders++
			switch {
			case target == "*":
				result.WildcardTargets++
			case target != "":
				result.FixedTargets++
				result.FixedTargetOrigins = appendUnique(result.FixedTargetOrigins, target)
			default:
				result.DynamicTargets++
			}
		case "worker":
			result.WorkerSenders++
		case "port":
			result.PortSenders++
		case "broadcast":
			result.BroadcastSenders++
		default:
			result.UnknownSenders++
		}
	}

	if result.Receivers == 0 && result.Senders == 0 {
		return nil
	}
	result.OriginChecks = result.OriginAndSource + result.OriginOnly
	result.SourceChecks = result.OriginAndSource + result.SourceOnly
	result.ValidatedOrigins = limitedSorted(validatedOrigins, 16)
	sortStrings(result.FixedTargetOrigins)
	if len(result.FixedTargetOrigins) > 16 {
		result.FixedTargetOrigins = result.FixedTargetOrigins[:16]
	}
	return result
}

func classifySender(call jsCall) (kind, target string) {
	receiver := strings.ToLower(call.receiver)
	if containsAny(receiver, "messageport", ".port", "channel.port") {
		return "port", ""
	}
	if containsAny(receiver, "broadcastchannel", "broadcast") {
		return "broadcast", ""
	}
	if receiver == "worker" || strings.HasSuffix(receiver, "worker") || containsAny(receiver, "serviceworker", ".worker", "worker.", "worker?") {
		return "worker", ""
	}
	explicitWindow := windowSenderReceiver(receiver)
	if len(call.args) < 2 {
		if explicitWindow {
			return "window", ""
		}
		return "unknown", ""
	}
	second := strings.TrimSpace(call.args[1])
	if field := targetOriginField.FindStringSubmatch(second); len(field) > 1 {
		if literal := stringLiteral(field[1]); validTargetOrigin(literal) {
			return "window", literal
		}
		return "window", ""
	}
	if literal := stringLiteral(second); literal != "" {
		if validTargetOrigin(literal) {
			return "window", literal
		}
		// An arbitrary second string is usually an argument to a custom,
		// Worker, or MessagePort implementation—not a valid targetOrigin.
		if explicitWindow {
			return "window", ""
		}
		return "unknown", ""
	}
	if containsAny(strings.ToLower(second), ".origin", ".protocol", "targetorigin") ||
		explicitWindow {
		return "window", ""
	}
	if strings.HasPrefix(second, "[") {
		return "worker", ""
	}
	return "unknown", ""
}

func validTargetOrigin(value string) bool {
	if value == "*" || value == "/" {
		return true
	}
	parsed, err := url.Parse(value)
	return err == nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Host != "" && (parsed.Path == "" || parsed.Path == "/") && parsed.RawQuery == "" && parsed.Fragment == ""
}

func callbackParameter(callback string) string {
	for _, pattern := range []*regexp.Regexp{functionParameter, arrowParameter} {
		if match := pattern.FindStringSubmatch(callback); len(match) > 1 {
			return match[1]
		}
	}
	return ""
}

func propertyValidation(callback, parameter, property string) (bool, bool, []string) {
	masked := maskNonCode(callback)
	propertyExpr := regexp.QuoteMeta(parameter) + `\s*\.\s*` + property
	comparison := regexp.MustCompile(`(?is)(?:` + propertyExpr + `\s*(?:===|==|!==|!=)\s*([^,;)&|}]{1,256})|([^,;(&|{]{1,256})\s*(?:===|==|!==|!=)\s*` + propertyExpr + `)`)
	allowlist := regexp.MustCompile(`(?is)\b(?:includes|has)\s*\(\s*` + propertyExpr + `\s*\)`)
	weak := regexp.MustCompile(`(?is)` + propertyExpr + `\s*\.\s*(?:includes|indexOf|endsWith|startsWith|match|test)\s*\(`)
	matches := comparison.FindAllStringSubmatch(masked, -1)
	values := stringSet()
	valid := false
	for _, match := range matches {
		for _, candidate := range match[1:] {
			candidate = strings.TrimSpace(candidate)
			if oneOfFold(candidate, "null", "undefined", "true", "false") || strings.Contains(candidate, parameter+"."+property) {
				continue
			}
			valid = true
		}
	}
	valid = valid || allowlist.MatchString(masked)
	// Recover literal allowlist values only from comparisons whose property
	// access is executable code, rather than text inside a string or comment.
	literalComparison := regexp.MustCompile(`(?is)` + propertyExpr + `\s*(?:===|==|!==|!=)\s*(["'][^"']{1,256}["'])`)
	for _, location := range literalComparison.FindAllStringSubmatchIndex(callback, -1) {
		if location[0] < len(masked) && strings.Contains(masked[location[0]:min(location[1], len(masked))], parameter) {
			if literal := stringLiteral(callback[location[2]:location[3]]); literal != "" && literal != "null" {
				values[literal] = struct{}{}
			}
		}
	}
	return valid, weak.MatchString(masked), sorted(values)
}

func windowEventReceiver(receiver string) bool {
	receiver = strings.ToLower(strings.TrimSpace(receiver))
	return receiver == "" || receiver == "window" || receiver == "globalthis" || receiver == "parent" || receiver == "top" || receiver == "opener"
}

func windowSenderReceiver(receiver string) bool {
	receiver = strings.ToLower(strings.TrimSpace(receiver))
	return receiver == "window" || receiver == "parent" || receiver == "top" || receiver == "opener" ||
		strings.Contains(receiver, "contentwindow") || strings.Contains(receiver, "window.parent") ||
		strings.Contains(receiver, "window.opener") || strings.Contains(receiver, "window.top")
}

func findJSCalls(source, name string) []jsCall {
	result := make([]jsCall, 0, 16)
	for cursor := 0; cursor < len(source); {
		if isQuote(source[cursor]) {
			cursor = skipQuoted(source, cursor)
			continue
		}
		if source[cursor] == '/' && cursor+1 < len(source) && source[cursor+1] == '*' {
			cursor += 2
			for cursor+1 < len(source) && (source[cursor] != '*' || source[cursor+1] != '/') {
				cursor++
			}
			cursor = min(len(source), cursor+2)
			continue
		}
		if source[cursor] == '/' && cursor+1 < len(source) && source[cursor+1] == '/' {
			cursor = skipLineComment(source, cursor)
			continue
		}
		if source[cursor] == '/' && regexStartsAt(source, cursor) {
			cursor = skipRegexLiteral(source, cursor)
			continue
		}
		if !strings.HasPrefix(source[cursor:], name) || !identifierBoundary(source, cursor-1) || !identifierBoundary(source, cursor+len(name)) {
			cursor++
			continue
		}
		open := cursor + len(name)
		for open < len(source) && isSpace(source[open]) {
			open++
		}
		if open >= len(source) || source[open] != '(' {
			cursor += len(name)
			continue
		}
		args, close, ok := parseCallArguments(source, open)
		if !ok {
			cursor += len(name)
			continue
		}
		if isFunctionDefinition(source, cursor, close) {
			cursor += len(name)
			continue
		}
		result = append(result, jsCall{receiver: receiverBefore(source, cursor), args: args})
		// Continue inside the parsed range so nested callbacks containing their
		// own calls are still discovered. Advancing past the identifier avoids
		// reporting the same call twice.
		cursor += len(name)
	}
	return result
}

func parseCallArguments(source string, open int) ([]string, int, bool) {
	start := open + 1
	paren, brace, bracket := 1, 0, 0
	args := make([]string, 0, 3)
	for cursor := start; cursor < len(source); cursor++ {
		if isQuote(source[cursor]) {
			cursor = skipQuoted(source, cursor) - 1
			continue
		}
		if source[cursor] == '/' && cursor+1 < len(source) && source[cursor+1] == '/' {
			cursor = skipLineComment(source, cursor) - 1
			continue
		}
		if source[cursor] == '/' && cursor+1 < len(source) && source[cursor+1] == '*' {
			cursor += 2
			for cursor+1 < len(source) && (source[cursor] != '*' || source[cursor+1] != '/') {
				cursor++
			}
			cursor = min(len(source), cursor+1)
			continue
		}
		if source[cursor] == '/' && regexStartsAt(source, cursor) {
			cursor = skipRegexLiteral(source, cursor) - 1
			continue
		}
		switch source[cursor] {
		case '(':
			paren++
		case ')':
			paren--
			if paren == 0 {
				if value := strings.TrimSpace(source[start:cursor]); value != "" || len(args) > 0 {
					args = append(args, value)
				}
				return args, cursor, true
			}
		case '{':
			brace++
		case '}':
			if brace > 0 {
				brace--
			}
		case '[':
			bracket++
		case ']':
			if bracket > 0 {
				bracket--
			}
		case ',':
			if paren == 1 && brace == 0 && bracket == 0 {
				args = append(args, strings.TrimSpace(source[start:cursor]))
				start = cursor + 1
			}
		}
	}
	return nil, 0, false
}

func isFunctionDefinition(source string, nameStart, close int) bool {
	after := close + 1
	for after < len(source) && isSpace(source[after]) {
		after++
	}
	if after >= len(source) || source[after] != '{' {
		return false
	}
	before := strings.TrimSpace(source[max(0, nameStart-24):nameStart])
	return strings.HasSuffix(before, "function") || receiverBefore(source, nameStart) == ""
}

func skipLineComment(source string, start int) int {
	cursor := start + 2
	for cursor < len(source) && source[cursor] != '\n' && source[cursor] != '\r' {
		cursor++
	}
	return cursor
}

func regexStartsAt(source string, start int) bool {
	previous := start - 1
	for previous >= 0 && isSpace(source[previous]) {
		previous--
	}
	if previous < 0 {
		return true
	}
	return strings.ContainsRune("(=:[,!&|?{;", rune(source[previous]))
}

func skipRegexLiteral(source string, start int) int {
	inClass := false
	for cursor := start + 1; cursor < len(source); cursor++ {
		if source[cursor] == '\\' && cursor+1 < len(source) {
			cursor++
			continue
		}
		switch source[cursor] {
		case '[':
			inClass = true
		case ']':
			inClass = false
		case '/':
			if !inClass {
				cursor++
				for cursor < len(source) && unicode.IsLetter(rune(source[cursor])) {
					cursor++
				}
				return cursor
			}
		case '\n', '\r':
			return cursor
		}
	}
	return len(source)
}

// maskNonCode preserves offsets and quote delimiters while replacing string
// contents and comments. Comparisons against a literal remain recognizable,
// but validation-looking prose inside a literal cannot become evidence.
func maskNonCode(source string) string {
	masked := []byte(source)
	for cursor := 0; cursor < len(source); {
		if isQuote(source[cursor]) {
			end := skipQuoted(source, cursor)
			for index := cursor + 1; index < end-1; index++ {
				masked[index] = ' '
			}
			cursor = end
			continue
		}
		if source[cursor] == '/' && cursor+1 < len(source) && source[cursor+1] == '/' {
			end := skipLineComment(source, cursor)
			for index := cursor; index < end; index++ {
				masked[index] = ' '
			}
			cursor = end
			continue
		}
		if source[cursor] == '/' && cursor+1 < len(source) && source[cursor+1] == '*' {
			end := cursor + 2
			for end+1 < len(source) && (source[end] != '*' || source[end+1] != '/') {
				end++
			}
			end = min(len(source), end+2)
			for index := cursor; index < end; index++ {
				masked[index] = ' '
			}
			cursor = end
			continue
		}
		if source[cursor] == '/' && regexStartsAt(source, cursor) {
			end := skipRegexLiteral(source, cursor)
			for index := cursor; index < end; index++ {
				masked[index] = ' '
			}
			cursor = end
			continue
		}
		cursor++
	}
	return string(masked)
}

func receiverBefore(source string, nameStart int) string {
	cursor := nameStart - 1
	for cursor >= 0 && isSpace(source[cursor]) {
		cursor--
	}
	if cursor < 0 || source[cursor] != '.' {
		return ""
	}
	end := cursor
	cursor--
	for cursor >= 0 {
		b := source[cursor]
		if isIdentifierByte(b) || b == '.' || b == ']' || b == '[' || b == ')' || b == '(' || b == '?' {
			cursor--
			continue
		}
		break
	}
	return strings.TrimSpace(source[cursor+1 : end])
}

func stringLiteral(value string) string {
	value = strings.TrimSpace(value)
	if len(value) < 2 || !isQuote(value[0]) || value[len(value)-1] != value[0] {
		return ""
	}
	return value[1 : len(value)-1]
}

func skipQuoted(source string, start int) int {
	quote := source[start]
	for cursor := start + 1; cursor < len(source); cursor++ {
		if source[cursor] == '\\' && cursor+1 < len(source) {
			cursor++
			continue
		}
		if source[cursor] == quote {
			return cursor + 1
		}
	}
	return len(source)
}

func identifierBoundary(source string, index int) bool {
	return index < 0 || index >= len(source) || !isIdentifierByte(source[index])
}
func isIdentifierByte(value byte) bool {
	return value == '_' || value == '$' || unicode.IsLetter(rune(value)) || unicode.IsDigit(rune(value))
}
func isQuote(value byte) bool { return value == '\'' || value == '"' || value == '`' }
func isSpace(value byte) bool { return value == ' ' || value == '\t' || value == '\r' || value == '\n' }
func containsAny(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.Contains(value, candidate) {
			return true
		}
	}
	return false
}
func appendUnique(values []string, value string) []string {
	for _, current := range values {
		if current == value {
			return values
		}
	}
	return append(values, value)
}
func limitedSorted(values map[string]struct{}, limit int) []string {
	result := sorted(values)
	if len(result) > limit {
		result = result[:limit]
	}
	return result
}
func sortStrings(values []string) { sort.Strings(values) }
func oneOfFold(value string, candidates ...string) bool {
	for _, candidate := range candidates {
		if strings.EqualFold(value, candidate) {
			return true
		}
	}
	return false
}
