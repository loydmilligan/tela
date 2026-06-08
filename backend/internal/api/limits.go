package api

// limits.go — metering & tiers (migration 0017). The single place quota policy
// lives: resolve a space/account to its owning *account*, look up that account's
// plan, count current usage, and gate a creation when it would exceed a limit.
//
// Design notes (kept deliberately refactorable):
//   - Limit *values* are data (the plans table), never hardcoded here.
//   - "Owning account" of a space = its org (spaces.org_id) → else its
//     personal_user_id → else the space_members owner (legacy team spaces).
//   - Quota checks run on s.DB just before the insert. The small TOCTOU window
//     (two concurrent creates racing a limit) is acceptable for soft caps; if it
//     ever needs to be exact, move the check inside the caller's tx — the counters
//     already take a queryer so a *sql.Tx drops in unchanged.
//   - All gate funcs return *apiErr{402, "quota_exceeded", …} so REST and MCP
//     surface it identically (agents key on the code).

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// queryer is the read surface shared by *sql.DB and *sql.Tx, so a counter can run
// against either without duplication.
type queryer interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const (
	accountUser = "user"
	accountOrg  = "org"
)

// account identifies a billable owner: a user's personal account or an org.
type account struct {
	Kind string // accountUser | accountOrg
	ID   int64
}

// plan mirrors a row of the plans table. A nil max_* means unlimited. Listed=false
// marks an internal/comp tier kept out of the public catalog (still assignable).
type plan struct {
	Key              string `json:"key"`
	AccountKind      string `json:"account_kind"`
	Name             string `json:"name"`
	MaxSpaces        *int64 `json:"max_spaces"`
	MaxPagesPerSpace *int64 `json:"max_pages_per_space"`
	MaxStorageBytes  *int64 `json:"max_storage_bytes"`
	MaxMembers       *int64 `json:"max_members"`
	Listed           bool   `json:"listed"`
	// Display pricing (no billing engine). PriceCents nil = custom/contact, 0 = free.
	PriceCents  *int64 `json:"price_cents"`
	PricePeriod string `json:"price_period"`
	// Features is the boolean entitlement map (e.g. managed_rag, publishing).
	// Quotas say "how many"; features say "is X allowed". Never nil after scan.
	Features map[string]bool `json:"features"`
	// MaxLLMCallsPerMonth caps managed LLM calls (ask/chat) per calendar month;
	// nil = unlimited. The compute meter, beside the resource quotas.
	MaxLLMCallsPerMonth *int64 `json:"max_llm_calls_per_month"`
}

func nullToPtr(n sql.NullInt64) *int64 {
	if !n.Valid {
		return nil
	}
	v := n.Int64
	return &v
}

// planCols is qualified with the `p` alias because the org JOIN below shares a
// `name` column with orgs — every query selecting these must alias plans as p.
// listed is INTEGER 0/1 (SQLite-era convention) — scanned into an int, not a
// bool, because pgx is strict about the integer→bool mismatch.
const planCols = `p.key, p.account_kind, p.name, p.max_spaces, p.max_pages_per_space, p.max_storage_bytes, p.max_members, p.listed, p.price_cents, p.price_period, p.features, p.max_llm_calls_per_month`

func scanPlan(row interface{ Scan(...any) error }) (plan, error) {
	var (
		p                                                plan
		spaces, pages, storage, members, cents, llmCalls sql.NullInt64
		listed                                           int
		featuresRaw                                      []byte
	)
	if err := row.Scan(&p.Key, &p.AccountKind, &p.Name, &spaces, &pages, &storage, &members, &listed, &cents, &p.PricePeriod, &featuresRaw, &llmCalls); err != nil {
		return plan{}, err
	}
	p.MaxSpaces, p.MaxPagesPerSpace = nullToPtr(spaces), nullToPtr(pages)
	p.MaxStorageBytes, p.MaxMembers = nullToPtr(storage), nullToPtr(members)
	p.Listed = listed == 1
	p.PriceCents = nullToPtr(cents)
	p.MaxLLMCallsPerMonth = nullToPtr(llmCalls)
	p.Features = map[string]bool{}
	if len(featuresRaw) > 0 {
		_ = json.Unmarshal(featuresRaw, &p.Features) // malformed JSON → empty map, never fatal
	}
	return p, nil
}

// spaceOwner resolves the account that owns spaceID. Errors with sql.ErrNoRows
// when the space doesn't exist.
func spaceOwner(ctx context.Context, q queryer, spaceID int64) (account, error) {
	var personalUserID, orgID sql.NullInt64
	err := q.QueryRowContext(ctx,
		`SELECT personal_user_id, org_id FROM spaces WHERE id = $1`, spaceID).
		Scan(&personalUserID, &orgID)
	if err != nil {
		return account{}, err
	}
	if orgID.Valid {
		return account{Kind: accountOrg, ID: orgID.Int64}, nil
	}
	if personalUserID.Valid {
		return account{Kind: accountUser, ID: personalUserID.Int64}, nil
	}
	// Legacy team space: owner is the space_members 'owner' row.
	var ownerID int64
	err = q.QueryRowContext(ctx,
		`SELECT user_id FROM space_members WHERE space_id = $1 AND role = 'owner' ORDER BY user_id LIMIT 1`,
		spaceID).Scan(&ownerID)
	if err != nil {
		return account{}, err
	}
	return account{Kind: accountUser, ID: ownerID}, nil
}

