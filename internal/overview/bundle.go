package overview

import (
	"fmt"
	"strings"
)

type chunkInfo struct {
	id        string
	modules   int
	global    string
	line      int
	factories []factoryRange
	startup   *chunkStartup
	recovered bool
}

type chunkStartup struct {
	waits, requires []string
}

type factoryRange struct {
	id, loader   string
	line, column int
	start, end   int
}

// findChunks recognizes every loader registration in a source. A single file
// can register several chunks, including chunks for unrelated runtimes.
func findChunks(tokens []token) []*chunkInfo {
	var chunks []*chunkInfo
	for i := 0; i+2 < len(tokens); i++ {
		if at(tokens, i) != "push" || at(tokens, i-1) != "." || at(tokens, i+1) != "(" {
			continue
		}
		args := argumentRanges(tokens, i+1)
		if len(args) != 1 {
			continue
		}
		outer := args[0][0]
		ids := outer + 1
		if at(tokens, outer) != "[" || at(tokens, ids) != "[" || tokens[ids].match <= ids {
			continue
		}
		moduleOpen := tokens[ids].match + 2
		if at(tokens, moduleOpen-1) != "," || at(tokens, moduleOpen) != "{" || tokens[moduleOpen].match <= moduleOpen {
			continue
		}
		info := &chunkInfo{line: tokens[i].line}
		for j := ids + 1; j < tokens[ids].match; j++ {
			if tokens[j].kind == 'n' || tokens[j].kind == 's' {
				value := tokens[j].text
				if tokens[j].kind == 's' {
					value = stringValue(value)
				}
				if len(value) <= 32 {
					if info.id != "" {
						info.id += ", "
					}
					info.id += value
				}
			}
			if len(info.id) > 64 {
				break
			}
		}
		mapEnd, runtimeStart := tokens[outer].match-1, -1
		if start := chunkRuntimeStart(tokens, tokens[outer].match); start >= 0 {
			mapEnd, runtimeStart = start-2, start
		}
		info.recovered = tokens[moduleOpen].match < mapEnd
		memberDepth := tokens[moduleOpen].depth + 1
		for j := moduleOpen + 1; j < mapEnd; j++ {
			depth := memberDepth
			if j > tokens[moduleOpen].match && tokens[moduleOpen].match < mapEnd {
				depth-- // recover when a minified expression hid one brace
			}
			if tokens[j].depth != depth || tokens[j].kind != 'n' && tokens[j].kind != 's' ||
				at(tokens, j+1) != ":" || !isFactoryValue(tokens, j+2) ||
				at(tokens, j-1) != "," && at(tokens, j-1) != "{" {
				continue
			}
			info.modules++
			if factory, ok := readFactory(tokens, j); ok && factory.end <= mapEnd {
				info.factories = append(info.factories, factory)
				j = factory.end
			}
		}
		info.global = chunkReceiver(tokens, i)
		if info.modules == 0 || info.global == "" {
			continue
		}
		if runtimeStart >= 0 {
			info.startup = readChunkStartup(tokens, runtimeStart, tokens[outer].match)
		}
		if !strings.Contains(strings.ToLower(info.global), "webpack") && info.startup == nil && !hasWebpackHelpers(tokens, info.factories) {
			continue
		}
		chunks = append(chunks, info)
		// The remainder of this registration cannot contain another top-level
		// registration. Skip its factories and any embedded .push() calls.
		i = tokens[i+1].match
	}
	return chunks
}

// findChunk is kept for callers that only need the first registration.
func findChunk(tokens []token) *chunkInfo {
	chunks := findChunks(tokens)
	if len(chunks) == 0 {
		return nil
	}
	return chunks[0]
}

