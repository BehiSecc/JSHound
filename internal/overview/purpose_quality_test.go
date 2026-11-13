package overview

import (
	"fmt"
	"reflect"
	"testing"
)

// qualityVocabulary adds names and sentences to a vocabulary.
func qualityVocabulary(names []string, sentences ...string) *vocabulary {
	v := newVocabulary()
	for _, name := range names {
		v.addName(name)
	}
	for _, sentence := range sentences {
		v.addProse(sentence, proseWords(sentence))
	}
	return v
}

// qualityEvidence indexes API calls and screens.
func qualityEvidence(api, screens []string) *evidence {
	ev := &evidence{claimed: map[string]struct{}{}, byNoun: map[string][]int{}}
	for _, path := range api {
		ev.addPath(path, pathAPI)
	}
	for _, path := range screens {
		ev.addPath(path, pathScreen)
	}
	ev.index()
	return ev
}

// qualityNames makes distinct names such as "deleteAlphaObs" for each word.
func qualityNames(format string, words ...string) []string {
	var names []string
	for _, word := range words {
		names = append(names, fmt.Sprintf(format, word))
	}
	return names
}

var qualityWords = []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf", "Hotel", "India", "Juliet", "Kilo", "Lima"}

func areaNames(features []Feature) []string {
	var areas []string
	for _, feature := range features {
		areas = append(areas, feature.Area)
	}
	return areas
}

func TestTrailingConventionWordIsNotAnArea(t *testing.T) {
	names := qualityNames("delete%sObs", qualityWords...)
	names = append(names, qualityNames("update%sObs", qualityWords...)...)
	// "report" also ends nearly every name, but a sentence and a route name it.
	names = append(names, qualityNames("export%sReport", qualityWords...)...)
	v := qualityVocabulary(names, "Your report is ready to download.")
	ev := qualityEvidence(nil, []string{"/reports", "/reports/:id"})
	areas := areaNames(v.features(ev))
	for _, area := range areas {
		if area == "obs" {
			t.Errorf("naming suffix reported as an area: %v", areas)
		}
	}
	found := false
	for _, area := range areas {
		found = found || area == "report"
	}
	if !found {
		t.Errorf("real area that ends names was hidden: %v", areas)
	}
	// Mentioned by text only, the same word is still kept.
	textOnly := qualityVocabulary(qualityNames("export%sReport", qualityWords...), "Your report is ready to download.", "The report could not be sent.")
	if got := areaNames(textOnly.features(nil)); !reflect.DeepEqual(got, []string{"report"}) {
		t.Errorf("text-backed trailing word = %v", got)
	}
}

func TestWordsWithoutVowelsAreNotNouns(t *testing.T) {
	for _, word := range []string{"qzxk", "xsrf", "bcdf"} {
		if domainNoun(word) {
			t.Errorf("%q accepted as a noun", word)
		}
	}
	for _, word := range []string{"invoice", "gym"} {
		if !domainNoun(word) {
			t.Errorf("%q rejected", word)
		}
	}
	v := qualityVocabulary([]string{"slide-top-in_qzxk"})
	if v.nouns["qzxk"] != 0 || v.nouns["slide"] != 1 {
		t.Errorf("nouns = %v", v.nouns)
	}
}

func TestSingularKeepsUnchangedAndIrregularPlurals(t *testing.T) {
	cases := map[string]string{
		"series": "series", "species": "species", "news": "news",
		"categories": "category", "orders": "order", "boxes": "box", "address": "address",
		"indices": "index", "cookies": "cookie", "statuses": "status",
	}
	for word, want := range cases {
		if got := singular(word); got != want {
			t.Errorf("singular(%q) = %q, want %q", word, got, want)
		}
	}
}

func TestPathPrefixWordKeepsOnlyItsScreens(t *testing.T) {
	var api []string
	for _, thing := range []string{"invoice", "voucher", "refund", "payout", "ledger", "budget", "tariff", "rebate", "coupon", "basket", "parcel", "tenant"} {
		api = append(api, "/hub/studio_api/"+thing+"/create")
	}
	names := qualityNames("open%sStudio", qualityWords[:6]...)
	names = append(names, qualityNames("studio%sPanel", qualityWords[:6]...)...)
	v := qualityVocabulary(names, "The studio is ready for you.")
	features := v.features(qualityEvidence(api, []string{"/studio/home"}))
	studio, ok := featureByAreaList(features, "studio")
	if !ok || studio.APICalls != 0 || studio.Screens != 1 {
		t.Errorf("prefix word kept its API calls: %+v", features)
	}
	// With few paths nothing says the word is a prefix.
	few := qualityEvidence(api[:4], nil)
	features = qualityVocabulary(names, "The studio is ready for you.").features(few)
	if studio, ok := featureByAreaList(features, "studio"); !ok || studio.APICalls != 4 {
		t.Errorf("short path list lost evidence: %+v", features)
	}
}

