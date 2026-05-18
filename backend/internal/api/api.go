package api

import "database/sql"

// Server bundles dependencies shared across HTTP handlers.
type Server struct {
	DB *sql.DB
}

func New(db *sql.DB) *Server {
	return &Server{DB: db}
}
