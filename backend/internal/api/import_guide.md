# Importing an existing folder of files into tela

You have a folder of existing documents — Word docs, spreadsheets, PDFs, notes — and want them in tela as a browsable **page tree**. tela's importer is **markdown-native**: it turns a tree of markdown files into a tree of pages (folders become index pages) in one transactional call, with a dry-run preview.

tela has **no server-side Office conversion or OCR**. You convert non-markdown files **locally first** (you have the files and a shell). The payoff: prose docs become pages, spreadsheets become **live tela sheets** (formulas recompute), and PDFs/images ride along as attachments.

## The mapping — what each file becomes

| Source | Convert locally with | Becomes |
|---|---|---|
| `.md`, `.markdown` | — (import as-is) | page |
| `.docx` | `pandoc f.docx -t gfm --wrap=none --extract-media=media -o f.md` | page (extracted images → attach after) |
| `.doc` (legacy) | `soffice --headless --convert-to docx f.doc`, then pandoc | page |
| `.xlsx`, `.xlsm` | script → one GFM table per worksheet + `sheet: true` frontmatter | **live sheet** |
| `.xls` (legacy) | `soffice --headless --convert-to xlsx f.xls`, then as `.xlsx` | live sheet |
| `.csv`, `.tsv` | → GFM table (+ `sheet: true` for a computing grid) | sheet / page |
| `.pdf` with a text layer | — (attach to a page) | attachment (searchable — RAG indexes it) |
| scanned `.pdf`, `.jpg`, `.png`, other binaries | — (attach) | attachment only (no OCR) |

Anything you can't convert still belongs in tela as an **attachment** on a page, so the archive stays complete and browsable.

## Converting a spreadsheet to a live sheet

A tela **sheet** is just a page with `sheet: true` in frontmatter and a body of compact GFM tables (Defter format) — one table per worksheet. **Preserve formulas**: write `=SUM(D2:D9)`, never the baked number. Read the workbook with a formula-preserving reader (Python `openpyxl.load_workbook(path)` keeps formula strings by default; `data_only=True` would drop them). Example `mizan.md`:

```markdown
---
sheet: true
---
| Hesap | Borç | Alacak |
|---|---|---|
| Kasa | 1000 | 0 |
| Banka | 5000 | 0 |
| **Toplam** | =SUM(B2:B3) | =SUM(C2:C3) |
```

Row 1 is the header; the first data row is row 2. For the full format (multiple sheets, number formats, styling, charts) call the `sheet_authoring_guide` tool.

## The import call

Upload the converted markdown tree to the import endpoint. Bytes go over REST (not through MCP), so run this from your shell with a **write-scoped PAT** (tela → Settings → API keys):

```bash
curl -sS -X POST "$TELA_BASE/api/spaces/$SPACE_ID/import" \
  -H "Authorization: Bearer $TELA_PAT" \
  -F "dry_run=true" \
  -F "parent_id=$PARENT_ID" \
  -F "files=@out/Fins/mizan.md;filename=Fins/mizan.md" \
  -F "files=@out/Fins/notes.md;filename=Fins/notes.md"
```

- **Each `files` part's `filename` is the file's RELATIVE PATH, with folders.** That path builds the tree — `Fins/mizan.md` nests `mizan` under a `Fins` index page.
- `parent_id` is optional — omit to import at the space root, or set it to nest the whole batch under an existing page.

## Enumerate → preview → confirm → import

1. Walk the folder; classify each file by the mapping table.
2. **Tell the user the plan** in plain language — how many become sheets, pages, attachments, and what can't be text-imported (scanned PDFs, images).
3. Run the import with `dry_run=true`. The response previews the exact result:

   ```json
   { "summary": {"created": 42, "skipped": 3, "conflicts_renamed": 1},
     "pages":   [{"title": "mizan", "path": "Fins/mizan.md", "id": -1}],
     "skipped": [{"path": "logo.png", "reason": "not_markdown"}] }
   ```

   (`not_markdown` just means "convert or attach it" — expected for anything you didn't turn into `.md`.)
4. Show the preview, **get the user's confirmation**, then re-run with `dry_run=false` to commit.
5. For each PDF/image: once its page exists, attach the original with `upload_attachment` (≤ 5 MB) or `request_attachment_upload` (larger, bytes off-context).

## Practical notes

- **Batch per top-level folder** rather than one giant POST — smaller requests, clearer tree. Nest a batch with `parent_id`.
- A `README.md` inside a folder becomes that folder's index-page body; otherwise the folder gets an empty index page titled by its name.
- Same-title siblings auto-rename `(2)`, `(3)`, …
- Frontmatter props are kept (`status`, `tags`, `sheet`, …); reserved keys (`id`, `title`, `slug`) are ignored. Title precedence: frontmatter `title:` → first `# H1` → filename.
- Wikilinks `[[X]]` and relative `[..](./y.md)` links import as-is (not rewritten).
- A text-layer PDF becomes searchable once attached (RAG indexes its text); a scanned PDF or image is a viewable attachment only — tela does no OCR.
