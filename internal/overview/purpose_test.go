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
	for _, expected := range []string{"### What it is", "Browser app using Angular, compiled by Closure Compiler.", "### What it does", "- creative: ", "--verbose"} {
		if !strings.Contains(output, expected) {
			t.Errorf("missing %q:\n%s", expected, output)
		}
	}
	for _, detail := range []string{"### Entry paths", "### Main units", "### Observed links", "mermaid"} {
		if strings.Contains(output, detail) {
			t.Errorf("default report includes %q:\n%s", detail, output)
		}
	}
	if !strings.Contains(verbose.String(), "### What this file does") || strings.Contains(verbose.String(), "### What it is") {
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
	if len(file.Purpose.Features) != 0 || strings.Contains(file.Purpose.Runs, "navigates") {
		t.Errorf("library purpose guessed app behavior: %+v", file.Purpose)
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
	all := collectVocabulary(&File{}, tokens, nil)
	none := collectVocabulary(&File{}, tokens, func(int) bool { return true })
	if all.nouns["invoice"] != 6 || len(none.nouns) != 0 {
		t.Errorf("all=%v none=%v", all.nouns, none.nouns)
	}
}
