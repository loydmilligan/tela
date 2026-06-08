package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

// Personal spaces (docs/visibility-model.md). Every user gets a private,
// one-member space as their default home for personal writing — "private" is
// modelled as a space only you belong to, so this is where solo notes live.
// Provisioning is idempotent ("ensure if missing"); if a user deletes their
// personal space it is re-provisioned on the next trigger, which suits a
// default home. Lives in api (not auth) so it can reuse the space slug helpers
// and be called from both the admin user-create handler and the startup
// backfill in main.go — auth.Bootstrap stays free of space concerns.

const personalSpaceName = "Personal"

// EnsurePersonalSpace creates userID's personal space if they don't already
// have one, adding them as its owner. Idempotent — returns the existing space
// id when one is present. Safe under the occasional race: a concurrent insert
// trips the partial UNIQUE index on personal_user_id and we fall back to the
// row the winner created.
func EnsurePersonalSpace(ctx context.Context, db *sql.DB, userID int64, username string) (int64, error) {
	if id, err := personalSpaceID(ctx, db, userID); err == nil {
		return id, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}

	slug, err := uniquePersonalSlug(ctx, db, username)
	if err != nil {
		return 0, err
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO spaces(name, slug, personal_user_id) VALUES ($1, $2, $3) RETURNING id`,
		personalSpaceName, slug, userID).Scan(&id)
	if err != nil {
		// Lost a race (someone else just made this user's personal space) —
		// return theirs instead of erroring.
		if isUniqueConstraintErr(err) {
			return personalSpaceID(ctx, db, userID)
		}
		return 0, fmt.Errorf("insert personal space: %w", err)
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO space_members(space_id, user_id, role) VALUES ($1, $2, 'owner')`,
		id, userID); err != nil {
		return 0, fmt.Errorf("assign personal space owner: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// EnsurePersonalSpacesForAll backfills a personal space for every active user
// that lacks one. Called once at startup so existing instances (and the
// bootstrap admin) get theirs. Errors on a single user are logged and skipped
// so one bad row can't block boot.
func EnsurePersonalSpacesForAll(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `
		SELECT u.id, u.username
		  FROM users u
		 WHERE u.is_active = 1
		   AND NOT EXISTS (
		     SELECT 1 FROM spaces s WHERE s.personal_user_id = u.id
		   )`)
	if err != nil {
		return fmt.Errorf("query users needing personal space: %w", err)
	}
	type pending struct {
		id       int64
		username string
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.id, &p.username); err != nil {
			rows.Close()
			return fmt.Errorf("scan user row: %w", err)
		}
		todo = append(todo, p)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	rows.Close()

	created := 0
	for _, p := range todo {
		if _, err := EnsurePersonalSpace(ctx, db, p.id, p.username); err != nil {
			slog.Error("personal space for user", "user_id", p.id, "username", p.username, "err", err)
			continue
		}
		created++
	}
	if created > 0 {
		slog.Info("provisioned personal spaces", "count", created)
	}
	return nil
}

func personalSpaceID(ctx context.Context, db *sql.DB, userID int64) (int64, error) {
	var id int64
	err := db.QueryRowContext(ctx,
		`SELECT id FROM spaces WHERE personal_user_id = $1`, userID).Scan(&id)
	return id, err
}

// uniquePersonalSlug derives a slug from the username and appends -2, -3, … on
// collision so the spaces.slug UNIQUE constraint never trips.
func uniquePersonalSlug(ctx context.Context, db *sql.DB, username string) (string, error) {
	base := normalizeSlug(username)
	if base == "" {
		base = "personal"
	}
	if len(base) > maxSpaceSlugLen {
		base = strings.TrimRight(base[:maxSpaceSlugLen], "-")
	}
	candidate := base
	for n := 2; ; n++ {
		var x int
		err := db.QueryRowContext(ctx, `SELECT 1 FROM spaces WHERE slug = $1`, candidate).Scan(&x)
		if errors.Is(err, sql.ErrNoRows) {
			return candidate, nil
		}
		if err != nil {
			return "", err
		}
		candidate = fmt.Sprintf("%s-%d", base, n)
	}
}
