package models

import "database/sql"

type Space struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

type Page struct {
	ID        int64         `json:"id"`
	SpaceID   int64         `json:"space_id"`
	ParentID  sql.NullInt64 `json:"parent_id"`
	Title     string        `json:"title"`
	Body      string        `json:"body"`
	Position  int64         `json:"position"`
	CreatedAt string        `json:"created_at"`
	UpdatedAt string        `json:"updated_at"`
}
