package auth

import (
	"testing"

	"github.com/AGlElectronics/den/backend/internal/groupmatch"
)

func TestInAnyGroup(t *testing.T) {
	cases := []struct {
		name   string
		rule   groupmatch.Config
		groups []string
		want   []string
		ok     bool
	}{
		{"plain", groupmatch.Config{}, []string{"den-admin"}, []string{"den-admin"}, true},
		{"full path form", groupmatch.Config{}, []string{"/den-admin"}, []string{"den-admin"}, true},
		{"case", groupmatch.Config{}, []string{"Den-Admin"}, []string{"den-admin"}, true},
		{"other groups", groupmatch.Config{}, []string{"stash-admin", "hr-admin"}, []string{"den-admin"}, false},
		{"none", groupmatch.Config{}, nil, []string{"den-admin"}, false},
		{"full needs whole path", groupmatch.Config{}, []string{"/teams/den-admin"}, []string{"den-admin"}, false},
		{"last segment", groupmatch.Config{Mode: groupmatch.ModeLast}, []string{"/teams/den-admin"}, []string{"den-admin"}, true},
		{"prefix", groupmatch.Config{Prefix: "/con2026"}, []string{"/con2026/den-admin"}, []string{"den-admin"}, true},
		{"other year", groupmatch.Config{Prefix: "/con2026"}, []string{"/con2025/den-admin"}, []string{"den-admin"}, false},
	}
	for _, c := range cases {
		if got := InAnyGroup(c.rule, c.groups, c.want); got != c.ok {
			t.Errorf("%s: InAnyGroup(%v, %v) = %v, want %v", c.name, c.groups, c.want, got, c.ok)
		}
	}
}
