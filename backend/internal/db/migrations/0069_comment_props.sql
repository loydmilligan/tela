-- 0069_comment_props.sql — structured, queryable comment metadata.
--
-- Mirrors pages.props (0005): a free-form JSONB bag + a GIN jsonb_path_ops
-- index so `props @> $1::jsonb` containment is indexed. Keeps the lanes
-- distinct: pages.props is a page's OWN data; comments.props is metadata about
-- a timestamped, authored event on that page (a change-comment: summary/type/
-- status/version), so a query can rebuild a changelog from the comment stream.
--
-- jsonb_path_ops (not the default jsonb_ops) for the same reason as 0005: it
-- indexes only containment, which is the single operator the query surface
-- exposes, and is smaller/faster for it.
--
-- Default '{}' so every existing comment has a valid empty bag and containment
-- against it behaves (an empty `where` matches everything, per @> semantics).
ALTER TABLE comments ADD COLUMN props JSONB NOT NULL DEFAULT '{}'::jsonb;

CREATE INDEX idx_comments_props ON comments USING GIN (props jsonb_path_ops);
