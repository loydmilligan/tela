package ee

import (
	"crypto/ed25519"
	"encoding/base64"
)

// licensePublicKey is the ed25519 public key that verifies tela Enterprise
// license keys. The matching private (signing) key is held OFFLINE by the vendor
// and is never in this repo — it signs keys via `tela license issue`. Rotating
// it invalidates every issued key (pre-release: safe to rotate). base64 RawStd.
const licensePublicKey = "n+oY6evb5tF6NLPhkf9NWACCjSFif1Uqu4d8QumcKy4"

// publicKey decodes the embedded verify key. Panics on a malformed constant —
// a build/release error, caught immediately rather than silently accepting keys.
func publicKey() ed25519.PublicKey {
	b, err := base64.RawStdEncoding.DecodeString(licensePublicKey)
	if err != nil || len(b) != ed25519.PublicKeySize {
		panic("ee: invalid embedded license public key")
	}
	return ed25519.PublicKey(b)
}
