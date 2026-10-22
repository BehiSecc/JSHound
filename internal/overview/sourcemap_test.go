package overview

import (
	"encoding/json"
	"strings"
	"testing"
)

func encodeVLQ(values ...int) string {
	const digits = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	var out strings.Builder
	for _, value := range values {
		v := value << 1
		if value < 0 {
			v = (-value << 1) | 1
		}
		for {
			digit := v & 31
			v >>= 5
			if v > 0 {
				digit |= 32
			}
			out.WriteByte(digits[digit])
			if v == 0 {
				break
			}
		}
	}
	return out.String()
}

func TestDecodeVLQ(t *testing.T) {
	values, ok := decodeVLQ(encodeVLQ(0, -1, 16, 1000, -2048))
	if !ok || len(values) != 5 || values[1] != -1 || values[3] != 1000 || values[4] != -2048 {
		t.Fatalf("decoded %v ok=%v", values, ok)
	}
	if _, ok := decodeVLQ("g"); ok {
		t.Fatal("truncated VLQ accepted")
	}
}

func TestSourceMapRestoresNamesAndOrigins(t *testing.T) {
	// Columns:  0         1         2         3
	//           0123456789012345678901234567890123456
	code := "function a(){b()}function b(){}a();"
	mappings := encodeVLQ(9, 0, 4, 0, 0) + "," + encodeVLQ(17, 1, 1, 0, 1)
	data, _ := json.Marshal(map[string]any{
		"version": 3, "sources": []string{"webpack://app/./src/main.ts", "webpack://app/./src/net/load.ts"},
		"names": []string{"startApp", "loadUsers"}, "mappings": mappings,
	})
	file := Analyze([]Source{{Name: "app.min.js", Data: []byte(code), Map: data, MapName: "app.min.js.map"}}).Files[0]
	units := make(map[string]Unit)
	for _, unit := range file.Units {
		units[unit.Name] = unit
	}
	if units["a"].OriginalName != "startApp" || units["a"].Origin != "src/main.ts:5" ||
		units["b"].OriginalName != "loadUsers" || units["b"].Origin != "src/net/load.ts:6" {
		t.Fatalf("units: %#v", file.Units)
	}
	if !hasLink(file.Links, Link{From: "startApp()", To: "loadUsers()", Line: 1}) || file.SourceMapUsed != "app.min.js.map" {
		t.Fatalf("links: %#v", file.Links)
	}
	if file.Modules == nil || file.Modules.Total != 2 || len(file.Modules.Areas) != 2 {
		t.Fatalf("modules: %#v", file.Modules)
	}
}

func TestSourceMapUsesUTF16Columns(t *testing.T) {
	code := "var s=\"é😀\";function a(){}a();"
	// "é" is one UTF-16 unit, the emoji two, so "a" sits at UTF-16 column 21.
	data, _ := json.Marshal(map[string]any{"version": 3, "sources": []string{"src/x.ts"}, "names": []string{"realName"}, "mappings": encodeVLQ(21, 0, 0, 0, 0)})
	file := Analyze([]Source{{Name: "x.js", Data: []byte(code), Map: data, MapName: "inline source map"}}).Files[0]
	if len(file.Units) != 1 || file.Units[0].OriginalName != "realName" {
		t.Fatalf("units: %#v", file.Units)
	}
}

func TestIndexedSourceMapSections(t *testing.T) {
	section := func(name string) json.RawMessage {
		data, _ := json.Marshal(map[string]any{"version": 3, "sources": []string{"src/" + name + ".ts"}, "names": []string{name}, "mappings": encodeVLQ(9, 0, 0, 0, 0)})
		return data
	}
	data, _ := json.Marshal(map[string]any{"version": 3, "sections": []map[string]any{
		{"offset": map[string]int{"line": 0, "column": 0}, "map": section("first")},
		{"offset": map[string]int{"line": 1, "column": 0}, "map": section("second")},
	}})
	file := Analyze([]Source{{Name: "x.js", Data: []byte("function a(){}\nfunction b(){}\na();b();"), Map: data, MapName: "x.js.map"}}).Files[0]
	names := map[string]string{}
	for _, unit := range file.Units {
		names[unit.Name] = unit.OriginalName
	}
	if names["a"] != "first" || names["b"] != "second" {
		t.Fatalf("units: %#v", file.Units)
	}
}