func chunkRuntimeStart(tokens []token, outerEnd int) int {
	close := outerEnd - 1
	if at(tokens, close) == "}" && tokens[close].match >= 0 {
		open := tokens[close].match
		for start := max(0, open-8); start < open; start++ {
			if at(tokens, start-1) != "," {
				continue
			}
			_, _, end, ok := callbackRange(tokens, start)
			if ok && end == close {
				return start
			}
		}
	}
	if at(tokens, close) == ")" {
		for start := max(0, outerEnd-16); start+3 < outerEnd; start++ {
			if at(tokens, start-1) == "," && tokens[start].kind == 'i' && at(tokens, start+1) == "=>" &&
				at(tokens, start+3) == "(" && tokens[start+3].match == close {
				return start
			}
		}
	}
	return -1
}

func hasWebpackHelpers(tokens []token, factories []factoryRange) bool {
	for _, factory := range factories {
		if factory.loader == "" {
			continue
		}
		for i := factory.start; i+2 < factory.end; i++ {
			if tokens[i].text == factory.loader && at(tokens, i+1) == "." {
				switch at(tokens, i+2) {
				case "d", "r", "e", "n", "O":
					return true
				}
			}
		}
	}
	return false
}

func readChunkStartup(tokens []token, start, end int) *chunkStartup {
	if start >= end || start < 0 {
		return nil
	}
	// The optional third element in a Webpack chunk is a runtime callback.
	alias, bodyStart, bodyEnd, ok := callbackRange(tokens, start)
	concise := false
	if !ok && tokens[start].kind == 'i' && at(tokens, start+1) == "=>" {
		alias, bodyStart, bodyEnd, ok, concise = tokens[start].text, start+2, end, true, true
	}
	if !ok || bodyEnd > end {
		return nil
	}
	startup := &chunkStartup{}
	bodyDepth := tokens[bodyStart-1].depth + 1
	if concise {
		bodyDepth = tokens[bodyStart].depth
	}
	nested := nestedFunctionMask(tokens, bodyStart, bodyEnd)
	entryNames := map[string]struct{}{alias: {}}
	for i := bodyStart; i+8 < bodyEnd; i++ {
		if tokens[i].depth != bodyDepth || at(tokens, i) != "var" && at(tokens, i) != "const" && at(tokens, i) != "let" ||
			tokens[i+1].kind != 'i' || at(tokens, i+2) != "=" {
			continue
		}
		name := tokens[i+1].text
		param := ""
		if tokens[i+3].kind == 'i' && at(tokens, i+4) == "=>" {
			param = tokens[i+3].text
		} else if at(tokens, i+3) == "function" && at(tokens, i+4) == "(" && i+5 < bodyEnd && tokens[i+5].kind == 'i' {
			param = tokens[i+5].text
		}
		if param == "" {
			continue
		}
		for j := i + 3; j+6 < min(bodyEnd, i+45); j++ {
			if at(tokens, j) == alias && at(tokens, j+1) == "(" && at(tokens, j+2) == alias &&
				at(tokens, j+3) == "." && at(tokens, j+4) == "s" && at(tokens, j+5) == "=" && at(tokens, j+6) == param {
				entryNames[name] = struct{}{}
				break
			}
		}
	}
	for i := bodyStart; i < bodyEnd; i++ {
		if tokens[i].depth != bodyDepth || nested[i-bodyStart] {
			continue
		}
		if alias != "" && tokens[i].text == alias && at(tokens, i+1) == "(" && i+3 < bodyEnd &&
			(tokens[i+2].kind == 'n' || tokens[i+2].kind == 's') && at(tokens, i+3) == ")" {
			id := tokens[i+2].text
			if tokens[i+2].kind == 's' {
				id = stringValue(id)
			}
			startup.requires = appendUnique(startup.requires, id)
		}
		if at(tokens, i) != "O" || at(tokens, i-2) != alias || at(tokens, i-1) != "." || at(tokens, i+1) != "(" {
			continue
		}
		args := argumentRanges(tokens, i+1)
		if len(args) < 3 || at(tokens, args[0][0]) != "0" || at(tokens, args[1][0]) != "[" {
			continue
		}
		for j := args[1][0] + 1; j < args[1][1]; j++ {
			if tokens[j].kind == 'n' || tokens[j].kind == 's' {
				value := tokens[j].text
				if tokens[j].kind == 's' {
					value = stringValue(value)
				}
				startup.waits = appendUnique(startup.waits, value)
			}
		}
		callbackStart, callbackEnd, callbackDepth := args[2][0], args[2][1], bodyDepth
		if _, nestedStart, nestedEnd, ok := callbackRange(tokens, callbackStart); ok && nestedEnd <= callbackEnd {
			callbackStart, callbackEnd = nestedStart, nestedEnd
			callbackDepth = tokens[nestedStart-1].depth + 1
		} else {
			arrows, firstArrow := 0, -1
			for j := callbackStart; j < callbackEnd; j++ {
				if at(tokens, j) == "=>" {
					arrows++
					firstArrow = j
				}
			}
			if arrows != 1 {
				continue
			}
			callbackStart = firstArrow + 1
		}
		callbackNested := nestedFunctionMask(tokens, callbackStart, callbackEnd)
		for j := callbackStart; j+3 < callbackEnd; j++ {
			if callbackNested[j-callbackStart] || tokens[j].depth != callbackDepth || at(tokens, j-1) == "." || at(tokens, j+1) != "(" ||
				(tokens[j+2].kind != 'n' && tokens[j+2].kind != 's') || at(tokens, j+3) != ")" {
				continue
			}
			if _, ok := entryNames[tokens[j].text]; ok {
				id := tokens[j+2].text
				if tokens[j+2].kind == 's' {
					id = stringValue(id)
				}
				startup.requires = appendUnique(startup.requires, id)
			}
		}
	}
	if len(startup.requires) == 0 {
		return nil
	}
	return startup
}

