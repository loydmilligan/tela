package api

import (
	"context"
	"database/sql"
	"net/http"
	"testing"

	"github.com/zcag/tela/backend/internal/auth"
)

func TestTransferSpace_OwnerToOrgAndBack(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw12", false)
	editor := seedUser(t, d, "editor", "editorpw1", false)
	orgID := seedOrg(t, d, "Acme", "acme")
	seedOrgMember(t, d, orgID, owner, "member")
	spaceID := seedSpace(t, d, "Docs", "docs", owner)
	// editor is an editor member (can edit, not owner) — to test the owner gate.
	if _, err := d.Exec(`INSERT INTO space_members (space_id, user_id, role) VALUES ($1, $2, 'editor')`, spaceID, editor); err != nil {
		t.Fatalf("seed editor: %v", err)
	}
	ctx := context.Background()
	ownerU := &auth.User{ID: owner}
	editorU := &auth.User{ID: editor}

	// An editor (non-owner) can't transfer.
	if _, ae := srv.transferSpaceCore(ctx, editorU, nil, spaceID, &orgID); ae == nil || ae.Status != http.StatusForbidden {
		t.Fatalf("editor transfer should be 403, got %+v", ae)
	}

	// Owner transfers to the org → org_id set, owner resolves to org, grant exists.
	if _, ae := srv.transferSpaceCore(ctx, ownerU, nil, spaceID, &orgID); ae != nil {
		t.Fatalf("transfer to org: %+v", ae)
	}
	var org sql.NullInt64
	if err := d.QueryRow(`SELECT org_id FROM spaces WHERE id=$1`, spaceID).Scan(&org); err != nil {
		t.Fatal(err)
	}
	if !org.Valid || org.Int64 != orgID {
		t.Fatalf("org_id=%v want %d", org, orgID)
	}
	acct, err := spaceOwner(ctx, d, spaceID)
	if err != nil || acct.Kind != accountOrg || acct.ID != orgID {
		t.Fatalf("spaceOwner=%+v err=%v want org %d", acct, err, orgID)
	}
	var grants int
	if err := d.QueryRow(`SELECT count(*) FROM space_grants WHERE space_id=$1 AND principal_kind='org' AND principal_id=$2`, spaceID, orgID).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if grants != 1 {
		t.Fatalf("org grants=%d want 1", grants)
	}

	// Transfer back to personal → org_id NULL.
	if _, ae := srv.transferSpaceCore(ctx, ownerU, nil, spaceID, nil); ae != nil {
		t.Fatalf("transfer back to personal: %+v", ae)
	}
	if err := d.QueryRow(`SELECT org_id FROM spaces WHERE id=$1`, spaceID).Scan(&org); err != nil {
		t.Fatal(err)
	}
	if org.Valid {
		t.Fatalf("org_id should be NULL after transfer to personal, got %v", org)
	}
}

func TestTransferSpace_RequiresOrgMembership(t *testing.T) {
	d := newAPITestDB(t)
	srv := New(d)
	owner := seedUser(t, d, "owner", "ownerpw12", false)
	orgID := seedOrg(t, d, "Acme", "acme") // owner is NOT a member
	spaceID := seedSpace(t, d, "Docs", "docs", owner)

	_, ae := srv.transferSpaceCore(context.Background(), &auth.User{ID: owner}, nil, spaceID, &orgID)
	if ae == nil || ae.Status != http.StatusForbidden {
		t.Fatalf("transfer to non-member org should be 403, got %+v", ae)
	}
}
