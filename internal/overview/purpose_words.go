package overview

import "strings"

// actionVerbs maps words that describe a user-level action to the form shown
// in the report. Plumbing verbs such as get, set, or handle are not actions.
var actionVerbs = map[string]string{
	"create": "create", "add": "add", "new": "create", "insert": "add",
	"delete": "delete", "remove": "delete", "destroy": "delete",
	"edit": "edit", "update": "edit", "modify": "edit", "rename": "rename",
	"save": "save", "copy": "copy", "duplicate": "copy", "clone": "copy",
	"approve": "approve", "reject": "reject", "submit": "submit", "publish": "publish", "unpublish": "unpublish",
	"upload": "upload", "download": "download", "import": "import", "export": "export",
	"share": "share", "invite": "invite", "preview": "preview", "search": "search",
	"schedule": "schedule", "send": "send", "login": "sign in", "logout": "sign out", "signin": "sign in", "signout": "sign out",
	"signup": "sign up", "register": "register", "verify": "verify", "confirm": "confirm",
	"archive": "archive", "unarchive": "unarchive", "restore": "restore", "move": "move", "assign": "assign", "unassign": "unassign",
	"link": "link", "unlink": "unlink", "enable": "enable", "disable": "disable", "transcode": "transcode",
	"sync": "sync", "ingest": "ingest", "notify": "notify", "subscribe": "subscribe", "unsubscribe": "unsubscribe",
	"follow": "follow", "unfollow": "unfollow", "star": "star", "unstar": "unstar", "reply": "reply",
	"merge": "merge", "fork": "fork", "pay": "pay", "purchase": "purchase", "checkout": "check out",
	"cancel": "cancel", "refund": "refund", "generate": "generate",
	"encrypt": "encrypt", "decrypt": "decrypt", "authenticate": "authenticate", "authorize": "authorize",
	"connect": "connect", "disconnect": "disconnect", "deploy": "deploy", "install": "install", "uninstall": "uninstall",
	"tag": "tag", "untag": "untag", "pin": "pin", "unpin": "unpin", "block": "block", "unblock": "unblock",
	"mute": "mute", "unmute": "unmute", "translate": "translate", "print": "print",
	"reset": "reset", "revoke": "revoke", "grant": "grant", "accept": "accept", "decline": "decline", "close": "close", "reopen": "reopen",
	"watch": "watch", "unwatch": "unwatch", "vote": "vote", "join": "join",
}

// libraryPrefixes are leading words of framework or widget-kit names, such as
// "mat-option" or "cdk-overlay". Names that start with one describe the UI
// toolkit, not the application.
var libraryPrefixes = setOf(
	"mat", "mdc", "cdk", "ng", "ngx", "aria", "data", "react", "vue", "svelte", "el", "ant", "chakra", "mui", "bs",
	"btn", "fa", "glyphicon", "material", "goog", "jscomp", "webkit", "moz", "ms", "xmlns", "ql", "ace", "cm",
	"swiper", "slick", "select2", "tippy", "popper", "tw", "lucide", "octicon", "prc", "primer", "gm", "gmp",
	"zod",
)

// serviceNames are the single-word names of the third-party services in
// knownServices, such as "sentry" and "stripe". A service is a dependency, not
// something the application does, so its name is never a feature area.
var serviceNames = func() map[string]struct{} {
	names := map[string]struct{}{}
	for _, service := range knownServices {
		if name := strings.ToLower(service.name); !strings.ContainsAny(name, " -") {
			names[name] = struct{}{}
		}
	}
	return names
}()

