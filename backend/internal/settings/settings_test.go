package settings

import (
	"bytes"
	"context"
	"testing"

	"github.com/zcag/tela/backend/internal/testdb"
)

func TestSetGetPersists(t *testing.T) {
	db := testdb.New(t)
	ctx := context.Background()
	st, err := New(ctx, db)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, ok := st.Get("missing"); ok {
		t.Fatal("expected missing key to be absent")
	}
	if err := st.Set(ctx, "registration_open", "false", nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if v, ok := st.Get("registration_open"); !ok || v != "false" {
		t.Fatalf("Get after Set = %q,%v; want false,true", v, ok)
	}
	// A fresh Store (simulating a restart) must see the persisted value.
	st2, err := New(ctx, db)
	if err != nil {
		t.Fatalf("New 2: %v", err)
	}
	if v, ok := st2.Get("registration_open"); !ok || v != "false" {
		t.Fatalf("persisted Get = %q,%v; want false,true", v, ok)
	}
}

func TestGetOrInitSecretStable(t *testing.T) {
	db := testdb.New(t)
	ctx := context.Background()
	st, err := New(ctx, db)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	first, err := st.GetOrInitSecret(ctx, "api_key", 32)
	if err != nil {
		t.Fatalf("GetOrInitSecret: %v", err)
	}
	if len(first) != 32 {
		t.Fatalf("secret len = %d; want 32", len(first))
	}
	// Same call returns the same bytes (idempotent, persisted).
	again, err := st.GetOrInitSecret(ctx, "api_key", 32)
	if err != nil {
		t.Fatalf("GetOrInitSecret 2: %v", err)
	}
	if !bytes.Equal(first, again) {
		t.Fatal("secret changed on second call — should be stable")
	}
	// A fresh Store reads the persisted secret, not a new one (the restart fix).
	st2, err := New(ctx, db)
	if err != nil {
		t.Fatalf("New 2: %v", err)
	}
	persisted, err := st2.GetOrInitSecret(ctx, "api_key", 32)
	if err != nil {
		t.Fatalf("GetOrInitSecret persisted: %v", err)
	}
	if !bytes.Equal(first, persisted) {
		t.Fatal("secret not stable across Store instances — restart would invalidate tokens")
	}
}

func TestAllExcludesSecrets(t *testing.T) {
	db := testdb.New(t)
	ctx := context.Background()
	st, err := New(ctx, db)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := st.GetOrInitSecret(ctx, "share", 32); err != nil {
		t.Fatalf("GetOrInitSecret: %v", err)
	}
	if err := st.Set(ctx, "registration_open", "true", nil); err != nil {
		t.Fatalf("Set: %v", err)
	}
	all := st.All()
	if _, ok := all["registration_open"]; !ok {
		t.Fatal("All() missing non-secret key")
	}
	for k := range all {
		if len(k) >= len(SecretPrefix) && k[:len(SecretPrefix)] == SecretPrefix {
			t.Fatalf("All() exposed a secret key: %q", k)
		}
	}
}
