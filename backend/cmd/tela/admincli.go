package main

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"text/tabwriter"

	"github.com/zcag/tela/backend/internal/api"
	"github.com/zcag/tela/backend/internal/auth"
)

// Operational CLI subcommands, dispatched from main after migrations run. They
// give a self-hoster headless parity for the common admin tasks the operations
// runbook references — without needing the running app or hand-written SQL.
// Each prints usage and exits non-zero on misuse.

// runCreateAdmin: `tela create-admin <username> <email> <password>` — create a
// (pre-verified) instance admin even when the users table is already populated
// (BootstrapAdmin only fires on an empty table). The recovery path when admin
// access is lost.
func runCreateAdmin(d *sql.DB, args []string) {
	if len(args) != 3 {
		fatal("usage: tela create-admin <username> <email> <password>")
	}
	username, email, password := args[0], args[1], args[2]
	hash, err := auth.HashPassword(password)
	if err != nil {
		fatal("create-admin: hash password", "err", err)
	}
	ctx := context.Background()
	var id int64
	err = d.QueryRowContext(ctx, `
		INSERT INTO users (username, email, email_verified_at, password_hash, is_instance_admin, is_active)
		VALUES ($1, $2, tela_now(), $3, 1, 1) RETURNING id`, username, email, hash).Scan(&id)
	if err != nil {
		fatal("create-admin", "err", err)
	}
	if err := api.EnsurePersonalSpacesForAll(ctx, d); err != nil {
		slog.Error("create-admin: personal space backfill", "err", err)
	}
	slog.Info("create-admin: created instance admin", "username", username, "id", id)
}

// runSetPlan: `tela set-plan <user|org> <id> <plan_key>` — assign a plan tier.
// Validates the plan's account_kind matches the target kind.
func runSetPlan(d *sql.DB, args []string) {
	if len(args) != 3 {
		fatal("usage: tela set-plan <user|org> <id> <plan_key>")
	}
	kind, idStr, planKey := args[0], args[1], args[2]
	if kind != "user" && kind != "org" {
		fatal("set-plan: kind must be 'user' or 'org'")
	}
	ctx := context.Background()
	var planKind string
	if err := d.QueryRowContext(ctx, `SELECT account_kind FROM plans WHERE key = $1`, planKey).Scan(&planKind); err != nil {
		fatal("set-plan: unknown plan_key", "plan_key", planKey)
	}
	if planKind != kind {
		fatal("set-plan: plan is for a different account kind", "plan_key", planKey, "plan_kind", planKind, "target_kind", kind)
	}
	table := "users"
	if kind == "org" {
		table = "orgs"
	}
	res, err := d.ExecContext(ctx,
		`UPDATE `+table+` SET plan_key = $1, updated_at = tela_now() WHERE id = $2`, planKey, idStr)
	if err != nil {
		fatal("set-plan", "err", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		fatal("set-plan: no matching row", "kind", kind, "id", idStr)
	}
	slog.Info("set-plan", "kind", kind, "id", idStr, "plan_key", planKey)
}

// runListUsers: `tela list-users` — id, username, email, admin/active flags, plan.
func runListUsers(d *sql.DB) {
	ctx := context.Background()
	rows, err := d.QueryContext(ctx, `
		SELECT id, username, COALESCE(email, ''), is_instance_admin, is_active, plan_key
		FROM users ORDER BY username`)
	if err != nil {
		fatal("list-users", "err", err)
	}
	defer rows.Close()
	tw := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tUSERNAME\tEMAIL\tADMIN\tACTIVE\tPLAN")
	for rows.Next() {
		var (
			id            int64
			username      string
			email         string
			admin, active int
			plan          string
		)
		if err := rows.Scan(&id, &username, &email, &admin, &active, &plan); err != nil {
			fatal("list-users: scan", "err", err)
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%t\t%t\t%s\n", id, username, email, admin == 1, active == 1, plan)
	}
	if err := rows.Err(); err != nil {
		fatal("list-users", "err", err)
	}
	tw.Flush()
}
