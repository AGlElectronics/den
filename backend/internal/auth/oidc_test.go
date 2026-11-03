package auth

import "testing"

func TestMergeUnique(t *testing.T) {
	got := mergeUnique([]string{"den-admin", "member", ""}, []string{"member", "events-admin"})
	want := []string{"den-admin", "member", "events-admin"}
	if len(got) != len(want) {
		t.Fatalf("len=%d want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestDisplayNameFromClaims(t *testing.T) {
	c := &OIDCClaims{Subject: "sub-1", PreferredUsername: "alice", Email: "a@x"}
	if got := displayNameFromClaims(c); got != "alice" {
		t.Fatalf("got %q", got)
	}
	c = &OIDCClaims{Subject: "sub-2"}
	if got := displayNameFromClaims(c); got != "sub-2" {
		t.Fatalf("got %q", got)
	}
}
