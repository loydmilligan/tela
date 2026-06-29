package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/zcag/tela/backend/internal/ee"
)

// `tela license …` — offline Enterprise-key tooling. `issue` mints a signed key
// from the vendor's private signing key (TELA_LICENSE_SIGNING_KEY, base64 RawStd
// ed25519); `verify` checks any key against the embedded public key. No DB.

func runLicense(args []string) {
	if len(args) < 1 {
		fatal("usage: tela license <issue|verify> …")
	}
	switch args[0] {
	case "issue":
		runLicenseIssue(args[1:])
	case "verify":
		runLicenseVerify(args[1:])
	default:
		fatal("license: unknown command (known: issue, verify)", "cmd", args[0])
	}
}

func runLicenseIssue(args []string) {
	fs := flag.NewFlagSet("license issue", flag.ExitOnError)
	customer := fs.String("customer", "", "customer / org label")
	tier := fs.String("tier", "enterprise", "tier label")
	seats := fs.Int("seats", 0, "max seats (0 = unspecified)")
	features := fs.String("features", "*", "comma-separated feature grants, or * for all")
	days := fs.Int("days", 0, "days until expiry (0 = perpetual)")
	expires := fs.String("expires", "", "explicit expiry YYYY-MM-DD (overrides --days)")
	_ = fs.Parse(args)

	keyB64 := strings.TrimSpace(os.Getenv("TELA_LICENSE_SIGNING_KEY"))
	if keyB64 == "" {
		fatal("license issue: TELA_LICENSE_SIGNING_KEY is required (base64 RawStd ed25519 private key)")
	}
	raw, err := base64.RawStdEncoding.DecodeString(keyB64)
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		fatal("license issue: TELA_LICENSE_SIGNING_KEY is not a valid base64 ed25519 private key")
	}
	priv := ed25519.PrivateKey(raw)

	feat := map[string]bool{}
	for _, f := range strings.Split(*features, ",") {
		if f = strings.TrimSpace(f); f != "" {
			feat[f] = true
		}
	}
	lic := ee.License{
		Customer: *customer, Tier: *tier, Seats: *seats,
		Features: feat, IssuedAt: time.Now().Unix(),
	}
	switch {
	case *expires != "":
		t, err := time.Parse("2006-01-02", *expires)
		if err != nil {
			fatal("license issue: --expires must be YYYY-MM-DD", "err", err)
		}
		lic.ExpiresAt = t.Unix()
	case *days > 0:
		lic.ExpiresAt = time.Now().AddDate(0, 0, *days).Unix()
	}

	tok, err := ee.Sign(priv, lic)
	if err != nil {
		fatal("license issue: sign", "err", err)
	}
	// Sanity: a freshly minted key must verify against the EMBEDDED public key,
	// catching a signing-key / build-key mismatch before it's handed to a customer.
	if _, err := ee.Verify(tok); err != nil {
		fatal("license issue: minted key does not verify against the embedded public key — signing key and build are out of sync", "err", err)
	}
	fmt.Println(tok)
}

func runLicenseVerify(args []string) {
	if len(args) < 1 {
		fatal("usage: tela license verify <token>")
	}
	lic, err := ee.Verify(args[0])
	if err != nil {
		fatal("license verify: invalid", "err", err)
	}
	b, _ := json.MarshalIndent(lic.Status(), "", "  ")
	fmt.Println(string(b))
}
