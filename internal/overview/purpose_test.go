package overview

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// closureAngularApp mirrors Closure-compiled Angular output: every identifier
// is renamed, but RPC method names, component selectors, style markers, and
// user-facing messages survive as strings.
const closureAngularApp = `'use strict';function aa(){throw Error("Cannot find global object");}
function qS(){this.i="CreativeService";this.b=eS(this,"approveCreatives");this.c=eS(this,"copyCreative");this.d=eS(this,"submitCreatives");this.f=eS(this,"rejectCreatives");this.g=eS(this,"getCreativeById")}
function kS(){this.i="AssetService";this.b=eS(this,"uploadAsset");this.c=eS(this,"deleteAssets");this.d=eS(this,"transcodeAsset")}
H3.X=D({type:H3,F:[["creative-table"]],oa:[".row[_ngcontent-%COMP%]{padding:8px 16px 8px 96px}"]});
H4.X=D({type:H4,F:[["creative-details"]]});H5.X=D({type:H5,F:[["creative-preview"]]});
i3.X=D({type:i3,F:[["asset-library"]]});i4.X=D({type:i4,F:[["asset-preview-dialog"]]});
var xa=["xap-picker","xap-picker-option","xap-nav-link","xapSelection","xapSelectAll","xap-bubble","xap-card-header","xap-card-footer","xap-filterbar","xap-uploader","xap-tabstop"];
var ya="xap-picker-icon xap-picker-search-icon";
function zz(a){if(!a)throw Error("Campaign input is required for the creative preview.");a.l.set("Uploading the asset failed. Please try again.");a.m.set("Your creative was approved.")}
var bb=[{path:"creatives",component:H3},{path:"assets",component:i3}];
document.title="Studio";`

func TestPurposeDescribesClosureCompiledAngularApp(t *testing.T) {
	file := Analyze([]Source{{Name: "studio_compiled.js", Data: []byte(closureAngularApp)}}).Files[0]
	p := file.Purpose
	if p == nil {
		t.Fatal("missing purpose")
	}
	if p.Is != "Browser app using Angular, compiled by Closure Compiler." {
		t.Errorf("is = %q", p.Is)
	}
	areas := map[string]Feature{}
	for _, feature := range p.Features {
		areas[feature.Area] = feature
	}
	creative, ok := areas["creative"]
	if !ok || len(p.Features) == 0 || p.Features[0].Area != "creative" {
		t.Fatalf("creative should lead the features: %+v", p.Features)
	}
	for _, action := range []string{"approve", "copy", "submit", "reject"} {
		if !strings.Contains(strings.Join(creative.Actions, ","), action) {
			t.Errorf("creative actions %v missing %q", creative.Actions, action)
		}
	}
	if asset, ok := areas["asset"]; !ok || !strings.Contains(strings.Join(asset.Actions, ","), "upload") {
		t.Errorf("asset feature = %+v", asset)
	}
	if _, ok := areas["xap"]; ok {
		t.Errorf("UI kit prefix reported as a feature: %+v", p.Features)
	}
}