func callbackRange(tokens []token, start int) (alias string, bodyStart, bodyEnd int, ok bool) {
	i := start
	if at(tokens, i) == "function" {
		i++
		if at(tokens, i) != "(" {
			i++
		}
	}
	if at(tokens, i) == "(" && tokens[i].match > i {
		parts := argumentRanges(tokens, i)
		if len(parts) > 0 && parts[0][1] == parts[0][0]+1 {
			alias = at(tokens, parts[0][0])
		}
		i = tokens[i].match + 1
	} else if tokens[i].kind == 'i' && at(tokens, i+1) == "=>" {
		alias = tokens[i].text
		i++
	}
	if at(tokens, i) == "=>" {
		i++
	}
	if at(tokens, i) != "{" || tokens[i].match <= i {
		return "", 0, 0, false
	}
	return alias, i + 1, tokens[i].match, true
}

func readFactory(tokens []token, key int) (factoryRange, bool) {
	factory := factoryRange{id: tokens[key].text, line: tokens[key].line, column: tokens[key].column}
	if tokens[key].kind == 's' {
		factory.id = stringValue(factory.id)
	}
	value := key + 2
	if at(tokens, value) == "async" {
		value++
	}
	if at(tokens, value) == "function" {
		value++
		if at(tokens, value) != "(" {
			value++ // named function
		}
	}
	params := -1
	if at(tokens, value) == "(" && tokens[value].match > value {
		params = value
		value = tokens[value].match + 1
	} else if at(tokens, value+1) == "=>" {
		value += 2 // single-parameter arrow
	}
	if at(tokens, value) == "=>" {
		value++
	}
	if params >= 0 {
		parts := argumentRanges(tokens, params)
		if len(parts) >= 3 && parts[2][1] == parts[2][0]+1 && tokens[parts[2][0]].kind == 'i' {
			factory.loader = tokens[parts[2][0]].text
		}
	}
	if at(tokens, value) != "{" || tokens[value].match <= value {
		// Webpack also accepts concise arrow factories. Keep their expression
		// inside this module rather than letting it spill into the next key.
		if value >= len(tokens) {
			return factory, false
		}
		depth, paren := tokens[value].depth, tokens[value].paren
		factory.start = value
		for j := value; j < len(tokens); j++ {
			if tokens[j].depth < depth || tokens[j].depth == depth && tokens[j].paren == paren && at(tokens, j) == "," {
				factory.end = j
				return factory, j > value
			}
		}
		return factory, false
	}
	factory.start, factory.end = value+1, tokens[value].match
	return factory, true
}

