package http

import (
	"testing"

	"github.com/google/uuid"
)

func TestDeriveCredential(t *testing.T) {
	key := []byte("0123456789abcdef0123456789abcdef")
	id := uuid.MustParse("6f1d2a40-0000-4000-8000-000000000001")
	a, b := deriveCredential(key, id), deriveCredential(key, id)
	if a != b || len(a) != 43 {
		t.Fatalf("derived credential not stable or wrong length: %q %q", a, b)
	}
	if deriveCredential([]byte("another-key-another-key-another-k"), id) == a {
		t.Fatal("different keys must give different credentials")
	}
	if deriveCredential(key, uuid.New()) == a {
		t.Fatal("different ids must give different credentials")
	}

	h := &pairingHandler{}
	r1, _ := h.newCredential(id)
	r2, _ := h.newCredential(id)
	if r1 == r2 {
		t.Fatal("without a key credentials must be random")
	}
}
