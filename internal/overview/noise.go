package overview

import "strings"

// Bundlers inject the same small helpers into every output file. They never
// describe the application, so they are kept out of units, links, and entries.
var bundlerHelpers = map[string]struct{}{
	// esbuild
	"__create": {}, "__defProp": {}, "__getOwnPropDesc": {}, "__getOwnPropNames": {}, "__getProtoOf": {},
	"__hasOwnProp": {}, "__esm": {}, "__commonJS": {}, "__export": {}, "__copyProps": {}, "__toESM": {},
	"__toCommonJS": {}, "__require": {}, "__name": {}, "__publicField": {}, "__reExport": {}, "__init": {},
	"__defNormalProp": {}, "__spreadValues": {}, "__spreadProps": {}, "__objRest": {}, "__async": {},
	"__decorateClass": {}, "__privateGet": {}, "__privateAdd": {}, "__privateSet": {}, "__privateMethod": {},
	"__using": {}, "__callDispose": {}, "__template": {}, "__glob": {}, "__vite__mapDeps": {},
	// TypeScript
	"__awaiter": {}, "__generator": {}, "__extends": {}, "__assign": {}, "__rest": {}, "__decorate": {},
	"__param": {}, "__metadata": {}, "__importDefault": {}, "__importStar": {}, "__createBinding": {},
	"__setModuleDefault": {}, "__exportStar": {}, "__spreadArray": {}, "__values": {}, "__read": {},
	"__classPrivateFieldGet": {}, "__classPrivateFieldSet": {}, "__makeTemplateObject": {}, "__asyncValues": {},
	// Babel
	"_interopRequireDefault": {}, "_interopRequireWildcard": {}, "_classCallCheck": {}, "_createClass": {},
	"_defineProperty": {}, "_asyncToGenerator": {}, "_inherits": {}, "_possibleConstructorReturn": {},
	"_getPrototypeOf": {}, "_setPrototypeOf": {}, "_typeof": {}, "_slicedToArray": {}, "_toConsumableArray": {},
	"_objectSpread": {}, "_objectSpread2": {}, "_objectWithoutProperties": {}, "_extends": {},
	"_assertThisInitialized": {}, "_createSuper": {}, "_isNativeReflectConstruct": {}, "_regeneratorRuntime": {},
	"_toPropertyKey": {}, "_toPrimitive": {}, "_arrayLikeToArray": {}, "_unsupportedIterableToArray": {},
	"_iterableToArray": {}, "_arrayWithoutHoles": {}, "_nonIterableSpread": {}, "_arrayWithHoles": {},
	"_iterableToArrayLimit": {}, "_nonIterableRest": {}, "_createForOfIteratorHelper": {}, "_taggedTemplateLiteral": {},
}

// esbuild declares its module runtime helpers at the top of the output file.
func esbuildRuntime(tokens []token) bool {
	for i := 0; i+3 < min(len(tokens), 4000); i++ {
		if tokens[i].depth == 0 && tokens[i].paren == 0 && (tokens[i].text == "var" || tokens[i].text == "const") && at(tokens, i+2) == "=" {
			switch tokens[i+1].text {
			case "__commonJS", "__esm", "__toESM", "__toCommonJS":
				return true
			}
		}
	}
	return false
}

func isBundlerHelper(name string) bool {
	_, ok := bundlerHelpers[name]
	return ok
}

// Language built-ins called at module level set up data; they are not where
// application behavior starts.
var builtinCallees = map[string]struct{}{
	"Map": {}, "Set": {}, "WeakMap": {}, "WeakSet": {}, "WeakRef": {}, "RegExp": {}, "Symbol": {}, "Error": {},
	"TypeError": {}, "RangeError": {}, "SyntaxError": {}, "Date": {}, "Promise": {}, "Proxy": {}, "Array": {},
	"Object": {}, "Number": {}, "String": {}, "Boolean": {}, "BigInt": {}, "Function": {}, "parseInt": {},
	"parseFloat": {}, "isNaN": {}, "isFinite": {}, "encodeURIComponent": {}, "decodeURIComponent": {},
	"encodeURI": {}, "decodeURI": {}, "Uint8Array": {}, "Uint16Array": {}, "Uint32Array": {}, "Int32Array": {},
	"Float32Array": {}, "Float64Array": {}, "ArrayBuffer": {}, "DataView": {}, "TextEncoder": {}, "TextDecoder": {},
	"URL": {}, "URLSearchParams": {}, "AbortController": {}, "FinalizationRegistry": {}, "Intl": {},
	"createRequire": {}, "fileURLToPath": {}, "promisify": {},
}

var builtinReceivers = map[string]struct{}{
	"Object": {}, "Array": {}, "Promise": {}, "JSON": {}, "Math": {}, "Reflect": {}, "Number": {}, "String": {},
	"Symbol": {}, "Date": {}, "console": {}, "Intl": {}, "Buffer": {}, "BigInt": {},
}

