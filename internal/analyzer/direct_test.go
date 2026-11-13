package analyzer

import (
	"math/rand"
	"regexp"
	"strings"
	"testing"
)

// directPathReference is the regular expression directPaths replaces.
var directPathReference = regexp.MustCompile(`(?i)["']((?:/|\./|\.\./)[^"'<>\\\s]{2,1000}[^"'<>\\\s]{0,1000}[^"'<>\\\s]{0,48})["']`)

func TestDirectPathsMatchTheExpressionTheyReplace(t *testing.T) {
	check := func(source string) {
		t.Helper()
		var want []directMatch
		for _, m := range directPathReference.FindAllStringSubmatchIndex(source, -1) {
			want = append(want, directMatch{open: m[0], start: m[2], end: m[3]})
		}
		got := directPaths(source)
		if len(got) != len(want) {
			t.Fatalf("%q: got %v want %v", source, got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("%q: got %v want %v", source, got, want)
			}
		}
	}
	for _, source := range []string{
		``, `"/ab"`, `"/a"`, `'./ab'`, `"../ab"`, `"..//a"`, `"/ab' '/cd"`, `"/ab""/cd"`, `x"/a b"`, `"/a\b"`, `"/a<b"`,
		"\"/a\vb\"", "\"/a\fb\"", "\"/é/ü\"", "\"/\xff\xfe\"", `"//host/x"`, `"/api/v1/users" + '/api/v2'`,
		`"` + "/" + strings.Repeat("a", 2048) + `"`, `"` + "/" + strings.Repeat("a", 2049) + `"`, `"` + "/" + strings.Repeat("é", 1500) + `"`,
	} {
		check(source)
	}
	alphabet := []string{`"`, `'`, `/`, `.`, `a`, `b`, ` `, "\n", "\v", `<`, `\`, `é`, "\xff"}
	random := rand.New(rand.NewSource(1))
	for n := 0; n < 4000; n++ {
		var b strings.Builder
		for i := random.Intn(24); i > 0; i-- {
			b.WriteString(alphabet[random.Intn(len(alphabet))])
		}
		check(b.String())
	}
}
