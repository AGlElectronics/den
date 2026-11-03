package auth

import "testing"

func TestInAnyGroup(t *testing.T) {
	want := []string{"den-admin"}
	cases := []struct {
		groups []string
		ok     bool
	}{
		{[]string{"den-admin"}, true},
		{[]string{"/den-admin"}, true},
		{[]string{"Den-Admin"}, true},
		{[]string{"stash-admin", "hr-admin"}, false},
		{nil, false},
	}
	for _, c := range cases {
		if got := InAnyGroup(c.groups, want); got != c.ok {
			t.Errorf("InAnyGroup(%v) = %v, want %v", c.groups, got, c.ok)
		}
	}
}