func featureByAreaList(features []Feature, area string) (Feature, bool) {
	for _, feature := range features {
		if feature.Area == area {
			return feature, true
		}
	}
	return Feature{}, false
}

func TestOverlappingAreasKeepTheBetterSupported(t *testing.T) {
	names := qualityNames("promo%sCard", qualityWords[:6]...)
	names = append(names, qualityNames("promotion%sRow", qualityWords[:6]...)...)
	sentences := []string{"Your promo was saved.", "The promotion has ended."}
	shared := []string{"/v1/promo/promotion/list", "/v1/promo/promotion/create", "/v1/promotion/promo/edit", "/v1/promotion/promo/delete"}
	v := qualityVocabulary(names, sentences...)
	areas := areaNames(v.features(qualityEvidence(shared, nil)))
	if len(areas) != 1 {
		t.Errorf("areas that share their evidence were both listed: %v", areas)
	}
	separate := []string{"/v1/promo/list", "/v1/promo/create", "/v1/promotion/edit", "/v1/promotion/delete"}
	areas = areaNames(qualityVocabulary(names, sentences...).features(qualityEvidence(separate, nil)))
	if len(areas) != 2 {
		t.Errorf("areas with their own evidence were merged: %v", areas)
	}
}

func TestNameOnlyMentionNeedsMoreWhereEvidenceIsRich(t *testing.T) {
	var api []string
	for i := 0; i < minRichEvidence; i++ {
		api = append(api, fmt.Sprintf("/v1/order/item%d/create", i))
	}
	names := qualityNames("open%sGadget", qualityWords[:8]...)
	const sentence = "Your gadget is ready today."
	if got := areaNames(qualityVocabulary(names, sentence).features(qualityEvidence(api[:3], nil))); !reflect.DeepEqual(got, []string{"gadget"}) {
		t.Errorf("text-backed area in a small file = %v", got)
	}
	for _, area := range areaNames(qualityVocabulary(names, sentence).features(qualityEvidence(api, nil))) {
		if area == "gadget" {
			t.Errorf("area backed by one sentence listed in an evidence-rich file")
		}
	}
}

func TestAPIExamplesPreferTheAreaAndAnAction(t *testing.T) {
	items := []evidenceItem{
		{label: "/orders/list"},
		{label: "/bind", verbs: []string{"link"}},
		{label: "/x/y/z/orders/delete", verbs: []string{"delete"}},
		{label: "/orders/create", verbs: []string{"create"}},
		{label: "/orders/zzzzzz"},
	}
	want := []string{"/orders/create", "/x/y/z/orders/delete", "/orders/list"}
	if got := apiExamples(items, "order"); !reflect.DeepEqual(got, want) {
		t.Errorf("examples = %v, want %v", got, want)
	}
}

func TestPathSegmentsNameAreasOnlyWhenNamesGiveNone(t *testing.T) {
	screens := []string{"/reports", "/reports/:id", "/reports/new", "/orders/:id", "/orders/new"}
	ev := qualityEvidence(nil, screens)
	features := newVocabulary().features(ev)
	if len(features) != 1 || features[0].Area != "report" || features[0].Screens != 3 || features[0].Names != 0 {
		t.Errorf("path fallback = %+v", features)
	}
	// Names that give an area win; the paths do not add to them.
	names := qualityNames("open%sInvoice", qualityWords[:8]...)
	v := qualityVocabulary(names, "Your invoice was sent.")
	if got := areaNames(v.features(ev)); !reflect.DeepEqual(got, []string{"invoice"}) {
		t.Errorf("areas = %v", got)
	}
	// One root shared by every API path is a service, not an area.
	root := qualityEvidence([]string{"/svc/v1/a/list", "/svc/v1/b/list", "/svc/v1/c/list"}, nil)
	if got := newVocabulary().features(root); got != nil {
		t.Errorf("service root reported: %+v", got)
	}
	if got := newVocabulary().features(qualityEvidence(nil, screens[:2])); got != nil {
		t.Errorf("two screens reported: %+v", got)
	}
}
