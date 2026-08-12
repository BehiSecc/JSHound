package wordlist

import (
	"slices"
	"testing"
)

func TestBuild(t *testing.T) {
	got := Build([]string{"/api/v2/users/:userId"}, []string{"https://target.test/admin/login?next=x"}, []string{"assets/config.json"}, []string{"next", "userId"})
	for _, want := range []string{"admin", "api", "config", "login", "next", "userid", "users"} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %q in %#v", want, got)
		}
	}
	if slices.Contains(got, "v2") == false {
		t.Errorf("expected version segment in %#v", got)
	}
}
