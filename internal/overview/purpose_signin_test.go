package overview

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// signInFixture builds names and API paths for sign-in areas and product
// areas. Each sign-in area has actions and paths about getting into an
// account; each product area has paths about its own objects.
func signInFixture(signIn, product []string) (names, api []string) {
	for _, noun := range signIn {
		title := strings.ToUpper(noun[:1]) + noun[1:]
		names = append(names, qualityNames("verify"+title+"%s", qualityWords[:3]...)...)
		names = append(names, qualityNames("register"+title+"%s", qualityWords[:3]...)...)
		names = append(names, qualityNames("reset"+title+"%s", qualityWords[:3]...)...)
		for _, action := range []string{"login", "verify", "register", "reset"} {
			api = append(api, fmt.Sprintf("/v1/%s/%s", noun, action))
		}
	}
	for _, noun := range product {
		title := strings.ToUpper(noun[:1]) + noun[1:]
		names = append(names, qualityNames("create"+title+"%s", qualityWords[:3]...)...)
		names = append(names, qualityNames("delete"+title+"%s", qualityWords[:3]...)...)
		names = append(names, qualityNames("archive"+title+"%s", qualityWords[:3]...)...)
		for _, action := range []string{"list", "create", "delete", "archive"} {
			api = append(api, fmt.Sprintf("/v1/%s/%s", noun, action))
		}
	}
	return names, api
}

var signInProducts = []string{"invoice", "customer", "shipment", "warehouse", "supplier", "coupon", "review"}

func TestSignInAreasFoldIntoOneAndProductAreasFillTheSlots(t *testing.T) {
	names, api := signInFixture([]string{"email", "mobile", "password"}, signInProducts)
	// One call names two sign-in areas: it counts once in the folded area.
	api = append(api, "/v1/auth/email/mobile/login")
	features := qualityVocabulary(names).features(qualityEvidence(api, nil))
	if len(features) != maxFeatures {
		t.Fatalf("features = %v, want %d", areaNames(features), maxFeatures)
	}
	fold, ok := featureByAreaList(features, "sign-in")
	if !ok {
		t.Fatalf("no sign-in area: %v", areaNames(features))
	}
	if len(fold.Folded) != 3 {
		t.Errorf("folded = %v", fold.Folded)
	}
	for _, word := range []string{"email", "mobile", "password"} {
		if _, ok := featureByAreaList(features, word); ok {
			t.Errorf("%s stayed a separate area", word)
		}
	}
	if fold.APICalls != 13 {
		t.Errorf("sign-in API calls = %d, want 13 distinct paths", fold.APICalls)
	}
	if fold.Names != 27 {
		t.Errorf("sign-in names = %d, want 27", fold.Names)
	}
	if len(fold.APIExamples) > 3 {
		t.Errorf("examples = %v", fold.APIExamples)
	}
	products := 0
	for _, area := range areaNames(features) {
		for _, noun := range signInProducts {
			if area == noun {
				products++
			}
		}
	}
	if products != maxFeatures-1 {
		t.Errorf("product areas = %d in %v, want %d", products, areaNames(features), maxFeatures-1)
	}
}

func TestProductAreaNamedEmailIsNotFolded(t *testing.T) {
	var names, api []string
	for _, action := range []string{"send", "archive", "delete", "compose"} {
		names = append(names, qualityNames(action+"Email%s", qualityWords[:4]...)...)
		api = append(api, "/v1/email/"+action)
	}
	for _, action := range []string{"register", "wipe", "locate"} {
		names = append(names, qualityNames(action+"Mobile%s", qualityWords[:4]...)...)
		api = append(api, "/v1/mobile/"+action)
	}
	features := qualityVocabulary(names).features(qualityEvidence(api, nil))
	for _, feature := range features {
		if feature.Area == "sign-in" || len(feature.Folded) > 0 {
			t.Errorf("a product area was folded: %+v", feature)
		}
	}
	if _, ok := featureByAreaList(features, "email"); !ok {
		t.Errorf("email area missing: %v", areaNames(features))
	}
	if _, ok := featureByAreaList(features, "mobile"); !ok {
		t.Errorf("mobile area missing: %v", areaNames(features))
	}
}

