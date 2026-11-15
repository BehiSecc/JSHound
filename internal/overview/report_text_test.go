package overview

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

func TestTextOutputCarriesNoControlCharacters(t *testing.T) {
	code := "fetch(\"/api/\x1b]0;owned\x07\");var t=\"\u009b2J\";" + `ReactDOM.createRoot(document.getElementById("root")).render(App);`
	result := Analyze([]Source{{Name: "app\x1b[2J.js", Data: []byte(code)}})
	style := regexp.MustCompile("\x1b\\[[0-9;]*m")
	for _, opts := range []TextOptions{{}, {Verbose: true}, {Terminal: true, Color: true}, {Terminal: true, Color: true, Verbose: true}} {
		var out bytes.Buffer
		if err := WriteText(&out, result, opts); err != nil {
			t.Fatal(err)
		}
		text := out.String()
		if opts.Color {
			// The writer's own styling is the only escape sequence allowed.
			text = style.ReplaceAllString(text, "")
		}
		if strings.ContainsFunc(text, func(r rune) bool { return r != '\n' && r != '\t' && isControl(r) }) {
			t.Errorf("%+v: control character in output: %q", opts, text)
		}
	}
}