// planFor loads acct's plan. A missing account row (shouldn't happen behind auth)
// surfaces as sql.ErrNoRows.
func planFor(ctx context.Context, q queryer, acct account) (plan, error) {
	var src string
	switch acct.Kind {
	case accountOrg:
		src = `SELECT ` + planCols + ` FROM plans p JOIN orgs o ON o.plan_key = p.key WHERE o.id = $1`
	default:
		// Resolve the EFFECTIVE plan: the trial tier while trial_ends_at is in
		// the future, else the base plan_key. Text-datetime comparison is
		// chronological for the fixed 'YYYY-MM-DD HH:MM:SS' format. Expiry needs
		// no job — a past trial_ends_at simply stops winning the CASE.
		src = `SELECT ` + planCols + ` FROM plans p JOIN users u ON p.key = CASE
			WHEN u.trial_ends_at IS NOT NULL AND u.trial_ends_at > tela_now() THEN u.trial_plan_key
			ELSE u.plan_key END
			WHERE u.id = $1`
	}
	return scanPlan(q.QueryRowContext(ctx, src, acct.ID))
}

// featureEnabled reports whether acct's effective plan grants the named feature.
// Errors (missing account, etc.) resolve to false — fail closed. This is the
// boolean-entitlement sibling to the quota gates; the cloud-connect path will
// later overlay remote entitlements here.
func (s *Server) featureEnabled(ctx context.Context, acct account, feat string) bool {
	p, err := planFor(ctx, s.DB, acct)
	if err != nil {
		return false
	}
	return p.Features[feat]
}

// ── usage counters ────────────────────────────────────────────────────────────

// countOwnedSpaces counts spaces the account owns for the space quota. A user's
// auto-provisioned personal home is exempt (it's mandatory, not a created space).
func countOwnedSpaces(ctx context.Context, q queryer, acct account) (int64, error) {
	var n int64
	var err error
	if acct.Kind == accountOrg {
		err = q.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM spaces WHERE org_id = $1`, acct.ID).Scan(&n)
	} else {
		err = q.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM spaces s
			  JOIN space_members m ON m.space_id = s.id AND m.user_id = $1 AND m.role = 'owner'
			 WHERE s.personal_user_id IS NULL AND s.org_id IS NULL`, acct.ID).Scan(&n)
	}
	return n, err
}

// sumOwnedStorage sums live attachment bytes across the account's owned spaces
// (for a user: their personal home + team spaces they own).
func sumOwnedStorage(ctx context.Context, q queryer, acct account) (int64, error) {
	var n int64
	var err error
	if acct.Kind == accountOrg {
		err = q.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(sf.byte_size), 0)
			  FROM space_files sf JOIN spaces s ON s.id = sf.space_id
			 WHERE sf.deleted_at IS NULL AND s.org_id = $1`, acct.ID).Scan(&n)
	} else {
		err = q.QueryRowContext(ctx, `
			SELECT COALESCE(SUM(sf.byte_size), 0)
			  FROM space_files sf JOIN spaces s ON s.id = sf.space_id
			 WHERE sf.deleted_at IS NULL AND s.org_id IS NULL AND (
			       s.personal_user_id = $1
			    OR (s.personal_user_id IS NULL AND EXISTS (
			          SELECT 1 FROM space_members m
			           WHERE m.space_id = s.id AND m.user_id = $1 AND m.role = 'owner'))
			 )`, acct.ID).Scan(&n)
	}
	return n, err
}

func countOrgMembers(ctx context.Context, q queryer, orgID int64) (int64, error) {
	var n int64
	err := q.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM org_members WHERE org_id = $1`, orgID).Scan(&n)
	return n, err
}

// ── gates ─────────────────────────────────────────────────────────────────────

func quotaErr(format string, args ...any) *apiErr {
	return &apiErr{http.StatusPaymentRequired, "quota_exceeded", fmt.Sprintf(format, args...)}
}

func internalQuotaErr() *apiErr {
	return &apiErr{http.StatusInternalServerError, "internal", "quota check failed"}
}