func TestSingleSignInAreaIsStillOneSignInEntry(t *testing.T) {
	names, api := signInFixture([]string{"email"}, signInProducts[:3])
	features := qualityVocabulary(names).features(qualityEvidence(api, nil))
	fold, ok := featureByAreaList(features, "sign-in")
	if !ok || !reflect.DeepEqual(fold.Folded, []string{"email"}) {
		t.Fatalf("features = %+v", features)
	}
	if fold.label() != "sign-in (email)" {
		t.Errorf("label = %q", fold.label())
	}
}

func TestSignInNeedsBothActionsAndPaths(t *testing.T) {
	access := map[string]int{"sign in": 6, "verify": 4}
	login := evidenceItem{verbs: []string{"sign in"}}
	plain := evidenceItem{}
	if !signInArea(access, []evidenceItem{login, login, plain}) {
		t.Error("access actions and paths were not read as sign-in")
	}
	if signInArea(access, []evidenceItem{plain, plain, plain}) {
		t.Error("access actions with product paths were read as sign-in")
	}
	if signInArea(map[string]int{"send": 8, "verify": 2}, []evidenceItem{login, login, login}) {
		t.Error("product actions with sign-in paths were read as sign-in")
	}
	// Without API calls the actions alone must be nearly all access actions.
	if !signInArea(map[string]int{"sign in": 12, "send": 1}, nil) {
		t.Error("many access actions without paths were not read as sign-in")
	}
	if signInArea(map[string]int{"sign in": 8, "send": 2}, nil) {
		t.Error("few actions without paths were read as sign-in")
	}
}

func TestFoldedLabelNamesAFewAreas(t *testing.T) {
	feature := Feature{Area: "sign-in", Folded: []string{"email", "mobile", "password", "auth", "sms"}}
	if got := feature.label(); got != "sign-in (email, mobile, password, +2 more)" {
		t.Errorf("label = %q", got)
	}
	out := &strings.Builder{}
	writeFeatures(out, []Feature{{Area: "sign-in", Folded: []string{"email"}, Actions: []string{"sign in"}, APICalls: 4}, {Area: "invoice"}})
	want := "- sign-in (email): sign in\n    4 API calls\n- invoice\n"
	if out.String() != want {
		t.Errorf("report = %q, want %q", out.String(), want)
	}
}

func TestFoldedFieldIsAdditiveInJSON(t *testing.T) {
	data, err := json.Marshal([]Feature{{Area: "sign-in", Names: 9, Folded: []string{"email", "mobile"}}, {Area: "invoice", Names: 7}})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), `[{"area":"sign-in","names":9,"folded":["email","mobile"]},{"area":"invoice","names":7}]`; got != want {
		t.Errorf("json = %s, want %s", got, want)
	}
}

func TestSignInFoldsAcrossSeveralFiles(t *testing.T) {
	names, api := signInFixture([]string{"email", "mobile"}, signInProducts[:5])
	quote := func(words []string) string { return `"` + strings.Join(words, `","`) + `"` }
	half := len(names) / 2
	a := `import "./b.js";var n=[` + quote(names[:half]) + `];var u=[` + quote(api) + `];`
	b := `var n=[` + quote(names[half:]) + `];`
	result := Analyze([]Source{src("d/a.js", a), src("d/b.js", b)})
	if result.Overall == nil {
		t.Fatal("no overall")
	}
	fold, ok := featureByAreaList(result.Overall.Features, "sign-in")
	if !ok {
		t.Fatalf("no sign-in area: %v", featureAreas(result.Overall.Features))
	}
	if len(fold.Folded) != 2 || fold.APICalls != 8 {
		t.Errorf("sign-in = %+v", fold)
	}
	for _, feature := range result.Overall.Features {
		if feature.Area == "email" || feature.Area == "mobile" {
			t.Errorf("%s stayed a separate area", feature.Area)
		}
	}
}