func chunkReceiver(tokens []token, push int) string {
	receiver := push - 2
	start := receiver
	if at(tokens, receiver) == ")" && tokens[receiver].match >= 0 {
		start = tokens[receiver].match
	} else if at(tokens, receiver) == "]" && tokens[receiver].match >= 0 {
		start = tokens[receiver].match - 1
	} else {
		start = max(0, receiver-2)
	}
	for i := start; i+2 < push; i++ {
		if at(tokens, i) != "self" && at(tokens, i) != "window" && at(tokens, i) != "globalThis" && at(tokens, i) != "this" {
			continue
		}
		if at(tokens, i+1) == "." && tokens[i+2].kind == 'i' {
			return tokens[i+2].text
		}
		if at(tokens, i+1) == "[" && tokens[i+2].kind == 's' {
			return stringValue(tokens[i+2].text)
		}
	}
	return ""
}

func isFactoryValue(tokens []token, i int) bool {
	if at(tokens, i) == "function" || at(tokens, i) == "async" && at(tokens, i+1) == "function" {
		return true
	}
	if at(tokens, i) == "(" && tokens[i].match > i && at(tokens, tokens[i].match+1) == "=>" {
		return true
	}
	return i < len(tokens) && tokens[i].kind == 'i' && at(tokens, i+1) == "=>"
}

func firstGlobalName(tokens []token, limit int) string {
	for i := 0; i+3 < len(tokens) && i < limit; i++ {
		if at(tokens, i) != "this" && at(tokens, i) != "window" && at(tokens, i) != "self" && at(tokens, i) != "globalThis" {
			continue
		}
		if at(tokens, i+1) == "." && tokens[i+2].kind == 'i' && at(tokens, i+3) == "=" {
			return tokens[i+2].text
		}
		if at(tokens, i+1) == "[" && tokens[i+2].kind == 's' && at(tokens, i+3) == "]" {
			return stringValue(tokens[i+2].text)
		}
	}
	return ""
}

