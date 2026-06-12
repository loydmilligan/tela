# Attachments

Files attached to a page — uploads from the editor **and** files
[rclone-synced](webdav-sync.md) into the page's folder — backed by the unified
`space_files` blob store (migration `0015`). The same store backs editor image
uploads.

## Model

A page's attachments are the `space_files` whose `parent_page_id` is that page.
A file dropped into `pageX/` over WebDAV is already parented to `pageX`, so
synced files become attachments with **no body edit** and no sync-layer rewrite
of the markdown.

- **Inline** placement lives in the body (markdown): images as `![](url)`,
  other files as a `:::file{name="…" size="…"}\n<url>\n:::` card
  (`milkdown-file.ts`, block id `file`). Authoritative position, syncs as text.
- **The Attachments strip** (below the title, reader + editor) is a sidecar
  rendered from SQL — it lists *all* files on the page. A chip carries an
  "embedded" marker when the body already references that file's hash (computed
  by scanning the body, stateless). In the editor a chip can be deleted.

## API

- `GET  /api/pages/{id}/attachments` — session-authed; lists the page's files
  with `{id, name, mime, byte_size, hash, url, embedded}`.
- `POST /api/pages/{id}/attachments` — editor+; multipart `file`. Dedupes
  identical bytes; disambiguates a name collision with a `-<hash8>` suffix so a
  distinct upload never clobbers an existing one (e.g. two pasted `image.png`).
- `DELETE /api/pages/{id}/attachments/{file_id}` — editor+; soft-delete.
- `GET /api/files/{space_id}/{hash}.{ext}` — **public**, content-addressed,
  immutable cache. Keyed by hash (not path) so a body embed survives a sync
  rename. Raster images (png/jpeg/gif/webp) serve **inline**; everything else is
  forced to download (`Content-Disposition: attachment` + `nosniff`) so embedded
  HTML/SVG can't execute as stored-XSS from our origin.

The list/upload/delete handlers are thin wrappers over `listPageAttachmentsCore`
/ `uploadPageAttachmentCore` / `deletePageAttachmentCore` — the same cores the
MCP tools call, so REST and agents share one code path.

## MCP tools

Agents get the same surface (put an image/PDF on a page, or read what's
attached):

- `list_attachments(page_id)` — read; each file plus an absolute `download_url`
  (fetchable over HTTP) and a ready-to-paste `markdown` embed snippet.
- `upload_attachment(page_id, name, data_base64)` — editor+; stores the bytes
  (inline base64, so reasonably-sized files) and returns the attachment +
  `markdown`. The agent then `update_page`/`patch_page`s the snippet into the
  body — `![](…)` for images, a `:::file` card otherwise.
- `delete_attachment(page_id, id)` — editor+; soft-delete. Does **not** edit the
  body, so an inline embed must be removed separately.

Binary over MCP: download is just the public `download_url` (the host fetches it
directly, per the MCP guidance for web-fetchable resources); upload is inline
base64 in the tool argument. A signed upload-URL handshake is the future path for
large files. PDF attachments render inline in the reader via a client-side
viewer (`ui/pdf-viewer.tsx`).

## Notes

- **Serve identity is a capability hash.** Like `/api/images/`, anyone with the
  URL can fetch the bytes regardless of space privacy. Fine for most wikis; if
  strict private-space enforcement is needed, gate the serve route on visibility
  (costs the immutable-cache win).
- **Storage** is Postgres `bytea`, capped per file by `TELA_WEBDAV_FILE_MAX_BYTES`
  (default 50 MiB). It bloats DB/backups; the serve-route abstraction lets blobs
  move to filesystem/S3 later without changing URLs.
- **Legacy image serve** (`/api/images/`) is now **serve-only** — the old
  `POST /api/pages/{id}/images` upload route + `UploadPageImage` were retired
  (the editor uploads everything through `/api/pages/{id}/attachments`). The GET
  route + `page_images` table stay so images already embedded in historical page
  bodies keep resolving; dropping the table would need a body-URL rewrite + data
  migration first. `/api/diagrams/` is unchanged.
- **Conflicts** on a file are last-write-wins (see [webdav-sync.md](webdav-sync.md));
  no per-file history.
