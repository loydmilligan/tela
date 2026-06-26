package engine

import "github.com/zcag/tela/backend/internal/atlas/core"

// SupportsType reports whether a source type has a registered connector. The
// server uses it to validate a new source's type at creation, so unsupported
// types are rejected up front rather than failing at acquire. It reads the same
// connectors registry the source-front stages select from.
func SupportsType(t core.SourceType) bool {
	_, ok := connectors[string(t)]
	return ok
}
