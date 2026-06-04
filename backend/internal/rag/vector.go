package rag

import (
	"database/sql/driver"
	"encoding/binary"
	"math"

	sqlite "modernc.org/sqlite"
)

// EncodeVector packs a float32 slice into a little-endian BLOB (4 bytes/elem).
// This is the on-disk format for page_chunks.embedding.
func EncodeVector(v []float32) []byte {
	b := make([]byte, len(v)*4)
	for i, f := range v {
		binary.LittleEndian.PutUint32(b[i*4:], math.Float32bits(f))
	}
	return b
}

// DecodeVector reverses EncodeVector. Returns nil for a malformed length.
func DecodeVector(b []byte) []float32 {
	if len(b)%4 != 0 {
		return nil
	}
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))
	}
	return v
}

// cosineBytes computes cosine similarity between two packed float32 BLOBs.
// Mismatched or malformed inputs yield 0 ("no similarity") rather than an
// error so a single bad row can't fail a whole ranking query.
func cosineBytes(a, b []byte) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) || len(a)%4 != 0 {
		return 0
	}
	var dot, na, nb float64
	for i := 0; i < len(a); i += 4 {
		x := float64(math.Float32frombits(binary.LittleEndian.Uint32(a[i:])))
		y := float64(math.Float32frombits(binary.LittleEndian.Uint32(b[i:])))
		dot += x * y
		na += x * x
		nb += y * y
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

func toBytes(v driver.Value) []byte {
	switch t := v.(type) {
	case []byte:
		return t
	case string:
		return []byte(t)
	default:
		return nil
	}
}

// init registers tela_cosine(a BLOB, b BLOB) -> REAL at the driver level, the
// same mechanism db.tela_strip_excalidraw uses. This lets brute-force kNN run
// inside SQLite (ORDER BY tela_cosine(embedding, :qvec)) as a plain UDF. We go
// this route rather than the sqlite-vec C extension only because the current
// driver (modernc.org/sqlite) is pure Go and can't load C extensions — a fact
// about today's build, not a goal. Registration happens once per process when
// this package is imported; re-registration panics, so it must live in init()
// (not Open()).
func init() {
	sqlite.MustRegisterDeterministicScalarFunction(
		"tela_cosine", 2,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			if len(args) != 2 {
				return float64(0), nil
			}
			return cosineBytes(toBytes(args[0]), toBytes(args[1])), nil
		},
	)
}
