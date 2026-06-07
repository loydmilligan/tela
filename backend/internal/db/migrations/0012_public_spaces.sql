-- 0012_public_spaces.sql — space-level public visibility (blog-style spaces).
--
-- A space is either 'private' (resting — readable only by its members, Axis 1
-- of the visibility model) or 'public' (the WHOLE space is readable by anyone,
-- no login, at a clean URL — a blog). This is the space-level companion to
-- per-page share links: a share link is a capability ("anyone with the token"),
-- a public space is an ambient property ("anyone, at the page's own address").
--
-- Read-only OUTBOUND exposure only. Public never grants write: it adds no rows
-- to space_access, and every mutation stays gated on membership/role. Public is
-- whole-space by design — there are no per-page exceptions. See
-- docs/public-spaces.md.

ALTER TABLE spaces
  ADD COLUMN visibility TEXT NOT NULL DEFAULT 'private'
  CHECK (visibility IN ('private', 'public'));
