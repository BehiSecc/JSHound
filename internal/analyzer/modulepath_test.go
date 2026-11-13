package analyzer

import (
	"slices"
	"testing"
)

func TestModulePathsAreNotEndpoints(t *testing.T) {
	cases := map[string]string{
		"webpack module map":    `var m={"./src/app/index.ts":(e,t,n)=>{n(1)},"../../node_modules/react/index.js":function(e){}};`,
		"module map comment":    `var m={"./util/helpers":/***/ (function(e){})};`,
		"module map arrow":      `var m={"./util/helpers":e=>e};`,
		"context map":           `var m={"./locales/en.json":[1,2],"./locales/fr.json":[3,4]};`,
		"require call":          `var a=require("./lib/deflate"),b=require('../utils/common');`,
		"require.resolve":       `var a=require.resolve("./lib/deflate");`,
		"dynamic import":        `import("./pages/settings");`,
		"import from":           `import{a}from"./chunk-3G6DP5DN.js";import b from './stream/worker';`,
		"side-effect import":    `import "./polyfills";`,
		"node_modules path":     `var p="../../node_modules/lodash/fp/unset.js", q="node_modules/react/index";`,
		"source path":           `var p="../../packages/components/src/Button/size.ts", q="./src/App.tsx";`,
		"chunk file":            `var p="./chunk-3G6DP5DN.js";`,
		"bare specifier":        `var p="./crc32", q="../GenericWorker";`,
		"nested bare":           `var p="./stream/DataWorker";`,
		"style source":          `var p="./theme/vars.scss", q="./theme/vars.less";`,
		"module path has users": `var u=require("./users");`,
	}
	for name, source := range cases {
		t.Run(name, func(t *testing.T) {
			if got := Analyze([]byte(source)).Endpoints; len(got) != 0 {
				t.Errorf("endpoints = %v, want none", got)
			}
		})
	}
}

func TestRelativeRequestPathsRemainEndpoints(t *testing.T) {
	cases := []struct{ source, want string }{
		{`var a="./api/users";`, "./api/users"},
		{`var a="../rest/v1/items?x=1";`, "../rest/v1/items?x=1"},
		{`var a="../v1/items?x=";`, "../v1/items?x="},
		{`fetch("./data");`, "./data"},
		{`axios.get('../lookup/names');`, "../lookup/names"},
		{`$.ajax("./feed.json");`, "./feed.json"},
		{`x({url:"./probe"});`, "./probe"},
		{`var a="./data.json";`, "./data.json"},
		{`var a="./report.js?v=3";`, "./report.js?v=3"},
		{`var a=cond?"./api/users":"./api/other";`, "./api/users"},
		{`var a="/api/users/:id";`, "/api/users/:id"},
		{`var a="api/users";`, "api/users"},
	}
	for _, tc := range cases {
		if got := Analyze([]byte(tc.source)).Endpoints; !slices.Contains(got, tc.want) {
			t.Errorf("%s: endpoints = %v, want %q", tc.source, got, tc.want)
		}
	}
}

func TestModulePathsStayFileCandidates(t *testing.T) {
	got := Analyze([]byte(`var w="./worker.js"; require("./src/app.ts");`)).Files
	if !slices.Contains(got, "./worker.js") || !slices.Contains(got, "./src/app.ts") {
		t.Errorf("files = %v, want the quoted file paths", got)
	}
}
