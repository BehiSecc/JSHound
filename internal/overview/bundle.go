package overview

import (
	"fmt"
)

type chunkInfo struct {
	id      string
	modules int
	global  string
}

// findChunk recognizes the loader registration shape used by Webpack-style
// bundles: global.push([[chunk IDs], { module ID: factory, ... }]).
func findChunk(tokens []token) *chunkInfo {
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
		info := &chunkInfo{}
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
		memberDepth := tokens[moduleOpen].depth + 1
		for j := moduleOpen + 1; j < tokens[moduleOpen].match; j++ {
			if tokens[j].depth == memberDepth && (tokens[j].kind == 'n' || tokens[j].kind == 's') &&
				at(tokens, j+1) == ":" && isFactoryValue(tokens, j+2) {
				info.modules++
			}
		}
		info.global = chunkReceiver(tokens, i)
		if info.modules == 0 || info.global == "" {
			continue
		}
		return info
	}
	return nil
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
		file.Shape = append(file.Shape, "Selects a CommonJS, AMD, or browser-global export at load time.")
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
		file.Notes = append(file.Notes, "The loader decides which factories run; this file alone does not show application startup.")
	} else if global := firstGlobalName(tokens, min(len(tokens), 150)); global != "" {
		file.Format = "compiled script"
		file.Shape = append(file.Shape, "Initializes global namespace "+global+".")
		if containsIIFE(tokens) {
			file.EntryPoints = []Entry{{Trigger: "module load", Action: "runs the enclosing IIFE", Line: firstLine(tokens)}}
		}
	} else if containsIIFE(tokens) {
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
	}
	interactions := make(map[string]struct{})
	for i, t := range tokens {
		switch t.text {
		case "localStorage", "sessionStorage":
			if t.kind == 'i' {
				interactions[t.text] = struct{}{}
			}
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
	file.Notes = append(file.Notes, "Compiled names and call paths are omitted because they do not reliably describe the original modules.")
}

func chunkDescription(chunk *chunkInfo) string {
	detail := "Registers "
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
