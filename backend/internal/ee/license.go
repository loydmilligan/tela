// tela Enterprise Edition — NOT licensed under the AGPL. See ee/LICENSE.md.
// Production use of the features this enables requires a valid license key;
// removing or circumventing the entitlement gate is prohibited.

// Package ee implements the self-host Enterprise entitlement: an offline,
// signed license key that unlocks ee-gated features without a phone-home.
//
// A key is `tela_lic_<b64url(payload)>.<b64url(sig)>` where payload is the
// JSON License and sig is its ed25519 signature under the vendor's offline
// signing key (the matching public key is embedded — keys.go). Verification is
// fully offline, so an air-gapped instance works. A verified *License satisfies
// the api licenser interface via Grants, and api.entitled() consults it.
package ee

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
)

const tokenPrefix = "tela_lic_"

var (
	// ErrMalformed — the token isn't a well-formed tela license key.
	ErrMalformed = errors.New("ee: malformed license key")
	// ErrSignature — the signature doesn't verify under the embedded key.
	ErrSignature = errors.New("ee: license key signature is invalid")
	// ErrExpired — the key verified but its expiry has passed.
	ErrExpired = errors.New("ee: license key has expired")
)

// License is the signed payload of a key: who it's for, what it grants, and when
// it lapses. JSON keys are short to keep tokens compact.
type License struct {
	Customer  string          `json:"cust"`           // human label (company / org)
	Tier      string          `json:"tier"`           // e.g. "enterprise"
	Seats     int             `json:"seats,omitempty"`// max seats; 0 = unspecified
	Features  map[string]bool `json:"feat"`           // explicit grants; "*" = all features
	IssuedAt  int64           `json:"iat,omitempty"`  // unix seconds
	ExpiresAt int64           `json:"exp,omitempty"`  // unix seconds; 0 = perpetual
}

// Sign mints a license key from a License using the offline private key. Used by
// `tela license issue`; never on the request path.
func Sign(priv ed25519.PrivateKey, l License) (string, error) {
	if len(priv) != ed25519.PrivateKeySize {
		return "", fmt.Errorf("ee: signing key must be %d bytes", ed25519.PrivateKeySize)
	}
	payload, err := json.Marshal(l)
	if err != nil {
		return "", err
	}
	sig := ed25519.Sign(priv, payload)
	return tokenPrefix +
		base64.RawURLEncoding.EncodeToString(payload) + "." +
		base64.RawURLEncoding.EncodeToString(sig), nil
}

// Verify decodes and authenticates a license key against the embedded public
// key, rejecting expired keys. The returned *License is safe to trust.
func Verify(token string) (*License, error) { return verifyWith(publicKey(), token) }

// verifyWith is Verify against an explicit key — the seam tests sign+verify
// against an ephemeral keypair rather than the embedded production key.
func verifyWith(pub ed25519.PublicKey, token string) (*License, error) {
	raw, ok := strings.CutPrefix(strings.TrimSpace(token), tokenPrefix)
	if !ok {
		return nil, ErrMalformed
	}
	payloadB64, sigB64, ok := strings.Cut(raw, ".")
	if !ok {
		return nil, ErrMalformed
	}
	payload, err := base64.RawURLEncoding.DecodeString(payloadB64)
	if err != nil {
		return nil, ErrMalformed
	}
	sig, err := base64.RawURLEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, ErrMalformed
	}
	if len(pub) != ed25519.PublicKeySize || !ed25519.Verify(pub, payload, sig) {
		return nil, ErrSignature
	}
	var l License
	if err := json.Unmarshal(payload, &l); err != nil {
		return nil, ErrMalformed
	}
	if l.expired() {
		return nil, ErrExpired
	}
	return &l, nil
}

func (l *License) expired() bool {
	return l != nil && l.ExpiresAt != 0 && time.Now().Unix() > l.ExpiresAt
}

// Grants reports whether the license includes feature and is still valid. A nil
// receiver (no license installed) grants nothing; "*" grants every feature.
// This is the method api.entitled() calls — fail-closed by construction.
func (l *License) Grants(feature string) bool {
	if l == nil || l.expired() {
		return false
	}
	return l.Features["*"] || l.Features[feature]
}

// Status is the license summary surfaced to the admin License screen — never the
// raw token, and never the signature.
type Status struct {
	Customer  string   `json:"customer"`
	Tier      string   `json:"tier"`
	Seats     int      `json:"seats"`
	Features  []string `json:"features"`
	IssuedAt  string   `json:"issued_at,omitempty"`
	ExpiresAt string   `json:"expires_at,omitempty"`
	Valid     bool     `json:"valid"`
}

// Status renders the license for display, with features sorted for a stable view.
func (l *License) Status() Status {
	if l == nil {
		return Status{}
	}
	feats := make([]string, 0, len(l.Features))
	for f, on := range l.Features {
		if on {
			feats = append(feats, f)
		}
	}
	sort.Strings(feats)
	st := Status{
		Customer: l.Customer,
		Tier:     l.Tier,
		Seats:    l.Seats,
		Features: feats,
		Valid:    !l.expired(),
	}
	if l.IssuedAt != 0 {
		st.IssuedAt = time.Unix(l.IssuedAt, 0).UTC().Format(time.RFC3339)
	}
	if l.ExpiresAt != 0 {
		st.ExpiresAt = time.Unix(l.ExpiresAt, 0).UTC().Format(time.RFC3339)
	}
	return st
}
