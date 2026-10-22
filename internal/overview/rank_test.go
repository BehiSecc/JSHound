package overview

import (
	"fmt"
	"strings"
	"testing"
)

func TestRankingFavorsCentralDispatcherOverHelperChains(t *testing.T) {
	var code strings.Builder
	// A startup chain of tiny helpers declared first...
	code.WriteString("function boot(){ envA(); envB(); }\nfunction envA(){ flag(); }\nfunction envB(){ flag(); }\nfunction flag(){}\n")
	// ...and a dispatcher reached from an input event, declared later.
	code.WriteString("function handle(req){ switch(req.method){ case 'a': return opA(); case 'b': return opB(); case 'c': return opC(); case 'd': return opD(); } }\n")
	for _, name := range []string{"opA", "opB", "opC", "opD"} {
		fmt.Fprintf(&code, "function %s(){ store(); }\n", name)
	}
	code.WriteString("function store(){}\nfor (let i = 0; i < 20; i++) {}\n")
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&code, "function unused%d(){}\n", i)
	}
	code.WriteString("boot();\ninput.on('line', handle);\n")
	file := Analyze([]Source{{Name: "worker.js", Data: []byte(code.String())}}).Files[0]
	if len(file.Units) == 0 || file.Units[0].Name != "handle" {
		t.Fatalf("first unit: %#v", file.Units)
	}
	for _, expected := range []string{`["input line event"] --> `, `["handle()"] --> `, `["module load"] --> `} {
		if !strings.Contains(file.Diagram, expected) {
			t.Errorf("diagram misses %q:\n%s", expected, file.Diagram)
		}
	}
	if file.Links[0].From != "module load" && file.Links[0].From != "input line event" {
		t.Fatalf("links should start at an entry point: %#v", file.Links[:3])
	}
}
