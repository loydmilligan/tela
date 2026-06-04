// Package testdb provisions throwaway PostgreSQL databases for tests.
//
// SQLite's `:memory:` is gone; Postgres has no in-process DB. The simplest fast
// pattern that gives every test full isolation is: connect once to a server,
// CREATE DATABASE a uniquely-named throwaway, migrate it, hand back a pool, and
// DROP it on cleanup. Because every test gets its own real database, the old
// "`:memory:` is per-connection" hazard (which forced on-disk DBs for anything
// touching a second connection / goroutine) simply disappears — a pool against
// one Postgres database is shared across all connections natively.
//
// Configure the server via TELA_TEST_DATABASE_URL — a maintenance DSN pointing
// at an existing database (e.g. `postgres`) on a server where the role may
// CREATE DATABASE. Defaults to the local dev Postgres if unset. Tests Skip with
// a clear message when no server is reachable, so `go test ./...` degrades to a
// skip instead of a failure when nobody booted a Postgres (use `make test`,
// which boots one).
package testdb

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/zcag/tela/backend/internal/db"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// defaultBaseDSN matches the throwaway Postgres `make test` boots locally.
const defaultBaseDSN = "postgres://tela:tela@localhost:55432/postgres?sslmode=disable"

var (
	maintOnce sync.Once
	maintDB   *sql.DB
	maintErr  error
	baseURL   *url.URL
	dbCounter atomic.Int64
)

func baseDSN() string {
	if v := os.Getenv("TELA_TEST_DATABASE_URL"); v != "" {
		return v
	}
	return defaultBaseDSN
}

// maintenance returns a process-shared pool to the maintenance database, used
// only to CREATE/DROP per-test databases.
func maintenance() (*sql.DB, *url.URL, error) {
	maintOnce.Do(func() {
		u, err := url.Parse(baseDSN())
		if err != nil {
			maintErr = fmt.Errorf("parse TELA_TEST_DATABASE_URL: %w", err)
			return
		}
		baseURL = u
		mdb, err := sql.Open("pgx", baseDSN())
		if err != nil {
			maintErr = err
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := mdb.PingContext(ctx); err != nil {
			mdb.Close()
			maintErr = err
			return
		}
		maintDB = mdb
	})
	return maintDB, baseURL, maintErr
}

// New creates a fresh, migrated Postgres database and returns a pool to it. The
// database is dropped (force-terminating any lingering connections) when the
// test finishes. If no Postgres server is reachable, the test is skipped.
func New(t *testing.T) *sql.DB {
	t.Helper()

	mdb, u, err := maintenance()
	if err != nil {
		t.Skipf("testdb: no Postgres reachable at %s (%v) — run `make test` to boot one", baseDSN(), err)
	}

	name := fmt.Sprintf("tela_test_%d_%d_%d", os.Getpid(), dbCounter.Add(1), time.Now().UnixNano())
	if _, err := mdb.Exec(`CREATE DATABASE "` + name + `"`); err != nil {
		t.Fatalf("testdb: create database %s: %v", name, err)
	}

	// Build a DSN for the new database by swapping the path of the maintenance URL.
	child := *u
	child.Path = "/" + name
	d, err := db.Open(child.String())
	if err != nil {
		dropDatabase(mdb, name)
		t.Fatalf("testdb: open %s: %v", name, err)
	}
	if err := db.Migrate(context.Background(), d); err != nil {
		d.Close()
		dropDatabase(mdb, name)
		t.Fatalf("testdb: migrate %s: %v", name, err)
	}

	t.Cleanup(func() {
		d.Close()
		dropDatabase(mdb, name)
	})
	return d
}

func dropDatabase(mdb *sql.DB, name string) {
	// WITH (FORCE) terminates stragglers (PG13+); the pool's conns are already
	// closed by the time cleanup runs, so this is belt-and-suspenders.
	_, _ = mdb.Exec(`DROP DATABASE IF EXISTS "` + name + `" WITH (FORCE)`)
}
