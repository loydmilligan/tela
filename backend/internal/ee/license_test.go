package ee

import (
	"crypto/ed25519"
	"crypto/rand"
	"strings"
	"testing"
	"time"
)

func testKeys(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func TestSignVerifyRoundTrip(t *testing.T) {
	pub, priv := testKeys(t)
	in := License{
		Customer: "Acme Inc", Tier: "enterprise", Seats: 50,
		Features: map[string]bool{"sso": true, "audit": true},
		IssuedAt: time.Now().Unix(),
	}
	tok, err := Sign(priv, in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(tok, tokenPrefix) {
		t.Fatalf("token missing prefix: %q", tok)
	}
	got, err := verifyWith(pub, tok)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if got.Customer != "Acme Inc" || got.Seats != 50 {
		t.Fatalf("round-trip mismatch: %+v", got)
	}
	if !got.Grants("sso") || !got.Grants("audit") {
		t.Fatal("expected sso+audit grants")
	}
	if got.Grants("scim") {
		t.Fatal("did not expect scim grant")
	}
}

func TestWildcardGrantsEverything(t *testing.T) {
	pub, priv := testKeys(t)
	tok, _ := Sign(priv, License{Tier: "enterprise", Features: map[string]bool{"*": true}})
	l, err := verifyWith(pub, tok)
	if err != nil {
		t.Fatal(err)
	}
	if !l.Grants("sso") || !l.Grants("anything") {
		t.Fatal("wildcard should grant any feature")
	}
}

func TestExpiredKeyRejected(t *testing.T) {
	pub, priv := testKeys(t)
	tok, _ := Sign(priv, License{Tier: "enterprise", ExpiresAt: time.Now().Add(-time.Hour).Unix(), Features: map[string]bool{"sso": true}})
	// verifyWith still returns the parsed license alongside ErrExpired (so
	// expiry-tolerant callers like ParseSigned can read the lid), but the error is
	// set so entitlement callers reject it.
	l, err := verifyWith(pub, tok)
	if err != ErrExpired {
		t.Fatalf("want ErrExpired, got %v", err)
	}
	if l == nil || l.Tier != "enterprise" {
		t.Fatalf("expired key should still parse (for refresh lookup), got %v", l)
	}
	if l.Grants("sso") {
		t.Fatal("an expired license must not Grant")
	}
}

func TestTamperedPayloadRejected(t *testing.T) {
	pub, priv := testKeys(t)
	tok, _ := Sign(priv, License{Tier: "enterprise", Features: map[string]bool{"sso": true}})
	// Flip a character in the payload segment → signature no longer matches.
	body := strings.TrimPrefix(tok, tokenPrefix)
	payload, sig, _ := strings.Cut(body, ".")
	bad := tokenPrefix + payload[:len(payload)-1] + flip(payload[len(payload)-1:]) + "." + sig
	if _, err := verifyWith(pub, bad); err != ErrSignature && err != ErrMalformed {
		t.Fatalf("want signature/malformed error, got %v", err)
	}
}

func TestWrongKeyRejected(t *testing.T) {
	_, priv := testKeys(t)
	otherPub, _ := testKeys(t)
	tok, _ := Sign(priv, License{Tier: "enterprise", Features: map[string]bool{"sso": true}})
	if _, err := verifyWith(otherPub, tok); err != ErrSignature {
		t.Fatalf("want ErrSignature, got %v", err)
	}
}

func TestMalformedRejected(t *testing.T) {
	pub, _ := testKeys(t)
	for _, tok := range []string{"", "nope", "tela_lic_", "tela_lic_only-one-part", "tela_lic_!!!.@@@"} {
		if _, err := verifyWith(pub, tok); err == nil {
			t.Fatalf("expected error for %q", tok)
		}
	}
}

func TestNilGrantsNothing(t *testing.T) {
	var l *License
	if l.Grants("sso") {
		t.Fatal("nil license must grant nothing")
	}
	if l.Status().Valid {
		t.Fatal("nil license status must be invalid")
	}
}

func TestEmbeddedPublicKeyValid(t *testing.T) {
	if got := publicKey(); len(got) != ed25519.PublicKeySize {
		t.Fatalf("embedded public key wrong size: %d", len(got))
	}
}

func flip(s string) string {
	if s == "A" {
		return "B"
	}
	return "A"
}