// Collection and string methods on a local value read or prepare data.
var dataMethods = map[string]struct{}{
	"includes": {}, "indexOf": {}, "map": {}, "forEach": {}, "filter": {}, "reduce": {}, "find": {}, "some": {},
	"every": {}, "push": {}, "pop": {}, "shift": {}, "unshift": {}, "splice": {}, "slice": {}, "concat": {},
	"join": {}, "split": {}, "replace": {}, "replaceAll": {}, "trim": {}, "toString": {}, "toLowerCase": {},
	"toUpperCase": {}, "startsWith": {}, "endsWith": {}, "match": {}, "test": {}, "exec": {}, "keys": {},
	"values": {}, "entries": {}, "has": {}, "set": {}, "add": {}, "delete": {}, "fill": {}, "sort": {},
	"bind": {}, "freeze": {}, "assign": {}, "padStart": {}, "padEnd": {}, "charCodeAt": {}, "flat": {},
}

func isNoiseCall(c call) bool {
	if isBundlerHelper(c.name) {
		return true
	}
	if c.receiver == "" {
		_, builtin := builtinCallees[c.name]
		return builtin
	}
	if _, builtin := builtinReceivers[c.receiver]; builtin {
		return true
	}
	_, data := dataMethods[c.name]
	return data
}

// Automation scripts pass functions to a browser page. Their storage and DOM
// use belongs to that page, not to the file being analyzed.
var pageScriptCalls = map[string]struct{}{
	"evaluate": {}, "evaluateHandle": {}, "evaluateAll": {}, "$eval": {}, "$$eval": {}, "addInitScript": {},
	"executeScript": {}, "executeAsyncScript": {}, "waitForFunction": {},
}

// Functions passed by name are reported separately so their declarations can
// be masked too.
func pageScriptMask(tokens []token) ([]bool, map[string]struct{}, bool) {
	var masked []bool
	names := make(map[string]struct{})
	for i := 1; i+1 < len(tokens); i++ {
		if _, ok := pageScriptCalls[tokens[i].text]; !ok || tokens[i].kind != 'i' || at(tokens, i-1) != "." && at(tokens, i-1) != "?." {
			continue
		}
		open := i + 1
		if at(tokens, open) != "(" || tokens[open].match <= open {
			continue
		}
		if masked == nil {
			masked = make([]bool, len(tokens))
		}
		for j := open; j <= tokens[open].match; j++ {
			masked[j] = true
		}
		if args := argumentRanges(tokens, open); len(args) > 0 && args[0][1]-args[0][0] == 1 && tokens[args[0][0]].kind == 'i' {
			names[tokens[args[0][0]].text] = struct{}{}
		}
		i = tokens[open].match
	}
	return masked, names, masked != nil
}

func maskPageFunctions(mask []bool, names map[string]struct{}, units []unitRange) {
	for _, unit := range units {
		if _, ok := names[unit.Name]; ok {
			for j := max(0, unit.start); j < min(len(mask), unit.end); j++ {
				mask[j] = true
			}
		}
	}
}

func isMasked(mask []bool, index int) bool {
	return index >= 0 && index < len(mask) && mask[index]
}

// storageUse reports Web Storage only when the global is actually used, not
// when a parameter or property merely shares its name.
func storageUse(tokens []token, i int) string {
	name := tokens[i].text
	if tokens[i].kind != 'i' || name != "localStorage" && name != "sessionStorage" {
		return ""
	}
	if next := at(tokens, i+1); next != "." && next != "?." && next != "[" {
		return ""
	}
	if prev := at(tokens, i-1); prev == "." || prev == "?." {
		switch at(tokens, i-2) {
		case "window", "self", "globalThis":
		default:
			return ""
		}
	}
	return name
}

// enclosingIIFE reports whether one immediately invoked function wraps the
// whole script, as opposed to a polyfill IIFE that precedes other code.
func enclosingIIFE(tokens []token) bool {
	start := 0
	for start < len(tokens) && (tokens[start].text == ";" || tokens[start].kind == 's' && at(tokens, start+1) == ";") {
		if tokens[start].kind == 's' {
			start++
		}
		start++
	}
	switch at(tokens, start) {
	case "(", "!", "+", "~", "void":
	default:
		return false
	}
	head := false
	for i := start; i < min(len(tokens), start+4); i++ {
		head = head || tokens[i].text == "function" || tokens[i].text == "=>" || tokens[i].text == "(" && at(tokens, i+1) == "("
	}
	if !head {
		return false
	}
	for i := start; i < len(tokens); i++ {
		if tokens[i].depth == 0 && tokens[i].paren == 0 && tokens[i].text == ";" {
			return len(tokens)-i <= 2
		}
		if tokens[i].match > i {
			i = tokens[i].match
		}
	}
	return true
}

func eventTrigger(receiver, event string) string {
	switch receiver {
	case "", "document", "window", "self", "globalThis", "this", ")", "]":
		return event + " event"
	}
	if len(receiver) < 3 || strings.HasPrefix(receiver, "_") && len(receiver) < 4 {
		return event + " event"
	}
	return receiver + " " + event + " event"
}
