package rag

import (
	"context"
	"strings"
)

// Write-time link suggestions — assisted authoring. Given draft text (a page
// being written, a selection, an agent's draft), return the existing pages it
// should probably link to, by semantic similarity. This refactors authoring from
// "blank page in a silo" into "connected by default": the wiki proposes the
// links so knowledge doesn't fragment into orphans. Needs a live embedder (the
// draft isn't indexed yet, so it's embedded on the fly).

// SuggestLinks returns existing pages semantically related to the draft text,
// access-scoped, for inline link suggestions while authoring. spaceID narrows to
// one space. Embeds the draft as a passage (content↔content similarity) and
// reuses the related-pages ranking core. Empty/blank text yields no suggestions.
func (s *Service) SuggestLinks(ctx context.Context, userID int64, text string, spaceID *int64, limit int) ([]RelatedPage, error) {
	if !s.Enabled() {
		return nil, errEmbedderDisabled
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return []RelatedPage{}, nil
	}
	vec, err := s.emb.Embed(ctx, text)
	if err != nil {
		return nil, err
	}
	return s.nearestPagesByVector(ctx, userID, vecLiteral(vec), nil, spaceID, limit)
}
