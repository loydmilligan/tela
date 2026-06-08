// Package settings is the instance-level runtime configuration substrate: a
// cached key/value store backed by the instance_settings table.
//
// It is the single foundation for operator-editable config, persisted secrets,
// per-plan feature flags, and the cloud-connect token. Reads are served from an
// in-memory cache (settings are consulted on hot paths like rate limits and
// TTLs, so a per-request DB hit is unacceptable); writes update the row and the
// cache together.
//
// Precedence is the caller's job, not the store's: a typical resolve is
// `env override → store.Get → code default`. The store only owns the middle
// tier. Env, when set, always wins and is never written back.
//
// Secret values are stored under the `secret/` key prefix and are excluded from
// the admin-facing listing by the API layer — the store itself treats them like
// any other key.
package settings

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"sync"
)

// SecretPrefix marks keys whose values must never be exposed through the admin
// settings API (api-key secret, share secret, cloud token, …).
const SecretPrefix = "secret/"

// Store is a cached view of instance_settings. Safe for concurrent use.
type Store struct {
	db    *sql.DB
	mu    sync.RWMutex
	cache map[string]string
}

// New builds a Store and eagerly loads the table into cache. A load error is
// returned rather than swallowed — at boot the DB is already up (migrations ran
// first), so a failure here is a real problem the caller should surface.
func New(ctx context.Context, db *sql.DB) (*Store, error) {
	s := &Store{db: db, cache: map[string]string{}}
	if err := s.reload(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) reload(ctx context.Context) error {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM instance_settings`)
	if err != nil {
		return err
	}
	defer rows.Close()
	next := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return err
		}
		next[k] = v
	}
	if err := rows.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	s.cache = next
	s.mu.Unlock()
	return nil
}

// Get returns the cached value for key and whether it was present.
func (s *Store) Get(key string) (string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	v, ok := s.cache[key]
	return v, ok
}

// All returns a copy of every key/value, with secret-prefixed keys omitted.
// This is what the admin listing reads — secrets never leave the process.
func (s *Store) All() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]string, len(s.cache))
	for k, v := range s.cache {
		if len(k) >= len(SecretPrefix) && k[:len(SecretPrefix)] == SecretPrefix {
			continue
		}
		out[k] = v
	}
	return out
}

// Set upserts a key. updatedBy is the acting user id (nil for system writes).
func (s *Store) Set(ctx context.Context, key, value string, updatedBy *int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO instance_settings (key, value, updated_by)
		VALUES ($1, $2, $3)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value,
			updated_at = tela_now(), updated_by = EXCLUDED.updated_by`,
		key, value, updatedBy)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.cache[key] = value
	s.mu.Unlock()
	return nil
}

// GetOrInitSecret returns a stable hex-encoded random secret of nbytes for key,
// generating and persisting it on first call. This is the fix for the
// "random per-process secret invalidates every restart" footgun: the value is
// written once and reused for the life of the database. Returns the raw bytes.
//
// Concurrency: a row-level race (two boots inserting at once) resolves via the
// ON CONFLICT upsert path being idempotent-on-read — we re-read after a failed
// insert so all callers converge on the same persisted value.
func (s *Store) GetOrInitSecret(ctx context.Context, key string, nbytes int) ([]byte, error) {
	fullKey := SecretPrefix + key
	if v, ok := s.Get(fullKey); ok {
		return hex.DecodeString(v)
	}
	buf := make([]byte, nbytes)
	if _, err := rand.Read(buf); err != nil {
		return nil, err
	}
	enc := hex.EncodeToString(buf)
	// INSERT ... DO NOTHING so a concurrent boot doesn't clobber; then read back
	// the winning value (ours or the racer's) so everyone agrees.
	if _, err := s.db.ExecContext(ctx, `
		INSERT INTO instance_settings (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO NOTHING`, fullKey, enc); err != nil {
		return nil, err
	}
	var stored string
	if err := s.db.QueryRowContext(ctx,
		`SELECT value FROM instance_settings WHERE key = $1`, fullKey).Scan(&stored); err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.cache[fullKey] = stored
	s.mu.Unlock()
	return hex.DecodeString(stored)
}