func analyzeCompiled(file *File, tokens []token, chunk *chunkInfo) {
	if isUMD(tokens) {
		file.Format = "UMD bundle"
		file.Shape = append(file.Shape, "Runs an environment-aware wrapper that invokes the bundle factory at load time.")
		file.EntryPoints = []Entry{{Trigger: "module load", Action: "runs the UMD wrapper", Line: firstLine(tokens)}}
		if chunk != nil {
			file.Shape = append(file.Shape, chunkDescription(chunk))
		}
	} else if chunk != nil {
		file.Format = "loader chunk"
		line := 1
		if len(tokens) > 0 {
			line = tokens[0].line
		}
		file.EntryPoints = []Entry{{Trigger: "module load", Action: "registers chunk with loader", Line: line}}
		file.Shape = append(file.Shape, chunkDescription(chunk))
		if chunk.startup == nil {
			file.Notes = append(file.Notes, "No direct startup module require was identified in this chunk; the host runtime decides when factories run.")
		}
	} else if global := firstGlobalName(tokens, min(len(tokens), 150)); global != "" {
		file.Format = "compiled script"
		file.Shape = append(file.Shape, "Initializes global namespace "+global+".")
		if enclosingIIFE(tokens) {
			file.EntryPoints = []Entry{{Trigger: "module load", Action: "runs the enclosing IIFE", Line: firstLine(tokens)}}
		}
	} else if enclosingIIFE(tokens) {
		file.Format = "compiled IIFE"
		file.EntryPoints = []Entry{{Trigger: "module load", Action: "runs the enclosing IIFE", Line: firstLine(tokens)}}
	} else {
		file.Format = "compiled script"
		functions := 0
		for _, t := range tokens {
			if t.text == "function" && t.depth == 0 && t.paren == 0 {
				functions++
			}
		}
		if functions > 0 {
			file.Shape = append(file.Shape, fmt.Sprintf("Defines %d top-level functions in a single compiled script.", functions))
		}
		if line := trailingIIFELine(tokens); line > 0 {
			file.EntryPoints = []Entry{{Trigger: "module load", Action: "invokes a final top-level IIFE", Line: line}}
		}
	}
	if chunk != nil {
		return
	}
	interactions := make(map[string]struct{})
	pageMask, _, pageScripts := pageScriptMask(tokens)
	if pageScripts {
		interactions["scripts evaluated in browser pages"] = struct{}{}
	}
	for i, t := range tokens {
		if isMasked(pageMask, i) {
			continue
		}
		if storage := storageUse(tokens, i); storage != "" {
			interactions[storage] = struct{}{}
		}
		if t.kind != 'i' || at(tokens, i+1) != "(" || tokens[i+1].match < 0 || !isCall(tokens, i) {
			continue
		}
		receiver := ""
		if at(tokens, i-1) == "." || at(tokens, i-1) == "?." {
			receiver = at(tokens, i-2)
		}
		for _, item := range callInteractions(tokens, call{name: t.text, receiver: receiver}) {
			interactions[item] = struct{}{}
		}
	}
	file.Interactions = sortedKeys(interactions)
	file.Notes = append(file.Notes, "Compiled names may be shortened; source locations and observed calls refer to the generated file.")
}

func trailingIIFELine(tokens []token) int {
	for i := len(tokens) - 1; i >= max(0, len(tokens)-5000); i-- {
		if at(tokens, i) != "(" || tokens[i].depth != 0 || tokens[i].paren != 0 ||
			at(tokens, i+1) != "function" || tokens[i].match <= i || at(tokens, i-1) == "=" {
			continue
		}
		call := tokens[i].match + 1
		if at(tokens, call) == "(" && tokens[call].match > call && len(tokens)-tokens[call].match < 64 {
			return tokens[i].line
		}
	}
	return 0
}

func chunkDescription(chunk *chunkInfo) string {
	detail := "Registers "
	if chunk.recovered {
		detail = "Recognizes at least "
	}
	if chunk.modules > 0 {
		detail += fmt.Sprintf("%d module factories", chunk.modules)
	} else {
		detail += "module factories"
	}
	if chunk.id != "" {
		detail += " in chunk " + chunk.id
	}
	if chunk.global != "" {
		detail += " via " + chunk.global
	}
	return detail + "."
}

func firstLine(tokens []token) int {
	if len(tokens) > 0 {
		return tokens[0].line
	}
	return 1
}

func isUMD(tokens []token) bool {
	limit := min(len(tokens), 250)
	amd, commonJS := false, false
	for i := 0; i+2 < limit; i++ {
		if at(tokens, i) == "define" && at(tokens, i+1) == "." && at(tokens, i+2) == "amd" {
			amd = true
		}
		if at(tokens, i) == "module" && at(tokens, i+1) == "." && at(tokens, i+2) == "exports" {
			commonJS = true
		}
	}
	return amd && commonJS && containsIIFE(tokens)
}

func containsIIFE(tokens []token) bool {
	limit := min(len(tokens), 250)
	for i := 0; i+2 < limit; i++ {
		if tokens[i].depth == 0 && (at(tokens, i) == "(" && (at(tokens, i+1) == "function" || at(tokens, i+1) == "(") ||
			at(tokens, i) == "!" && at(tokens, i+1) == "function") {
			return true
		}
	}
	return false
}
