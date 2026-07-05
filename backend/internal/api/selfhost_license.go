package api

// Self-serve self-host Enterprise licensing — the ISSUANCE side, on the managed
// cloud. A buyer subscribes to the self-host Enterprise Polar product; the
// webhook reconciler mints a signed offline ee key (ee.Sign, using the vendor's
// private TELA_LICENSE_SIGNING_KEY), stores it in selfhost_licenses, and emails
// it. The buyer pastes it into THEIR self-hosted instance (Settings → License),
// which verifies it offline against the embedded public key (see license.go —
// that's the CONSUMPTION side; a self-host instance never signs).
//
// Distinct from the plan reconciler: a self-host license is NOT an account plan
// (the buyer may sit on Free cloud). Its product is deliberately kept OUT of the
// TELA_POLAR_PRODUCTS map so PlanFor ignores it; PolarWebhook routes its events
// here instead.

import (
	"cmp"
	"context"
	"crypto/ed25519"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zcag/tela/backend/internal/billing"
	"github.com/zcag/tela/backend/internal/ee"
	"github.com/zcag/tela/backend/internal/mailer"
)

// errSelfHostSignerMissing is returned when a self-host license event arrives but
// the signing key isn't configured — a misconfiguration (product wired, signer
// absent). Surfaced as a 500 so Polar redelivers once the signer is set.
var errSelfHostSignerMissing = errors.New("selfhost license: signing key not configured")

// selfHostLicenseGraceDays extends a minted key past the paid-through date so a
// renewal that lands a little late (or a clock skew on the self-hosted box)
// doesn't briefly lock a paying customer out of EE.
const selfHostLicenseGraceDays = 5

// loadLicenseSigner reads the vendor's ed25519 private signing key from
// TELA_LICENSE_SIGNING_KEY (base64 RawStd, same format as the `tela license
// issue` CLI). Set only on the managed-cloud issuer; unset/invalid leaves the
// signer nil so self-serve issuance no-ops. Never boot-fatal.
func (s *Server) loadLicenseSigner() {
	b64 := strings.TrimSpace(os.Getenv("TELA_LICENSE_SIGNING_KEY"))
	if b64 == "" {
		return
	}
	raw, err := base64.RawStdEncoding.DecodeString(b64)
	if err != nil || len(raw) != ed25519.PrivateKeySize {
		slog.Warn("selfhost license: TELA_LICENSE_SIGNING_KEY is not a valid base64 ed25519 private key — self-serve issuance disabled")
		return
	}
	s.licenseSigner = ed25519.PrivateKey(raw)
	slog.Info("selfhost license: signing key loaded — self-serve issuance enabled")
}

// selfHostIssuanceEnabled reports whether this instance can sell + mint self-host
// Enterprise keys: Polar transacts, a product is wired, and the signing key is
// present. All three or the sales surface 503s / the webhook branch is inert.
func (s *Server) selfHostIssuanceEnabled() bool {
	return s.billing.Enabled() && s.selfHostProductID != "" && s.licenseSigner != nil
}

// mintSelfHostLicense signs a perpetual-feature ("*") Enterprise key for the
// buyer, expiring at expiresAt. seats is advisory (0 = unspecified). Errors if
// the signer isn't configured — the caller must gate on selfHostIssuanceEnabled.
func (s *Server) mintSelfHostLicense(customer string, seats int, expiresAt time.Time) (string, error) {
	lic := ee.License{
		Customer:  customer,
		Tier:      "enterprise",
		Seats:     seats,
		Features:  map[string]bool{"*": true},
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: expiresAt.Unix(),
	}
	return ee.Sign(s.licenseSigner, lic)
}

// CreateSelfHostCheckout starts a Polar checkout for the self-host Enterprise
// license and returns its hosted URL. Session-authed: the buyer is a cloud user
// (external_customer_id = user:<id>) so the webhook can attribute the key and the
// buyer can retrieve it later from /api/licenses. seats seeds the seat-based
// quantity.
func (s *Server) CreateSelfHostCheckout(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if !s.selfHostIssuanceEnabled() {
		writeError(w, http.StatusServiceUnavailable, "billing_disabled", "self-host license sales are not configured on this instance")
		return
	}
	var req struct {
		Seats int `json:"seats"`
	}
	// Body is optional; default to a single seat.
	_ = json.NewDecoder(r.Body).Decode(&req)
	seats := req.Seats
	if seats < 1 {
		seats = 1
	}

	acct := account{Kind: accountUser, ID: u.ID}
	url, err := s.billing.CreateCheckout(r.Context(), billing.CheckoutInput{
		ProductID:          s.selfHostProductID,
		SuccessURL:         s.linkOrigin(r) + "/settings?tab=licenses&checkout={CHECKOUT_ID}",
		ExternalCustomerID: acctExternalID(acct),
		CustomerEmail:      u.Email,
		Seats:              seats,
		Metadata: map[string]string{
			"kind":  "selfhost_license",
			"seats": strconv.Itoa(seats),
		},
	})
	if err != nil {
		slog.Error("selfhost license: create checkout", "user", u.ID, "err", err)
		writeError(w, http.StatusBadGateway, "billing_error", "could not start checkout")
		return
	}
	s.audit(r.Context(), r, "license.checkout", "user", u.ID, "selfhost_enterprise")
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
}

// selfHostLicenseDTO is one of the caller's purchased keys — the full token
// included so they can copy it into their instance.
type selfHostLicenseDTO struct {
	ID        int64  `json:"id"`
	Tier      string `json:"tier"`
	Seats     int    `json:"seats"`
	Status    string `json:"status"`
	Token     string `json:"token"`
	IssuedAt  string `json:"issued_at"`
	ExpiresAt string `json:"expires_at,omitempty"`
}

