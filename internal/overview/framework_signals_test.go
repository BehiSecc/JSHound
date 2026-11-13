package overview

import (
	"strings"
	"testing"
)

// A framework is named from code that calls, defines, or assigns its
// identifiers. Reading one, or testing whether the page has it, is not use.
func TestFrameworkNeedsUseNotMention(t *testing.T) {
	for _, tc := range []struct {
		framework string
		mentions  []string
		uses      []string
	}{
		{"jQuery",
			[]string{
				`var $ = typeof o !== "undefined" && (o.jQuery || o.Zepto); if ($ && !$.ua) { run(); }`,
				`if (typeof jQuery !== "undefined") { run(); }`,
				`if (window.jQuery) { var j = window.jQuery; run(j); }`,
				`var globals = { jQuery: true, $: true }; run(globals);`,
				`if (a.jQuery == null) { run(); }`,
			},
			[]string{
				`jQuery(".menu").hide(); run();`,
				`jQuery.fn.plugin = function () { run(); };`,
				`window.jQuery = window.$ = factory(); function factory() {}`,
				`var el = window.jQuery(".menu"); run(el);`,
			}},
		{"Next.js",
			[]string{
				`var props = window.__NEXT_DATA__.props; run(props);`,
				`if (window.__NEXT_DATA__) { run(); }`,
			},
			[]string{
				`window.__NEXT_DATA__ = JSON.parse(text); run();`,
			}},
		{"Angular",
			[]string{
				`var v = root.getAttribute("ng-version"); run(v);`,
				`if (root.hasAttribute("ng-version")) { run(); }`,
				`if (core.ɵɵdefineComponent) { run(); }`,
				`var define = core.ɵɵdefineComponent; run(define);`,
			},
			[]string{
				`var attrs = ["ng-version", "17.0.0"]; run(attrs);`,
				`class A { static ɵcmp = i0.ɵɵdefineComponent({ type: A }); }`,
				`function ɵɵdefineComponent(def) { return def; }`,
				`var exported = { ɵɵdefineComponent: define }; function define() {}`,
			}},
	} {
		for _, code := range tc.mentions {
			if got := summaryOf(t, "app.js", code); strings.Contains(got, tc.framework) {
				t.Errorf("%s named from a mention in %q: %q", tc.framework, code, got)
			}
		}
		for _, code := range tc.uses {
			if got := summaryOf(t, "app.js", code); !strings.Contains(got, "using "+tc.framework) {
				t.Errorf("%s not named from use in %q: %q", tc.framework, code, got)
			}
		}
	}
}

func TestFrameworkFromImportsAndPackageRecordsIsKept(t *testing.T) {
	if got := summaryOf(t, "app.js", `import $ from "jquery"; import { createApp } from "vue"; createApp({}).mount("#app");`); !strings.Contains(got, "Vue") {
		t.Errorf("import evidence lost: %q", got)
	}
	code := "var __commonJS = (cb, mod) => function __require() { return mod; };\n" +
		"// node_modules/jquery/dist/jquery.js\nvar j = {};\n// src/app.ts\nexport function api() { return 1; }\n"
	if got := summaryOf(t, "out.js", code); !strings.Contains(got, "jQuery") {
		t.Errorf("bundled package record lost: %q", got)
	}
}
