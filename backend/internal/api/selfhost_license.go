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
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
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
// renewal that lands late — or a buyer slow to re-install the renewed key —
// doesn't lock a paying customer out of EE. Generous on purpose: the offline key
// can't be refreshed remotely, so this window is the whole cushion against the
// renewal cliff.
const selfHostLicenseGraceDays = 14

// isSelfHostSubscription reports whether subID is a subscription we've issued a
// self-host license against. Lets the webhook route cancel/revoke events (which
// may omit product_id) to license handling instead of the plan reconciler.
func (s *Server) isSelfHostSubscription(ctx context.Context, subID string) bool {
	if strings.TrimSpace(subID) == "" {
		return false
	}
	var exists bool
	_ = s.DB.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM selfhost_licenses WHERE polar_subscription_id = $1)`, subID).Scan(&exists)
	return exists
}

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
// buyer, expiring at expiresAt. seats is advisory (0 = unspecified). licenseID is
// the stable per-subscription handle embedded so the instance can refresh the key
// from the cloud. Errors if the signer isn't configured — gate on
// selfHostIssuanceEnabled.
func (s *Server) mintSelfHostLicense(customer string, seats int, expiresAt time.Time, licenseID string) (string, error) {
	lic := ee.License{
		Customer:  customer,
		Tier:      "enterprise",
		Seats:     seats,
		Features:  map[string]bool{"*": true},
		IssuedAt:  time.Now().Unix(),
		ExpiresAt: expiresAt.Unix(),
		LicenseID: licenseID,
	}
	return ee.Sign(s.licenseSigner, lic)
}

// newRefreshID returns a random opaque handle for a subscription's license,
// stable across renewals (stored once, reused). Not a secret.
func newRefreshID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
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
	// sales_enabled gates the buyer UI: true only where this instance can actually
	// sell + mint (managed cloud with the product + signer wired). Elsewhere the
	// panel hides its Buy affordance (a self-hosted instance doesn't sell to itself).
	writeJSON(w, http.StatusOK, map[string]any{"licenses": out, "sales_enabled": s.selfHostIssuanceEnabled()})
}

// CreateSelfHostPortal opens the Polar customer portal for the caller so they can
// manage/cancel/update their self-host license subscription. Keyed by the account
// external id (CreateCustomerSession works off it), NOT users.polar_customer_id —
// a self-host buyer typically sits on the Free cloud plan and has no cloud
// customer id, so the plan-side portal (CreateBillingPortal) would 400 for them.
func (s *Server) CreateSelfHostPortal(w http.ResponseWriter, r *http.Request) {
	u, ok := requireUser(w, r)
	if !ok {
		return
	}
	if !s.billing.Enabled() {
		writeError(w, http.StatusServiceUnavailable, "billing_disabled", "billing is not configured on this instance")
		return
	}
	url, err := s.billing.CreateCustomerSession(r.Context(), acctExternalID(account{Kind: accountUser, ID: u.ID}))
	if err != nil {
		slog.Error("selfhost license: create portal", "user", u.ID, "err", err)
		writeError(w, http.StatusBadGateway, "billing_error", "could not open the billing portal")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"url": url})
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
		periodEnd := time.Now().AddDate(1, 0, 0)
		if evt.Data.CurrentPeriodEnd != nil {
			periodEnd = *evt.Data.CurrentPeriodEnd
		}
		expiresAt := periodEnd.AddDate(0, 0, selfHostLicenseGraceDays)
		expStr := expiresAt.UTC().Format("2006-01-02 15:04:05")

		// Already issued for this exact period? Then this is a redelivery or a
		// same-term update — do NOT re-mint (a fresh signature would make the stored
		// key diverge from the one the buyer was emailed and installed). Just make
		// sure the row reads active and stop.
		var prevExp, prevRefresh sql.NullString
		existed := s.DB.QueryRowContext(ctx,
			`SELECT expires_at, refresh_id FROM selfhost_licenses WHERE polar_subscription_id = $1`, subID).Scan(&prevExp, &prevRefresh) == nil
		if existed && prevExp.String == expStr {
			_, err := s.DB.ExecContext(ctx,
				`UPDATE selfhost_licenses SET status = 'active', updated_at = tela_now() WHERE polar_subscription_id = $1`, subID)
			return err
		}

		// First issuance or a genuine renewal (period advanced) → mint + persist +
		// email the (new) key.
		if s.licenseSigner == nil {
			// Product wired but no signer: fail loud (→ 500 → Polar redelivers) so a
			// paying customer isn't silently left keyless once the signer is set.
			return errSelfHostSignerMissing
		}
		// The refresh handle is stable across renewals — reuse the row's if present.
		refreshID := cmp.Or(strings.TrimSpace(prevRefresh.String), newRefreshID())
		seats := metadataInt(evt.Data.Metadata, "seats")
		var email sql.NullString
		_ = s.DB.QueryRowContext(ctx, `SELECT email FROM users WHERE id = $1`, acct.ID).Scan(&email)
		customer := cmp.Or(strings.TrimSpace(email.String), acctExternalID(acct))

		token, err := s.mintSelfHostLicense(customer, seats, expiresAt, refreshID)
		if err != nil {
			return err
		}
		if _, err := s.DB.ExecContext(ctx, `
			INSERT INTO selfhost_licenses
			  (owner_user_id, polar_subscription_id, polar_customer_id, tier, seats, status, token, expires_at, refresh_id, issued_at, updated_at)
			VALUES ($1, $2, $3, 'enterprise', $4, 'active', $5, $6, $7, tela_now(), tela_now())
			ON CONFLICT (polar_subscription_id) WHERE polar_subscription_id IS NOT NULL
			DO UPDATE SET
			  seats = EXCLUDED.seats, status = 'active', token = EXCLUDED.token,
			  expires_at = EXCLUDED.expires_at, refresh_id = EXCLUDED.refresh_id,
			  issued_at = tela_now(), updated_at = tela_now()`,
			acct.ID, subID, nullIfEmpty(evt.Data.CustomerID), seats, token, expStr, refreshID); err != nil {
			return err
		}
		if email.Valid && strings.TrimSpace(email.String) != "" {
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

// ── cloud license refresh (issuer side, PUBLIC) ─────────────────────────────

// RefreshSelfHostLicense returns the CURRENT signed key for the subscription that
// the presented key belongs to. A self-hosted instance polls this so a renewal's
// new key installs without a manual re-paste. It's PUBLIC (under /api/public/),
// self-authenticating: only a validly-SIGNED tela key is accepted (ParseSigned,
// which tolerates expiry so a lapsed instance can still recover), and it returns
// ONLY the current key for that same subscription (matched by the embedded lid) —
// never anyone else's. 404 when the subscription isn't active, so the instance
// lets its key lapse. Holding a signed key already grants the license, so this
// exposes nothing new.
func (s *Server) RefreshSelfHostLicense(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if token == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "token is required")
		return
	}
	lic, err := ee.ParseSigned(token)
	if err != nil || lic.LicenseID == "" {
		writeError(w, http.StatusUnauthorized, "invalid_license", "not a valid tela license key")
		return
	}
	var current, status string
	err = s.DB.QueryRowContext(r.Context(),
		`SELECT token, status FROM selfhost_licenses WHERE refresh_id = $1`, lic.LicenseID).Scan(&current, &status)
	if err != nil || status != "active" {
		writeError(w, http.StatusNotFound, "no_active_license", "no active license to refresh")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": current})
}

// ── cloud license refresh (consumer side, self-host) ────────────────────────

// licenseRefreshURL is the cloud base a self-hosted instance polls for its
// renewed key. Defaults to the public tela cloud (where the license was bought);
// set TELA_LICENSE_REFRESH_URL empty to disable (air-gapped installs re-paste by
// hand). Only instances with a purchased, non-env key ever call it.
func licenseRefreshURL() string {
	if v, ok := os.LookupEnv("TELA_LICENSE_REFRESH_URL"); ok {
		return strings.TrimRight(strings.TrimSpace(v), "/")
	}
	return "https://telawiki.com"
}

// licenseRefreshLoop pulls the current key for this instance's installed license
// from the cloud and installs it when it's newer — so a renewal lands without a
// manual re-paste. Self-host only (managed cloud grants via plan flags); no-op
// without an installed non-env key or a reachable URL. A network failure is a
// silent no-op, so it never disrupts an air-gapped instance.
func (s *Server) licenseRefreshLoop(ctx context.Context) {
	base := licenseRefreshURL()
	if base == "" || s.managedCloud {
		return
	}
	s.refreshLicenseOnce(ctx, base)
	t := time.NewTicker(12 * time.Hour)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.refreshLicenseOnce(ctx, base)
		}
	}
}

func (s *Server) refreshLicenseOnce(ctx context.Context, base string) {
	if envLicensed() { // an env-pinned key always wins — never overwrite it
		return
	}
	cur := s.license.Load()
	if cur == nil || cur.LicenseID == "" {
		return // no refreshable key installed (Community, or a pre-lid legacy key)
	}
	token, _ := s.settings.Get(licenseTokenSettingKey)
	if strings.TrimSpace(token) == "" {
		return
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		base+"/api/public/license/refresh?token="+url.QueryEscape(strings.TrimSpace(token)), nil)
	if err != nil {
		return
	}
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		return // offline / air-gapped → no-op, manual re-paste still works
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	var out struct {
		Token string `json:"token"`
	}
	if json.NewDecoder(resp.Body).Decode(&out) != nil || out.Token == "" {
		return
	}
	newLic, err := ee.Verify(out.Token) // must be a valid, unexpired key
	if err != nil || newLic.ExpiresAt <= cur.ExpiresAt {
		return // not newer → nothing to do
	}
	if err := s.settings.Set(ctx, licenseTokenSettingKey, out.Token, nil); err != nil {
		slog.Warn("license: auto-refresh save failed", "err", err)
		return
	}
	s.loadLicense(ctx)
	slog.Info("license: auto-refreshed the Enterprise key from the cloud", "expires_at", time.Unix(newLic.ExpiresAt, 0).UTC())
}
