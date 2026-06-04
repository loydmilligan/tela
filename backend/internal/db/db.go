package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

// Open opens a connection pool to the PostgreSQL database named by dsn — a libpq
// keyword string or a URL ("postgres://user:pass@host:5432/db?sslmode=disable"),
// both accepted by the pgx stdlib driver.
//
// It pings with a short retry loop so the backend tolerates the gap between
// Postgres accepting connections (what `pg_isready` / compose depends_on waits
// on) and being ready to serve its first query — without it, a cold `make up`
// can crash-loop the backend once before restart recovers it.
func Open(dsn string) (*sql.DB, error) {
	d, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open postgres: %w", err)
	}
	// Modest pool: the backend is I/O-light per request and tests open many
	// short-lived pools against one server, so we stay well under Postgres's
	// default max_connections.
	d.SetMaxOpenConns(10)
	d.SetMaxIdleConns(5)
	d.SetConnMaxIdleTime(5 * time.Minute)

	var pingErr error
	for i := 0; i < 30; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		pingErr = d.PingContext(ctx)
		cancel()
		if pingErr == nil {
			return d, nil
		}
		time.Sleep(time.Second)
	}
	d.Close()
	return nil, fmt.Errorf("ping postgres: %w", pingErr)
}