// genericWords never name a feature: programming plumbing, UI widgets, layout,
// common English, and browser or framework vocabulary.
var genericWords = setOf(
	// plumbing
	"get", "set", "has", "the", "and", "for", "with", "from", "into", "onto", "not", "all", "any", "can", "should", "will", "use",
	"handle", "handler", "init", "initialize", "load", "loaded", "loading", "fetch", "render", "rendered", "build", "make", "compute",
	"check", "ensure", "apply", "parse", "format", "convert", "compare", "emit", "dispatch", "listen", "listener", "observe",
	"observer", "observable", "process", "run", "call", "invoke", "next", "map", "filter", "find", "sort", "reduce", "keys", "values",
	"value", "data", "item", "items", "list", "array", "object", "string", "number", "boolean", "function", "symbol", "bigint",
	"type", "kind", "name", "key", "index", "idx", "count", "total", "size", "length", "state", "status", "config", "configuration",
	"option", "setting", "param", "parameter", "arg", "argument", "prop", "property", "attr", "attribute", "result", "response",
	"request", "error", "err", "warning", "warn", "info", "debug", "log", "logger", "trace", "message", "msg", "event", "callback",
	"promise", "async", "await", "sync", "task", "job", "queue", "stack", "buffer", "stream", "cache", "store", "storage", "local",
	"global", "default", "custom", "base", "core", "common", "util", "utils", "helper", "internal", "private", "public", "static",
	"instance", "factory", "provider", "service", "module", "component", "directive", "pipe", "controller", "manager", "context",
	"model", "schema", "entity", "record", "field", "column", "row", "cell", "node", "element", "child", "children", "parent", "root",
	"tree", "path", "url", "uri", "href", "src", "http", "https", "api", "json", "xml", "html", "css", "dom", "rpc", "ajax",
	"true", "false", "null", "undefined", "nan", "prototype", "constructor", "class", "method", "id", "uuid", "guid", "ref",
	"current", "previous", "prev", "last", "first", "start", "end", "begin", "finish", "done", "complete", "completed", "pending",
	"success", "failure", "fail", "failed", "invalid", "valid", "validate", "validation", "validator", "required", "optional",
	"enabled", "disabled", "active", "inactive", "visible", "hidden", "show", "hide", "open", "opened", "closed", "selected", "select",
	"selection", "toggle", "toggled", "expanded", "collapsed", "expand", "collapse", "focus", "focused", "blur", "hover", "click",
	"change", "changed", "input", "output", "keydown", "keyup", "keypress", "mouse", "mousedown", "mouseup", "mousemove", "touch",
	"pointer", "drag", "drop", "scroll", "resize", "wheel", "submit", "reset", "time", "timer", "timeout", "interval", "delay", "date",
	"day", "month", "year", "week", "hour", "minute", "second", "millisecond", "now", "today", "min", "max", "top", "left", "right",
	"bottom", "width", "height", "position", "offset", "margin", "padding", "border", "color", "background", "font", "style", "theme",
	"dark", "light", "primary", "secondary", "tertiary", "small", "medium", "large", "full", "auto", "inline", "block", "flex", "grid",
	"text", "label", "title", "description", "content", "container", "wrapper", "header", "footer", "body", "main", "section", "page",
	"view", "layout", "panel", "pane", "sidebar", "nav", "navigation", "navbar", "menu", "menuitem", "tab", "tabs", "button", "btn",
	"icon", "image", "img", "link", "anchor", "form", "dialog", "modal", "popup", "popover", "tooltip", "overlay", "backdrop", "drawer",
	"toast", "snackbar", "banner", "alert", "badge", "chip", "card", "divider", "spinner", "progress", "loader", "skeleton", "placeholder",
	"checkbox", "radio", "switch", "slider", "stepper", "step", "paginator", "pagination", "table", "display", "detail", "details",
	"summary", "picker", "datepicker", "calendar", "autocomplete", "dropdown", "combobox", "listbox", "option", "group", "toolbar",
	"bar", "box", "area", "region", "frame", "iframe", "window", "document", "screen", "viewport", "virtual", "portal", "outlet",
	"router", "route", "routing", "template", "slot", "host", "ripple", "animation", "animate", "transition", "keyframe", "accordion",
	"expansion", "action", "actions", "control", "controls", "trigger", "target", "source", "destination", "origin", "range",
	"line", "point", "level", "mode", "flag", "feature", "features", "support", "supported", "version", "test", "spec", "mock", "dev",
	"prod", "production", "development", "env", "environment", "browser", "chrome", "client", "server", "user", "users", "app", "application",
	"web", "site", "native", "platform", "system", "window", "self", "this", "that", "other", "more", "less", "new", "old", "empty",
	"none", "some", "each", "every", "only", "also", "just", "than", "then", "when", "where", "which", "while", "what", "who", "how",
	"why", "you", "your", "our", "their", "its", "his", "her", "are", "was", "were", "been", "being", "have", "had", "does", "did",
	"this", "these", "those", "there", "here", "out", "off", "over", "under", "after", "before", "about", "above", "below", "between",
	"through", "during", "without", "within", "again", "once", "may", "might", "must", "shall", "would", "could", "cannot", "please",
	"try", "again", "unable", "unknown", "loading", "wrapper", "inner", "outer", "head", "tail", "prefix", "suffix", "extra", "meta",
	"info", "item", "entry", "entries", "collection", "set", "map", "pair", "tuple", "byte", "bytes", "bit", "char", "code", "hash",
	"token", "chunk", "bundle", "lazy", "polyfill", "shim", "native", "proxy", "wrapper", "adapter", "plugin", "extension", "hook",
	"effect", "memo", "ref", "reducer", "action", "selector", "zone", "injector", "injection", "inject", "provide", "lifecycle", "init",
	"destroy", "mount", "unmount", "update", "dirty", "pristine", "touched", "untouched", "focusable", "tabindex", "role", "label",
	"described", "labelledby", "aria", "live", "polite", "assertive", "region", "contenteditable", "draggable", "spellcheck",
	"translate", "dir", "ltr", "rtl", "lang", "locale", "intl", "i18n", "currency", "decimal", "percent", "plural", "ordinal",
	"short", "long", "narrow", "numeric", "digit", "era", "zero", "one", "two", "few", "many", "align", "center", "middle",
	"justify", "stretch", "baseline", "space", "nowrap", "wrap", "overflow", "ellipsis", "visibility", "opacity", "transform",
	"translate", "rotate", "scale", "matrix", "shadow", "outline", "radius", "solid", "dashed", "dotted", "transparent", "white",
	"black", "gray", "grey", "red", "green", "blue", "yellow", "orange", "purple", "pink", "important", "none", "inherit", "initial",
	"unset", "normal", "bold", "italic", "underline", "uppercase", "lowercase", "capitalize", "cursor", "pointer", "events",
	"sticky", "fixed", "absolute", "relative", "visible", "zindex", "index", "order", "column", "columns", "gap", "basis",
	"shrink", "grow", "fill", "stroke", "svg", "path", "rect", "circle", "line", "polygon", "polyline", "ellipse", "defs", "use",
	"viewbox", "xlink", "canvas", "context", "webgl", "audio", "video", "media", "track", "source", "picture", "figure",
	"caption", "legend", "fieldset", "textarea", "span", "div", "paragraph", "heading", "strong", "emphasis", "list", "ordered",
	"unordered", "small", "big", "sub", "sup", "pre", "code", "kbd", "samp", "var", "abbr", "cite", "quote", "blockquote", "mark",
	"del", "ins", "time", "meter", "output", "details", "summary", "dialog", "menu", "noscript", "script", "link", "meta", "base",
	"star", "tag", "own", "query", "scope", "like", "leave", "rate", "track",
	"sdk", "match", "location", "read", "write", "flush", "batch", "ids",
	// programming concepts that name no product area
	"expression", "expr", "stmt", "literal", "binding", "defer", "catch", "xhr", "middleware", "namespace", "manifest",
	"let", "keyword", "double", "interpolation",
	// build tools and module systems
	"webpack", "rspack", "esm", "cjs", "umd", "amd", "vite", "rollup", "esbuild", "parcel", "babel", "require", "exports", "chunk",
	"runtime", "dist", "min", "map", "src", "lib", "vendor", "node", "modules", "index",
	// common English filler in prose
	"please", "sorry", "unexpected", "occurred", "something", "went", "wrong", "already", "exist", "exists", "found", "missing",
	"allowed", "able", "need", "needs", "must", "want", "like", "see", "learn", "here", "click", "tap", "enter", "choose", "provided",
	"provide", "using", "used", "make", "sure", "yet", "still", "now", "back", "right", "away", "way", "well", "good", "best", "same",
)

func setOf(words ...string) map[string]struct{} {
	set := make(map[string]struct{}, len(words))
	for _, word := range words {
		set[word] = struct{}{}
	}
	return set
}

// accessActions are the report forms of actions that get a user into an
// account. An area whose actions and API paths are nearly all of these is a
// way to sign in, not a product area.
var accessActions = setOf("sign in", "sign out", "sign up", "register", "verify", "confirm", "authenticate", "authorize", "reset")