// checkSpaceQuota gates creating one more space owned by acct.
func (s *Server) checkSpaceQuota(ctx context.Context, acct account) *apiErr {
	p, err := planFor(ctx, s.DB, acct)
	if err != nil {
		return internalQuotaErr()
	}
	if p.MaxSpaces == nil {
		return nil
	}
	used, err := countOwnedSpaces(ctx, s.DB, acct)
	if err != nil {
		return internalQuotaErr()
	}
	if used+1 > *p.MaxSpaces {
		return quotaErr("%s plan space limit reached (%d) — upgrade to add more spaces", p.Name, *p.MaxSpaces)
	}
	return nil
}

// checkPageQuota gates creating one more page in spaceID.
func (s *Server) checkPageQuota(ctx context.Context, spaceID int64) *apiErr {
	return s.checkPageQuotaN(ctx, spaceID, 1)
}

// checkPageQuotaN gates adding n pages to spaceID against the space's owning
// account's per-space page limit. Used by the single create paths (n=1) and the
// bulk paths — import (n=files) and cross-space move (n=subtree) — so a quota
// can't be sidestepped in bulk.
func (s *Server) checkPageQuotaN(ctx context.Context, spaceID, n int64) *apiErr {
	if n <= 0 {
		return nil
	}
	acct, err := spaceOwner(ctx, s.DB, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // space doesn't exist yet/anymore; the caller's own checks handle it
	}
	if err != nil {
		return internalQuotaErr()
	}
	p, err := planFor(ctx, s.DB, acct)
	if err != nil {
		return internalQuotaErr()
	}
	if p.MaxPagesPerSpace == nil {
		return nil
	}
	used, err := countLiveSpacePages(ctx, s.DB, spaceID)
	if err != nil {
		return internalQuotaErr()
	}
	if used+n > *p.MaxPagesPerSpace {
		return quotaErr("%s plan page limit for this space reached (%d) — upgrade for more", p.Name, *p.MaxPagesPerSpace)
	}
	return nil
}

// checkStorageQuota gates adding addBytes of attachment data to spaceID.
func (s *Server) checkStorageQuota(ctx context.Context, spaceID, addBytes int64) *apiErr {
	if addBytes <= 0 {
		return nil
	}
	acct, err := spaceOwner(ctx, s.DB, spaceID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return internalQuotaErr()
	}
	p, err := planFor(ctx, s.DB, acct)
	if err != nil {
		return internalQuotaErr()
	}
	if p.MaxStorageBytes == nil {
		return nil
	}
	used, err := sumOwnedStorage(ctx, s.DB, acct)
	if err != nil {
		return internalQuotaErr()
	}
	if used+addBytes > *p.MaxStorageBytes {
		return quotaErr("%s plan storage limit reached (%d bytes) — upgrade for more", p.Name, *p.MaxStorageBytes)
	}
	return nil
}

// checkSeatQuota gates adding one more member to orgID.
func (s *Server) checkSeatQuota(ctx context.Context, orgID int64) *apiErr {
	p, err := planFor(ctx, s.DB, account{Kind: accountOrg, ID: orgID})
	if err != nil {
		return internalQuotaErr()
	}
	if p.MaxMembers == nil {
		return nil
	}
	used, err := countOrgMembers(ctx, s.DB, orgID)
	if err != nil {
		return internalQuotaErr()
	}
	if used+1 > *p.MaxMembers {
		return quotaErr("%s plan seat limit reached (%d) — upgrade to add members", p.Name, *p.MaxMembers)
	}
	return nil
}

// checkAndRecordLLMCall gates AND records one managed LLM call (ask/chat)
// against acct's monthly cap. NULL cap = unlimited (no metering). Unlike the
// count-based soft caps above, this is a single ATOMIC conditional upsert: the
// increment fires only while under the cap (the ON CONFLICT WHERE clause), so
// check-and-record has no TOCTOU window. A no-row result = the cap was already
// reached → 402.
func (s *Server) checkAndRecordLLMCall(ctx context.Context, acct account) *apiErr {
	p, err := planFor(ctx, s.DB, acct)
	if err != nil {
		return internalQuotaErr()
	}
	if p.MaxLLMCallsPerMonth == nil {
		return nil // unlimited tier — not metered
	}
	var n int64
	err = s.DB.QueryRowContext(ctx, `
		INSERT INTO cloud_usage (account_kind, account_id, period, llm_calls)
		VALUES ($1, $2, to_char((now() AT TIME ZONE 'UTC'), 'YYYY-MM'), 1)
		ON CONFLICT (account_kind, account_id, period)
		DO UPDATE SET llm_calls = cloud_usage.llm_calls + 1, updated_at = tela_now()
		WHERE cloud_usage.llm_calls < $3
		RETURNING llm_calls`,
		acct.Kind, acct.ID, *p.MaxLLMCallsPerMonth).Scan(&n)
	if errors.Is(err, sql.ErrNoRows) {
		return quotaErr("%s plan monthly AI limit reached (%d) — upgrade for more", p.Name, *p.MaxLLMCallsPerMonth)
	}
	if err != nil {
		return internalQuotaErr()
	}
	return nil
}
