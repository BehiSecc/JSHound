package overview

import (
	"reflect"
	"strings"
	"testing"
)

const basketTable = `var base="/gateway",links="/ui";
var T={SEND_BASKET_LABEL:base+"/basket/send_label/",CANCEL_BASKET:base+"/basket/handle/",TRACK_BASKET:base+"/basket/track/",VERIFY_ADDRESS:"/address/verify/"};
var names=["basketTable","basket-list","basket_row","basketCard","basketMenu","basketForm","basketTab","basket-view"];
var text=["Your basket was sent.","The basket could not be saved."];`

func TestEndpointTableCountsAsAPICallsWithKeyWords(t *testing.T) {
	tokens := lex(basketTable)
	ev := collectEvidence(&File{}, tokens, nil)
	if total := ev.totals(); total == nil || total.APICalls != 4 || total.Screens != 0 {
		t.Fatalf("totals = %+v, items = %+v", total, ev.items)
	}
	cancel := ev.items[1]
	if cancel.label != "/basket/handle" || !reflect.DeepEqual(cancel.verbs, []string{"cancel"}) || !reflect.DeepEqual(cancel.nouns, []string{"basket"}) {
		t.Errorf("key words are not evidence: %+v", cancel)
	}
	// The paths and the keys are API evidence, so they are not names as well.
	vocab := collectVocabulary(&File{}, tokens, nil, ev)
	if _, ok := vocab.seen["SEND_BASKET_LABEL"]; ok {
		t.Errorf("table key read as a name: %v", vocab.nouns)
	}
	if vocab.nouns["basket"] != 8 {
		t.Errorf("basket names = %d, want the 8 listed names", vocab.nouns["basket"])
	}
	// Without evidence the same tokens are names.
	if without := collectVocabulary(&File{}, tokens, nil, nil); without.nouns["basket"] <= vocab.nouns["basket"] {
		t.Errorf("names without evidence = %d", without.nouns["basket"])
	}
}

func TestEndpointTableAttachesToArea(t *testing.T) {
	file := Analyze([]Source{{Name: "baskets.js", Data: []byte(basketTable)}}).Files[0]
	basket, ok := featureByArea(file.Purpose, "basket")
	if !ok {
		t.Fatalf("basket area missing: %+v", file.Purpose.Features)
	}
	if basket.APICalls != 3 || !strings.Contains(strings.Join(basket.Actions, ","), "cancel") {
		t.Errorf("basket = %+v", basket)
	}
	if totals := file.Purpose.Evidence; totals == nil || totals.APICalls != 4 {
		t.Errorf("totals = %+v", totals)
	}
}

func TestRouteConfigurationStaysScreens(t *testing.T) {
	ev := collectEvidence(&File{}, lex(`var r=[{path:"/baskets",redirect:"/baskets/list",alias:"/shipments/all"},{path:"/baskets/:id",component:X},{path:"/baskets/:id/edit",component:Y}];`), nil)
	total := ev.totals()
	if total == nil || total.APICalls != 0 || total.Screens != 4 {
		t.Fatalf("totals = %+v, items = %+v", total, ev.items)
	}
	// A configuration with several path strings and a "path" property is a route
	// declaration, never an endpoint table.
	ev = collectEvidence(&File{}, lex(`var r={path:"/a/b/c",redirect:"/d/e/f",alias:"/g/h/i",also:"/j/k/l"};`), nil)
	if ev.totals() != nil {
		t.Errorf("route declaration read as an endpoint table: %+v", ev.items)
	}
}

func TestTablesOfLinksAndFilesAreNotAPICalls(t *testing.T) {
	for name, source := range map[string]string{
		"links":     `var L={home:"/home",about:"/about",contact:"/contact",help:"/help"};`,
		"few":       `var L={SEND:"/basket/send/",CANCEL:"/basket/cancel/"};`,
		"mixed":     `var L={a:"/basket/send/",b:"/basket/cancel/",c:"/basket/track/",d:1,e:2,f:fn(),g:"x"};`,
		"files":     `var L={a:"/static/a.png",b:"/static/b.css",c:"/static/c.js"};`,
		"urls":      `var L={a:"//cdn.test/a/b",b:"//cdn.test/c/d",c:"//cdn.test/e/f"};`,
		"machine":   `var L={a:"/usr/bin/node",b:"/etc/hosts",c:"/Applications/Edge.app/Contents"};`,
		"numbers":   `var L={"1.0":"/8","1.2":"/1","1.3":"/3"};`,
		"screens":   `var L={a:"/basket/:id",b:"/basket/:id/edit",c:"/box/:id"};`,
		"methods":   `var L={a:"/basket/send/",b:"/basket/cancel/",c:"/basket/track/",d(){}};`,
		"spread":    `var L={...base,a:"/basket/send/",b:"/basket/cancel/",c:"/basket/track/"};`,
		"templates": "var L={a:`/basket/${x}/send`,b:`/basket/${x}/cancel`,c:`/basket/${x}/track`};",
	} {
		ev := collectEvidence(&File{}, lex(source), nil)
		for _, item := range ev.items {
			if !item.screen {
				t.Errorf("%s: table counted as API calls: %+v", name, ev.items)
				break
			}
		}
		if ev.totals() != nil && ev.totals().APICalls != 0 {
			t.Errorf("%s: totals = %+v", name, ev.totals())
		}
	}
}

func TestSingleSegmentTableNeedsConstantKeys(t *testing.T) {
	constants := collectEvidence(&File{}, lex(`var T={SEND_BASKET:"/send_basket/",CANCEL_BASKET:"/cancel_basket/",TRACK_BASKET:"/track_basket/"};`), nil)
	if total := constants.totals(); total == nil || total.APICalls != 3 {
		t.Errorf("constants: %+v", constants.items)
	}
	words := collectEvidence(&File{}, lex(`var T={sendBasket:"/send_basket/",cancelBasket:"/cancel_basket/",trackBasket:"/track_basket/"};`), nil)
	if words.totals() != nil {
		t.Errorf("single-segment camelCase table counted: %+v", words.items)
	}
	namespaced := collectEvidence(&File{}, lex(`var T={sendBasket:"/basket/send/",cancelBasket:"/basket/cancel/",trackBasket:"/basket/track/"};`), nil)
	if total := namespaced.totals(); total == nil || total.APICalls != 3 {
		t.Errorf("namespaced: %+v", namespaced.items)
	}
}

func TestTablePathIsNotBothScreenAndAPICall(t *testing.T) {
	// "/baskets/new" shares its first segment with a declared screen, so the
	// sibling rule makes it a screen and the table does not claim it again.
	ev := collectEvidence(&File{}, lex(`var r=["/baskets/:id"];var T={A:"/baskets/new/",B:"/baskets/old/",C:"/boxes/open/",D:"/boxes/shut/"};`), nil)
	screens, api := map[string]bool{}, map[string]bool{}
	for _, item := range ev.items {
		if item.screen {
			screens[item.key] = true
		} else {
			api[item.key] = true
		}
	}
	for key := range screens {
		if api[key] {
			t.Errorf("%q is a screen and an API call", key)
		}
	}
	if total := ev.totals(); total == nil || total.Screens != 3 || total.APICalls != 2 {
		t.Errorf("totals = %+v, items = %+v", total, ev.items)
	}
}

func TestTableOfBundledPackageIsSkipped(t *testing.T) {
	ev := collectEvidence(&File{}, lex(`var T={A:"/basket/send/",B:"/basket/cancel/",C:"/basket/track/"};`), func(int) bool { return true })
	if ev.totals() != nil {
		t.Errorf("package table counted: %+v", ev.items)
	}
}
