package auth

import (
	"strings"
	"testing"
)

func TestHashPasswordPrefixAndUniqueSalt(t *testing.T) {
	const pw = "correct horse battery staple"

	a, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	b, err := HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	if !strings.HasPrefix(a, argonPrefix) {
		t.Fatalf("hash missing argon2id PHC prefix: %q", a)
	}
	if !strings.HasPrefix(b, argonPrefix) {
		t.Fatalf("hash missing argon2id PHC prefix: %q", b)
	}
	if a == b {
		t.Fatalf("two hashes of same input produced identical output (salt not random)")
	}

	for _, h := range []string{a, b} {
		ok, err := VerifyPassword(pw, h)
		if err != nil {
			t.Fatalf("VerifyPassword: %v", err)
		}
		if !ok {
			t.Fatalf("VerifyPassword returned false for matching password (hash=%q)", h)
		}
	}
}

func TestVerifyPasswordRejectsNearMiss(t *testing.T) {
	h, err := HashPassword("hunter2")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	ok, err := VerifyPassword("hunter3", h)
	if err != nil {
		t.Fatalf("VerifyPassword on mismatch returned error: %v", err)
	}
	if ok {
		t.Fatalf("VerifyPassword accepted a near-miss password")
	}
}

func TestVerifyPasswordRejectsEmptyAsNonMatch(t *testing.T) {
	h, err := HashPassword("hunter2")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	ok, err := VerifyPassword("", h)
	if err != nil {
		t.Fatalf("VerifyPassword on empty input returned error: %v", err)
	}
	if ok {
		t.Fatalf("VerifyPassword accepted empty password against a real hash")
	}
}

func TestVerifyPasswordRejectsInvalidEncoding(t *testing.T) {
	cases := []string{
		"",
		"not-a-hash",
		"$argon2id$v=19$m=65536,t=3,p=4$",
		"$argon2id$v=19$m=65536,t=3,p=4$onlyonepart",
		"$argon2id$v=19$m=65536,t=3,p=4$!!notbase64!!$hash",
		"$argon2id$v=19$m=65536,t=3,p=4$c2FsdA$!!notbase64!!",
		"$argon2i$v=19$m=65536,t=3,p=4$c2FsdA$aGFzaA",
	}
	for _, c := range cases {
		ok, err := VerifyPassword("x", c)
		if err == nil {
			t.Errorf("VerifyPassword(%q) returned no error; want ErrInvalidEncoding", c)
		}
		if ok {
			t.Errorf("VerifyPassword(%q) returned ok=true on malformed input", c)
		}
	}
}
