package main

import (
	"runtime/debug"
	"testing"
)

func TestResolveVersion(t *testing.T) {
	build := func(v string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: "github.com/BehiSecc/JSHound", Version: v}}
	}
	tests := []struct {
		name   string
		linked string
		info   *debug.BuildInfo
		want   string
	}{
		{"link-time version wins", "v9.9.9", build("v1.2.3"), "v9.9.9"},
		{"release tag", "", build("v1.2.3"), "v1.2.3"},
		{"untagged commit", "", build("v0.0.0-20261113104841-3a94441a0fa7"), "v0.0.0-20261113104841-3a94441a0fa7"},
		{"uncommitted changes", "", build("v1.2.4-0.20261113104841-3a94441a0fa7+dirty"), "v1.2.4-0.20261113104841-3a94441a0fa7+dirty"},
		{"no VCS information", "", build("(devel)"), "dev"},
		{"empty module version", "", build(""), "dev"},
		{"no build info", "", nil, "dev"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resolveVersion(tt.linked, tt.info); got != tt.want {
				t.Fatalf("resolveVersion(%q) = %q, want %q", tt.linked, got, tt.want)
			}
		})
	}
}
