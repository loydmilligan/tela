# WebDAV file-sync (rclone)

tela exposes every space you can access as a **WebDAV** tree at `/dav`, so you
can two-way-sync your wiki to local markdown files with stock
[`rclone`](https://rclone.org) (or mount it with Finder / Windows Explorer / any
WebDAV client). This is the dogfood path of the sync feature (spec §9); a
tela-own engine with push + in-app conflict UX comes later.

- **Endpoint:** `https://tela.cagdas.io/dav/`
- **Auth:** a **Personal Access Token (PAT)** as the password (any username).
  Mint one in the app under API keys. Use a **write**-scope key to sync up,
  **read** to sync down only. A **space-pinned** key exposes just that one space.
- **Layout:** the root lists each space as a folder (`<space-slug>/`); inside,
  pages are `<slug>.md`. A page that has children is *also* a `<slug>/` folder
  holding them (the sibling-folder layout — identical to `export.zip`).

```
/dav/
  engineering/
    onboarding.md          ← a page
    onboarding/            ← its child pages
      laptop-setup.md
    roadmap.md
  personal/
    journal.md
```

## How identity works (read this once)

Every file carries an `id:` in its YAML frontmatter — that, **not the path**, is
the page's identity. So you can rename or move a file and tela rebinds the same
page (a retitle / reparent), instead of creating a duplicate. A brand-new file
(no `id:`) creates a page and gets an id assigned on the server; that id rides
back into the file on your next sync-down.

The body is pure markdown; the frontmatter (`id`, `title`, `slug`, `created`,
`updated`, plus any custom keys) is a generated view. Re-uploading a file tela
just gave you is a **no-op** — no churn, no duplicate.

### Conflict handling — server-side 3-way merge

tela merges on the server. When a page changed both in the app and in your local
file since your last sync, **non-overlapping edits combine automatically** (you
edited the intro, a teammate edited the footer → both land). Edits to the **same
lines** are a conflict: your local edit wins the visible copy, the page is
flagged for review, and the overridden server version is kept as a revision
(`source = sync-conflict`) — nothing is ever lost. Merge is line-based, so
edits on adjacent lines may be treated as one conflicting block.

> **First-edit caveat:** the merge needs a *base* (what your client last sent).
> A page created in the app and edited locally **before your client has ever
> uploaded it** has no base yet, so that first write is last-write-wins. After
> the client has uploaded a page once, edits merge.

## ⚠️ Always pass `--ignore-size`

tela **transforms files on write** — it renders the YAML frontmatter (id,
title, …) and may merge your edit with the server's. So the bytes stored differ
from the bytes you uploaded, and rclone's default post-transfer **size check
will call every upload "corrupted" and roll it back** (it can even delete the
page). Pass `--ignore-size` on every command that writes:

```bash
rclone copy   ./engineering tela:engineering --ignore-size
rclone bisync ./engineering tela:engineering --ignore-size ...
```

rclone then uses modtime (not size) to decide what changed — which is why
modtime support (rclone ≥ 1.66) matters here. Pure sync-*down* doesn't need it.

## rclone config

`rclone config` → `n` (new remote) → name it `tela` → `webdav`, then:

```ini
# ~/.config/rclone/rclone.conf
[tela]
type = webdav
url = https://tela.cagdas.io/dav/
vendor = other
user = you@example.com
pass = <obscured PAT>     # set via: rclone obscure 'tela_pat_xxx'
```

Generate the obscured password with `rclone obscure 'tela_pat_xxx'` (rclone
stores passwords obscured, not plain).

### Sync down (one space → a local folder)

```bash
rclone sync tela:engineering ./engineering --create-empty-src-dirs
```

### Two-way sync (bisync)

First run establishes the baseline; subsequent runs reconcile both directions:

```bash
# one-time baseline
rclone bisync tela:engineering ./engineering --resync --ignore-size

# ongoing (cron / systemd timer)
rclone bisync tela:engineering ./engineering \
  --ignore-size --max-delete 25 --check-access \
  --exclude-from ~/.config/rclone/tela-excludes.txt
```

`--max-delete` is a safety rail (refuse a run that would delete an anomalous
fraction of files); `--check-access` aborts if a side looks empty/unmounted.

### Mount (read-friendly)

```bash
rclone mount tela: ~/tela --vfs-cache-mode full --dir-cache-time 10s
```

WebDAV has no change-notification, so reads are polling-bound — lower
`--dir-cache-time` for fresher reads at the cost of more PROPFINDs. Sub-second
end-to-end is the job of the future push-based engine, not rclone.

### Excludes (`tela-excludes.txt`)

Keep OS/editor junk out of your wiki (tela also drops these server-side, but
filtering on the client saves round-trips):

```
.DS_Store
._*
*.swp
*.tmp
~$*
Thumbs.db
.git/**
.obsidian/**
```

## Notes & limits

- **Modtime needs rclone ≥ 1.66** on WebDAV; tela reports `updated_at` as the
  modification time and a strong `ETag` (page id + version) so unchanged files
  are skipped without re-hashing.
- **Renames over `rclone bisync`** show up as delete + re-upload (rclone doesn't
  emit `MOVE` for `vendor=other`); because the re-uploaded file keeps its `id:`,
  tela resurrects/rebinds the same page rather than forking it.
- **Renaming a `.md` file** (e.g. in a mounted client) retitles the page;
  renaming a folder reparents it. Editing only the `slug:` in frontmatter is
  ignored — the slug is always derived from the title.
- **Viewers** (read-only space role) get a read-only tree; `PUT`/`MKCOL`/`DELETE`
  return 403.
- **Disable** the surface entirely with `TELA_WEBDAV_ENABLED=0`.
- **Cloudflare WAF:** the non-standard WebDAV verbs (PROPFIND/MKCOL/MOVE/…) must
  be allowed through to `/dav/*` at the edge; some managed WAF profiles block
  them by default.
