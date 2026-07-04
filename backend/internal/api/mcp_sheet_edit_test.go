package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// applySheetOps is the HTTP bridge to the sidecar /apply endpoint. Its
// three-valued return must cleanly separate: a successful rewrite (newBody), a
// user-fixable rejected op (opErr, from a 422), and a transport/config failure
// (err). These are the contracts edit_sheet's error mapping depends on.
func TestApplySheetOps(t *testing.T) {
	// Fake sidecar: echoes ops it received back into the body on success; 422s a
	// deleteSheet of "Nope" with a message; 200s everything else.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Body string           `json:"body"`
			Ops  []map[string]any `json:"ops"`
		}
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &in)
		if len(in.Ops) == 1 && in.Ops[0]["kind"] == "deleteSheet" {
			w.Header().Set("content-type", "application/json")
			w.WriteHeader(http.StatusUnprocessableEntity)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "unknown sheet: Nope"})
			return
		}
		w.Header().Set("content-type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"body": in.Body + "\n<edited>"})
	}))
	defer srv.Close()

	t.Setenv("TELA_DECK_URL", srv.URL)
	ctx := context.Background()

	// Success: newBody set, both error channels empty.
	body, opErr, err := applySheetOps(ctx, "| A |\n|---|\n| 1 |", []map[string]any{{"kind": "setCells"}})
	if err != nil || opErr != "" {
		t.Fatalf("success path: opErr=%q err=%v", opErr, err)
	}
	if !strings.HasSuffix(body, "<edited>") {
		t.Fatalf("success path: body not rewritten: %q", body)
	}

	// Rejected op: opErr carries the engine message, err nil (not a transport fault).
	_, opErr, err = applySheetOps(ctx, "x", []map[string]any{{"kind": "deleteSheet", "sheet": "Nope"}})
	if err != nil {
		t.Fatalf("rejected op should not be a transport error: %v", err)
	}
	if opErr != "unknown sheet: Nope" {
		t.Fatalf("rejected op message = %q", opErr)
	}

	// Unconfigured sidecar: a transport/config error, not a rejected op.
	t.Setenv("TELA_DECK_URL", "")
	_, opErr, err = applySheetOps(ctx, "x", []map[string]any{{"kind": "setCells"}})
	if err == nil || opErr != "" {
		t.Fatalf("unset TELA_DECK_URL should error: opErr=%q err=%v", opErr, err)
	}
}
