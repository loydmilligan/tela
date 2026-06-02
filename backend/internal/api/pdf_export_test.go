package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"testing"
	"time"
)

func TestPrintToken(t *testing.T) {
	s := &Server{shareSecret: []byte("unit-test-share-secret-aaaaaaaaaa")}

	t.Run("round trip", func(t *testing.T) {
		tok := s.mintPrintToken(42)
		id, ok := s.verifyPrintToken(tok)
		if !ok || id != 42 {
			t.Fatalf("verify = (%d,%v), want (42,true)", id, ok)
		}
	})

	t.Run("tampered signature rejected", func(t *testing.T) {
		tok := s.mintPrintToken(7)
		if _, ok := s.verifyPrintToken(tok + "x"); ok {
			t.Fatal("tampered token accepted")
		}
	})

	t.Run("wrong secret rejected", func(t *testing.T) {
		tok := s.mintPrintToken(7)
		other := &Server{shareSecret: []byte("a-completely-different-secret-key")}
		if _, ok := other.verifyPrintToken(tok); ok {
			t.Fatal("token verified under a different secret")
		}
	})

	t.Run("garbage rejected", func(t *testing.T) {
		for _, bad := range []string{"", "nope", "a.b.c", "not.atoken", "...."} {
			if _, ok := s.verifyPrintToken(bad); ok {
				t.Fatalf("garbage %q accepted", bad)
			}
		}
	})

	t.Run("expired rejected", func(t *testing.T) {
		// Hand-craft a correctly-signed but past-expiry token.
		payload := fmt.Sprintf("%d.%d", 99, time.Now().Add(-time.Minute).Unix())
		mac := hmac.New(sha256.New, s.printTokenKey())
		mac.Write([]byte(payload))
		expired := base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
			base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
		if _, ok := s.verifyPrintToken(expired); ok {
			t.Fatal("expired token accepted")
		}
	})
}
