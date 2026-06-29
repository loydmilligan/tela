# Licensing — open core + Enterprise

tela is **open core**. The model and pricing live in
[`editions-and-pricing.md`](editions-and-pricing.md); this is the licensing
mechanics.

## Three licenses, one repo

| Part | License | What it is |
|---|---|---|
| **Community core** | AGPL-3.0 (`LICENSE`) | The whole product. Free to self-host, modify, redistribute under the AGPL. |
| **Enterprise Edition** | tela EE license (`backend/internal/ee/LICENSE.md`) | The `ee/` code + EE-marked files. Source-available, **not** AGPL; production use needs a license key. |
| **Commercial license** | per-contract | AGPL relief for companies that can't ship copyleft — even without EE features. |

The core is **already AGPL and stays AGPL**. Enterprise is **additive** — a
separately-licensed module — so adopting this model relicenses nothing and takes
nothing away from existing self-hosters.

## How entitlement works

One gate, two unlock paths (`backend/internal/api/limits.go` → `entitled()`):

```
entitled(account, feature) =
      cloud plan grants it          // managed cloud: featureEnabled(plans.features)
   || license key grants it          // self-host: a verified Enterprise key
```

- **Cloud** unlocks Enterprise features through the account's plan (Polar-driven).
- **Self-host** unlocks them through an **offline, ed25519-signed license key**
  (`backend/internal/ee`). Verification is fully local — no phone-home, works
  air-gapped. The signing (private) key is held offline by the vendor; the
  verify (public) key is embedded in the binary (`ee/keys.go`).

## Installing a key (self-host)

- **Env:** set `TELA_LICENSE_KEY=tela_lic_…` (pins it read-only; wins every boot).
- **UI:** Settings → Instance admin → **License** → paste the key.

Both verify the key before activating it; an invalid/expired key leaves the
instance on Community (fail-closed), never bricks it.

## Issuing a key (vendor)

```
TELA_LICENSE_SIGNING_KEY=<base64 ed25519 private key> \
  tela license issue --customer "Acme Inc" --tier enterprise \
    --seats 50 --features sso,audit,scim --days 365
```

`--features '*'` grants all Enterprise features. `tela license verify <token>`
checks any key against the embedded public key. Both run offline (no DB). The
signing key lives **only** offline (never in the repo); rotating it invalidates
every issued key.

## Trademark

The tela name and logo are trademarks, reserved separately from the code license
— see [`TRADEMARK.md`](../TRADEMARK.md).