func TestPurposeTextIsShortByDefault(t *testing.T) {
	result := Analyze([]Source{{Name: "studio_compiled.js", Data: []byte(closureAngularApp)}})
	var plain, verbose bytes.Buffer
	if err := WriteText(&plain, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := WriteText(&verbose, result, TextOptions{Verbose: true}); err != nil {
		t.Fatal(err)
	}
	output := plain.String()
	for _, expected := range []string{"### Type", "Browser app using Angular, compiled by Closure Compiler.", "### Features", "- creative: ", "### Behavior", "--verbose"} {
		if !strings.Contains(output, expected) {
			t.Errorf("missing %q:\n%s", expected, output)
		}
	}
	for _, detail := range []string{"### Entry paths", "### Main units", "### Observed links", "mermaid"} {
		if strings.Contains(output, detail) {
			t.Errorf("default report includes %q:\n%s", detail, output)
		}
	}
	if strings.Contains(verbose.String(), "### What this file does") || !strings.Contains(verbose.String(), "### Type") {
		t.Errorf("verbose report changed shape:\n%s", verbose.String())
	}
}

func TestLicenseBannerNamesTheLibrary(t *testing.T) {
	cases := []struct {
		raw, name, version string
	}{
		{"/*! jQuery v3.7.1 | (c) OpenJS Foundation and other contributors | jquery.org/license */\n!function(e,t){}", "jQuery", "3.7.1"},
		{"/**\n* vue v3.4.38\n* (c) 2018-present Yuxi (Evan) You and Vue contributors\n* @license MIT\n**/var Vue=function(e){}", "vue", "3.4.38"},
		{"/**\n * @license React\n * react-dom.production.min.js\n *\n * Copyright (c) Facebook, Inc.\n */\n(function(){})()", "react-dom", ""},
		{"/*! For license information please see main.js.LICENSE.txt */\n(()=>{})()", "", ""},
		{"var a=1;/*! jQuery v3.7.1 */", "", ""},
		{"/* Copyright 2024 Example Inc. Licensed under MIT 2.0 */\nvar a", "", ""},
	}
	for _, c := range cases {
		name, version := licenseBanner(c.raw)
		if name != c.name || version != c.version {
			t.Errorf("licenseBanner(%.40q) = %q %q, want %q %q", c.raw, name, version, c.name, c.version)
		}
	}
}

func TestLibraryPurposeOmitsFeatureGuesses(t *testing.T) {
	source := "/*! jQuery v3.7.1 | (c) OpenJS Foundation */\n!function(e,t){var userList=[\"user-table\"];new XMLHttpRequest}(window);"
	file := Analyze([]Source{{Name: "jquery.min.js", Data: []byte(source)}}).Files[0]
	if file.Purpose == nil || file.Purpose.Library != "jQuery 3.7.1" || !strings.HasPrefix(file.Purpose.Is, "The jQuery 3.7.1 library") {
		t.Fatalf("purpose = %+v", file.Purpose)
	}
	if len(file.Purpose.Features) != 0 {
		t.Errorf("library purpose guessed features: %+v", file.Purpose)
	}
	for _, fact := range file.Purpose.Runs {
		if fact.Aspect == "Starts" || fact.Aspect == "Navigation" {
			t.Errorf("library purpose guessed app behavior: %+v", fact)
		}
	}
}

func TestNameWordsSplitsStructuredNames(t *testing.T) {
	cases := map[string][]string{
		"approveCreatives":     {"approve", "creative"},
		"XMLHttpRequest":       {"xml", "http", "request"},
		"asset_library":        {"asset", "library"},
		"creative-preview-dlg": {"creative", "preview", "dlg"},
		"getRepos":             {"get", "repository"},
		"CategoriesView":       {"category", "view"},
	}
	for name, want := range cases {
		if got := nameWords(name); !reflect.DeepEqual(got, want) {
			t.Errorf("nameWords(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestProseWordsRejectsClassListsAndCode(t *testing.T) {
	if words := proseWords("Your Studio session has expired. Refresh the page to continue."); len(words) < 8 {
		t.Errorf("message words = %v", words)
	}
	for _, value := range []string{"xap-picker-icon xap-picker-search-icon", "a.b && c(d)", "mat-mdc-button mdc-button--raised", "Hello"} {
		if words := proseWords(value); words != nil {
			t.Errorf("proseWords(%q) = %v", value, words)
		}
	}
}

func TestWeakVocabularyReportsNoFeatures(t *testing.T) {
	source := `document.body.className="user-card";var a={loadSettings:1,saveDraft:2};`
	file := Analyze([]Source{{Name: "small.js", Data: []byte(source)}}).Files[0]
	if file.Purpose == nil || len(file.Purpose.Features) != 0 {
		t.Errorf("features from weak evidence: %+v", file.Purpose)
	}
	var output bytes.Buffer
	if err := WriteText(&output, Result{Sources: 1, Files: []File{file}}, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "Feature areas") {
		t.Errorf("weak file lists features:\n%s", output.String())
	}
}

func TestVocabularySkipsBundledPackageTokens(t *testing.T) {
	source := `var a=["invoiceTable","invoiceList","sendInvoice","invoice-details","invoice-preview","payInvoice"];`
	tokens := lex(source)
	all := collectVocabulary(&File{}, tokens, nil, nil)
	none := collectVocabulary(&File{}, tokens, func(int) bool { return true }, nil)
	if all.nouns["invoice"] != 6 || len(none.nouns) != 0 {
		t.Errorf("all=%v none=%v", all.nouns, none.nouns)
	}
}

func runFactMap(facts []RunFact) map[string]string {
	result := make(map[string]string, len(facts))
	for _, fact := range facts {
		result[fact.Aspect] = fact.Detail
	}
	return result
}

func TestRunFactsDescribeAppBehavior(t *testing.T) {
	source := `var api=axios.create();api.interceptors.request.use(function(c){c.headers={"x-app-csrf-token":csrf(),"X-Request-Id":id()};return c});
fetch("/v1/user");var db=window.indexedDB.open("cache");document.cookie="a=1";history.pushState({},"","/home");window.addEventListener("popstate",f);
var w=new Worker("w.js");w.postMessage(1);var r=new FileReader;var p=["two_step_verification","/passport/sso","landing_espresso_btn","two_factor_code","sso_login"];
!function(e,t,n){e.src="https://connect.facebook.net/en_US/fbevents.js"}();var site="https://www.facebook.com/exampleapp";
var h={"X-Amz-Date":d,partNumber:1,uploadId:u};
ReactDOM.createRoot(document.getElementById("root")).render(App);`
	facts := runFactMap(runFacts(scanRunSignals(lex(source), nil), []string{"React"}, false, contextPage))
	want := map[string]string{
		"Starts":        "mounts a React UI into #root",
		"Navigation":    "client-side routing with the History API",
		"Server calls":  "fetch; sends x-app-csrf-token header; interceptors wrap requests",
		"Sign-in":       "two-step verification and single sign-on",
		"Files":         "chunked uploads to S3-compatible storage with signed requests, reads files in the browser",
		"Storage":       "cookies and IndexedDB",
		"Background":    "web workers and messages with other windows or workers",
		"Third parties": "Facebook Pixel (ads)",
	}
	if !reflect.DeepEqual(facts, want) {
		t.Errorf("facts:\n got %v\nwant %v", facts, want)
	}
}

func TestRunFactsNameUploadProtocols(t *testing.T) {
	google := runFactMap(runFacts(scanRunSignals(lex(`x.setRequestHeader("X-Goog-Upload-Protocol","resumable")`), nil), nil, false, contextPage))
	if google["Files"] != "resumable uploads with the X-Goog-Upload protocol" {
		t.Errorf("google upload = %q", google["Files"])
	}
	signed := runFactMap(runFacts(scanRunSignals(lex(`var h={"x-amz-content-sha256":s}`), nil), nil, false, contextPage))
	if signed["Files"] != "uploads to S3-compatible storage with signed requests" {
		t.Errorf("signed upload = %q", signed["Files"])
	}
}

func TestRunFactsIgnoreMentionsAndLibraries(t *testing.T) {
	source := `var m="Set the X-Auth-Token header",u="https://www.facebook.com/brand",l="https://stripe.com/docs",k="landing_espresso_btn";
history.pushState({},"","/");ReactDOM.render(App,document.getElementById("app"));`
	facts := runFactMap(runFacts(scanRunSignals(lex(source), nil), []string{"React"}, true, contextPage))
	for _, aspect := range []string{"Starts", "Navigation", "Server calls", "Sign-in", "Third parties"} {
		if detail, ok := facts[aspect]; ok {
			t.Errorf("library or mention produced %s: %q", aspect, detail)
		}
	}
}

func TestUMDHelperDoesNotMakeAnAppALibrary(t *testing.T) {
	umd := `(function(root,f){typeof define=="function"&&define.amd?define([],f):typeof module=="object"&&module.exports?module.exports=f():root.X=f()})(this,function(){return 1});`
	app := Analyze([]Source{{Name: "app.js", Data: []byte(umd + `ReactDOM.createRoot(document.getElementById("root")).render(App);`)}}).Files[0]
	if strings.Contains(app.Purpose.Is, "library") {
		t.Errorf("app with UMD helper = %q", app.Purpose.Is)
	}
	lib := Analyze([]Source{{Name: "lib.js", Data: []byte(umd + `window.document.title="x";`)}}).Files[0]
	if !strings.Contains(lib.Purpose.Is, "library") {
		t.Errorf("UMD library = %q", lib.Purpose.Is)
	}
}
