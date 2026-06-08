package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
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
		log.Fatalf("usage: tela create-admin <username> <email> <password>")
	}
	username, email, password := args[0], args[1], args[2]
	hash, err := auth.HashPassword(password)
	if err != nil {
		log.Fatalf("create-admin: hash password: %v", err)
	}
	ctx := context.Background()
	var id int64
	err = d.QueryRowContext(ctx, `
		INSERT INTO users (username, email, email_verified_at, password_hash, is_instance_admin, is_active)
		VALUES ($1, $2, tela_now(), $3, 1, 1) RETURNING id`, username, email, hash).Scan(&id)
	if err != nil {
		log.Fatalf("create-admin: %v", err)
	}
	if err := api.EnsurePersonalSpacesForAll(ctx, d); err != nil {
		log.Printf("create-admin: personal space backfill: %v", err)
	}
	log.Printf("create-admin: created instance admin %q (id %d)", username, id)
}

// runSetPlan: `tela set-plan <user|org> <id> <plan_key>` — assign a plan tier.
// Validates the plan's account_kind matches the target kind.
func runSetPlan(d *sql.DB, args []string) {
	if len(args) != 3 {
		log.Fatalf("usage: tela set-plan <user|org> <id> <plan_key>")
	}
	kind, idStr, planKey := args[0], args[1], args[2]
	if kind != "user" && kind != "org" {
		log.Fatalf("set-plan: kind must be 'user' or 'org'")
	}
	ctx := context.Background()
	var planKind string
	if err := d.QueryRowContext(ctx, `SELECT account_kind FROM plans WHERE key = $1`, planKey).Scan(&planKind); err != nil {
		log.Fatalf("set-plan: unknown plan_key %q", planKey)
	}
	if planKind != kind {
		log.Fatalf("set-plan: plan %q is for %q accounts, not %q", planKey, planKind, kind)
	}
	table := "users"
	if kind == "org" {
		table = "orgs"
	}
	res, err := d.ExecContext(ctx,
		`UPDATE `+table+` SET plan_key = $1, updated_at = tela_now() WHERE id = $2`, planKey, idStr)
	if err != nil {
		log.Fatalf("set-plan: %v", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		log.Fatalf("set-plan: no %s with id %s", kind, idStr)
	}
	log.Printf("set-plan: %s %s → %s", kind, idStr, planKey)
}

// runListUsers: `tela list-users` — id, username, email, admin/active flags, plan.
func runListUsers(d *sql.DB) {
	ctx := context.Background()
	rows, err := d.QueryContext(ctx, `
		SELECT id, username, COALESCE(email, ''), is_instance_admin, is_active, plan_key
		FROM users ORDER BY username`)
	if err != nil {
		log.Fatalf("list-users: %v", err)
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
			log.Fatalf("list-users: scan: %v", err)
		}
		fmt.Fprintf(tw, "%d\t%s\t%s\t%t\t%t\t%s\n", id, username, email, admin == 1, active == 1, plan)
	}
	if err := rows.Err(); err != nil {
		log.Fatalf("list-users: %v", err)
	}
	tw.Flush()
}