// ListSelfHostLicenses returns the caller's self-host Enterprise license keys so
// they can (re)copy a key without digging through email. Session-authed; scoped
// to the caller — a user only ever sees their own keys.
func (s *Server) ListSelfHostLicenses(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	rows, err := s.DB.QueryContext(r.Context(), `
		SELECT id, tier, seats, status, token, issued_at, COALESCE(expires_at, '')
		FROM selfhost_licenses WHERE owner_user_id = $1 ORDER BY created_at DESC`, u.ID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "load licenses failed")
		return
	}
	defer rows.Close()
	out := []selfHostLicenseDTO{}
	for rows.Next() {
		var d selfHostLicenseDTO
		if err := rows.Scan(&d.ID, &d.Tier, &d.Seats, &d.Status, &d.Token, &d.IssuedAt, &d.ExpiresAt); err != nil {
			writeError(w, http.StatusInternalServerError, "internal", "scan license failed")
			return
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, map[string]any{"licenses": out})
}

// reconcileSelfHostLicense handles a Polar event for the self-host license
// product: (re)mint + persist + email on an active subscription, mark on
// cancel/revoke. Idempotent per subscription (upsert on polar_subscription_id).
// Called from reconcileBilling when the event's product is the self-host product.
func (s *Server) reconcileSelfHostLicense(ctx context.Context, evt billing.Event, acct account) error {
	if acct.Kind != accountUser {
		slog.Warn("selfhost license: non-user buyer ignored", "kind", acct.Kind, "id", acct.ID)
		return nil
	}
	subID := strings.TrimSpace(evt.Data.ID)
	if subID == "" {
		return nil // can't dedup an event with no subscription id
	}

	switch evt.Type {
	case "subscription.created", "subscription.active", "subscription.updated":
		status := cmp.Or(evt.Data.Status, "active")
		if status != "active" && status != "trialing" {
			// past_due etc — leave the existing key (it lapses on its own expiry).
			return nil
		}
		if s.licenseSigner == nil {
			// Product wired but no signer: fail loud (→ 500 → Polar redelivers) so a
			// paying customer isn't silently left keyless once the signer is set.
			return errSelfHostSignerMissing
		}
		periodEnd := time.Now().AddDate(1, 0, 0)
		if evt.Data.CurrentPeriodEnd != nil {
			periodEnd = *evt.Data.CurrentPeriodEnd
		}
		expiresAt := periodEnd.AddDate(0, 0, selfHostLicenseGraceDays)
		expStr := expiresAt.UTC().Format("2006-01-02 15:04:05")
		seats := metadataInt(evt.Data.Metadata, "seats")

		var email sql.NullString
		_ = s.DB.QueryRowContext(ctx, `SELECT email FROM users WHERE id = $1`, acct.ID).Scan(&email)
		customer := cmp.Or(strings.TrimSpace(email.String), acctExternalID(acct))

		token, err := s.mintSelfHostLicense(customer, seats, expiresAt)
		if err != nil {
			return err
		}

		// Was this subscription already issued, and did its period advance? Drives
		// whether we email (first issuance or a renewal, not a no-op re-delivery).
		var prevExp sql.NullString
		existed := s.DB.QueryRowContext(ctx,
			`SELECT expires_at FROM selfhost_licenses WHERE polar_subscription_id = $1`, subID).Scan(&prevExp) == nil

		if _, err := s.DB.ExecContext(ctx, `
			INSERT INTO selfhost_licenses
			  (owner_user_id, polar_subscription_id, polar_customer_id, tier, seats, status, token, expires_at, issued_at, updated_at)
			VALUES ($1, $2, $3, 'enterprise', $4, 'active', $5, $6, tela_now(), tela_now())
			ON CONFLICT (polar_subscription_id) WHERE polar_subscription_id IS NOT NULL
			DO UPDATE SET
			  seats = EXCLUDED.seats, status = 'active', token = EXCLUDED.token,
			  expires_at = EXCLUDED.expires_at, issued_at = tela_now(), updated_at = tela_now()`,
			acct.ID, subID, nullIfEmpty(evt.Data.CustomerID), seats, token, expStr); err != nil {
			return err
		}

		if (!existed || prevExp.String != expStr) && email.Valid && strings.TrimSpace(email.String) != "" {
			manage := cmp.Or(canonicalBaseURL(), devBaseURL) + "/settings?tab=licenses"
			if e := s.Mailer.Send(ctx, mailer.SelfHostLicense(email.String, token, seats, expiresAt, manage)); e != nil {
				slog.Warn("selfhost license: delivery email failed", "user", acct.ID, "err", e)
			}
		}
		return nil

	case "subscription.canceled":
		_, err := s.DB.ExecContext(ctx,
			`UPDATE selfhost_licenses SET status = 'canceled', updated_at = tela_now() WHERE polar_subscription_id = $1`, subID)
		return err

	case "subscription.revoked":
		_, err := s.DB.ExecContext(ctx,
			`UPDATE selfhost_licenses SET status = 'revoked', updated_at = tela_now() WHERE polar_subscription_id = $1`, subID)
		return err
	}
	return nil
}

// metadataInt reads an integer from Polar metadata, tolerant of JSON string or
// number encodings (0 when absent/unparseable).
func metadataInt(m map[string]any, key string) int {
	switch v := m[key].(type) {
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	case float64:
		return int(v)
	}
	return 0
}
