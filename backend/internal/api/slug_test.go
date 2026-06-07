package api

import "testing"

// Slug derivation itself is tested in package pagemd; this covers the api-layer
// permalink builder on top of it.
func TestPagePermalinkPath(t *testing.T) {
	if got := pagePermalinkPath(326, "Case 12 — RAN Site Outage RCA: Data Flow"); got != "/p/326/case-12-ran-site-outage-rca-data-flow" {
		t.Errorf("permalink = %q", got)
	}
	if got := pagePermalinkPath(7, "🎉"); got != "/p/7" {
		t.Errorf("emoji-only permalink should be bare id, got %q", got)
	}
}
